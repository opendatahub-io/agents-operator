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

	//"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/go-logr/logr"
	platformv1alpha1 "github.com/kagenti/operator/platform/api/v1alpha1"
)

// PlatformReconciler reconciles a Platform object
type PlatformReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

const platformFinalizer = "kagenti.operator.dev/finalizer"

func (r *PlatformReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Log.WithValues("platform", req.Name, "Namespace", req.Namespace)
	logger.Info("Reconciling agentic platform")

	platform := &platformv1alpha1.Platform{}
	if err := r.Client.Get(ctx, req.NamespacedName, platform); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if platform.Status.Phase == "" {
		if err := r.initializePlatformStatus(ctx, platform); err != nil {
			logger.Error(err, "Failed to initialize platform status")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	if !controllerutil.ContainsFinalizer(platform, platformFinalizer) {
		controllerutil.AddFinalizer(platform, platformFinalizer)
		if err := r.Client.Update(ctx, platform); err != nil {
			logger.Error(err, "Unable to add finalizer to Platform")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if !platform.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.deletePlatform(ctx, platform)
	}

	if err := r.reconcileComponents(ctx, platform, platform.Spec.Infrastructure, "infrastructure"); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileComponents(ctx, platform, platform.Spec.Tools, "tools"); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.reconcileComponents(ctx, platform, platform.Spec.Agents, "agents"); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.updatePlatformStatus(ctx, platform); err != nil {
		logger.Error(err, "Faled to update platform status")
		return ctrl.Result{}, err
	}
	logger.Info("Agentic platform", "phase", platform.Status.Phase)

	return ctrl.Result{RequeueAfter: time.Second * 30}, nil
}
func (r *PlatformReconciler) updatePlatformStatus(ctx context.Context, platform *platformv1alpha1.Platform) error {

	allReady := true
	failedComponents := []string{}

	checkComponentList := func(components []platformv1alpha1.DeploymentStatus) {
		for _, comp := range components {
			if comp.Status != "Ready" {
				allReady = false
				if comp.Status == "Failed" {
					failedComponents = append(failedComponents, comp.Name)
				}
			}
		}
	}
	checkComponentList(platform.Status.Components.Infrastructure)
	checkComponentList(platform.Status.Components.Tools)
	checkComponentList(platform.Status.Components.Agents)

	oldPhase := platform.Status.Phase
	if allReady {
		platform.Status.Phase = "Ready"
	} else if len(failedComponents) > 0 {
		platform.Status.Phase = "Failed"
	} else {
		platform.Status.Phase = "Deploying"
	}
	readyCondition := metav1.Condition{
		Type:               "Ready",
		LastTransitionTime: metav1.Now(),
	}

	if allReady {
		readyCondition.Status = metav1.ConditionTrue
		readyCondition.Reason = "AllComponentsReady"
		readyCondition.Message = "All platform components are ready"
	} else if len(failedComponents) > 0 {
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = "Components Failed"
		readyCondition.Message = fmt.Sprintf("Some components failed: %v", failedComponents)
	} else {
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = "ComponentsDeploying"
		readyCondition.Message = "Components are still being deployed"
	}
	r.updateCondition(&platform.Status.Conditions, readyCondition)
	if oldPhase != platform.Status.Phase {
		r.Log.Info("Updating platform status",
			"platform", platform.Name,
			"oldPhase", oldPhase,
			"newPhase", platform.Status.Phase)
		return r.Client.Status().Update(ctx, platform)
	}
	return nil
}
func (r *PlatformReconciler) updateCondition(conditions *[]metav1.Condition, condition metav1.Condition) {
	for i, cond := range *conditions {
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
			(*conditions)[i] = condition
			return
		}
	}
	*conditions = append(*conditions, condition)
}
func (r *PlatformReconciler) reconcileComponents(ctx context.Context, platform *platformv1alpha1.Platform, components []platformv1alpha1.PlatformComponentRef, componentType string) error {
	logger := r.Log.WithValues("platform", platform.Name, "Namespace", platform.Namespace, "Type", componentType)
	logger.Info("Reconciling components", "count", len(components))

	for _, compRef := range components {
		component := &platformv1alpha1.Component{}
		nn := types.NamespacedName{
			Name:      compRef.ComponentReference.Name,
			Namespace: r.getNamespace(compRef.ComponentReference.Namespace, platform.Namespace),
		}
		err := r.Client.Get(ctx, nn, component)
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "Failed to fetch component", "component", compRef.Name, "namespace", compRef.ComponentReference.Namespace)
			return err
		} else if err != nil {
			logger.Error(err, "Platform component not found", "component", compRef.Name, "namespace", compRef.ComponentReference.Namespace)
			return err
		}
		if err := r.setComponentOwner(ctx, platform, component, compRef); err != nil {
			logger.Error(err, "Failed to set component ownership", "component", compRef.Name)
		}
		r.updateComponentStatusFromComponent(ctx, platform, component, compRef.Name, componentType)
	}
	return nil
}

func (r *PlatformReconciler) updateComponentStatusFromComponent(ctx context.Context, platform *platformv1alpha1.Platform, component *platformv1alpha1.Component, componentName, componentType string) {
	status := "unknown"
	errorMsg := ""

	for _, condition := range component.Status.Conditions {
		if condition.Type == "Ready" {
			if condition.Status == metav1.ConditionTrue {
				status = "Ready"
			} else {
				status = condition.Reason
				errorMsg = condition.Message
			}
			break
		}
	}
	r.updateComponentStatusInPlatform(ctx, platform, componentName, componentType, status, errorMsg)
}
func (r *PlatformReconciler) setComponentOwner(ctx context.Context, platform *platformv1alpha1.Platform, component *platformv1alpha1.Component, compRef platformv1alpha1.PlatformComponentRef) error {

	if metav1.IsControlledBy(component, platform) {
		return nil
	}
	if err := controllerutil.SetControllerReference(platform, component, r.Scheme); err != nil {
		return err
	}
	if platform.Spec.GlobalConfig.Labels != nil {
		if component.Labels == nil {
			component.Labels = make(map[string]string)
		}
		for k, v := range platform.Spec.GlobalConfig.Labels {
			component.Labels[k] = v
		}
	}
	if platform.Spec.GlobalConfig.Annotations != nil {
		if component.Annotations == nil {
			component.Annotations = make(map[string]string)
		}
		for k, v := range platform.Spec.GlobalConfig.Annotations {
			component.Annotations[k] = v
		}
	}
	if component.Labels == nil {
		component.Labels = make(map[string]string)
	}
	component.Labels["platform.operator.dev/platform"] = platform.Name
	component.Labels["platform.operator.dev/component"] = compRef.Name
	return r.Client.Update(ctx, component)
}
func (r *PlatformReconciler) updateComponentStatusInPlatform(ctx context.Context, platform *platformv1alpha1.Platform, componentName, componentType, status, errorMsg string) {
	compStatus := platformv1alpha1.DeploymentStatus{
		Name:   componentName,
		Status: status,
		Error:  errorMsg,
	}
	switch componentType {
	case "infrastructure":
		r.updateComponentStatusList(&platform.Status.Components.Infrastructure, compStatus)
	case "tools":
		r.updateComponentStatusList(&platform.Status.Components.Tools, compStatus)
	case "agents":
		r.updateComponentStatusList(&platform.Status.Components.Agents, compStatus)
	}
}
func (r *PlatformReconciler) updateComponentStatusList(statusList *[]platformv1alpha1.DeploymentStatus, status platformv1alpha1.DeploymentStatus) {
	for i, s := range *statusList {
		if s.Name == status.Name {
			(*statusList)[i].Status = status.Status
			(*statusList)[i].Error = status.Error
			return
		}

	}
	*statusList = append(*statusList, platformv1alpha1.DeploymentStatus{
		Name:   status.Name,
		Status: status.Status,
		Error:  status.Error,
	})
}
func (r *PlatformReconciler) deletePlatform(ctx context.Context, platform *platformv1alpha1.Platform) (ctrl.Result, error) {
	logger := r.Log.WithValues("platform", platform.Name, "Namespace", platform.Namespace)
	logger.Info("Deleting platform")

	allComponents := []platformv1alpha1.PlatformComponentRef{}
	allComponents = append(allComponents, platform.Spec.Infrastructure...)
	allComponents = append(allComponents, platform.Spec.Tools...)
	allComponents = append(allComponents, platform.Spec.Agents...)

	for _, compRef := range allComponents {

		component := &platformv1alpha1.Component{}
		nn := types.NamespacedName{
			Name:      compRef.Name,
			Namespace: r.getNamespace(compRef.ComponentReference.Namespace, platform.Namespace),
		}
		if err := r.Client.Get(ctx, nn, component); err != nil {
			if metav1.IsControlledBy(component, platform) {
				logger.Info("Deleting component", "component", component.Name)
				if err := r.Client.Delete(ctx, component); err != nil {
					logger.Error(err, "Failed to delete component", "component", compRef.Name)
					return ctrl.Result{}, err
				}
				return ctrl.Result{Requeue: true}, nil
			}
		}
	}
	controllerutil.RemoveFinalizer(platform, platformFinalizer)

	return ctrl.Result{}, nil
}

func (r *PlatformReconciler) initializePlatformStatus(ctx context.Context, platform *platformv1alpha1.Platform) error {
	platform.Status.Phase = "Initializing"
	platform.Status.Components = platformv1alpha1.ComponentsStatus{
		Infrastructure: []platformv1alpha1.DeploymentStatus{},
		Tools:          []platformv1alpha1.DeploymentStatus{},
		Agents:         []platformv1alpha1.DeploymentStatus{},
	}

	platform.Status.Conditions = []metav1.Condition{
		{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             "Initializing",
			Message:            "Platform is being initialized",
		},
	}
	return r.Client.Status().Update(ctx, platform)
}
func (r *PlatformReconciler) getNamespace(providedNs, defaultNs string) string {
	if providedNs != "" {
		return providedNs
	}
	return defaultNs
}

// SetupWithManager sets up the controller with the Manager.
func (r *PlatformReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.Platform{}).
		Owns(&platformv1alpha1.Component{}).
		Complete(r)
}
