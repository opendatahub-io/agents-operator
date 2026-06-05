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

package injector

import (
	"context"
	"errors"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/kagenti/operator/internal/controller"
)

// DefaultsConfigReconciler watches cluster and namespace ConfigMaps and
// updates the kagenti.io/config-hash annotation on workloads that have
// the kagenti.io/type label but are NOT managed by an AgentRuntime CR.
//
// This ensures that sidecar configuration stays current even when no
// AgentRuntime CR exists (e.g. after CR deletion with the type label
// preserved, or workloads that rely purely on platform defaults).
//
// NOTE: This reconciler intentionally lives in the webhook package for
// now. It is expected to move to a dedicated controller in the future.
type DefaultsConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *DefaultsConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("configmap", req.NamespacedName)

	// Determine scope from the request key. We cannot rely solely on
	// fetching the ConfigMap because it may have been deleted — and a
	// deletion still changes the defaults-only hash (one input is gone).
	var namespaces []string

	cm := &corev1.ConfigMap{}
	err := r.Get(ctx, req.NamespacedName, cm)

	switch {
	case err == nil && isClusterConfigMap(cm):
		// Cluster-level ConfigMap updated — affects all namespaces.
		ns, err := r.namespacesWithKagentiWorkloads(ctx)
		if err != nil {
			logger.Error(err, "failed to list namespaces with kagenti workloads")
			return ctrl.Result{}, err
		}
		namespaces = ns

	case err == nil && isNamespaceDefaultsConfigMap(cm):
		// Namespace-level defaults ConfigMap updated.
		namespaces = []string{cm.Namespace}

	case err == nil:
		// ConfigMap exists but is not relevant (predicate should prevent this).
		return ctrl.Result{}, nil

	case apierrors.IsNotFound(err):
		// ConfigMap was deleted. Use the request key to infer scope.
		if isClusterConfigMapKey(req.NamespacedName) {
			ns, err := r.namespacesWithKagentiWorkloads(ctx)
			if err != nil {
				logger.Error(err, "failed to list namespaces with kagenti workloads")
				return ctrl.Result{}, err
			}
			namespaces = ns
		} else {
			// Namespace-level ConfigMap deleted — re-hash workloads in that namespace.
			namespaces = []string{req.Namespace}
		}
		logger.Info("ConfigMap deleted, re-hashing affected workloads")

	default:
		return ctrl.Result{}, err
	}

	var firstErr error
	for _, ns := range namespaces {
		if err := r.reconcileWorkloadsInNamespace(ctx, ns); err != nil {
			logger.Error(err, "failed to reconcile workloads", "namespace", ns)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return ctrl.Result{}, firstErr
}

// reconcileWorkloadsInNamespace updates config-hash on Deployments and
// StatefulSets that carry the kagenti.io/type label but are not managed
// by an AgentRuntime CR. Errors from individual workload updates are
// accumulated and returned so that controller-runtime requeues the request.
func (r *DefaultsConfigReconciler) reconcileWorkloadsInNamespace(ctx context.Context, namespace string) error {
	logger := log.FromContext(ctx).WithValues("namespace", namespace)
	var errs []error

	// Process Deployments
	deployList := &appsv1.DeploymentList{}
	if err := r.List(ctx, deployList,
		client.InNamespace(namespace),
		client.HasLabels{KagentiTypeLabel},
	); err != nil {
		return err
	}
	for i := range deployList.Items {
		dep := &deployList.Items[i]
		if isManagedByAgentRuntime(dep) {
			continue
		}
		if err := r.updateConfigHash(ctx, namespace, dep.Name, "Deployment"); err != nil {
			logger.Error(err, "failed to update Deployment config-hash", "name", dep.Name)
			errs = append(errs, err)
		}
	}

	// Process StatefulSets
	ssList := &appsv1.StatefulSetList{}
	if err := r.List(ctx, ssList,
		client.InNamespace(namespace),
		client.HasLabels{KagentiTypeLabel},
	); err != nil {
		return err
	}
	for i := range ssList.Items {
		ss := &ssList.Items[i]
		if isManagedByAgentRuntime(ss) {
			continue
		}
		if err := r.updateConfigHash(ctx, namespace, ss.Name, "StatefulSet"); err != nil {
			logger.Error(err, "failed to update StatefulSet config-hash", "name", ss.Name)
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// updateConfigHash computes the defaults-only hash and applies it to
// the workload's PodTemplateSpec if it differs from the current value.
func (r *DefaultsConfigReconciler) updateConfigHash(ctx context.Context, namespace, name, kind string) error {
	logger := log.FromContext(ctx).WithValues("workload", name, "kind", kind)

	configResult, err := controller.ComputeConfigHash(ctx, r.Client, namespace)
	if err != nil {
		return err
	}
	newHash := configResult.Hash

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		key := types.NamespacedName{Name: name, Namespace: namespace}

		switch kind {
		case "Deployment":
			dep := &appsv1.Deployment{}
			if err := r.Get(ctx, key, dep); err != nil {
				return client.IgnoreNotFound(err)
			}
			current := dep.Spec.Template.Annotations[controller.AnnotationConfigHash]
			if current == newHash {
				return nil
			}
			if dep.Spec.Template.Annotations == nil {
				dep.Spec.Template.Annotations = make(map[string]string)
			}
			dep.Spec.Template.Annotations[controller.AnnotationConfigHash] = newHash
			logger.Info("Updating config-hash to defaults-only",
				"oldHash", truncateHash(current), "newHash", truncateHash(newHash))
			return r.Update(ctx, dep)

		case "StatefulSet":
			ss := &appsv1.StatefulSet{}
			if err := r.Get(ctx, key, ss); err != nil {
				return client.IgnoreNotFound(err)
			}
			current := ss.Spec.Template.Annotations[controller.AnnotationConfigHash]
			if current == newHash {
				return nil
			}
			if ss.Spec.Template.Annotations == nil {
				ss.Spec.Template.Annotations = make(map[string]string)
			}
			ss.Spec.Template.Annotations[controller.AnnotationConfigHash] = newHash
			logger.Info("Updating config-hash to defaults-only",
				"oldHash", truncateHash(current), "newHash", truncateHash(newHash))
			return r.Update(ctx, ss)

		default:
			return fmt.Errorf("unsupported workload kind: %s", kind)
		}
	})
}

// namespacesWithKagentiWorkloads returns all namespaces that contain at
// least one Deployment or StatefulSet with the kagenti.io/type label.
// TODO: consider adding a field indexer on KagentiTypeLabel if cluster size grows.
func (r *DefaultsConfigReconciler) namespacesWithKagentiWorkloads(ctx context.Context) ([]string, error) {
	seen := make(map[string]bool)

	deployList := &appsv1.DeploymentList{}
	if err := r.List(ctx, deployList, client.HasLabels{KagentiTypeLabel}); err != nil {
		return nil, err
	}
	for i := range deployList.Items {
		seen[deployList.Items[i].Namespace] = true
	}

	ssList := &appsv1.StatefulSetList{}
	if err := r.List(ctx, ssList, client.HasLabels{KagentiTypeLabel}); err != nil {
		return nil, err
	}
	for i := range ssList.Items {
		seen[ssList.Items[i].Namespace] = true
	}

	namespaces := make([]string, 0, len(seen))
	for ns := range seen {
		namespaces = append(namespaces, ns)
	}
	return namespaces, nil
}

// SetupWithManager registers the reconciler with the controller-runtime manager.
func (r *DefaultsConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("defaults-config").
		For(&corev1.ConfigMap{}, builder.WithPredicates(kagentiConfigMapPredicate())).
		Complete(r)
}

// kagentiConfigMapPredicate filters to only cluster-level defaults and
// namespace-level defaults ConfigMaps.
func kagentiConfigMapPredicate() predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		cm, ok := obj.(*corev1.ConfigMap)
		if !ok {
			return false
		}
		return isClusterConfigMap(cm) || isNamespaceDefaultsConfigMap(cm)
	})
}

func isClusterConfigMap(cm *corev1.ConfigMap) bool {
	return isClusterConfigMapKey(types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace})
}

// isClusterConfigMapKey checks whether a NamespacedName refers to one of the
// cluster-level defaults ConfigMaps. Used for both live objects and deletion
// events where the object no longer exists.
func isClusterConfigMapKey(key types.NamespacedName) bool {
	if key.Namespace != controller.ClusterDefaultsNamespace {
		return false
	}
	return key.Name == controller.ClusterDefaultsConfigMapName ||
		key.Name == controller.ClusterFeatureGatesConfigMapName
}

func isNamespaceDefaultsConfigMap(cm *corev1.ConfigMap) bool {
	labels := cm.GetLabels()
	return labels != nil && labels[controller.LabelNamespaceDefaults] == "true"
}

// isManagedByAgentRuntime checks if a workload is actively managed by
// an AgentRuntime CR. The AgentRuntime controller sets this label when
// the CR is active and removes it on CR deletion.
func isManagedByAgentRuntime(obj client.Object) bool {
	labels := obj.GetLabels()
	return labels != nil && labels[controller.LabelManagedBy] == controller.LabelManagedByValue
}

func truncateHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
