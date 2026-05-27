# API Reference

This document provides a comprehensive reference for the Kagenti Operator Custom Resource Definitions (CRDs).

## Custom Resources

- [AgentCard](#agentcard) — Fetches and stores agent metadata for dynamic discovery
- [AgentRuntime](#agentruntime) — Configures identity and observability for agent/tool workloads

---

## AgentCard

The `AgentCard` Custom Resource stores agent metadata for dynamic discovery and introspection. It synchronizes agent card data from deployed agents that implement supported protocols (currently A2A).

### API Group and Version

- **API Group:** `agent.kagenti.dev`
- **API Version:** `v1alpha1`
- **Kind:** `AgentCard`
- **Short Names:** `agentcards`, `cards`

### Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `syncPeriod` | string | No | How often to re-fetch the agent card (default: "30s", format: "30s", "5m", etc.) |
| `targetRef` | [TargetRef](#targetref) | Yes | Identifies the workload backing this agent |
| `identityBinding` | [IdentityBinding](#identitybinding) | No | SPIFFE identity binding configuration |

#### TargetRef

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `apiVersion` | string | Yes | API version of the target resource (e.g., "apps/v1") |
| `kind` | string | Yes | Kind of the target resource (e.g., "Deployment", "StatefulSet") |
| `name` | string | Yes | Name of the target resource |

#### IdentityBinding

Configures workload identity binding for an AgentCard. The SPIFFE ID is extracted from the leaf certificate's SAN URI in the `x5c` chain during signature verification.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `trustDomain` | string | No | Overrides the operator-level `--spire-trust-domain` for this AgentCard. If empty, the operator flag value is used. |
| `strict` | boolean | No | Enables enforcement mode: binding failures trigger network isolation. When false (default), results are recorded in status only (audit mode). |

### Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `card` | [AgentCardData](#agentcarddata) | Cached agent card data from the agent |
| `conditions` | [][Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#condition-v1-meta) | Current state of indexing process |
| `lastSyncTime` | timestamp | When the agent card was last successfully fetched |
| `protocol` | string | Detected agent protocol (e.g., "a2a") |
| `targetRef` | [TargetRef](#targetref) | Resolved reference to the backing workload |
| `validSignature` | boolean | Whether the agent card JWS signature is valid |
| `signatureVerificationDetails` | string | Human-readable details about the last signature verification |
| `signatureKeyId` | string | Key ID (`kid`) from the JWS protected header |
| `signatureSpiffeId` | string | SPIFFE ID from the JWS protected header (set only when signature is valid) |
| `signatureIdentityMatch` | boolean | `true` when both signature verification AND identity binding pass |
| `cardId` | string | SHA256 hash of card content for drift detection |
| `expectedSpiffeID` | string | SPIFFE ID used for binding evaluation |
| `bindingStatus` | [BindingStatus](#bindingstatus) | Result of identity binding evaluation |

#### BindingStatus

| Field | Type | Description |
|-------|------|-------------|
| `bound` | boolean | Whether the verified SPIFFE ID is in the allowlist |
| `reason` | string | Machine-readable reason (`Bound`, `NotBound`, `AgentNotFound`) |
| `message` | string | Human-readable description |
| `lastEvaluationTime` | timestamp | When the binding was last evaluated |

#### AgentCardData

Represents the A2A agent card structure based on the [A2A specification](https://a2a-protocol.org/).

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Human-readable name of the agent |
| `description` | string | What the agent does |
| `version` | string | Agent version |
| `url` | string | Endpoint where the agent can be reached |
| `documentationUrl` | string | Link to the agent's documentation |
| `iconUrl` | string | Link to the agent's icon image |
| `provider` | [AgentProvider](#agentprovider) | Organization providing the agent |
| `capabilities` | [AgentCapabilities](#agentcapabilities) | Supported A2A features |
| `defaultInputModes` | []string | Default media types the agent accepts |
| `defaultOutputModes` | []string | Default media types the agent produces |
| `skills` | [][AgentSkill](#agentskill) | Skills/capabilities offered by the agent |
| `supportsAuthenticatedExtendedCard` | boolean | Whether agent has an extended card |
| `signatures` | [][AgentCardSignature](#agentcardsignature) | JWS signatures per A2A spec section 8.4.2 |

#### AgentProvider

| Field | Type | Description |
|-------|------|-------------|
| `organization` | string | Name of the provider organization |
| `url` | string | Provider's website |

#### AgentCapabilities

| Field | Type | Description |
|-------|------|-------------|
| `streaming` | boolean | Whether the agent supports streaming responses |
| `pushNotifications` | boolean | Whether the agent supports push notifications |
| `extensions` | [][AgentExtension](#agentextension) | A2A protocol extensions supported by the agent |

#### AgentExtension

| Field | Type | Description |
|-------|------|-------------|
| `uri` | string | Unique identifier for this extension |
| `description` | string | What this extension does |
| `required` | boolean | Whether this extension must be supported by the client |
| `params` | map[string]JSON | Extension-specific configuration parameters |

#### AgentSkill

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Unique identifier for this skill |
| `name` | string | Skill name |
| `description` | string | What this skill does |
| `tags` | []string | Keywords describing classes of capabilities for this skill |
| `examples` | []string | Sample scenarios demonstrating how the skill can be used |
| `inputModes` | []string | Media types this skill accepts |
| `outputModes` | []string | Media types this skill produces |
| `parameters` | [][SkillParameter](#skillparameter) | Parameters this skill accepts |

#### SkillParameter

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Parameter name |
| `type` | string | Parameter type (e.g., "string", "number", "boolean") |
| `description` | string | What this parameter is for |
| `required` | boolean | Whether this parameter must be provided |
| `default` | string | Default value for this parameter |

#### AgentCardSignature

| Field | Type | Description |
|-------|------|-------------|
| `protected` | string | Base64url-encoded JWS protected header (contains `alg`, `kid`, `typ`, `x5c`) |
| `signature` | string | Base64url-encoded JWS signature value |
| `header` | object | Optional unprotected JWS header parameters (e.g., `timestamp`) |

### Examples

#### Deploy Agent as a Standard Deployment (Recommended)

Create a standard Deployment with agent labels, and an AgentCard with `targetRef`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: weather-agent
  namespace: default
  labels:
    app.kubernetes.io/name: weather-agent
    kagenti.io/type: agent
    protocol.kagenti.io/a2a: ""
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: weather-agent
  template:
    metadata:
      labels:
        app.kubernetes.io/name: weather-agent
    spec:
      containers:
      - name: agent
        image: "ghcr.io/kagenti/agent-examples/weather_service:v0.0.1-alpha.3"
        ports:
        - containerPort: 8000
        env:
        - name: PORT
          value: "8000"
---
apiVersion: agent.kagenti.dev/v1alpha1
kind: AgentCard
metadata:
  name: weather-agent-card
  namespace: default
spec:
  syncPeriod: 30s
  targetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: weather-agent
```

#### AgentCard with Identity Binding

```yaml
apiVersion: agent.kagenti.dev/v1alpha1
kind: AgentCard
metadata:
  name: weather-agent-card
  namespace: default
spec:
  syncPeriod: "30s"
  targetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: weather-agent
  identityBinding:
    strict: true
```

The AgentCard can also be automatically created by the operator when agent labels are present on the Deployment.

#### View Discovered Agents

```bash
# List all agent cards
kubectl get agentcards

# Example output:
# NAME                        PROTOCOL   KIND         TARGET          AGENT             SYNCED   LASTSYNC   AGE
# weather-agent-deployment-card   a2a    Deployment   weather-agent   Weather Assistant  True     5m         10m

# Get detailed information
kubectl describe agentcard weather-agent-deployment-card
```

#### AgentCard Status Example

```yaml
apiVersion: agent.kagenti.dev/v1alpha1
kind: AgentCard
metadata:
  name: weather-agent-deployment-card
  namespace: default
  ownerReferences:
    - apiVersion: apps/v1
      kind: Deployment
      name: weather-agent
      controller: true
spec:
  syncPeriod: 30s
  targetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: weather-agent
status:
  protocol: a2a
  lastSyncTime: "2025-12-19T10:30:00Z"
  targetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: weather-agent

  card:
    name: "Weather Assistant"
    description: "Provides weather information using MCP tools"
    version: "1.0.0"
    url: "http://weather-agent.default.svc.cluster.local:8000"

    capabilities:
      streaming: true
      pushNotifications: false

    defaultInputModes:
      - text
    defaultOutputModes:
      - text

    skills:
      - id: "get-weather-001"
        name: "get-weather"
        description: "Get current weather for a city"
        tags:
          - weather
          - forecast
        examples:
          - "What is the weather in New York?"
          - "Get the forecast for London"
        inputModes:
          - text
        outputModes:
          - text
        parameters:
          - name: "city"
            type: "string"
            description: "City name to get weather for"
            required: true

  conditions:
    - type: Synced
      status: "True"
      reason: SyncSucceeded
      message: "Successfully fetched agent card for Weather Assistant"
      lastTransitionTime: "2025-12-19T10:30:00Z"
    - type: Ready
      status: "True"
      reason: ReadyToServe
      message: "Agent index is ready for queries"
      lastTransitionTime: "2025-12-19T10:30:00Z"
```

#### Query Agent Metadata

```bash
# Get agent name from card
kubectl get agentcard weather-agent-card \
  -o jsonpath='{.status.card.name}'

# List all skills
kubectl get agentcard weather-agent-card \
  -o jsonpath='{.status.card.skills[*].name}'

# Get agent endpoint
kubectl get agentcard weather-agent-card \
  -o jsonpath='{.status.card.url}'

# Check signature verification
kubectl get agentcard weather-agent-card \
  -o jsonpath='{.status.validSignature}'

# Check identity binding
kubectl get agentcard weather-agent-card \
  -o jsonpath='{.status.bindingStatus.bound}'
```

---

```yaml
apiVersion: agent.kagenti.dev/v1alpha1
kind: AgentCard
metadata:
  name: custom-agent-card
  namespace: default
spec:
  syncPeriod: "5m"  # Sync every 5 minutes instead of default 30s
  targetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: custom-agent
```

### Common Status Conditions

#### AgentCard Conditions

| Type | Status | Reason | Description |
|------|--------|--------|-------------|
| `Synced` | `True` | `SyncSucceeded` | Agent card fetched successfully |
| `Synced` | `False` | `WorkloadNotFound` | Referenced workload does not exist |
| `Synced` | `False` | `WorkloadNotReady` | Workload is not ready to serve |
| `Synced` | `False` | `NoProtocol` | Workload missing `protocol.kagenti.io/<name>` label |
| `Synced` | `False` | `FetchFailed` | Failed to fetch agent card from endpoint |
| `Synced` | `False` | `SignatureInvalid` | Signature verification failed (enforce mode) |
| `Ready` | `True` | `ReadyToServe` | Agent index ready for queries |
| `SignatureVerified` | `True` | `SignatureValid` | JWS signature verified successfully |
| `SignatureVerified` | `False` | `SignatureInvalid` | JWS signature verification failed |
| `Bound` | `True` | `Bound` | SPIFFE ID is in the allowlist |
| `Bound` | `False` | `NotBound` | SPIFFE ID is not in the allowlist |

---

## Required Labels for Workload-Based Agents

For Deployments and StatefulSets to be automatically discovered by the operator, the following labels are required:

| Label | Value | Required | Description |
|-------|-------|----------|-------------|
| `kagenti.io/type` | `agent` | Yes | Identifies the workload as an agent |
| `protocol.kagenti.io/<name>` | `""` (existence implies support) | Yes (at least one) | Protocol(s) the agent speaks (e.g., `protocol.kagenti.io/a2a`, `protocol.kagenti.io/mcp`) |
| `app.kubernetes.io/name` | `<agent-name>` | Recommended | Standard Kubernetes app name label |

---

## AgentRuntime

The `AgentRuntime` Custom Resource configures identity (SPIFFE) for agent and tool workloads. Unlike AgentCard, which handles discovery and metadata fetching, AgentRuntime provides runtime configuration for workload identity.

### API Group and Version

- **API Group:** `agent.kagenti.dev`
- **API Version:** `v1alpha1`
- **Kind:** `AgentRuntime`
- **Short Names:** `art`, `agentrt`

### Relationship to AgentCard

AgentRuntime and AgentCard serve complementary purposes:

- **AgentCard**: Fetches and stores agent metadata (capabilities, skills, endpoints) for dynamic discovery. Handles signature verification and identity binding validation.
- **AgentRuntime**: Configures identity (SPIFFE trust domain) for running workloads.

Both resources use the shared `TargetRef` type to reference the backing workload (Deployment, StatefulSet, etc.).

### Configuration Precedence

The controller merges configuration from three layers (highest priority wins):

1. **AgentRuntime CR spec** — per-workload overrides (trust domain, etc.)
2. **Namespace defaults** — ConfigMap with `kagenti.io/defaults=true` label in the workload's namespace
3. **Cluster defaults** — `kagenti-platform-config` ConfigMap in `kagenti-system`

> **Note:** Feature gates (`kagenti-feature-gates`) are platform-wide policy and are **not** overrideable by namespace defaults or AgentRuntime CRs. They control which AuthBridge components (envoy proxy, SPIFFE helper, client registration) are enabled globally, and whether OCI skill image mounting (`skillImageVolumes`) is active.

### Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | Yes | Classifies the workload as `agent` or `tool` |
| `targetRef` | [TargetRef](#targetref) | Yes | Identifies the workload backing this runtime (uses the same TargetRef type as AgentCard) |
| `identity` | [IdentitySpec](#identityspec) | No | Optional per-workload identity overrides |
| `skills` | [][SkillImageRef](#skillimageref) | No | OCI skill images to mount into the agent pod as Kubernetes ImageVolumes. Requires the `skillImageVolumes` feature gate and Kubernetes 1.31+. Max 20 items. |

#### IdentitySpec

Configures workload identity for an AgentRuntime.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spiffe` | [SPIFFEIdentity](#spiffeidentity) | No | SPIFFE identity configuration overrides |

#### SPIFFEIdentity

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `trustDomain` | string | No | Overrides the operator-level `--spire-trust-domain` for this workload. If empty, the operator flag value is used. Must match pattern: `^[a-zA-Z0-9]([a-zA-Z0-9\-\.]*[a-zA-Z0-9])?$` |

#### SkillImageRef

Identifies an OCI skill image to mount into the agent pod as a Kubernetes [ImageVolume](https://kubernetes.io/docs/tasks/configure-pod-container/image-volumes/). Skills are packaged as OCI images following the [skillimage](https://github.com/redhat-et/skillimage) convention (`FROM scratch` with `skill.yaml` + `SKILL.md`).

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Unique identifier for this skill mount. Used as the volume name suffix (`skill-<name>`). Must be a valid DNS label (lowercase alphanumeric or hyphens, max 58 characters). |
| `image` | string | Yes | OCI image reference for the skill (e.g., `ghcr.io/redhat-et/skillimage/resume-reviewer:v1.0.0`) |
| `mountPath` | string | Yes | Absolute path where the skill image is mounted in the container. Different agent frameworks expect skills in different locations (e.g., `/agent/skills/my-skill`, `/app/.claude/skills/my-skill`). |
| `pullPolicy` | string | No | Image pull policy: `Always`, `Never`, or `IfNotPresent`. Defaults to `Always` for `:latest` tags, `IfNotPresent` otherwise (standard Kubernetes behavior). |

**Prerequisites:**
- The `skillImageVolumes` feature gate must be enabled (defaults to `false`)
- Kubernetes 1.31+ with the `ImageVolume` feature gate enabled on the kubelet
- OpenShift 4.18+ (for OpenShift deployments)

**Behavior:**
- Each skill is mounted as a read-only ImageVolume at the specified `mountPath`
- Skill changes (add, remove, image update, mount path change) trigger rolling updates via config-hash
- Skill volumes use the `skill-` prefix and do not interfere with existing ConfigMap, Secret, or CSI volumes
- The operator sets a `kagenti.io/skills` annotation on the target workload's metadata containing a JSON array of skill names (e.g., `["weather-forecast","resume-reviewer"]`). Downstream systems such as agent card controllers or the Kagenti UI can read this annotation to discover which skills are mounted without inspecting the pod spec. The annotation is removed when skills are cleared or the AgentRuntime is deleted.
- On AgentRuntime deletion, all skill volumes and the `kagenti.io/skills` annotation are removed from the target workload
- If the feature gate is disabled but skills are defined, a `SkillsMounted=False` condition is set with reason `FeatureGateDisabled`

### Labels and Annotations Applied to Target Workloads

The AgentRuntime controller applies the following labels and annotations to the target workload:

**Workload metadata labels:**

| Label | Value | Description |
|-------|-------|-------------|
| `kagenti.io/type` | `agent` or `tool` | Classifies the workload (from `spec.type`) |
| `app.kubernetes.io/managed-by` | `kagenti-operator` | Indicates the workload is managed by the operator. Removed on AgentRuntime deletion. |

**Workload metadata annotations:**

| Annotation | Value | Description |
|------------|-------|-------------|
| `kagenti.io/skills` | JSON array of skill names | Lists mounted skill names (e.g., `["weather-forecast","resume-reviewer"]`). Only set when the `skillImageVolumes` feature gate is enabled and skills are defined. Removed when skills are cleared or on AgentRuntime deletion. |

**PodTemplateSpec labels:**

| Label | Value | Description |
|-------|-------|-------------|
| `kagenti.io/type` | `agent` or `tool` | Classifies pods spawned by this workload |

**PodTemplateSpec annotations:**

| Annotation | Value | Description |
|------------|-------|-------------|
| `kagenti.io/config-hash` | SHA-256 hex string | Deterministic hash of the resolved configuration. Changes trigger rolling updates. |

### Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `phase` | string | High-level state of the AgentRuntime (`Pending`, `Active`, or `Error`) |
| `configuredPods` | int32 | Count of pods with expected labels/configuration |
| `conditions` | [][Condition](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#condition-v1-meta) | Current state of the AgentRuntime |

#### Conditions

| Condition | Status | Reason | Description |
|-----------|--------|--------|-------------|
| `TargetResolved` | True | `TargetFound` | Target Deployment/StatefulSet exists |
| `TargetResolved` | False | `TargetNotFound` | Target workload not found; controller requeues after 30s |
| `ConfigResolved` | True | `ConfigResolved` | Configuration merged successfully from all layers |
| `ConfigResolved` | True | `ConfigWarning` | Configuration merged but ambiguity detected (e.g., multiple namespace defaults ConfigMaps with `kagenti.io/defaults=true`). The warning is surfaced in the condition message and as a Kubernetes event. |
| `Ready` | True | `Configured` | Labels and config-hash applied to the target workload |
| `Ready` | False | `ConfigHashError` | Failed to compute the config hash |
| `Ready` | False | `ConfigApplyError` | Failed to apply labels/annotations to the workload |
| `SkillsMounted` | True | `SkillsApplied` | OCI skill ImageVolumes applied to the target workload |
| `SkillsMounted` | False | `FeatureGateDisabled` | Skills defined but `skillImageVolumes` feature gate is disabled |
| `SkillsMounted` | False | `UnsupportedWorkloadKind` | Skills defined but the target workload kind (e.g., Sandbox) does not support skill ImageVolumes |

### Admission Validation

A validating webhook prevents ownership conflicts:

- **Duplicate targetRef rejection**: If an AgentRuntime CR already targets a given workload (same `apiVersion` + `kind` + `name`) in the same namespace, creating or updating another AgentRuntime to target the same workload is rejected at admission time.
- **Fail-open on API errors**: If the webhook's internal list call fails (e.g., transient API server error), the request is allowed through to avoid blocking deployments. Note: the Kubernetes-level `failurePolicy` is set to `Fail`, so if the webhook pod itself is unreachable, the API server will reject AgentRuntime creates/updates. This is consistent with the AgentCard webhook.

This prevents conflicting label updates where two AgentRuntime CRs with different `type` values (e.g., `agent` vs `tool`) would fight over the same workload's `kagenti.io/type` label.

- **Skill validation**: Duplicate skill names and duplicate mount paths within the same AgentRuntime are rejected.

### Examples

#### Basic Agent Runtime

```yaml
apiVersion: agent.kagenti.dev/v1alpha1
kind: AgentRuntime
metadata:
  name: weather-agent-runtime
  namespace: default
spec:
  type: agent
  targetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: weather-agent
```

#### Agent Runtime with Identity Overrides

```yaml
apiVersion: agent.kagenti.dev/v1alpha1
kind: AgentRuntime
metadata:
  name: weather-agent-runtime
  namespace: default
spec:
  type: agent
  targetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: weather-agent
  identity:
    spiffe:
      trustDomain: custom.example.com
```

#### Tool Runtime

```yaml
apiVersion: agent.kagenti.dev/v1alpha1
kind: AgentRuntime
metadata:
  name: calculator-tool-runtime
  namespace: default
spec:
  type: tool
  targetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: calculator-tool
```

#### Agent Runtime with OCI Skill Images

Mount OCI-packaged skills into the agent pod. Requires the `skillImageVolumes` feature gate enabled in the `kagenti-feature-gates` ConfigMap and Kubernetes 1.31+.

```yaml
apiVersion: agent.kagenti.dev/v1alpha1
kind: AgentRuntime
metadata:
  name: resume-agent-runtime
  namespace: default
spec:
  type: agent
  targetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: resume-agent
  skills:
    - name: resume-reviewer
      image: ghcr.io/redhat-et/skillimage/resume-reviewer:v1.0.0
      mountPath: /agent/skills/resume-reviewer
    - name: blog-writer
      image: ghcr.io/redhat-et/skillimage/blog-writer:latest
      mountPath: /agent/skills/blog-writer
      pullPolicy: Always
```

To enable the feature gate:

```yaml
# In the kagenti-feature-gates ConfigMap (kagenti-system namespace)
# or via Helm values:
featureGates:
  skillImageVolumes: true
```

### kubectl Usage Examples

```bash
# List all agent runtimes (using short name)
kubectl get art

# List agent runtimes with full name
kubectl get agentruntimes

# Example output:
# NAME                      TYPE    TARGET          PHASE    AGE
# weather-agent-runtime     agent   weather-agent   Active   5m
# calculator-tool-runtime   tool    calculator-tool Active   3m

# Get detailed information
kubectl describe agentruntime weather-agent-runtime

# View runtime phase
kubectl get art weather-agent-runtime -o jsonpath='{.status.phase}'

# View configured pods count
kubectl get art weather-agent-runtime -o jsonpath='{.status.configuredPods}'
```

---

## Additional Resources

- [Dynamic Agent Discovery](./dynamic-agent-discovery.md) — How AgentCard enables agent discovery
- [Signature Verification](./agentcard-signature-verification.md) — JWS signature verification setup
- [Identity Binding](./agentcard-identity-binding.md) — SPIFFE identity binding guide
- [Architecture Documentation](./architecture.md) — Operator design and components
- [Developer Guide](./dev.md) — Contributing and development
- [Getting Started Tutorial](../GETTING_STARTED.md) — Detailed tutorials and examples
