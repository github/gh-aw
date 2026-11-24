package workqueue

import (
	"context"
	"regexp"
	"slices"

	"github.com/github/gh-aw/pkg/parser"
	"go.yaml.in/yaml/v3"
)

var dispatcherNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

type DispatcherRoute struct {
	Repository string `json:"repository"`
	Workflow   string `json:"workflow"`
	Ref        string `json:"ref"`
	Revision   string `json:"revision"`
}

// DispatcherFromConfig resolves a dispatcher, never a worker assignment, at the
// repository's verified default revision. It does not grant or launch Claims.
func (b Branch) DispatcherFromConfig(ctx context.Context, name string) (DispatcherRoute, error) {
	if !dispatcherNamePattern.MatchString(name) {
		return DispatcherRoute{}, queueError("dispatcher_invalid", "expected a dispatcher basename, for example eslint-factory-dispatcher")
	}
	b, err := b.withClient()
	if err != nil {
		return DispatcherRoute{}, err
	}
	repository, ref, revision, err := b.verifiedDefaultRoute(ctx)
	if err != nil {
		return DispatcherRoute{}, err
	}
	b.Remote = repository
	source, err := b.configFile(ctx, workerWorkflowDirectory+name+".md", revision)
	if err != nil {
		return DispatcherRoute{}, err
	}
	workers, err := dispatcherSourceWorkflows(source)
	if err != nil {
		return DispatcherRoute{}, err
	}
	workflow := name + ".lock.yml"
	compiled, err := b.configFile(ctx, workerWorkflowDirectory+workflow, revision)
	if err != nil {
		return DispatcherRoute{}, err
	}
	if err := validateDispatcherLock(compiled, workers); err != nil {
		return DispatcherRoute{}, err
	}
	return DispatcherRoute{Repository: repository, Workflow: workflow, Ref: ref, Revision: revision}, nil
}

func dispatcherSourceWorkflows(source []byte) ([]string, error) {
	frontmatter, err := parser.ExtractFrontmatterFromContent(string(source))
	if err != nil {
		return nil, err
	}
	if frontmatter == nil {
		return nil, queueError("dispatcher_invalid", "dispatcher source requires agentic workflow frontmatter")
	}
	tools, ok := frontmatter.Frontmatter["tools"].(map[string]any)
	if !ok {
		return nil, queueError("dispatcher_invalid", "dispatcher must declare work-queue tools")
	}
	queue := tools["work-queue"]
	enabled := queue == true
	if options, ok := queue.(map[string]any); ok {
		enabled = options["worker"] != true && options["require-assignment"] != true
	}
	if !enabled {
		return nil, queueError("dispatcher_invalid", "expected an unassigned work-queue dispatcher; workers require authenticated Claims")
	}
	outputs, ok := frontmatter.Frontmatter["safe-outputs"].(map[string]any)
	if !ok {
		return nil, queueError("dispatcher_invalid", "dispatcher must declare safe-outputs")
	}
	dispatch, ok := outputs["dispatch-workflow"].(map[string]any)
	if !ok {
		return nil, queueError("dispatcher_invalid", "dispatcher must declare safe-outputs.dispatch-workflow")
	}
	return dispatcherWorkflowAllowlist(dispatch["workflows"])
}

func dispatcherWorkflowAllowlist(value any) ([]string, error) {
	workers, ok := value.([]any)
	if !ok || len(workers) == 0 {
		return nil, queueError("dispatcher_invalid", "dispatcher worker allowlist must be a nonempty array of workflow basenames")
	}
	names := make([]string, 0, len(workers))
	for _, worker := range workers {
		name, ok := worker.(string)
		if !ok || !dispatcherNamePattern.MatchString(name) {
			return nil, queueError("dispatcher_invalid", "dispatcher worker allowlist must contain workflow basenames")
		}
		names = append(names, name)
	}
	slices.Sort(names)
	return slices.Compact(names), nil
}

func validateDispatcherLock(compiled []byte, workers []string) error {
	var workflow struct {
		On   map[string]any `yaml:"on"`
		Env  map[string]any `yaml:"env"`
		Jobs map[string]struct {
			Env   map[string]any `yaml:"env"`
			Steps []struct {
				ID  string         `yaml:"id"`
				Env map[string]any `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(compiled, &workflow); err != nil {
		return err
	}
	trigger, ok := workflow.On["workflow_dispatch"]
	if !ok {
		return queueError("dispatcher_invalid", "compiled dispatcher must accept workflow_dispatch")
	}
	if dispatch, ok := trigger.(map[string]any); ok {
		if inputs, ok := dispatch["inputs"].(map[string]any); ok {
			if _, reserved := inputs["work_queue_assignment"]; reserved {
				return queueError("dispatcher_invalid", "cannot directly dispatch a Claim-assigned worker")
			}
		}
	}
	const controlConfig = "GH_AW_WORK_QUEUE_CONTROL_CONFIG"
	if _, exists := workflow.Env[controlConfig]; exists {
		return queueError("dispatcher_invalid", "compiled dispatcher control configuration must be scoped to its trusted control step")
	}
	hasRole, controls := false, 0
	for _, job := range workflow.Jobs {
		if job.Env["GH_AW_WORK_QUEUE_ROLE"] == "dispatcher" {
			hasRole = true
		}
		if _, exists := job.Env[controlConfig]; exists {
			return queueError("dispatcher_invalid", "compiled dispatcher control configuration must be scoped to its trusted control step")
		}
		for _, step := range job.Steps {
			value, exists := step.Env[controlConfig]
			if step.ID != "work_queue_controls" && !exists {
				continue
			}
			controls++
			if step.ID != "work_queue_controls" || !exists {
				return queueError("dispatcher_invalid", "compiled dispatcher requires a protected work_queue_controls configuration")
			}
			if err := validateDispatcherControlConfig(value, workers); err != nil {
				return err
			}
		}
	}
	if !hasRole {
		return queueError("dispatcher_invalid", "compiled workflow must contain the trusted work-queue dispatcher role")
	}
	if controls != 1 {
		return queueError("dispatcher_invalid", "compiled dispatcher must contain exactly one trusted work_queue_controls configuration")
	}
	return nil
}

func validateDispatcherControlConfig(value any, workers []string) error {
	encoded, ok := value.(string)
	if !ok {
		return queueError("dispatcher_invalid", "compiled dispatcher control configuration must be a JSON object")
	}
	decoded, err := decodeStrict([]byte(encoded))
	if err != nil {
		return queueError("dispatcher_invalid", "invalid compiled dispatcher control configuration: %v", err)
	}
	config, ok := decoded.(map[string]any)
	if !ok {
		return queueError("dispatcher_invalid", "compiled dispatcher control configuration must be a JSON object")
	}
	for _, field := range []string{"work_queue_workflows", "aw_context_workflows"} {
		names, err := dispatcherWorkflowAllowlist(config[field])
		if err != nil {
			return queueError("dispatcher_invalid", "compiled dispatcher %s: %v", field, err)
		}
		if !slices.Equal(names, workers) {
			return queueError("dispatcher_invalid", "compiled dispatcher %s must match source dispatch-workflow.workflows; recompile the dispatcher", field)
		}
	}
	return nil
}
