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

package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/go-logr/logr"
	platformv1alpha1 "github.com/kagenti/operator/platform/api/v1alpha1"
	"github.com/kagenti/operator/platform/internal/builder"
	"github.com/kagenti/operator/platform/internal/deployer"
)

// ComponentReconciler reconciles a Component object
type ComponentReconciler struct {
	Client          client.Client
	Scheme          *runtime.Scheme
	Log             logr.Logger
	Builder         builder.Builder
	DeployerFactory *deployer.DeployerFactory
}

const componentFinalizer = "kagenti.operator.dev/finalizer"

// +kubebuilder:rbac:groups=kagenti.operator.dev,resources=components,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kagenti.operator.dev,resources=components/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kagenti.operator.dev,resources=components/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services;configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete

func (r *ComponentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Log.WithValues("controller", req.Name, "Namespace", req.Namespace)
	logger.Info("Reconciling component")

	component := &platformv1alpha1.Component{}
	if err := r.Client.Get(ctx, req.NamespacedName, component); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !component.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.deleteComponent(ctx, component)
	}

	if len(component.Status.Conditions) == 0 {
		if err := r.initializeComponentStatus(ctx, component); err != nil {
			logger.Error(err, "Failed to initialize component status")
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}
	if !controllerutil.ContainsFinalizer(component, componentFinalizer) {
		controllerutil.AddFinalizer(component, componentFinalizer)
		if err := r.Client.Update(ctx, component); err != nil {
			logger.Error(err, "Unable to add finalizer to Component")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	doBuild, err := r.triggerBuild(component)
	if err != nil {
		logger.Error(err, "Failed to determine if build is needed")
		return ctrl.Result{}, err
	}
	logger.Info("Controller --------", "buildNeeded", doBuild)
	if doBuild {
		err := r.Builder.Build(ctx, component)
		if err != nil {
			logger.Error(err, "Failed to build component", "component", component.Name)
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}
	if component.Status.BuildStatus != nil {
		logger.Info("Controller --------", "build Phase", component.Status.BuildStatus.Phase)
		if component.Status.BuildStatus.Phase == "Building" {
			err := r.Builder.CheckStatus(ctx, component)
			if err != nil {
				logger.Error(err, "Failed to check component status", "component", component.Name)
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: time.Second * 5}, nil
		} else if component.Status.BuildStatus.Phase == "Succeeded" {
			err := r.Builder.Cleanup(ctx, component)
			if err != nil {
				logger.Error(err, "Failed to cleanup Builder resource", "component", component.Name)
				return ctrl.Result{}, err
			}
		}
	}

	doDeploy, err := r.isDeploymentNeeded(component)
	if err != nil {
		logger.Error(err, "Failed to determine if deployment is needed")
		return ctrl.Result{}, err
	}
	phase := ""
	if component.Status.DeploymentStatus != nil {
		phase = component.Status.DeploymentStatus.Phase
	}
	logger.Info("Component deployment phase", "phase", phase)
	if doDeploy && phase != "deploying" {
		logger.Info("Starting component deployment --------")
		component.Status.DeploymentStatus.Phase = "Deploying"
		component.Status.DeploymentStatus.DeploymentMessage = "Deployment in progress"
		if err := r.Client.Status().Update(ctx, component); err != nil {
			return ctrl.Result{}, err
		}

		deployer, err := r.DeployerFactory.GetDeployer(component)
		if err != nil {
			component.Status.DeploymentStatus.Phase = "Failed"
			component.Status.DeploymentStatus.DeploymentMessage = "Invalid deployer for the component"
			err = r.Client.Status().Update(ctx, component)
			if err != nil {
				return ctrl.Result{}, err
			}
		}
		err = deployer.Deploy(ctx, component)
		if err != nil {
			component.Status.DeploymentStatus.Phase = "Failed"
			component.Status.DeploymentStatus.DeploymentMessage = "Failed to deploy the component"
			err = r.Client.Status().Update(ctx, component)
			if err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{RequeueAfter: time.Second * 20}, nil
	}

	if component.Status.DeploymentStatus != nil && component.Status.DeploymentStatus.Phase == "Deploying" {
		if err := r.checkDeploymentStatus(ctx, component); err != nil {
			logger.Error(err, "Failed to check deployment status")
			return ctrl.Result{}, nil
		}
	}
	if r.updateComponentStatus(ctx, component); err != nil {
		logger.Error(err, "Failed to update component status")
		return ctrl.Result{}, nil
	}
	logger.Info("Component reconciliation completed successfully", "phase", phase)
	return ctrl.Result{RequeueAfter: time.Second * 10}, nil
}
func (r *ComponentReconciler) initializeComponentStatus(ctx context.Context, component *platformv1alpha1.Component) error {
	now := metav1.Now()
	component.Status.Conditions = []metav1.Condition{
		{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			LastTransitionTime: now,
			Reason:             "Initializing",
			Message:            "Component is being initialized",
		},
	}
	component.Status.LastTransitionTime = &now

	if r.hasBuildSpec(component) {
		component.Status.BuildStatus = &platformv1alpha1.BuildStatus{
			Phase:   "Pending",
			Message: "Build is pending",
		}
	}

	component.Status.DeploymentStatus = &platformv1alpha1.ComponentDeploymentStatus{
		Phase:             "Pending",
		DeploymentMessage: "Deployment is pending",
	}

	return r.Client.Status().Update(ctx, component)
}

func (r *ComponentReconciler) updateComponentStatus(ctx context.Context, component *platformv1alpha1.Component) error {
	ready := true
	reason := "Ready"
	message := "Component is ready"

	if r.hasBuildSpec(component) && component.Status.BuildStatus != nil {
		if component.Status.BuildStatus.Phase != "Succeeded" {
			ready = false
			reason = fmt.Sprintf("BuildNotReady:%s", component.Status.BuildStatus.Phase)
			message = fmt.Sprintf("Build is not ready: %s", component.Status.BuildStatus.Message)
		}
	}
	if component.Status.DeploymentStatus != nil && component.Status.DeploymentStatus.Phase != "Ready" {
		ready = false
		reason = fmt.Sprintf("DeploymentNotReady:%s", component.Status.DeploymentStatus.Phase)
		message = fmt.Sprintf("Deployment is not ready: %s", component.Status.DeploymentStatus.DeploymentMessage)
	}

	readyCondition := metav1.Condition{
		Type:               "Ready",
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}

	if ready {
		readyCondition.Status = metav1.ConditionTrue
	} else {
		readyCondition.Status = metav1.ConditionFalse
	}

	r.updateComponentCondition(component, readyCondition)

	now := metav1.Now()
	component.Status.LastTransitionTime = &now
	return r.Client.Status().Update(ctx, component)
}

func (r *ComponentReconciler) updateComponentCondition(component *platformv1alpha1.Component, condition metav1.Condition) {
	for i, cond := range component.Status.Conditions {
		if cond.Type == condition.Type {
			if cond.Status == condition.Status &&
				cond.Reason == condition.Reason &&
				cond.Message == cond.Message {
				return
			}
			if cond.Status != condition.Status {
				condition.LastTransitionTime = metav1.Now()
			} else {
				condition.LastTransitionTime = cond.LastTransitionTime
			}
			component.Status.Conditions[i] = condition
			return
		}
	}
	component.Status.Conditions = append(component.Status.Conditions, condition)
}
func (r *ComponentReconciler) checkDeploymentStatus(ctx context.Context, component *platformv1alpha1.Component) error {
	logger := r.Log.WithValues("component", component.Name, "Namespace", component.Namespace)
	logger.Info("Checking component status")

	deployer, err := r.DeployerFactory.GetDeployer(component)
	if err != nil {
		logger.Error(err, "Failed deployer lookup - invalid component")
		component.Status.DeploymentStatus.Phase = "Failed"
		component.Status.DeploymentStatus.DeploymentMessage = "Invalid deployer for the component"
		err = r.Client.Status().Update(ctx, component)
		if err != nil {
			return err
		}

	}
	logger.Info("checkDeploymentStatus", "deployer", deployer.GetName())
	ready, message, err := deployer.CheckComponentStatus(ctx, component)
	if err != nil {
		logger.Error(err, "Failed to check component status ")
		return err
	}
	component.Status.DeploymentStatus.DeploymentMessage = message

	logger.Info("checkDeploymentStatus", "phase", component.Status.DeploymentStatus.Phase, "isReady", ready)

	nn := types.NamespacedName{
		Name:      component.Name,
		Namespace: component.Namespace,
	}
	// Attempt to update the object with retry
	err2 := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &platformv1alpha1.Component{}

		if err := r.Client.Get(ctx, nn, current); err != nil {
			return err
		}
		if ready {
			current.Status.DeploymentStatus.Phase = "Ready"
			current.Status.DeploymentStatus.DeploymentMessage = "Component Deployed"
		} else {
			current.Status.DeploymentStatus.Phase = "Deploying"
		}

		return r.Client.Status().Update(ctx, current)
	})
	if err2 != nil {
		logger.Error(err, "Failed to update component status ")
		return err
	}
	return nil
}

func (r *ComponentReconciler) triggerBuild(component *platformv1alpha1.Component) (bool, error) {
	if !r.hasBuildSpec(component) {
		return false, nil
	}

	if component.Status.BuildStatus == nil || component.Status.BuildStatus.Phase == "Pending" {
		return true, nil
	}
	if component.Status.BuildStatus.Phase == "Failed" {
		return true, nil
	}

	return false, nil
}

func (r *ComponentReconciler) isDeploymentNeeded(component *platformv1alpha1.Component) (bool, error) {
	r.Log.Info("isDeploymentNeeded()", "deploymentStatus", component.Status.DeploymentStatus)
	if component.Status.DeploymentStatus == nil || component.Status.DeploymentStatus.Phase == "Pending" {
		return true, nil
	}
	if component.Status.DeploymentStatus.Phase == "Failed" {
		return true, nil
	}
	if r.hasBuildSpec(component) &&
		(component.Status.BuildStatus == nil || component.Status.BuildStatus.Phase != "Succeeded") {
		return false, nil
	}

	return false, nil
}
func (r *ComponentReconciler) hasBuildSpec(component *platformv1alpha1.Component) bool {
	return component.Spec.Agent != nil && component.Spec.Agent.Build != nil ||
		component.Spec.Tool != nil && component.Spec.Tool.Build != nil
}
func (r *ComponentReconciler) deleteComponent(ctx context.Context, component *platformv1alpha1.Component) (ctrl.Result, error) {

	if component.Status.BuildStatus != nil && component.Status.BuildStatus.Phase == "Building" {
		r.Log.Info("Cancelling in-progress build")
		if err := r.Builder.Cancel(ctx, component); err != nil {
			r.Log.Error(err, "Error while cancelling component build")
		}
	}
	if component.Status.DeploymentStatus != nil {
		componentDeployer, err := r.DeployerFactory.GetDeployer(component)
		if err != nil {
			r.Log.Error(err, "Unable to determine deployer for component")
			return ctrl.Result{}, err
		}
		if err := componentDeployer.Delete(ctx, component); err != nil {
			r.Log.Error(err, "Failed to delete component")
			return ctrl.Result{}, err
		}
	}
	controllerutil.RemoveFinalizer(component, componentFinalizer)
	if err := r.Client.Update(ctx, component); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ComponentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.Component{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Complete(r)
}
