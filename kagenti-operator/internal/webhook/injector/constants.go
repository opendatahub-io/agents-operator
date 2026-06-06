package injector

// Label constants used by the precedence evaluator.
const (
	// Per-sidecar workload labels — set value to "false" to disable injection
	LabelEnvoyProxyInject   = "kagenti.io/envoy-proxy-inject"
	LabelSpiffeHelperInject = "kagenti.io/spiffe-helper-inject"

	// LabelClientRegistrationInject — legacy sidecar opt-in: set to "true" to inject;
	// default is operator-managed Keycloak credentials (no sidecar).
	LabelClientRegistrationInject = "kagenti.io/client-registration-inject"
)

// AuthBridge deployment modes. Selected per workload via AgentRuntime
// CR `Spec.AuthBridgeMode`, falling back to the namespace
// `authbridge-runtime-config` ConfigMap's `mode` field, the deprecated
// per-pod annotation, then ModeProxySidecar as the cluster-wide default.
const (
	ModeEnvoySidecar = "envoy-sidecar" // iptables + Envoy + ext_proc
	ModeProxySidecar = "proxy-sidecar" // default: HTTP_PROXY env + authbridge proxy (full plugins)
	ModeLite         = "lite"          // same shape as proxy-sidecar; uses authbridge-lite image (auth-only)
	ModeWaypoint     = "waypoint"      // standalone deployment (not injected)

	// AnnotationAuthBridgeMode is the legacy per-pod mode selector. The
	// canonical surface is now AgentRuntime.Spec.AuthBridgeMode and the
	// namespace authbridge-runtime-config ConfigMap; this annotation is
	// only honored as a deprecated fallback so existing deployments do
	// not silently shape-shift to a different mode on first redeploy.
	//
	// Deprecated: set Spec.AuthBridgeMode on the AgentRuntime CR.
	AnnotationAuthBridgeMode = "kagenti.io/authbridge-mode"

	// Container name for proxy-sidecar mode
	AuthBridgeProxyContainerName = "authbridge-proxy"

	// Identity type constants
	IdentityTypeSpiffe         = "spiffe"
	ClientAuthTypeFederatedJWT = "federated-jwt"
)

// ProxyInitMode is the iptables strategy BuildProxyInitContainer passes to
// init-iptables.sh. It is a named type so callsites read intent-fully and the
// compiler rejects passing an unrelated typed value; BuildProxyInitContainer
// additionally fails closed on any unknown value (see its default branch),
// since an untyped string literal can still convert to this type at a callsite.
type ProxyInitMode string

// proxy-init MODE values.
const (
	// ProxyInitModeRedirect transparently REDIRECTs pod traffic to the Envoy
	// listeners (envoy-sidecar mode).
	ProxyInitModeRedirect ProxyInitMode = "redirect"
	// ProxyInitModeEnforceDrop installs the fail-closed egress guard that DROPs
	// any egress bypassing the forward proxy (proxy-sidecar egress enforcement).
	ProxyInitModeEnforceDrop ProxyInitMode = "enforce-drop"
)

// mTLS modes for the proxy-sidecar / lite paths. Selected per workload
// via AgentRuntime CR `Spec.MTLSMode`, falling back to the namespace
// `authbridge-runtime-config` ConfigMap's `mtls.mode` field, then
// MTLSModeDisabled. envoy-sidecar mode is incompatible with mTLS today
// (Envoy SDS not configured by the kagenti envoy-config) — admission
// rejects mtlsMode != disabled in that combination.
const (
	MTLSModeDisabled   = "disabled"
	MTLSModePermissive = "permissive"
	MTLSModeStrict     = "strict"
)
