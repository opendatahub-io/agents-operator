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

const (
	AgentsOperatorKind         = "AgentsOperator"
	AgentsOperatorInstanceName = "default-agentsoperator"
)

// ManagementState mirrors the OpenShift operator ManagementState values.
// +kubebuilder:validation:Enum=Managed;Removed;Force;Unmanaged
type ManagementState string

const (
	ManagementStateManaged   ManagementState = "Managed"
	ManagementStateRemoved   ManagementState = "Removed"
	ManagementStateForce     ManagementState = "Force"
	ManagementStateUnmanaged ManagementState = "Unmanaged"
)

// Phase represents the top-level lifecycle phase of the module.
// +kubebuilder:validation:Enum=Ready;"Not Ready"
type Phase string

const (
	PhaseReady    Phase = "Ready"
	PhaseNotReady Phase = "Not Ready"
)

// AgentsOperatorAuth holds platform-projected authentication settings.
type AgentsOperatorAuth struct {
	// Enabled indicates whether authentication integration is active.
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Audiences lists accepted token audiences.
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=256
	// +optional
	Audiences []string `json:"audiences,omitempty"`
}

// ComponentRelease describes an installed component version.
type ComponentRelease struct {
	// Name is the component name.
	Name string `json:"name"`

	// RepoURL is the source repository URL.
	RepoURL string `json:"repoUrl"`

	// Version is the installed version.
	Version string `json:"version"`
}

// AgentsOperatorSpec defines the desired state of AgentsOperator.
type AgentsOperatorSpec struct {
	// ManagementState controls whether the module is active.
	// +kubebuilder:validation:Enum=Managed;Removed;Force;Unmanaged
	// +kubebuilder:default=Managed
	ManagementState ManagementState `json:"managementState,omitempty"`

	// Auth is populated by the ODH platform operator.
	// +optional
	Auth *AgentsOperatorAuth `json:"auth,omitempty"`
}

// AgentsOperatorStatus defines the observed state of AgentsOperator.
type AgentsOperatorStatus struct {
	// Phase is the top-level lifecycle phase read by the platform for quick status summary.
	// +optional
	Phase Phase `json:"phase,omitempty"`

	// ObservedGeneration reflects the generation last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions report module health for platform aggregation.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Releases lists installed component versions.
	// +optional
	Releases []ComponentRelease `json:"releases,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:storageversion
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-agentsoperator'",message="AgentsOperator name must be default-agentsoperator"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// AgentsOperator is the platform module CR for the Agents Operator.
type AgentsOperator struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:default={}
	Spec   AgentsOperatorSpec   `json:"spec,omitempty"`
	Status AgentsOperatorStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AgentsOperatorList contains a list of AgentsOperator.
type AgentsOperatorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AgentsOperator `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AgentsOperator{}, &AgentsOperatorList{})
}
