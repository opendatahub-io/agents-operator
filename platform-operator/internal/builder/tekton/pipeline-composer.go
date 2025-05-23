package tekton

import (
	"context"
	"fmt"

	//	"sort"
	//	"strings"

	platformv1alpha1 "github.com/kagenti/operator/platform/api/v1alpha1"
	tektonv1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"

	//agentv1 "github.com/yourusername/agent-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"

	//"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	//metav1 "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// StepDefinition contains the full definition of a pipeline step
type StepDefinition struct {
	// Name is the identifier for the step
	Name string

	// ConfigMap is the name of the ConfigMap containing the step definition
	ConfigMap string

	// Metadata contains metadata about the step
	//	Metadata *StepMetadata

	// TaskSpec is the embedded task definition
	TaskSpec *tektonv1.TaskSpec

	// Parameters contains step-specific parameters
	Parameters []platformv1alpha1.ParameterSpec
}

// PipelineComposer handles composition of pipelines from individual steps
type PipelineComposer struct {
	client client.Client
}

// NewPipelineComposer creates a new pipeline composer
func NewPipelineComposer(c client.Client) *PipelineComposer {
	return &PipelineComposer{
		client: c,
	}
}

// ComposePipelineSpec builds a Tekton PipelineSpec object from individual step ConfigMaps
func (pc *PipelineComposer) ComposePipelineSpec(ctx context.Context, component *platformv1alpha1.Component) (*tektonv1.PipelineSpec, error) {

	// Load and validate all steps
	steps, order, err := pc.loadSteps(ctx, component)
	if err != nil {
		return nil, fmt.Errorf("failed to load pipeline steps: %w", err)
	}

	// Validate step dependencies
	//	if err := pc.validateStepDependencies(steps); err != nil {
	//		return nil, fmt.Errorf("invalid step dependencies: %w", err)
	//	}

	// Create ordered pipeline tasks with embedded specs
	pipelineTasks, err := pc.createPipelineTasks(steps, order, pc.collectPipelineParams(component))
	if err != nil {
		return nil, fmt.Errorf("failed to create pipeline tasks: %w", err)
	}

	// Create the pipeline spec
	pipelineSpec := &tektonv1.PipelineSpec{
		Tasks: pipelineTasks,
		//	Params: pc.collectPipelineParams(component),
		Workspaces: []tektonv1.PipelineWorkspaceDeclaration{
			{
				Name:        "shared-workspace",
				Description: "Workspace for source code and build artifacts",
			},
		},
	}

	return pipelineSpec, nil
}
func (pc *PipelineComposer) getBuildSpec(component *platformv1alpha1.Component) (*platformv1alpha1.BuildSpec, error) {
	if component.Spec.Agent != nil && component.Spec.Agent.Build != nil {
		return component.Spec.Agent.Build, nil
	} else if component.Spec.Tool != nil && component.Spec.Tool.Build != nil {
		return component.Spec.Tool.Build, nil
	}
	return nil, fmt.Errorf("Invalid BuildSpec for component %s ", component.Name)
}

// loadSteps loads all step definitions from ConfigMaps
func (pc *PipelineComposer) loadSteps(ctx context.Context, component *platformv1alpha1.Component) (map[string]*StepDefinition, []string, error) {
	steps := make(map[string]*StepDefinition)
	var order []string

	buildSpec, err := pc.getBuildSpec(component)
	if err != nil {

	}
	for _, stepSpec := range buildSpec.Pipeline.Steps {
		// Skip disabled steps
		if stepSpec.Enabled != nil && !*stepSpec.Enabled {
			continue
		}
		order = append(order, stepSpec.Name)

		// Load step ConfigMap
		configMap := &corev1.ConfigMap{}
		err := pc.client.Get(ctx, types.NamespacedName{
			Name:      stepSpec.ConfigMap,
			Namespace: component.Namespace,
		}, configMap)

		if err != nil {
			if errors.IsNotFound(err) {
				return nil, nil, fmt.Errorf("step ConfigMap %s not found", stepSpec.ConfigMap)
			}
			return nil, nil, fmt.Errorf("failed to get step ConfigMap %s: %w", stepSpec.ConfigMap, err)
		}
		/*
			// Extract step metadata
			metadataYaml, ok := configMap.Data["step-metadata.yaml"]
			if !ok {
				return nil, fmt.Errorf("step-metadata.yaml not found in ConfigMap %s", stepSpec.ConfigMap)
			}

			// Parse metadata
			metadata := &StepMetadata{}
			if err := yaml.Unmarshal([]byte(metadataYaml), metadata); err != nil {
				return nil, fmt.Errorf("failed to parse step metadata: %w", err)
			}
		*/
		// Extract task spec definition
		taskSpecYaml, ok := configMap.Data["task-spec.yaml"]
		if !ok {
			return nil, nil, fmt.Errorf("task-spec.yaml not found in ConfigMap %s", stepSpec.ConfigMap)
		}

		// Parse task spec
		taskSpec := &tektonv1.TaskSpec{}
		if err := yaml.Unmarshal([]byte(taskSpecYaml), taskSpec); err != nil {
			return nil, nil, fmt.Errorf("failed to parse task spec definition: %w", err)
		}

		// Create step definition
		step := &StepDefinition{
			Name:      stepSpec.Name,
			ConfigMap: stepSpec.ConfigMap,
			//			Metadata:   metadata,
			TaskSpec:   taskSpec,
			Parameters: stepSpec.Parameters,
		}

		steps[stepSpec.Name] = step
	}

	return steps, order, nil
}

/*
// validateStepDependencies ensures that step dependencies form a valid directed acyclic graph
func (pc *PipelineComposer) validateStepDependencies(steps map[string]*StepDefinition) error {
	// Check that all runAfter references are valid
	for name, step := range steps {
		for _, dep := range step.Metadata.RunAfter {
			if _, exists := steps[dep]; !exists {
				return fmt.Errorf("step %s depends on non-existent step %s", name, dep)
			}
		}
	}

	// Check for circular dependencies
	visited := make(map[string]bool)
	temp := make(map[string]bool)

	var checkCycle func(string) error
	checkCycle = func(node string) error {
		if temp[node] {
			return fmt.Errorf("circular dependency detected involving step %s", node)
		}
		if visited[node] {
			return nil
		}
		temp[node] = true

		for _, dep := range steps[node].Metadata.RunAfter {
			if err := checkCycle(dep); err != nil {
				return err
			}
		}

		temp[node] = false
		visited[node] = true
		return nil
	}

	for name := range steps {
		if !visited[name] {
			if err := checkCycle(name); err != nil {
				return err
			}
		}
	}

	return nil
}
// createPipelineTasks creates Tekton PipelineTask objects with embedded TaskSpecs
func (pc *PipelineComposer) createPipelineTasks(steps map[string]*StepDefinition) ([]tektonv1beta1.PipelineTask, error) {
	// Perform topological sort to determine execution order
	var order []string
	visited := make(map[string]bool)

	var visit func(string)
	visit = func(node string) {
		if visited[node] {
			return
		}
		visited[node] = true

		for _, dep := range steps[node].Metadata.RunAfter {
			visit(dep)
		}

		order = append(order, node)
	}

	// Visit all nodes
	for name := range steps {
		if !visited[name] {
			visit(name)
		}
	}

	// Create PipelineTasks in topological order
	tasks := make([]tektonv1beta1.PipelineTask, 0, len(order))

	for _, name := range order {
		step := steps[name]

		// Create task with embedded spec using EmbeddedTask
		task := tektonv1beta1.PipelineTask{
			Name: step.Name,
			// Correctly use TaskSpec via EmbeddedTask
			TaskSpec: &tektonv1beta1.EmbeddedTask{
				TypeMeta: runtime.TypeMeta{
					APIVersion: "tekton.dev/v1beta1",
					Kind:       "Task",
				},
				Spec: *step.TaskSpec,
			},
			Workspaces: []tektonv1beta1.WorkspacePipelineTaskBinding{
				{
					Name:      "source",
					Workspace: "shared-workspace",
				},
			},
		}

		// Add runAfter dependencies
		if len(step.Metadata.RunAfter) > 0 {
			task.RunAfter = step.Metadata.RunAfter
		}

		// Add parameters
		if len(step.Parameters) > 0 {
			params := make([]tektonv1beta1.Param, 0, len(step.Parameters))
			for _, param := range step.Parameters {
				params = append(params, tektonv1beta1.Param{
					Name: param.Name,
					Value: tektonv1beta1.ArrayOrString{
						Type:      tektonv1beta1.ParamTypeString,
						StringVal: param.Value,
					},
				})
			}
			task.Params = params
		}

		// Add result references
		for _, result := range step.Metadata.RequiresResults {
			task.Params = append(task.Params, tektonv1beta1.Param{
				Name: result.ParamName,
				Value: tektonv1beta1.ArrayOrString{
					Type:      tektonv1beta1.ParamTypeString,
					StringVal: fmt.Sprintf("$(tasks.%s.results.%s)", result.StepName, result.ResultName),
				},
			})
		}

		tasks = append(tasks, task)
	}

	return tasks, nil
}
*/

func (pc *PipelineComposer) createPipelineTasks(steps map[string]*StepDefinition, order []string, parameters []tektonv1.ParamSpec) ([]tektonv1.PipelineTask, error) {

	// Create PipelineTasks in topological order
	tasks := make([]tektonv1.PipelineTask, 0, len(order))
	for i, stepName := range order {
		stepDefinition := steps[stepName]

		stepParameters := pc.getTaskParams(stepDefinition.Parameters)
		// Create task with embedded spec using EmbeddedTask
		task := tektonv1.PipelineTask{
			Name: stepDefinition.Name,
			//	RunAfter: []string{previousTaskName},
			TaskSpec: &tektonv1.EmbeddedTask{
				TaskSpec: tektonv1.TaskSpec{
					Params:     stepParameters,
					Steps:      stepDefinition.TaskSpec.Steps,
					Workspaces: stepDefinition.TaskSpec.Workspaces,
				},
			},
			Workspaces: []tektonv1.WorkspacePipelineTaskBinding{
				{
					Name:      "source",
					Workspace: "shared-workspace",
				},
			},
		}
		if i > 0 {
			previousTaskName := order[i-1]
			task.RunAfter = []string{previousTaskName}
		}
		tasks = append(tasks, task)
	}

	/*

		TaskSpec: &tektonv1.EmbeddedTask{
			TaskSpec: tektonv1.TaskSpec{
				Steps: []tektonv1.Step{

	*/
	return tasks, nil
}
func (pc *PipelineComposer) getTaskParams(params []platformv1alpha1.ParameterSpec) []tektonv1.ParamSpec {
	taskParams := make([]tektonv1.ParamSpec, 0, len(params))

	for _, param := range params {
		p := tektonv1.ParamSpec{
			Name: param.Name,
			Default: &tektonv1.ParamValue{
				Type:      tektonv1.ParamTypeString,
				StringVal: param.Value,
			},
		}
		taskParams = append(taskParams, p)

	}
	/*
		params := []tektonv1.ParamSpec{

			{
				Name:        "git-url",
				Type:        tektonv1.ParamTypeString,
				Description: "Git repository URL",
				Default: &tektonv1.ParamValue{
					Type:      tektonv1.ParamTypeString,
					StringVal: buildSpec.SourceRepository,
				},
			},
			{
				Name:        "git-revision",
				Type:        tektonv1.ParamTypeString,
				Description: "Git revision (branch, tag, commit)",
				Default: &tektonv1.ParamValue{
					Type:      tektonv1.ParamTypeString,
					StringVal: buildSpec.SourceRevision,
				},
			},
			{
				Name:        "component-path",
				Type:        tektonv1.ParamTypeString,
				Description: "Path to the component code within the repository",
				Default: &tektonv1.ParamValue{
					Type:      tektonv1.ParamTypeString,
					StringVal: buildSpec.SourceSubfolder,
				},
			},
			{
				Name:        "image-name",
				Type:        tektonv1.ParamTypeString,
				Description: "Name for the built image",
				Default: &tektonv1.ParamValue{
					Type:      tektonv1.ParamTypeString,
					StringVal: fmt.Sprintf("%s:%s", component.Name, "latest"),
				},
			},
		}
	*/
	return taskParams
}

// collectPipelineParams gathers all parameters from the component
func (pc *PipelineComposer) collectPipelineParams(component *platformv1alpha1.Component) []tektonv1.ParamSpec {
	buildSpec, err := pc.getBuildSpec(component)
	if err != nil {
		return nil
	}
	// Standard parameters
	params := []tektonv1.ParamSpec{
		{
			Name:        "git-url",
			Type:        tektonv1.ParamTypeString,
			Description: "Git repository URL",
			Default: &tektonv1.ParamValue{
				Type:      tektonv1.ParamTypeString,
				StringVal: buildSpec.SourceRepository,
			},
		},
		{
			Name:        "git-revision",
			Type:        tektonv1.ParamTypeString,
			Description: "Git revision (branch, tag, commit)",
			Default: &tektonv1.ParamValue{
				Type:      tektonv1.ParamTypeString,
				StringVal: buildSpec.SourceRevision,
			},
		},
		{
			Name:        "component-path",
			Type:        tektonv1.ParamTypeString,
			Description: "Path to the component code within the repository",
			Default: &tektonv1.ParamValue{
				Type:      tektonv1.ParamTypeString,
				StringVal: buildSpec.SourceSubfolder,
			},
		},
		{
			Name:        "image-name",
			Type:        tektonv1.ParamTypeString,
			Description: "Name for the built image",
			Default: &tektonv1.ParamValue{
				Type:      tektonv1.ParamTypeString,
				StringVal: fmt.Sprintf("%s:%s", component.Name, "latest"),
			},
		},
	}

	// Add custom parameters from component spec
	for _, param := range buildSpec.Pipeline.Parameters {
		params = append(params, tektonv1.ParamSpec{
			Name: param.Name,
			Type: tektonv1.ParamTypeString,
			Default: &tektonv1.ParamValue{
				Type:      tektonv1.ParamTypeString,
				StringVal: param.Value,
			},
		})
	}
	/*
		// Add boolean flags for BuildOptions
		if buildSpec.BuildOptions.EnableSigning {
			params = append(params, tektonv1.ParamSpec{
				Name: "enable-signing",
				Type: tektonv1.ParamTypeString,
				Default: &tektonv1.ArrayOrString{
					Type:      tektonv1.ParamTypeString,
					StringVal: "true",
				},
			})
		}

		if component.Spec.BuildOptions.GenerateSBOM {
			params = append(params, tektonv1.ParamSpec{
				Name: "generate-sbom",
				Type: tektonv1.ParamTypeString,
				Default: &tektonv1.ArrayOrString{
					Type:      tektonv1.ParamTypeString,
					StringVal: "true",
				},
			})
		}
	*/
	return params
}
