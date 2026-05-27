/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:validation:Enum=agent;tool
type RuntimeType string

const (
	RuntimeTypeAgent RuntimeType = "agent"
	RuntimeTypeTool  RuntimeType = "tool"
)

// +kubebuilder:validation:Enum=Pending;Active;Error
type RuntimePhase string

const (
	RuntimePhasePending RuntimePhase = "Pending"
	RuntimePhaseActive  RuntimePhase = "Active"
	RuntimePhaseError   RuntimePhase = "Error"
)

// AgentRuntimeSpec defines the desired state of AgentRuntime.
type AgentRuntimeSpec struct {
	// Type classifies the workload as an agent or tool
	Type RuntimeType `json:"type"`

	// TargetRef identifies the workload backing this agent runtime (duck typing).
	TargetRef TargetRef `json:"targetRef"`

	// Identity specifies optional per-workload identity overrides
	// +optional
	Identity *IdentitySpec `json:"identity,omitempty"`

	// AuthBridgeMode selects the deployment shape for this workload's
	// authbridge sidecar. When unset, the namespace-level
	// authbridge-runtime-config ConfigMap's mode is used; if that is
	// also unset, the operator falls back to "proxy-sidecar".
	//
	// Four valid values:
	//
	//   proxy-sidecar  HTTP_PROXY env + authbridge-proxy (full plugin
	//                  set, including a2a/mcp/inference parsers) +
	//                  spiffe-helper bundled. No Envoy, no iptables.
	//                  Default mode.
	//   envoy-sidecar  Envoy + ext_proc authbridge + spiffe-helper
	//                  bundled. Requires the proxy-init iptables
	//                  container.
	//   lite           Same listener layout as proxy-sidecar but uses
	//                  the authbridge-lite image (jwt-validation +
	//                  token-exchange only, parsers dropped to shrink
	//                  the binary). For size-constrained deployments
	//                  that don't need protocol-aware abctl events.
	//   waypoint       Standalone deployment, not injected as a
	//                  sidecar. Used by Istio ambient mesh.
	//
	// Set this when a single workload needs a different shape than the
	// namespace default. Most deployments leave it unset and let the
	// namespace ConfigMap drive the choice.
	//
	// +optional
	// +kubebuilder:validation:Enum=proxy-sidecar;envoy-sidecar;lite;waypoint
	AuthBridgeMode string `json:"authBridgeMode,omitempty"`

	// MTLSMode selects the mTLS posture between authbridge sidecars on
	// the proxy-sidecar / lite paths. envoy-sidecar handles transport
	// security through Envoy SDS, which is currently not configured by
	// the kagenti envoy-config — admission rejects mtlsMode != disabled
	// when authBridgeMode is envoy-sidecar (tracked as a follow-up).
	//
	// Three valid values:
	//
	//   disabled    Plaintext between sidecars (default).
	//   permissive  Inbound: byte-peek listener accepts both TLS and
	//               plaintext on the same port. Outbound: tries TLS,
	//               falls back to plaintext on handshake failure (one-line
	//               WARN log per fallback). Use during rollout.
	//   strict      Inbound: TLS-only, plaintext callers closed at
	//               accept. Outbound: TLS-or-fail. Use after rollout
	//               completes.
	//
	// Resolution: AgentRuntime CR > namespace authbridge-runtime-config
	// mtls.mode > "disabled". Setting mtlsMode != disabled implicitly
	// requires SPIRE — the operator auto-enables spire for the workload.
	//
	// CR-empty vs CR="disabled" are observably different in
	// `kubectl get agentruntime -o yaml` (the former omits the field,
	// the latter shows mtlsMode: disabled) but produce the same
	// effective mode: empty falls through to the namespace ConfigMap,
	// "disabled" is an explicit override that pins mode off even when
	// the namespace default is non-disabled.
	//
	// Note: changing mtlsMode triggers a pod rollout because authbridge
	// cannot hot-reload mTLS config (the byte-peek listener is wired at
	// process start).
	//
	// +optional
	// +kubebuilder:validation:Enum=disabled;permissive;strict
	MTLSMode string `json:"mtlsMode,omitempty"`

	// Skills declares OCI skill images to mount into the agent pod as
	// Kubernetes ImageVolumes. Each skill is mounted read-only at
	// /agent/skills/<name>/. Requires the skillImageVolumes feature gate
	// and Kubernetes 1.31+ with the ImageVolume feature gate enabled.
	// +optional
	// +kubebuilder:validation:MaxItems=20
	Skills []SkillImageRef `json:"skills,omitempty"`
}

// IdentitySpec configures workload identity for an AgentRuntime.
type IdentitySpec struct {
	// SPIFFE specifies SPIFFE identity configuration overrides
	// +optional
	SPIFFE *SPIFFEIdentity `json:"spiffe,omitempty"`

	// AllowedAudiences specifies additional JWT audiences that the AuthProxy
	// sidecar should accept for inbound requests. This is a transitional
	// mechanism to support application-to-agent flows until the auth model
	// is finalized. See https://github.com/kagenti/kagenti-operator/issues/368
	// +optional
	AllowedAudiences []string `json:"allowedAudiences,omitempty"`
}

// SPIFFEIdentity configures SPIFFE workload identity for an AgentRuntime.
type SPIFFEIdentity struct {
	// TrustDomain overrides the operator-level --spire-trust-domain for this workload.
	// If empty, the operator flag value is used.
	// +optional
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9]([a-zA-Z0-9\-\.]*[a-zA-Z0-9])?$`
	TrustDomain string `json:"trustDomain,omitempty"`
}

// CardStatus holds the fetched A2A agent card data along with fetch metadata
// and optional verification results. Populated by the card discovery phase when
// --enable-card-discovery is set.
type CardStatus struct {
	AgentCardData `json:",inline"`

	// FetchedAt is the timestamp of the last successful card fetch.
	// +optional
	FetchedAt *metav1.Time `json:"fetchedAt,omitempty"`

	// CardID is a SHA-256 content hash of the fetched card data.
	// +optional
	CardID string `json:"cardId,omitempty"`

	// Protocol is the detected agent protocol (e.g., "a2a").
	// +optional
	Protocol string `json:"protocol,omitempty"`

	// ValidSignature is the result of JWS signature verification.
	// +optional
	ValidSignature *bool `json:"validSignature,omitempty"`

	// SignatureKeyID is the key ID from the verified JWS header.
	// +optional
	SignatureKeyID string `json:"signatureKeyID,omitempty"`

	// SignatureVerificationDetails contains details or errors from signature verification.
	// +optional
	SignatureVerificationDetails string `json:"signatureVerificationDetails,omitempty"`

	// AttestedAgentSpiffeID is the SPIFFE ID extracted from the mTLS peer certificate.
	// +optional
	AttestedAgentSpiffeID string `json:"attestedAgentSpiffeID,omitempty"`
}

// +kubebuilder:validation:Enum=Always;Never;IfNotPresent
type SkillPullPolicy string

const (
	SkillPullAlways       SkillPullPolicy = "Always"
	SkillPullNever        SkillPullPolicy = "Never"
	SkillPullIfNotPresent SkillPullPolicy = "IfNotPresent"
)

// SkillImageRef identifies an OCI skill image to mount into the agent pod.
type SkillImageRef struct {
	// Name is a unique identifier for this skill mount, used as the volume
	// name suffix (skill-<name>).
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=58
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([a-z0-9\-]*[a-z0-9])?$`
	Name string `json:"name"`

	// Image is the OCI image reference for the skill.
	// +kubebuilder:validation:MinLength=1
	Image string `json:"image"`

	// MountPath is the absolute path where the skill image is mounted in
	// the container. Different agent frameworks expect skills in different
	// locations (e.g. /agent/skills/my-skill, /app/.claude/skills/my-skill).
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^/.*`
	MountPath string `json:"mountPath"`

	// PullPolicy for pulling the OCI skill image. Defaults to Always for
	// :latest tags and IfNotPresent otherwise (standard Kubernetes behavior).
	// +optional
	PullPolicy SkillPullPolicy `json:"pullPolicy,omitempty"`
}

// AgentRuntimeStatus defines the observed state of AgentRuntime.
type AgentRuntimeStatus struct {
	// Phase is the high-level state of the AgentRuntime
	// +optional
	Phase RuntimePhase `json:"phase,omitempty"`

	// ConfiguredPods is the count of pods with expected labels/config
	// +optional
	ConfiguredPods int32 `json:"configuredPods,omitempty"`

	// Card holds A2A agent card data discovered from the workload's Service endpoint.
	// +optional
	Card *CardStatus `json:"card,omitempty"`

	// Conditions represent the current state of the AgentRuntime
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=art;agentrt
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.type",description="Workload Type"
// +kubebuilder:printcolumn:name="Target",type="string",JSONPath=".spec.targetRef.name",description="Target Workload"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Runtime Phase"
// +kubebuilder:printcolumn:name="CardSynced",type="string",JSONPath=".status.conditions[?(@.type=='CardSynced')].status",description="Card Sync Status",priority=1
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// AgentRuntime attaches runtime configuration to a backing workload classified as an
// agent or tool, providing per-workload overrides for SPIFFE identity.
// The controller reports pod configuration coverage and phase in status.
type AgentRuntime struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentRuntimeSpec   `json:"spec"`
	Status AgentRuntimeStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AgentRuntimeList contains a list of AgentRuntime.
type AgentRuntimeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AgentRuntime `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AgentRuntime{}, &AgentRuntimeList{})
}
