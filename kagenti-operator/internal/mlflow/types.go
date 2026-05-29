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

package mlflow

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// SchemeGroupVersion is the GVK for the mlflows.mlflow.opendatahub.io CRD.
	SchemeGroupVersion = schema.GroupVersion{Group: "mlflow.opendatahub.io", Version: "v1"}

	// SchemeBuilder is used to add the MLflow types to a scheme.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme registers the MLflow types with a runtime.Scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion,
		&MLflow{},
		&MLflowList{},
	)
	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)
	return nil
}

// MLflow is a minimal representation of the mlflows.mlflow.opendatahub.io/v1 CR.
// Only the fields needed for tracking URI discovery are included.
type MLflow struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Status            MLflowStatus `json:"status,omitempty"`
}

type MLflowStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// URL is the external gateway URL for the MLflow server (e.g. via the RHOAI data-science gateway).
	URL string `json:"url,omitempty"`
	// Address holds the in-cluster address for the MLflow server.
	Address *MLflowAddress `json:"address,omitempty"`
}

type MLflowAddress struct {
	// URL is the in-cluster service URL (e.g. https://mlflow.redhat-ods-applications.svc:8443).
	URL string `json:"url,omitempty"`
}

type MLflowList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MLflow `json:"items"`
}

func (in *MLflow) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

func (in *MLflow) DeepCopy() *MLflow {
	if in == nil {
		return nil
	}
	out := new(MLflow)
	in.DeepCopyInto(out)
	return out
}

func (in *MLflow) DeepCopyInto(out *MLflow) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Status.DeepCopyInto(&out.Status)
}

func (in *MLflowStatus) DeepCopyInto(out *MLflowStatus) {
	*out = *in
	if in.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(in.Conditions))
		for i := range in.Conditions {
			in.Conditions[i].DeepCopyInto(&out.Conditions[i])
		}
	}
	if in.Address != nil {
		out.Address = &MLflowAddress{URL: in.Address.URL}
	}
}

func (in *MLflowList) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

func (in *MLflowList) DeepCopy() *MLflowList {
	if in == nil {
		return nil
	}
	out := new(MLflowList)
	in.DeepCopyInto(out)
	return out
}

func (in *MLflowList) DeepCopyInto(out *MLflowList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]MLflow, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}
