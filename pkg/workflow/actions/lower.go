package actions

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"
)

// Lower resolves the graph and creates a deterministic YAML value tree.
func Lower(input *Workflow) (yaml.MapSlice, error) {
	resolved, err := Resolve(input)
	if err != nil {
		return nil, err
	}
	root := yaml.MapSlice{{Key: "name", Value: resolved.Name}}
	for _, item := range []yaml.MapItem{
		{Key: "run-name", Value: optionalString(resolved.RunName)},
		{Key: "on", Value: resolved.On},
		{Key: "permissions", Value: resolved.Permissions},
		{Key: "defaults", Value: optionalMap(resolved.Defaults)},
		{Key: "concurrency", Value: resolved.Concurrency},
		{Key: "cache-mode", Value: optionalString(resolved.CacheMode)},
	} {
		if item.Value != nil {
			root = append(root, item)
		}
	}
	if len(resolved.Env) > 0 {
		env, err := lowerValues(resolved.Env, "", resolved)
		if err != nil {
			return nil, err
		}
		root = append(root, yaml.MapItem{Key: "env", Value: env})
	}
	jobs := slices.Clone(resolved.Jobs)
	slices.SortFunc(jobs, func(a, b *Job) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	jobMap := make(yaml.MapSlice, 0, len(jobs))
	for _, job := range jobs {
		fields, err := lowerJob(job, resolved)
		if err != nil {
			return nil, fmt.Errorf("lowering workflow job %q: %w", job.ID, err)
		}
		jobMap = append(jobMap, yaml.MapItem{Key: string(job.ID), Value: fields})
	}
	return append(root, yaml.MapItem{Key: "jobs", Value: jobMap}), nil
}

func lowerJob(job *Job, resolved *Workflow) (map[string]any, error) {
	fields, err := nativeJobFields(job)
	if err != nil {
		return nil, err
	}
	if err := lowerJobExpressions(fields, job, resolved); err != nil {
		return nil, err
	}
	if job.Name != "" {
		fields["name"] = job.Name
	}
	if job.RunsOn != nil {
		fields["runs-on"] = job.RunsOn
	}
	if len(job.Needs) > 0 {
		needs := make([]string, 0, len(job.Needs))
		for _, id := range job.Needs {
			needs = append(needs, string(id))
		}
		fields["needs"] = needs
	}
	if job.If != nil {
		value, err := RenderExpression(job.If, job.ID, resolved, false)
		if err != nil {
			return nil, err
		}
		fields["if"] = "${{ " + value + " }}"
	}
	if len(job.Env) > 0 {
		env, err := lowerValues(job.Env, job.ID, resolved)
		if err != nil {
			return nil, err
		}
		fields["env"] = env
	}
	if len(job.Outputs) > 0 {
		outputs := make(map[string]any, len(job.Outputs))
		for _, name := range sortedOutputNames(job.Outputs) {
			value, err := RenderExpression(job.Outputs[name], job.ID, resolved, true)
			if err != nil {
				return nil, err
			}
			outputs[string(name)] = value
		}
		fields["outputs"] = outputs
	}
	if len(job.Steps) > 0 {
		steps := make([]map[string]any, 0, len(job.Steps))
		for _, step := range job.Steps {
			value, err := LowerStep(step, job.ID, resolved)
			if err != nil {
				return nil, err
			}
			steps = append(steps, value)
		}
		fields["steps"] = steps
	}
	return fields, nil
}

func nativeJobFields(job *Job) (map[string]any, error) {
	if err := validateJob(job); err != nil {
		return nil, err
	}
	fields := make(map[string]any)
	for key, value := range map[string]any{
		"permissions": job.Permissions, "defaults": optionalMap(job.Defaults),
		"cache-mode":  optionalString(job.CacheMode),
		"concurrency": job.Concurrency, "environment": job.Environment,
		"strategy": optionalMap(job.Strategy), "container": job.Container,
		"services": optionalMap(job.Services), "uses": optionalString(job.Uses),
	} {
		if value != nil {
			fields[key] = value
		}
	}
	if job.TimeoutMinutes > 0 || job.TimeoutMinutesSet {
		fields["timeout-minutes"] = job.TimeoutMinutes
	}
	return fields, nil
}

// LowerStep is also used by the compiler, retaining its existing
// field ordering, action-pin comments, and runner-specific rendering.
func LowerStep(step Step, consumer JobID, resolved *Workflow) (map[string]any, error) {
	opts, err := options(step)
	if err != nil {
		return nil, err
	}
	if opts.TimeoutMinutes < 0 {
		return nil, errors.New("step timeout-minutes cannot be negative")
	}
	if opts.TimeoutMinutes != 0 && opts.TimeoutMinutesExpression != nil {
		return nil, errors.New("step cannot combine a literal and expression timeout-minutes")
	}
	fields := make(map[string]any)
	switch value := step.(type) {
	case RunStep:
		if value.Run == "" {
			return nil, errors.New("run step requires a nonempty script")
		}
		if strings.Contains(value.Run, "${{") {
			return nil, errors.New("typed run steps must pass Actions expressions through env instead of interpolating them into scripts")
		}
		fields["run"] = value.Run
		if value.Shell != "" {
			fields["shell"] = value.Shell
		}
		if value.WorkingDirectory != "" {
			fields["working-directory"] = value.WorkingDirectory
		}
	case ActionStep:
		if value.Uses == "" {
			return nil, errors.New("action step requires a nonempty uses reference")
		}
		fields["uses"] = value.Uses
		if len(value.With) > 0 {
			with, err := lowerInputs(value.With, consumer, resolved)
			if err != nil {
				return nil, err
			}
			fields["with"] = with
		}
	case RawStep:
		if err := validateRawStep(value.Fields); err != nil {
			return nil, err
		}
		return maps.Clone(value.Fields), nil
	}
	if err := lowerStepOptions(fields, opts, consumer, resolved); err != nil {
		return nil, err
	}
	return fields, nil
}

func lowerStepOptions(fields map[string]any, opts StepOptions, consumer JobID, resolved *Workflow) error {
	if opts.TimeoutMinutesExpression != nil {
		value, err := RenderExpression(opts.TimeoutMinutesExpression, consumer, resolved, true)
		if err != nil {
			return err
		}
		fields["timeout-minutes"] = value
	}
	if opts.ID != "" {
		fields["id"] = string(opts.ID)
	}
	if opts.Name != "" {
		fields["name"] = opts.Name
	}
	if opts.If != nil {
		value, err := RenderExpression(opts.If, consumer, resolved, false)
		if err != nil {
			return err
		}
		fields["if"] = "${{ " + value + " }}"
	}
	if len(opts.Env) > 0 {
		env, err := lowerValues(opts.Env, consumer, resolved)
		if err != nil {
			return err
		}
		fields["env"] = env
	}
	if opts.ContinueOnError != nil {
		value, err := RenderExpression(opts.ContinueOnError, consumer, resolved, true)
		if err != nil {
			return err
		}
		fields["continue-on-error"] = value
	}

	if opts.TimeoutMinutes > 0 {
		fields["timeout-minutes"] = opts.TimeoutMinutes
	}
	return nil
}

func optionalString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func optionalMap(value map[string]any) any {
	if value == nil {
		return nil
	}
	return value
}

func lowerValues(values map[string]Expression, consumer JobID, resolved *Workflow) (map[string]any, error) {
	result := make(map[string]any, len(values))
	for _, key := range sortedKeys(values) {
		value, err := RenderExpression(values[key], consumer, resolved, true)
		if err != nil {
			return nil, fmt.Errorf("workflow value %q: %w", key, err)
		}
		result[key] = value
	}
	return result, nil
}

func Emit(input *Workflow) (string, error) {
	value, err := Lower(input)
	if err != nil {
		return "", err
	}
	if input.source != nil {
		content, err := snapshotYAMLValue(value)
		if err != nil {
			return "", err
		}
		return input.source.emit(content)
	}
	content, err := yaml.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("serializing workflow YAML: %w", err)
	}
	return string(content), nil
}
