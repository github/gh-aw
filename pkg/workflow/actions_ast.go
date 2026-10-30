package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/goccy/go-yaml"
)

// resolveActions bridges existing jobs to the typed graph. Existing YAML remains
// untouched; only typed contributions are lowered, using the existing renderer.
// Commit changes only after every job has resolved and lowered successfully.
func (jm *JobManager) resolveActions() error {
	ir := &actions.Workflow{}
	for _, name := range jm.jobOrder {
		job, err := jobToActions(jm.jobs[name])
		if err != nil {
			return err
		}
		ir.Jobs = append(ir.Jobs, job)
	}
	resolved, err := actions.Resolve(ir)
	if err != nil {
		return err
	}
	lowered := make(map[string]*Job, len(resolved.Jobs))
	for _, irJob := range resolved.Jobs {
		name := string(irJob.ID)
		original := jm.jobs[name]
		job := *original
		job.Needs = slices.Clone(original.Needs)
		job.Outputs = maps.Clone(original.Outputs)
		for _, dep := range irJob.Needs {
			if !slices.Contains(job.Needs, string(dep)) {
				job.Needs = append(job.Needs, string(dep))
			}
		}
		for key, value := range irJob.Outputs {
			if _, legacy := original.Outputs[string(key)]; legacy {
				continue
			}
			rendered, err := renderActionsScalar(value, irJob.ID, resolved)
			if err != nil {
				return err
			}
			if job.Outputs == nil {
				job.Outputs = make(map[string]string)
			}
			job.Outputs[string(key)] = rendered
		}
		if typed := original.Actions; typed != nil {
			if err := lowerActionsContribution(&job, typed, irJob.ID, resolved); err != nil {
				return fmt.Errorf("lowering Actions workflow job %q: %w", name, err)
			}
			if err := actions.ValidateJob(irJob); err != nil {
				return fmt.Errorf("validating Actions workflow job %q: %w", name, err)
			}
			job.Actions = nil
		}
		if err := validateJobDefinition(&job); err != nil {
			return err
		}
		lowered[name] = &job
	}
	jm.jobs = lowered
	return nil
}

func jobToActions(original *Job) (*actions.Job, error) {
	name := original.Name
	job := &actions.Job{
		ID: actions.JobID(name), Outputs: make(map[actions.OutputName]actions.Expression),
		Uses: original.Uses, CacheMode: original.CacheMode,
	}
	if original.RunsOn != "" {
		job.RunsOn = original.RunsOn
	}
	for _, dep := range original.Needs {
		job.Needs = append(job.Needs, actions.JobID(dep))
	}
	for key, value := range original.Outputs {
		// Existing output bindings are opaque; their declarations still let
		// typed consumers validate and depend on them.
		job.Outputs[actions.OutputName(key)] = actions.Literal(value)
	}
	typed := original.Actions
	if typed == nil {
		return job, nil
	}
	if typed.ID != "" && typed.ID != job.ID {
		return nil, fmt.Errorf("typed Actions workflow job id %q does not match job %q", typed.ID, name)
	}
	if hasActionsExecutionSettings(typed) {
		return nil, fmt.Errorf("job %q must set name, runs-on, and native fields on the enclosing job, not its typed contribution", name)
	}
	copyEnclosingJobSettings(job, original, typed)
	job.If = typed.If
	job.Needs = append(job.Needs, typed.Needs...)
	for key, value := range typed.Outputs {
		if _, exists := job.Outputs[key]; exists {
			return nil, fmt.Errorf("job %q has both legacy and typed output %q", name, key)
		}
		job.Outputs[key] = value
	}
	if len(original.Steps) > 0 {
		var steps []map[string]any
		if err := yaml.Unmarshal([]byte(strings.Join(original.Steps, "")), &steps); err != nil {
			return nil, fmt.Errorf("indexing legacy steps in job %q: %w", name, err)
		}
		for _, fields := range steps {
			job.Steps = append(job.Steps, actions.RawStep{Fields: fields})
		}
	}
	job.Steps = append(job.Steps, typed.Steps...)
	return job, nil
}

func copyEnclosingJobSettings(job *actions.Job, original *Job, typed *actions.Job) {
	if len(original.Env)+len(typed.Env) > 0 {
		job.Env = make(map[string]actions.Expression, len(original.Env)+len(typed.Env))
	}
	for key, value := range original.Env {
		job.Env[key] = actions.Literal(value)
	}
	maps.Copy(job.Env, typed.Env)
	if len(original.With) > 0 {
		job.With = make(map[string]actions.Expression, len(original.With))
		for key, value := range original.With {
			job.With[key] = actions.Literal(fmt.Sprint(value))
		}
	}
	if len(original.Secrets) > 0 {
		job.Secrets = make(map[string]actions.Expression, len(original.Secrets))
		for key, value := range original.Secrets {
			job.Secrets[key] = actions.Literal(value)
		}
	}
	job.SecretsInherit = original.SecretsInherit
	if original.Environment != "" {
		job.Environment = original.Environment
	}
	if original.Container != "" {
		job.Container = original.Container
	}
	if original.Services != "" {
		job.Services = map[string]any{"legacy": original.Services}
	}
	job.TimeoutMinutes = original.TimeoutMinutes
	if original.TimeoutMinutesExpression != "" {
		job.TimeoutMinutesExpression = actions.Literal(original.TimeoutMinutesExpression)
	}
	if original.ContinueOnError != nil {
		job.ContinueOnError = actions.Boolean(*original.ContinueOnError)
	}
}

func hasActionsExecutionSettings(job *actions.Job) bool {
	return job.Name != "" || job.RunsOn != nil || job.Permissions != nil || job.Defaults != nil ||
		job.CacheMode != "" ||
		job.Concurrency != nil || job.Environment != nil || job.Strategy != nil ||
		job.Container != nil || job.Services != nil || job.TimeoutMinutes != 0 || job.TimeoutMinutesExpression != nil || job.ContinueOnError != nil ||
		job.Uses != "" || len(job.With) > 0 || len(job.Secrets) > 0 || job.SecretsInherit || len(job.CallOutputs) > 0
}

func lowerActionsContribution(job *Job, typed *actions.Job, id actions.JobID, resolved *actions.Workflow) error {
	if job.Uses != "" && (len(typed.Steps) > 0 || len(typed.Env) > 0 || len(typed.Outputs) > 0) {
		return errors.New("reusable workflow job cannot declare typed steps, env, or outputs")
	}
	if typed.If != nil {
		if job.If != "" {
			return errors.New("cannot set both a legacy and typed job condition")
		}
		rendered, err := actions.RenderExpression(typed.If, id, resolved, false)
		if err != nil {
			return err
		}
		job.If = "${{ " + rendered + " }}"
		job.actionsCondition = true
	}
	job.Env = maps.Clone(job.Env)
	for key, value := range typed.Env {
		if _, exists := job.Env[key]; exists {
			return fmt.Errorf("cannot set both a legacy and typed environment value %q", key)
		}
		rendered, err := renderActionsScalar(value, id, resolved)
		if err != nil {
			return err
		}
		if job.Env == nil {
			job.Env = make(map[string]string)
		}
		job.Env[key] = rendered
	}
	job.Steps = slices.Clone(job.Steps)
	for _, step := range typed.Steps {
		fields, err := actions.LowerStep(step, id, resolved)
		if err != nil {
			return err
		}
		rendered, err := ConvertStepToYAML(fields)
		if err != nil {
			return err
		}
		job.Steps = append(job.Steps, rendered)
	}
	return nil
}

func renderActionsScalar(value actions.Expression, id actions.JobID, resolved *actions.Workflow) (string, error) {
	rendered, err := actions.RenderExpression(value, id, resolved, true)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(rendered)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
