package taint

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
)

// Analyze resolves a copy of the AST and visits jobs in dependency order.
// Environment scopes and step execution order are preserved. Shell-local
// assignments are conservative unions across control-flow paths.
func Analyze(input *actions.Workflow, rule Rule) (Result, error) {
	return analyze(input, rule, nil)
}

func analyze(input *actions.Workflow, rule Rule, document *aw.Document) (Result, error) {
	if rule.ID == "" || rule.IsSource == nil || rule.IsSink == nil {
		return Result{}, errors.New("taint rule requires an id, sources, and sinks")
	}
	resolved, err := actions.Resolve(input)
	if err != nil {
		return Result{}, err
	}
	a := &analyzer{rule: rule, workflowContext: true}
	bindings := make(scope)
	jobs := make(map[actions.JobID]*actions.Job)
	for _, job := range resolved.Jobs {
		jobs[job.ID] = job
	}
	visited := make(map[actions.JobID]bool)
	var visit func(*actions.Job) error
	visit = func(job *actions.Job) error {
		if visited[job.ID] {
			return nil
		}
		for _, dep := range job.Needs {
			if err := visit(jobs[dep]); err != nil {
				return err
			}
		}
		a.safeOutputJob = slices.Contains(rule.SafeOutputJobs, string(job.ID))
		if err := a.job(job, resolved, bindings); err != nil {
			return err
		}
		visited[job.ID] = true
		return nil
	}
	for _, job := range resolved.Jobs {
		if err := visit(job); err != nil {
			return Result{}, err
		}
	}
	for path, text := range map[string]string{"name": resolved.Name, "run-name": resolved.RunName} {
		sources, err := a.text(text, path, nil)
		if err != nil {
			return Result{}, err
		}
		a.emit(Site{Kind: Display, Path: path}, sources)
	}
	if document != nil {
		sources, err := a.text(document.Markdown, "markdown", a.promptScope)
		if err != nil {
			return Result{}, err
		}
		a.emit(Site{Kind: Prompt, Path: "markdown"}, sources)
	}
	slices.SortFunc(a.result.Findings, func(x, y Finding) int {
		if n := strings.Compare(x.Site.Path, y.Site.Path); n != 0 {
			return n
		}
		if x.Site.Line != y.Site.Line {
			return x.Site.Line - y.Site.Line
		}
		return strings.Compare(string(x.Site.Kind), string(y.Site.Kind))
	})
	return a.result, nil
}

func safeOutputIntegrationJobs(rule Rule, document *aw.Document) ([]string, error) {
	jobs := slices.Clone(rule.SafeOutputJobs)
	if document == nil {
		return jobs, nil
	}
	blocks, err := aw.ExecutionBlocks(context.Background(), document)
	if err != nil {
		return nil, err
	}
	for _, block := range blocks {
		for _, prefix := range []string{"safe-outputs.jobs.", "safe-jobs."} {
			if name, ok := strings.CutPrefix(block.Path, prefix); ok {
				jobs = append(jobs, stringutil.NormalizeSafeOutputIdentifier(name))
			}
		}
	}
	return jobs, nil
}

// AnalyzeDocument checks the AW prompt and script entry points alongside its
// compiled Actions graph. Fast mode avoids generated JavaScript traversal.
// Callers compile through workflow.Compiler.CompileDocument so imports and
// compiler-generated steps use exactly the same execution model as gh aw.
func AnalyzeDocument(document *aw.Document, compiled *actions.Workflow, rule Rule) (Result, error) {
	if document == nil {
		return Result{}, errors.New("AW document cannot be nil")
	}
	jobs, err := safeOutputIntegrationJobs(rule, document)
	if err != nil {
		return Result{}, err
	}
	rule.SafeOutputJobs = jobs
	ctx := context.Background()
	if rule.JavaScript != nil && rule.JavaScript.Context != nil {
		ctx = rule.JavaScript.Context
	}
	compiledRule := rule
	fast := rule.JavaScript != nil && rule.JavaScript.Mode == JavaScriptFast
	if fast {
		compiledRule.JavaScript = nil
	}
	result, err := analyze(compiled, compiledRule, document)
	var scripts Result
	var scriptErr error
	if fast {
		result, scriptErr = analyzeFastJavaScript(ctx, document, compiled, rule, result)
		result.Gaps = append(result.Gaps, CoverageGap{
			Path: "workflow", Reason: "fast JavaScript analysis checks authored scripts and reviewed API summaries; generated JavaScript and cross-job JavaScript flows require deep mode",
		})
	} else {
		scripts, scriptErr = AnalyzeAuthoredScripts(ctx, document, rule)
	}
	result.Findings = append(result.Findings, scripts.Findings...)
	result.Gaps = append(result.Gaps, scripts.Gaps...)
	return result, errors.Join(err, scriptErr)
}

func (a *analyzer) expressions(values map[string]actions.Expression, path string, job actions.JobID, workflow *actions.Workflow, parent scope) (scope, error) {
	result := maps.Clone(parent)
	if result == nil {
		result = make(scope)
	}
	for _, name := range slices.Sorted(maps.Keys(values)) {
		text, err := actions.RenderExpression(values[name], job, workflow, true)
		if err != nil {
			return nil, err
		}
		sources, err := a.text(text, path+"."+name, parent)
		if err != nil {
			return nil, err
		}
		if name == "GH_AW_OTLP_ATTRIBUTES" || name == "OTEL_RESOURCE_ATTRIBUTES" {
			a.emit(Site{Kind: OTLPAttribute, Path: path + "." + name, Input: name}, sources)
		}
		result["env."+strings.ToLower(name)] = sources
		result["shell."+name] = sources
	}
	return result, nil
}

func (a *analyzer) job(job *actions.Job, workflow *actions.Workflow, outputs scope) error {
	path := jobPath(job.ID)
	bindings := make(scope)
	for _, dep := range job.Needs {
		prefix := "needs." + strings.ToLower(string(dep)) + ".outputs."
		for key, sources := range outputs {
			if strings.HasPrefix(key, prefix) {
				bindings[key] = sources
			}
		}
	}
	env, err := a.expressions(workflow.Env, "env", job.ID, workflow, bindings)
	if err != nil {
		return err
	}
	env, err = a.expressions(job.Env, path+".env", job.ID, workflow, env)
	if err != nil {
		return err
	}
	if err := a.inputs(job.With, Site{Kind: WorkflowInput, Path: path + ".with", Uses: job.Uses}, job.ID, workflow, env); err != nil {
		return err
	}
	persisted := make(scope)
	files := make(scope)
	if err := a.jobSteps(job, workflow, path, env, persisted, files); err != nil {
		return err
	}
	outputBindings := maps.Clone(env)
	maps.Copy(outputBindings, persisted)
	if environment, ok := job.Environment.(map[string]any); ok {
		if rawURL, exists := environment["url"]; exists {
			sources, err := a.text(fmt.Sprint(rawURL), path+".environment.url", outputBindings)
			if err != nil {
				return err
			}
			a.emit(Site{Kind: Display, Path: path + ".environment.url"}, sources)
		}
	}
	if err := a.jobOutputs(job, workflow, path, outputs, outputBindings); err != nil {
		return err
	}
	sources, err := a.text(job.Name, path+".name", env)
	if err != nil {
		return err
	}
	a.emit(Site{Kind: Display, Path: path + ".name"}, sources)
	if job.ID == actions.JobID(constants.ActivationJobName) {
		a.promptScope = maps.Clone(env)
		maps.Copy(a.promptScope, persisted)
	}
	return nil
}

func (a *analyzer) jobSteps(job *actions.Job, workflow *actions.Workflow, path string, env, persisted, files scope) error {
	workflowRunDefaults := runDefaults(workflow.Defaults)
	jobRunDefaults := runDefaults(job.Defaults)
	for i, step := range job.Steps {
		fields, err := actions.LowerStep(step, job.ID, workflow)
		if err != nil {
			return err
		}
		stepPath := fmt.Sprintf("%s.steps[%d]", path, i)
		if _, exists := fields["shell"]; !exists {
			shell := inheritedRunSetting("shell", jobRunDefaults, workflowRunDefaults, runnerShell(job.RunsOn))
			if shell == unknownRunSetting {
				shell = "unknown"
			}
			fields["shell"] = shell
		}
		if _, exists := fields["working-directory"]; !exists {
			workingDirectory := inheritedRunSetting("working-directory", jobRunDefaults, workflowRunDefaults, "")
			if workingDirectory == unknownRunSetting {
				a.gap(stepPath+".run", "working directory could not be modeled")
			} else if workingDirectory != "" {
				fields["working-directory"] = workingDirectory
			}
		}
		if err := a.step(fields, stepPath, env, persisted, files); err != nil {
			return err
		}
	}
	return nil
}

func (a *analyzer) jobOutputs(job *actions.Job, workflow *actions.Workflow, path string, outputs, outputBindings scope) error {
	for _, name := range slices.Sorted(maps.Keys(job.Outputs)) {
		text, err := actions.RenderExpression(job.Outputs[name], job.ID, workflow, true)
		if err != nil {
			return err
		}
		field := path + ".outputs." + string(name)
		sources, err := a.text(text, field, outputBindings)
		if err != nil {
			return err
		}
		outputs["needs."+strings.ToLower(string(job.ID))+".outputs."+strings.ToLower(string(name))] = sources
		a.emit(Site{Kind: Output, Path: field}, sources)
	}
	return nil
}

func runDefaults(defaults map[string]any) map[string]any {
	raw, exists := defaults["run"]
	if exists {
		run, ok := raw.(map[string]any)
		if !ok {
			return map[string]any{"shell": unknownRunSetting, "working-directory": unknownRunSetting}
		}
		return run
	}
	return nil
}

const unknownRunSetting = "\x00unknown"

func inheritedRunSetting(key string, jobDefaults, workflowDefaults map[string]any, fallback string) string {
	for _, defaults := range []map[string]any{jobDefaults, workflowDefaults} {
		if value, exists := defaults[key]; exists {
			text, ok := value.(string)
			if !ok {
				return unknownRunSetting
			}
			if text != "" {
				return text
			}
		}
	}
	return fallback
}

func runnerShell(runsOn any) string {
	var labels []string
	switch value := runsOn.(type) {
	case string:
		labels = append(labels, value)
	case []any:
		for _, label := range value {
			labels = append(labels, fmt.Sprint(label))
		}
	default:
		return "unknown"
	}
	windows, unix := false, false
	for _, label := range labels {
		label = strings.ToLower(label)
		if strings.Contains(label, "${{") {
			return "unknown"
		}
		if strings.Contains(label, "windows") {
			windows = true
		}
		if strings.Contains(label, "ubuntu") || strings.Contains(label, "linux") || strings.Contains(label, "macos") {
			unix = true
		}
	}
	if windows {
		return "pwsh"
	}
	if unix {
		return "bash"
	}
	return "unknown"
}

func (a *analyzer) inputs(inputs map[string]actions.Expression, site Site, job actions.JobID, workflow *actions.Workflow, bindings scope) error {
	for _, key := range slices.Sorted(maps.Keys(inputs)) {
		text, err := actions.RenderExpression(inputs[key], job, workflow, true)
		if err != nil {
			return err
		}
		current := site
		current.Path = site.Path + "." + key
		current.Input = key
		sources, err := a.text(text, current.Path, bindings)
		if err != nil {
			return err
		}
		a.emit(current, sources)
	}
	return nil
}

func (a *analyzer) step(fields map[string]any, path string, env, persisted, files scope) error {
	// Runner command files belong to one step, unlike ordinary workspace files.
	delete(files, "$GITHUB_OUTPUT")
	parent := maps.Clone(env)
	maps.Copy(parent, persisted)
	bindings := maps.Clone(parent)
	id, err := optionalField[string](fields, "id", path)
	if err != nil {
		return err
	}
	previous := a.stepOutput
	a.stepOutput = &stepOutputTransport{id: id, bindings: bindings, modeled: a.workflowContext, recordStateKnown: true}
	defer func() { a.stepOutput = previous }()
	stepEnv, err := optionalField[map[string]any](fields, "env", path)
	if err != nil {
		return err
	}
	for _, key := range slices.Sorted(maps.Keys(stepEnv)) {
		sources, err := a.text(fmt.Sprint(stepEnv[key]), path+".env."+key, parent)
		if err != nil {
			return err
		}
		if key == "GH_AW_OTLP_ATTRIBUTES" || key == "OTEL_RESOURCE_ATTRIBUTES" {
			a.emit(Site{Kind: OTLPAttribute, Path: path + ".env." + key, Input: key}, sources)
		}
		bindings["env."+strings.ToLower(key)] = sources
		bindings["shell."+key] = sources
	}
	name, err := optionalField[string](fields, "name", path)
	if err != nil {
		return err
	}
	sources, err := a.text(name, path+".name", bindings)
	if err != nil {
		return err
	}
	a.emit(Site{Kind: Display, Path: path + ".name"}, sources)
	if run, ok := fields["run"].(string); ok {
		if err := a.script(run, fields, path+".run", bindings, persisted, files); err != nil {
			return err
		}
	} else if err := a.action(fields, path, bindings, files); err != nil {
		return err
	}
	for key, sources := range bindings {
		if strings.HasPrefix(key, "steps.") {
			env[key] = sources
		}
	}
	return nil
}

func (a *analyzer) action(fields map[string]any, path string, bindings, files scope) error {
	uses, err := optionalField[string](fields, "uses", path)
	if err != nil {
		return err
	}
	inputs, err := optionalField[map[string]any](fields, "with", path)
	if err != nil {
		return err
	}
	if actionName(uses) == "actions/github-script" && a.rule.JavaScript != nil {
		if script, ok := inputs["script"].(string); ok {
			if err := a.javascriptInline(script, path+".with.script", bindings, files, false); err != nil {
				return err
			}
		}
	}
	for _, key := range slices.Sorted(maps.Keys(inputs)) {
		sources, err := a.text(fmt.Sprint(inputs[key]), path+".with."+key, bindings)
		if err != nil {
			return err
		}
		a.emit(Site{Kind: ActionInput, Path: path + ".with." + key, Uses: uses, Input: key}, sources)
		if actionName(uses) == "actions/upload-artifact" && key == "path" {
			var contents value
			patterns := strings.Split(fmt.Sprint(inputs[key]), "\n")
			for file, sources := range files {
				if artifactIncludes(patterns, file) {
					contents = union(contents, sources)
				}
			}
			a.emit(Site{Kind: Artifact, Path: path + ".with.path", Uses: uses, Input: key}, contents)
		}
	}
	if a.rule.ActionSources != nil {
		id, err := optionalField[string](fields, "id", path)
		if err != nil {
			return err
		}
		if id != "" {
			for _, name := range a.rule.ActionSources(uses) {
				ref := "steps." + strings.ToLower(id) + ".outputs." + strings.ToLower(name)
				bindings[ref] = value{{Reference: ref, Path: path}}
			}
		}
	}
	return nil
}

func optionalField[T any](fields map[string]any, key, path string) (T, error) {
	var zero T
	raw, exists := fields[key]
	if !exists {
		return zero, nil
	}
	typed, ok := raw.(T)
	if !ok {
		return zero, fmt.Errorf("invalid field type at %s.%s", path, key)
	}
	return typed, nil
}
