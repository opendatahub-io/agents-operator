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

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	platformv1alpha1 "github.com/kagenti/operator/api/platform/v1alpha1"
)

const (
	conditionReady                 = "Ready"
	conditionProvisioningSucceeded = "ProvisioningSucceeded"
	conditionDegraded              = "Degraded"

	reasonAvailable            = "Available"
	reasonProvisioningComplete = "ProvisioningComplete"
	reasonNoWarnings           = "NoWarnings"
	reasonRemoved              = "Removed"
	reasonUnmanaged            = "Unmanaged"

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

// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=agentsoperators,verbs=get;list;watch
// +kubebuilder:rbac:groups=components.platform.opendatahub.io,resources=agentsoperators/status,verbs=get;update

func (r *AgentsOperatorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	ao := &platformv1alpha1.AgentsOperator{}
	if err := r.Get(ctx, req.NamespacedName, ao); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	var (
		ready        metav1.ConditionStatus
		readyReason  string
		readyMessage string
		prov         metav1.ConditionStatus
		provReason   string
		provMessage  string
	)

	switch ao.Spec.ManagementState {
	case platformv1alpha1.ManagementStateManaged, platformv1alpha1.ManagementStateForce:
		ready = metav1.ConditionTrue
		readyReason = reasonAvailable
		readyMessage = "Agents Operator is running"
		prov = metav1.ConditionTrue
		provReason = reasonProvisioningComplete
		provMessage = "Module controller is active"
	case platformv1alpha1.ManagementStateRemoved:
		ready = metav1.ConditionFalse
		readyReason = reasonRemoved
		readyMessage = "Agents Operator management state is Removed"
		prov = metav1.ConditionFalse
		provReason = reasonRemoved
		provMessage = "Module is removed"
	default:
		ready = metav1.ConditionFalse
		readyReason = reasonUnmanaged
		readyMessage = "Agents Operator management state is not Managed"
		prov = metav1.ConditionFalse
		provReason = reasonUnmanaged
		provMessage = "Module is unmanaged"
	}

	apimeta.SetStatusCondition(&ao.Status.Conditions, metav1.Condition{
		Type:    conditionReady,
		Status:  ready,
		Reason:  readyReason,
		Message: readyMessage,
	})
	apimeta.SetStatusCondition(&ao.Status.Conditions, metav1.Condition{
		Type:    conditionProvisioningSucceeded,
		Status:  prov,
		Reason:  provReason,
		Message: provMessage,
	})
	apimeta.SetStatusCondition(&ao.Status.Conditions, metav1.Condition{
		Type:    conditionDegraded,
		Status:  metav1.ConditionFalse,
		Reason:  reasonNoWarnings,
		Message: "",
	})

	if ready == metav1.ConditionTrue {
		ao.Status.Phase = platformv1alpha1.PhaseReady
	} else {
		ao.Status.Phase = platformv1alpha1.PhaseNotReady
	}

	ao.Status.ObservedGeneration = ao.Generation
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
