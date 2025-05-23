package tekton

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	platformv1alpha1 "github.com/kagenti/operator/platform/api/v1alpha1"
	"github.com/kagenti/operator/platform/internal/builder"
	tektonv1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var _ builder.Builder = &TektonBuilder{}

type TektonBuilder struct {
	client.Client
	Scheme           *runtime.Scheme
	Log              logr.Logger
	PipelineComposer *PipelineComposer
	WorkspaceManager *WorkspaceManager
}

func NewTektonBuilder(client client.Client, log logr.Logger, scheme *runtime.Scheme, composer *PipelineComposer, workspaceMgr *WorkspaceManager) *TektonBuilder {
	return &TektonBuilder{
		Client:           client,
		Scheme:           scheme,
		Log:              log,
		PipelineComposer: composer,
		WorkspaceManager: workspaceMgr,
	}
}
func (b *TektonBuilder) Build(ctx context.Context, component *platformv1alpha1.Component) error {
	b.Log.Info("TektonBuilder - building Tekton PipelineRun")

	if err := b.WorkspaceManager.CreateWorkspacePVC(ctx, component); err != nil {
		b.Log.Error(err, "Failed to verify if workspace PVC is available")
	}
	if b.triggerNewBuild(component) {
		err := b.createPipelineRun(ctx, component)
		if err != nil {
			return err
		}
		return nil
	}
	if err := b.CheckStatus(ctx, component); err != nil {
		return err
	}
	return nil
}
func (b *TektonBuilder) Cleanup(ctx context.Context, component *platformv1alpha1.Component) error {

	// Get the pods associated with the PipelineRun
	pipelineRunName := component.Status.BuildStatus.PipelineRunName

	// List the pods with the PipelineRun label
	podList := &corev1.PodList{}
	err := b.List(ctx, podList, client.MatchingLabels{"tekton.dev/pipelineRun": pipelineRunName})
	if err != nil {
		b.Log.Error(err, "Failed to list pods for PipelineRun", "pipelineRunName", pipelineRunName)
		return err
	}

	// Filter for completed pods (Succeeded)
	var completedPods []corev1.Pod
	for _, pod := range podList.Items {
		if pod.Status.Phase == corev1.PodSucceeded {
			completedPods = append(completedPods, pod)
		}
	}

	// Delete completed pods
	for _, pod := range completedPods {
		b.Log.Info("Deleting completed pod", "podName", pod.Name)
		err := b.Delete(ctx, &pod)
		if err != nil {
			b.Log.Error(err, "Failed to delete completed pod", "podName", pod.Name)
		}
	}
	return nil
}
func (b *TektonBuilder) createPipelineRun(ctx context.Context, component *platformv1alpha1.Component) error {
	logger := b.Log.WithValues("Tekton builder", component.Name, "Namespace", component.Namespace)
	logger.Info("Creating new PipelineRun")

	pipelineSpec, err := b.PipelineComposer.ComposePipelineSpec(ctx, component)
	if err != nil {
		return err
	}
	pp, err := json.MarshalIndent(pipelineSpec, "", "  ")

	fmt.Println("PipelineSpec:::" + string(pp))

	pipelineRunName := fmt.Sprintf("%s-%s", component.Name, time.Now().Format("20250102150405"))
	pipelineRun := &tektonv1.PipelineRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pipelineRunName,
			Namespace: component.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/part-of":   "platform-operator",
				"app.kubernetes.io/component": component.Name,
			},
		},
		Spec: tektonv1.PipelineRunSpec{
			PipelineSpec: pipelineSpec,
			Workspaces:   b.WorkspaceManager.GetWorkspaceBindings(component),
		},
	}

	if err := controllerutil.SetControllerReference(component, pipelineRun, b.Scheme); err != nil {
		b.Log.Error(err, "Failed to set owner reference on PipelineRun")
		return err
	}
	b.Log.Info("Creating a new PipelineRun", "Namespece", component.Namespace, "PipelineRun.Name", pipelineRun.Name)
	if err := b.Client.Create(ctx, pipelineRun); err != nil {
		b.Log.Error(err, "Failed to create a PipelineRun")
		return err
	}

	component.Status.BuildStatus.PipelineRunName = pipelineRunName
	component.Status.BuildStatus.Phase = "Building"
	component.Status.BuildStatus.LastBuildTime = &metav1.Time{Time: time.Now()}
	if err := b.Client.Status().Update(ctx, component); err != nil {
		b.Log.Error(err, "Failed to update PipelineRun status")
		return err
	}

	return nil
}
func (b *TektonBuilder) Cancel(ctx context.Context, component *platformv1alpha1.Component) error {
	return nil
}
func (b *TektonBuilder) CheckStatus(ctx context.Context, component *platformv1alpha1.Component) error {
	logger := b.Log.WithValues("Tekton builder", component.Name, "Namespace", component.Namespace)
	logger.Info("Checking build status")

	if component.Status.BuildStatus == nil || component.Status.BuildStatus.Phase == "" {
		return nil
	}
	pipelineRun := &tektonv1.PipelineRun{}
	err := b.Client.Get(ctx, types.NamespacedName{
		Name:      component.Status.BuildStatus.PipelineRunName,
		Namespace: component.Namespace,
	}, pipelineRun)
	if err != nil {
		if errors.IsNotFound(err) {
			component.Status.BuildStatus.Phase = "Failed"
			component.Status.BuildStatus.Message = "Pipeline run not found"
			return b.Client.Status().Update(ctx, component)
		}
		return err
	}
	logger.Info("tekton Pipeline Build", "phase", pipelineRun.Status.Status)
	if pipelineRun.Status.CompletionTime != nil {
		// Build is complete
		for _, condition := range pipelineRun.Status.Conditions {
			if condition.Type == "Succeeded" {
				if condition.Status == corev1.ConditionTrue {
					component.Status.BuildStatus.Phase = "Succeeded"
					component.Status.BuildStatus.Message = "Build completed successfully"
				} else {
					component.Status.BuildStatus.Phase = "Failed"
					component.Status.BuildStatus.Message = fmt.Sprintf("Build failed: %s", condition.Message)
				}
				component.Status.BuildStatus.LastBuildTime = &metav1.Time{Time: time.Now()}

				return b.Client.Status().Update(ctx, component)
			}
		}
	}

	return nil
}
func (b *TektonBuilder) updateComponentStatus(ctx context.Context, component *platformv1alpha1.Component) error {
	//	pipelineRun := component.Status.BuildStatus.
	//	succeded := false

	return nil
}
func (b *TektonBuilder) triggerNewBuild(component *platformv1alpha1.Component) bool {
	b.Log.Info("triggerNewBuild()", "BuildStatus", component.Status.BuildStatus)
	if component.Status.BuildStatus.LastBuildTime.IsZero() || component.Status.BuildStatus.PipelineRunName == "" {
		return true
	}
	for _, condition := range component.Status.Conditions {
		if condition.Type == "BuildSucceeded" && condition.Status == metav1.ConditionFalse {
			return true
		}
	}

	return false
}
func (b *TektonBuilder) GetStatus(ctx context.Context, component *platformv1alpha1.Component) (platformv1alpha1.BuildStatus, error) {
	return platformv1alpha1.BuildStatus{}, nil
}
