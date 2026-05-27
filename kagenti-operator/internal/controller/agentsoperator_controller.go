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

package controller

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	platformv1alpha1 "github.com/kagenti/operator/api/platform/v1alpha1"
)

const (
	agentsOperatorReleaseName    = "agents-operator"
	agentsOperatorReleaseRepoURL = "https://github.com/opendatahub-io/agents-operator"
	agentsOperatorReleaseVersion = "0.2.0-alpha.24"
)

// AgentsOperatorReconciler reconciles the platform AgentsOperator CR and reports
// status for ODH/RHOAI platform aggregation.
type AgentsOperatorReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=agentsoperators,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=agentsoperators/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=agentsoperators/finalizers,verbs=update

func (r *AgentsOperatorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	ao := &platformv1alpha1.AgentsOperator{}
	if err := r.Get(ctx, req.NamespacedName, ao); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	now := metav1.Now()
	ready := metav1.ConditionFalse
	readyReason := "NotManaged"
	readyMessage := "Agents Operator module is not managed"
	prov := metav1.ConditionFalse
	provReason := "NotManaged"
	provMessage := "Module is not managed"
	degraded := metav1.ConditionFalse
	degradedReason := "NoWarnings"
	degradedMessage := ""

	switch ao.Spec.ManagementState {
	case platformv1alpha1.ManagementStateManaged, platformv1alpha1.ManagementStateForce:
		ready = metav1.ConditionTrue
		readyReason = "Available"
		readyMessage = "Agents Operator is running"
		prov = metav1.ConditionTrue
		provReason = "ProvisioningComplete"
		provMessage = "Module controller is active"
	case platformv1alpha1.ManagementStateRemoved:
		readyReason = "Removed"
		readyMessage = "Agents Operator management state is Removed"
		provReason = "Removed"
		provMessage = "Module is removed"
	default:
		readyReason = "Unmanaged"
		readyMessage = "Agents Operator management state is not Managed"
		provReason = "Unmanaged"
		provMessage = "Module is unmanaged"
	}

	conditions := []metav1.Condition{
		{
			Type:               "Ready",
			Status:             ready,
			Reason:             readyReason,
			Message:            readyMessage,
			LastTransitionTime: now,
		},
		{
			Type:               "ProvisioningSucceeded",
			Status:             prov,
			Reason:             provReason,
			Message:            provMessage,
			LastTransitionTime: now,
		},
		{
			Type:               "Degraded",
			Status:             degraded,
			Reason:             degradedReason,
			Message:            degradedMessage,
			LastTransitionTime: now,
		},
	}

	ao.Status.ObservedGeneration = ao.Generation
	ao.Status.Conditions = conditions
	ao.Status.Releases = []platformv1alpha1.ComponentRelease{
		{
			Name:    agentsOperatorReleaseName,
			RepoURL: agentsOperatorReleaseRepoURL,
			Version: agentsOperatorReleaseVersion,
		},
	}

	if err := r.Status().Update(ctx, ao); err != nil {
		logger.Error(err, "failed to update AgentsOperator status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (r *AgentsOperatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.AgentsOperator{}).
		Complete(r)
}
