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

// +kubebuilder:validation:Enum=grpc;http
type TraceProtocol string

const (
	TraceProtocolGRPC TraceProtocol = "grpc"
	TraceProtocolHTTP TraceProtocol = "http"
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

	// Trace specifies optional per-workload observability overrides
	// +optional
	Trace *TraceSpec `json:"trace,omitempty"`

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
}

// IdentitySpec configures workload identity for an AgentRuntime.
type IdentitySpec struct {
	// SPIFFE specifies SPIFFE identity configuration overrides
	// +optional
	SPIFFE *SPIFFEIdentity `json:"spiffe,omitempty"`
}

// SPIFFEIdentity configures SPIFFE workload identity for an AgentRuntime.
type SPIFFEIdentity struct {
	// TrustDomain overrides the operator-level --spire-trust-domain for this workload.
	// If empty, the operator flag value is used.
	// +optional
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9]([a-zA-Z0-9\-\.]*[a-zA-Z0-9])?$`
	TrustDomain string `json:"trustDomain,omitempty"`
}

// TraceSpec configures observability for an AgentRuntime.
type TraceSpec struct {
	// Endpoint is the OTEL collector endpoint override
	// +optional
	Endpoint string `json:"endpoint,omitempty"`

	// Protocol is the OTEL export protocol (grpc or http)
	// +optional
	Protocol TraceProtocol `json:"protocol,omitempty"`

	// Sampling specifies trace sampling configuration
	// +optional
	Sampling *SamplingSpec `json:"sampling,omitempty"`
}

// SamplingSpec configures trace sampling for an AgentRuntime.
type SamplingSpec struct {
	// Rate is the sampling rate (0.0-1.0)
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1
	Rate float64 `json:"rate"`
}

// AgentRuntimeStatus defines the observed state of AgentRuntime.
type AgentRuntimeStatus struct {
	// Phase is the high-level state of the AgentRuntime
	// +optional
	Phase RuntimePhase `json:"phase,omitempty"`

	// ConfiguredPods is the count of pods with expected labels/config
	// +optional
	ConfiguredPods int32 `json:"configuredPods,omitempty"`

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
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// AgentRuntime attaches runtime configuration to a backing workload classified as an
// agent or tool, providing per-workload overrides for SPIFFE identity and OpenTelemetry
// tracing. The controller reports pod configuration coverage and phase in status.
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
