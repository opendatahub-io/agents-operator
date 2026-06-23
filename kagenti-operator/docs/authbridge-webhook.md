# AuthBridge Webhook Design

The AuthBridge webhook is a Kubernetes mutating admission webhook that injects sidecar containers into agent and tool Pods. It runs as part of the kagenti-operator binary and intercepts Pod CREATE requests to add networking, identity, and registration sidecars.

## Deployment Modes

The webhook supports multiple deployment modes controlled by the `kagenti.io/authbridge-mode` annotation on the workload's pod template. If not set, the default is `envoy-sidecar`.

| Mode | Annotation value | Image | How it works |
|------|-----------------|-------|-------------|
| **envoy-sidecar** (default) | `envoy-sidecar` or absent | `authbridge-envoy` (140 MB) | iptables intercepts all traffic → Envoy → ext_proc for auth |
| **proxy-sidecar** | `proxy-sidecar` | `authbridge-light` (29 MB) | `HTTP_PROXY` env vars route outbound traffic through authbridge |
| **waypoint** | `waypoint` | N/A | Skips injection (standalone deployment, not a sidecar) |

### envoy-sidecar (default)

Transparent interception via iptables. All inbound/outbound traffic is redirected through Envoy, which delegates auth decisions to the authbridge binary via ext_proc gRPC. The agent is unaware of the proxy.

Containers injected: `proxy-init` (init), `envoy-proxy`, `spiffe-helper`, `kagenti-client-registration`

### proxy-sidecar

Lightweight mode without Envoy or iptables. The webhook:

1. **Steals the agent's port** — the reverse proxy takes over the agent's original port (e.g., `:8000`) for inbound JWT validation
2. **Moves the agent** — finds a free port and patches the agent's `PORT` env var (e.g., `:8001`)
3. **Injects `HTTP_PROXY`** — all app containers get `HTTP_PROXY`/`HTTPS_PROXY` env vars pointing to the forward proxy for outbound traffic
4. Port assignment uses collision detection — scans all container ports to avoid conflicts

Containers injected: `authbridge-proxy`, `spiffe-helper`, `kagenti-client-registration`
No init containers (no iptables).

The `authbridge-proxy` container receives `REVERSE_PROXY_ADDR`, `REVERSE_PROXY_BACKEND`, and `FORWARD_PROXY_ADDR` env vars with the dynamically assigned ports. These are expanded via `${...}` placeholders in the authbridge config YAML.

### waypoint

Not a sidecar — the waypoint proxy runs as a standalone Deployment. The webhook logs a message and skips injection. Waypoint deployment is managed separately.

## Sidecar Containers

The webhook can inject up to four containers depending on mode:

| Container | Type | Modes | Purpose |
|-----------|------|-------|---------|
| `envoy-proxy` | sidecar | envoy-sidecar | Transparent proxy with ext-proc filter for token exchange (uses `authbridge-envoy` image) |
| `authbridge-proxy` | sidecar | proxy-sidecar | Reverse + forward proxy for JWT validation and token exchange (uses `authbridge-light` image) |
| `proxy-init` | init | envoy-sidecar | iptables rules to redirect traffic through envoy-proxy |
| `spiffe-helper` | sidecar | envoy-sidecar, proxy-sidecar | Obtains and rotates SPIFFE SVIDs via the SPIRE workload API |
| `kagenti-client-registration` | sidecar | envoy-sidecar, proxy-sidecar | Registers the workload as an OAuth2 client in Keycloak |

In envoy-sidecar mode, `proxy-init` always follows `envoy-proxy` — if envoy is skipped, proxy-init is also skipped.

## Injection Trigger

Injection requires **all** of the following:

1. The Pod has label `kagenti.io/type=agent` (or `tool` with the `injectTools` gate enabled)
2. A matching **AgentRuntime CR** exists in the same namespace with `spec.targetRef.name` matching the workload name
3. The global feature gate is enabled
4. The Pod has not opted out via `kagenti.io/inject=disabled`
5. At least one sidecar passes the per-sidecar precedence chain

The AgentRuntime CR requirement means Pods deployed before the CR is created will **not** receive sidecars. The AgentRuntime CR acts as the explicit trigger for injection.

**Note:** The `client-registration` sidecar uses **opt-in** semantics (see [Per-Sidecar Precedence Evaluation](#phase-2-per-sidecar-precedence-evaluation)). It is only injected when `kagenti.io/client-registration-inject=true` is set; the default path is operator-managed Keycloak registration. See [Operator-Managed Client Registration](operator-managed-client-registration.md).

## Injection Precedence Chain

The webhook evaluates injection in two phases: **workload-level pre-filtering** and **per-sidecar precedence evaluation**.

### Phase 1: Workload-Level Pre-Filtering

These checks run first in `PodMutator.InjectAuthBridge()` (`internal/webhook/injector/pod_mutator.go`). Any "no" short-circuits the entire injection — no sidecars are added.

| Order | Check | Source | Skip condition |
|-------|-------|--------|----------------|
| 1 | Workload type | Pod label `kagenti.io/type` | Not `agent` or `tool` |
| 2 | Global kill switch | `featureGates.globalEnabled` | `false` |
| 3 | Tool gate | `featureGates.injectTools` | `kagenti.io/type=tool` and gate is `false` |
| 4 | Workload opt-out | Pod label `kagenti.io/inject` | Value is `disabled` |
| 5 | Per-sidecar precedence | See Phase 2 | All sidecars evaluate to skip |
| 6 | AgentRuntime CR | Matches by `spec.targetRef.name` | No matching CR found in namespace |

### Phase 2: Per-Sidecar Precedence Evaluation

After pre-filtering passes, `PrecedenceEvaluator.Evaluate()` (`internal/webhook/injector/precedence.go`) runs a two-layer chain for each sidecar independently:

| Layer | Source | Effect |
|-------|--------|--------|
| 1 (highest) | Feature gate (`featureGates.<sidecar>`) | `false` disables the sidecar cluster-wide |
| 2 | Workload label (`kagenti.io/<sidecar>-inject`) | `false` disables the sidecar for this workload |

The per-sidecar labels are:

| Label | Controls |
|-------|----------|
| `kagenti.io/envoy-proxy-inject` | envoy-proxy + proxy-init |
| `kagenti.io/spiffe-helper-inject` | spiffe-helper |
| `kagenti.io/client-registration-inject` | client-registration (**opt-in**: must be `"true"` to inject) |

**Client-registration uses opt-in semantics**: unlike envoy-proxy and spiffe-helper (which inject by default and can be opted out), the client-registration sidecar only injects when `kagenti.io/client-registration-inject=true` is explicitly set. The default path is operator-managed Keycloak registration, which creates a Secret and mounts it via pod annotation.

If all sidecars evaluate to "skip", no mutation occurs (equivalent to a pre-filter rejection).

### Precedence Flow Diagram

```
Pod CREATE request
  │
  ├─ kagenti.io/type not agent|tool? ──→ ALLOW (no mutation)
  ├─ globalEnabled=false? ─────────────→ ALLOW (no mutation)
  ├─ type=tool and injectTools=false? ─→ ALLOW (no mutation)
  ├─ kagenti.io/inject=disabled? ──────→ ALLOW (no mutation)
  │
  ├─ Per-sidecar precedence evaluation
  │   ├─ envoy-proxy:  gate → label → inject?
  │   ├─ proxy-init:   follows envoy-proxy
  │   ├─ spiffe-helper: gate → label → inject?
  │   └─ client-registration: gate → label="true"? → inject (opt-in)
  │
  ├─ No sidecars to inject? ───────────→ ALLOW (no mutation)
  ├─ No matching AgentRuntime CR? ─────→ ALLOW (no mutation)
  │
  ├─ Build containers + volumes → PATCH
  └─ Mount operator Keycloak Secret (if annotation present) → PATCH
```

## Configuration Merge (Webhook Config Resolution)

When the `perWorkloadConfigResolution` feature gate is enabled, the webhook resolves configuration values at admission time instead of deferring to kubelet's ConfigMapKeyRef/SecretKeyRef resolution. This merge happens in `ResolveConfig()` (`internal/webhook/injector/resolved_config.go`).

### Merge Layers

```
┌──────────────────────────────────────┐
│ Layer 2: Namespace ConfigMaps        │  ← higher precedence
│   (authbridge-config,                │
│    authbridge-runtime-config,        │
│    spiffe-helper-config, etc.)       │
├──────────────────────────────────────┤
│ Layer 1: PlatformConfig              │  ← lowest precedence
│   (compiled defaults + config.yaml)  │
└──────────────────────────────────────┘
```

> **Note:** The webhook does **not** read AgentRuntime CR spec fields
> (`authBridgeMode`, `mtlsMode`, `tlsBridgeMode`, `egressEnforcement`)
> at admission time. Mode resolution uses the namespace
> `authbridge-runtime-config` ConfigMap exclusively. CR spec fields are
> consumed by the **controller** (for annotations and status conditions)
> and the **validating webhook** (for compatibility checks), not by the
> mutating webhook's injection logic.

### Layer 1: PlatformConfig (compiled defaults + config file)

**Source**: `internal/webhook/config/defaults.go` (compiled defaults) merged with `/etc/kagenti/config.yaml` (file overrides).

**Loaded by**: `ConfigLoader` (`internal/webhook/config/loader.go`) with fsnotify hot-reload.

**Contains**: Container images, proxy ports/UID, resource requests/limits, token exchange defaults, SPIFFE trust domain/socket path, observability settings, per-sidecar enable/disable defaults.

**Merge behavior**: YAML file fields overlay onto compiled defaults. Missing fields retain compiled default values. The merged result is validated by `PlatformConfig.Validate()`.

### Layer 2: Namespace ConfigMaps

**Source**: Well-known ConfigMaps in the workload's namespace, read at admission time by `ReadNamespaceConfig()` (`internal/webhook/injector/namespace_config.go`).

| ConfigMap | Keys | Purpose |
|-----------|------|---------|
| `authbridge-config` | `KEYCLOAK_URL`, `KEYCLOAK_REALM`, `PLATFORM_CLIENT_IDS`, `TOKEN_URL`, `ISSUER`, `EXPECTED_AUDIENCE`, `TARGET_AUDIENCE`, `TARGET_SCOPES`, `DEFAULT_OUTBOUND_POLICY` | Identity and token exchange settings |
| `spiffe-helper-config` | `helper.conf` | SPIFFE helper configuration |
| `authproxy-routes` | `routes.yaml` | Auth proxy route definitions |

**Merge behavior**: Each ConfigMap is read independently. Missing ConfigMaps result in empty strings for those fields. Non-empty namespace values override PlatformConfig defaults.

### Mode Resolution (authBridgeMode, mtlsMode, tlsBridgeMode, egressEnforcement)

The webhook resolves operational modes from the namespace `authbridge-runtime-config` ConfigMap using `Extract*()` helpers (`internal/webhook/injector/namespace_config.go`). Each mode falls back to a hardcoded default if the ConfigMap key is absent:

| Mode | ConfigMap key | Extractor | Default |
|------|--------------|-----------|---------|
| `authBridgeMode` | `mode:` | `ExtractMode()` | `proxy-sidecar` |
| `mtlsMode` | `mtls.mode:` | `ExtractMTLSMode()` | `permissive` |
| `tlsBridgeMode` | `tls_bridge.mode:` | `ExtractTLSBridgeMode()` | `disabled` |
| `egressEnforcement` | `egressEnforcement:` | `ExtractEgressEnforcement()` | `enforce-redirect` |

### Merge Code Path

```
PodMutator.InjectAuthBridge()                       ← pod_mutator.go
  │
  ├─ ReadNamespaceConfig(ctx, client, ns)            ← namespace_config.go
  │     Reads well-known ConfigMaps from namespace
  │     Returns *NamespaceConfig
  │
  ├─ Extract*(nsConfig.AuthBridgeRuntimeYAML)        ← namespace_config.go
  │     Resolves authBridgeMode, mtlsMode,
  │     tlsBridgeMode, egressEnforcement
  │
  ├─ [if perWorkloadConfigResolution=true]
  │   ├─ ResolveConfig(platform, nsConfig)           ← resolved_config.go
  │   │     Merges namespace CM values with PlatformConfig
  │   │     Returns *ResolvedConfig
  │   │
  │   └─ NewResolvedContainerBuilder(resolved)       ← container_builder.go
  │         Builds containers with literal env var values
  │
  └─ [if perWorkloadConfigResolution=false (default)]
      └─ NewContainerBuilder(platformConfig)         ← container_builder.go
            Builds containers with ValueFrom ConfigMapKeyRef/SecretKeyRef
            Kubelet resolves values at container start time
```

## Feature Gates

Feature gates are loaded from `/etc/kagenti/feature-gates/feature-gates.yaml` with fsnotify hot-reload. Defined in `internal/webhook/config/feature_gates.go`.

| Gate | Default | Effect |
|------|---------|--------|
| `globalEnabled` | `true` | Master kill switch — `false` disables all injection cluster-wide |
| `envoyProxy` | `true` | Enable/disable envoy-proxy + proxy-init injection |
| `spiffeHelper` | `true` | Enable/disable spiffe-helper injection |
| `clientRegistration` | `true` | Enable/disable client-registration injection |
| `injectTools` | `false` | Allow injection for `kagenti.io/type=tool` workloads |
| `perWorkloadConfigResolution` | `false` | Switch from ValueFrom refs to literal env var injection |

## Workload Name Derivation

At Pod CREATE time, the Pod name is often empty (generated by the API server). The webhook derives the **Deployment or StatefulSet name** from the Pod metadata:

```
Deployment "myapp" → ReplicaSet "myapp-7d4f8b9c5" → Pod GenerateName="myapp-7d4f8b9c5-"
  pod-template-hash="7d4f8b9c5" → strip "-7d4f8b9c5" suffix → "myapp"

StatefulSet "myapp" → Pod GenerateName="myapp-"
  No pod-template-hash → strip trailing "-" → "myapp"

Bare Pod Name="my-bare-pod" → "my-bare-pod"
```

For Deployment-managed Pods, the `pod-template-hash` label (set by the ReplicaSet controller) is used to strip the ReplicaSet hash suffix and recover the Deployment name. This ensures the derived name matches `AgentRuntime.spec.targetRef.name`.

This name is used for:
- AgentRuntime CR `spec.targetRef.name` matching
- ServiceAccount creation (SPIFFE identity)
- Client registration naming

Implementation: `deriveWorkloadName()` in `internal/webhook/v1alpha1/authbridge_webhook.go`.

## Idempotency and Reinvocation

The webhook is idempotent. If any injected container (`envoy-proxy`, `spiffe-helper`, `kagenti-client-registration`, `authbridge`) or init container (`proxy-init`) is already present in the Pod spec, the webhook skips full mutation. This is checked by `isAlreadyInjected()` in `authbridge_webhook.go` before `InjectAuthBridge()` is called.

Additionally, each container and volume append in `InjectAuthBridge()` is guarded by `containerExists()`/`volumeExists()` checks.

The MutatingWebhookConfiguration uses `reinvocationPolicy: IfNeeded` so the webhook is re-invoked if another mutating webhook modifies the Pod after the initial mutation.

### Operator Secret Mount Reinvocation

When sidecars are already injected but operator-managed Keycloak client credentials are not yet mounted, the webhook applies **only** the Secret volume mounts on reinvocation:

1. `NeedsKeycloakClientCredentialsVolumePatch()` checks if the Pod annotation `kagenti.io/keycloak-client-credentials-secret-name` is set but the corresponding Secret volume is missing.
2. If so, `ApplyKeycloakClientCredentialsSecretVolumes()` adds the Secret volume and subPath mounts (`client-id.txt`, `client-secret.txt`) into containers that have `shared-data` volume mounts.

This handles the case where the operator annotates the pod template **after** the first webhook pass (e.g., the operator creates the Keycloak Secret and patches the annotation between the initial injection and a rolling restart).

## Port Exclusion Annotations

Per-workload iptables overrides for proxy-init:

| Annotation | Effect |
|------------|--------|
| `kagenti.io/outbound-ports-exclude` | Comma-separated ports appended to the mandatory 8080 exclusion |
| `kagenti.io/inbound-ports-exclude` | Comma-separated ports excluded from inbound interception |

## Key Source Files

| File | Purpose |
|------|---------|
| `internal/webhook/v1alpha1/authbridge_webhook.go` | Admission handler, Pod decoding, workload name derivation, idempotency check |
| `internal/webhook/injector/pod_mutator.go` | Central orchestrator — pre-filtering, precedence evaluation, AgentRuntime gate, container/volume injection |
| `internal/webhook/injector/precedence.go` | Per-sidecar 2-layer precedence chain (feature gate > workload label); opt-in semantics for client-registration |
| `internal/webhook/injector/keycloak_client_credentials.go` | Operator-managed Keycloak Secret volume mounts and reinvocation patch logic |
| `internal/webhook/injector/resolved_config.go` | Webhook config merge: PlatformConfig < namespace CMs < AgentRuntime CR overrides (at admission time) |
| `internal/webhook/injector/agentruntime_config.go` | Typed AgentRuntime CR lookup and override extraction |
| `internal/webhook/injector/namespace_config.go` | Reads well-known ConfigMaps from workload namespace |
| `internal/webhook/injector/container_builder.go` | Dual-mode container construction (ValueFrom vs literal env vars) |
| `internal/webhook/injector/volume_builder.go` | Volume definitions for both config resolution modes |
| `internal/webhook/config/types.go` | PlatformConfig struct definitions |
| `internal/webhook/config/defaults.go` | Compiled default values |
| `internal/webhook/config/feature_gates.go` | FeatureGates struct and defaults |
| `internal/webhook/config/loader.go` | ConfigLoader with fsnotify hot-reload |
| `internal/webhook/config/feature_gate_loader.go` | FeatureGateLoader with fsnotify hot-reload |
