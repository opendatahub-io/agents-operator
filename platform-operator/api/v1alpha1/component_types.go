/*
Copyright 2025.

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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ComponentSpec struct {

	// Component Types
	// Union pattern: only one of the following components should be specified.
	Agent *AgentComponent `json:"agent,omitempty"`
	// MCP Servers, Utilities, etc
	Tool *ToolComponent `json:"tool,omitempty"`
	// Redis, Postgresql, etc
	Infra *InfraComponent `json:"infra,omitempty"`

	// --------------------------

	// Common fields for all component types
	// Deployment strategy for the component: Helm, K8s manifest(deployments), OLM (operators)
	Deployer DeployerSpec `json:"deployer"`

	// Description is a human-readable description of the component
	// +optional
	Description string `json:"description,omitempty"`

	// Dependencies defines other components this agent depends on
	// +optional
	Dependencies []DependencySpec `json:"dependencies,omitempty"`
}

type AgentComponent struct {
	// Agent specific attributes

	// Build configuration for building the agent from source
	// +optional
	Build *BuildSpec `json:"build,omitempty"`
}

type ToolComponent struct {
	// tool specific attributes

	// Build configuration for building the tool from source
	// +optional
	Build *BuildSpec `json:"build,omitempty"`

	// ToolType specifies the type of tool
	// MCP;Utility
	ToolType string `json:"toolType"`
}

type InfraComponent struct {
	// Infra specific attributes

	// InfraType specifies the type of infrastructure
	// Database;Cache;Queue;StorageService;SearchEngine
	InfraType string `json:"infraType,omitempty"`

	// InfraProvider specifies the infrastructure provider
	// PostgreSQL;MySQL;MongoDB;Redis;Kafka;ElasticSearch;MinIO
	InfraProvider string `json:"infraProvider,omitempty"`

	// Version specifies the version of the infrastructure component
	Version string `json:"version,omitempty"`

	// SecretRef reference to secrets containing credentials
	// +optional
	SecretRef *corev1.LocalObjectReference `json:"secretRef,omitempty"`
}

// DependencySpec defines a dependency on another component
type DependencySpec struct {
	// Name is the name of the component
	Name string `json:"name"`

	// Kind is the kind of the component
	// +kubebuilder:validation:Enum=Agent;Tool;Infra
	Kind string `json:"kind"`

	// Version is the version of the component
	// +optional
	Version string `json:"version,omitempty"`
}

// DeployerSpec defines how to deploy a component
type DeployerSpec struct {
	// Only one of the following deployment methods should be specified.
	Helm       *HelmSpec       `json:"helm,omitempty"`
	Kubernetes *KubernetesSpec `json:"kubernetes,omitempty"`
	Olm        *OlmSpec        `json:"olm,omitempty"`
	// Common deployment settings

	// Name of the k8s resource
	Name string `json:"name,omitempty"`

	// Namespace to deploy to, defaults to the namespace of the CR
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Environment variables for the component
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// DeployAfterBuild indicates whether to automatically deploy the component after build
	// +optional
	DeployAfterBuild bool `json:"deployAfterBuild,omitempty"`
}

// BuildSpec defines how to build a component from source
type BuildSpec struct {
	// SourceRepository is the Git repository URL
	// +optional
	SourceRepository string `json:"sourceRepository,omitempty"`

	// SourceRevision is the Git revision (branch, tag, commit)
	// +optional
	SourceRevision string `json:"sourceRevision,omitempty"`

	// SourceSubfolder is the folder within the repository containing the source
	// +optional
	SourceSubfolder string `json:"sourceSubfolder,omitempty"`

	// RepoUser is the username in the Git repository containing the source
	// +optional
	RepoUser string `json:"repoUser,omitempty"`

	// SourceCredentials is a reference to a secret containing Git credentials
	// +optional
	SourceCredentials *corev1.LocalObjectReference `json:"sourceCredentials,omitempty"`

	// Pipeline specifies the pipeline configuration
	Pipeline PipelineSpec `json:"pipeline"`

	// BuildArgs are arguments to pass to the build process
	// +optional
	BuildArgs []ParameterSpec `json:"buildArgs,omitempty"`

	// BuildOutput specifies where to store build artifacts
	// +optional
	BuildOutput *BuildOutput `json:"buildOutput,omitempty"`

	// CleanupAfterBuild indicates whether to automatically cleanup after build
	// +optional
	CleanupAfterBuild bool `json:"cleanupAfterBuild,omitempty"`
}

// PipelineSpec defines how the pipeline should be configured
type PipelineSpec struct {
	// Steps is an ordered list of pipeline steps to execute
	Steps []PipelineStepSpec `json:"steps"`

	// Parameters contains additional parameters to pass to the pipeline
	Parameters []ParameterSpec `json:"parameters,omitempty"`
}

// PipelineStepSpec defines a single step in the pipeline
type PipelineStepSpec struct {
	// Name is the identifier for the step
	Name string `json:"name"`

	// ConfigMap references the ConfigMap containing the step definition
	ConfigMap string `json:"configMap"`

	// Enabled indicates whether this step should be included in the pipeline
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Parameters contains step-specific parameters that override global parameters
	// +optional
	Parameters []ParameterSpec `json:"parameters,omitempty"`
}

/*
// BuildArg defines a build argument
type BuildArg struct {
	// Name of the build argument
	Name string `json:"name"`

	// Value of the build argument
	Value string `json:"value"`
}
*/
// BuildOutput defines where to store build artifacts
type BuildOutput struct {
	// Image is the name of the image to build
	// +kubebuilder:validation:Required
	Image string `json:"image"`

	// ImageTag is the tag to apply to the built image
	// +kubebuilder:validation:Required
	ImageTag string `json:"imageTag"`

	// ImageRegistry is the container registry where the image will be pushed
	// +kubebuilder:validation:Required
	ImageRegistry string `json:"imageRegistry"`
}

// HelmSpec defines Helm deployment configuration
type OlmSpec struct {
}

// HelmSpec defines Helm deployment configuration
type HelmSpec struct {
	// ChartName is the name of the Helm chart
	ChartName string `json:"chartName"`

	// ChartVersion is the version of the Helm chart
	// +optional
	ChartVersion string `json:"chartVersion,omitempty"`

	// ChartRepoName is the repository for the Helm chart
	// +optional
	ChartRepoName string `json:"chartRepoName,omitempty"`

	// ChartRepoUrl is the repository URL for the Helm chart
	// +optional
	ChartRepoUrl string `json:"chartRepoUrl,omitempty"`

	// Parameters
	// +optional
	Parameters []ParameterSpec `json:"parameters,omitempty"`

	// ReleaseName is the name of the Helm release
	// +optional
	ReleaseName string `json:"releaseName,omitempty"`
}

// Parameter defines an argument
type ParameterSpec struct {
	// Name of the  argument
	Name string `json:"name"`

	// Value of the argument
	Value string `json:"value"`
}

// KubernetesSpec defines Kubernetes manifest deployment
type KubernetesSpec struct {
	ImageSpec ImageSpec `json:"imageSpec,omitempty"`

	// Resources is the compute resources required by the container
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	ContainerPorts []corev1.ContainerPort `json:"containerPorts,omitempty"`

	ServicePorts []corev1.ServicePort `json:"servicePorts,omitempty"`

	// ServiceType is the type of service to create
	// +kubebuilder:validation:Enum=ClusterIP;NodePort;LoadBalancer
	// +optional
	ServiceType string `json:"serviceType,omitempty"`
}

type ImageSpec struct {
	// Image is the name of the image to use
	// +kubebuilder:validation:Required
	Image string `json:"image"`

	// ImageTag is the tag assigned to image
	// +kubebuilder:validation:Required
	ImageTag string `json:"imageTag"`

	// ImageRegistry is the container registry where the image is going to be pulled from
	// +kubebuilder:validation:Required
	ImageRegistry string `json:"imageRegistry"`

	// ImagePullPolicy defines when to pull the image
	// +optional
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`
}

// ComponentStatus defines the observed state of Component.
type ComponentStatus struct {
	// ComponentType indicates the type of component (Agent, Tool, Infra)
	// +optional
	ComponentType string `json:"componentType,omitempty"`

	// Conditions represent overall status
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Build status
	// +optional
	BuildStatus *BuildStatus `json:"buildStatus,omitempty"`

	// Deployment status
	// +optional
	DeploymentStatus *ComponentDeploymentStatus `json:"deploymentStatus,omitempty"`

	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`
}

type BuildStatus struct {
	// Current build phase: Pending, Building, Succeeded, Failed
	Phase string `json:"phase,omitempty"`

	// Build Message
	Message string `json:"message,omitempty"`

	// PipelineRun name
	PipelineRunName string `json:"pipelineRunName,omitempty"`

	// Last build time
	LastBuildTime *metav1.Time `json:"lastBuildTime,omitempty"`

	// pipeline completion time
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	BuiltImage string `json:"builtImage,omitempty"`
}

type ComponentDeploymentStatus struct {
	// Current deployment phase: Pending, Deploying, Ready, Failed
	Phase string `json:"phase,omitempty"`

	// Deployment message
	DeploymentMessage string `json:"deploymentMessage,omitempty"`

	// Deployment completion time
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Component is the Schema for the components API.
type Component struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ComponentSpec   `json:"spec,omitempty"`
	Status ComponentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ComponentList contains a list of Component.
type ComponentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Component `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Component{}, &ComponentList{})
}
