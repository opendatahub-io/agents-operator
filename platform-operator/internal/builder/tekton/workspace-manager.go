package tekton

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	platformv1alpha1 "github.com/kagenti/operator/platform/api/v1alpha1"
	tektonv1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type WorkspaceManager struct {
	client client.Client
	Log    logr.Logger
}

func NewWorkspaceManager(c client.Client, log logr.Logger) *WorkspaceManager {
	return &WorkspaceManager{
		client: c,
		Log:    log,
	}
}
func (wm *WorkspaceManager) CreateWorkspacePVC(ctx context.Context, component *platformv1alpha1.Component) error {
	pvcName := fmt.Sprintf("%s-workspace", component.Name)
	pvc := &corev1.PersistentVolumeClaim{}
	err := wm.client.Get(ctx, types.NamespacedName{
		Name:      pvcName,
		Namespace: component.Namespace,
	}, pvc)
	if err == nil {
		return nil
	}
	if !errors.IsNotFound(err) {
		return fmt.Errorf("Failed to check if workspace PVC exisits: %w", err)
	}
	pvc = &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: component.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/part-of":   "platform-operator",
				"app.kubernetes.io/component": component.Name,
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("1Gi"),
				},
			},
		},
	}
	if err := controllerutil.SetControllerReference(component, pvc, wm.client.Scheme()); err != nil {
		return fmt.Errorf("Failed to set owner reference on workspace PVC: %w", err)
	}
	err = wm.client.Create(ctx, pvc)
	if err != nil {
		return fmt.Errorf("Failed to create workspace PVC: %w", err)
	}
	return nil
}
func (wm *WorkspaceManager) GetWorkspaceBindings(component *platformv1alpha1.Component) []tektonv1.WorkspaceBinding {
	return []tektonv1.WorkspaceBinding{
		{
			Name: "shared-workspace",
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: fmt.Sprintf("%s-workspace", component.Name),
			},
		},
	}
}
func (wm *WorkspaceManager) ListComponentWorkspaces(ctx context.Context, namespace string) (*corev1.PersistentVolumeClaimList, error) {
	pvcList := &corev1.PersistentVolumeClaimList{}
	err := wm.client.List(ctx, pvcList,
		client.InNamespace(namespace),
		client.MatchingLabels{"app.kubernetes.io/part-of": "platform-operator"},
	)
	if err != nil {
		return nil, fmt.Errorf("Failed to list component workspaces: %w", err)
	}
	return pvcList, nil
}
func (wm *WorkspaceManager) CleanupUnusedWorkspaces(ctx context.Context, namespace string) error {
	components := &platformv1alpha1.ComponentList{}
	err := wm.client.List(ctx, components, client.InNamespace(namespace))
	if err != nil {
		return fmt.Errorf("Failed to list components: %w", err)

	}
	activeComponents := make(map[string]bool)
	for _, component := range components.Items {
		activeComponents[component.Name] = true
	}
	workspaces, err := wm.ListComponentWorkspaces(ctx, namespace)
	if err != nil {
		return err
	}
	for _, pvc := range workspaces.Items {
		componentName, ok := pvc.Labels["app.kubernetes.io/component"]
		if !ok {
			continue
		}
		if !activeComponents[componentName] {
			err := wm.client.Delete(ctx, &pvc)
			if err != nil && !errors.IsNotFound(err) {
				return fmt.Errorf("Failed to deleted unused workspace: %w", err)
			}
		}
	}
	return nil
}
