package taint

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/aw"
)

// AnalyzeAuthoredDocument follows explicit AW execution nodes and the prompt.
// It does not compile, expand imports, or assume generated execution behavior.
func AnalyzeAuthoredDocument(ctx context.Context, document *aw.Document, rule Rule) (Result, error) {
	return analyzeAuthoredWithContext(ctx, document, rule, false)
}

func analyzeAuthoredWithContext(ctx context.Context, document *aw.Document, rule Rule, workflowContext bool) (Result, error) {
	result, err := analyzeAuthoredDocumentWithContext(ctx, document, rule, workflowContext)
	if document == nil {
		return result, err
	}
	scripts, scriptErr := AnalyzeAuthoredScripts(ctx, document, rule)
	result.Findings = append(result.Findings, scripts.Findings...)
	result.Gaps = append(result.Gaps, scripts.Gaps...)
	return result, errors.Join(err, scriptErr)
}

func analyzeAuthoredDocumentWithContext(ctx context.Context, document *aw.Document, rule Rule, workflowContext bool) (Result, error) {
	if rule.ID == "" || rule.IsSource == nil || rule.IsSink == nil {
		return Result{}, errors.New("taint rule requires an id, sources, and sinks")
	}

	blocks, err := aw.ExecutionBlocks(ctx, document)
	if document == nil {
		return Result{}, err
	}
	a := &analyzer{rule: rule, workflowContext: workflowContext}
	a.gap("workflow", "only authored AW execution nodes are available; imports, generated steps, cross-job flows and compiler defaults require the compiled Actions AST")
	failures := []error{err}
	parent, err := a.authoredEnvironment(ctx, document.Frontmatter, "env", nil)
	if err != nil {
		return a.result, err
	}
	for _, block := range blocks {
		a.safeOutputJob = strings.HasPrefix(block.Path, "safe-outputs.jobs.") || strings.HasPrefix(block.Path, "safe-jobs.")
		if ctx.Err() != nil {
			return a.result, errors.Join(append(failures, ctx.Err())...)
		}
		bindings := maps.Clone(parent)
		if block.Path != "" {
			bindings, err = a.authoredEnvironment(ctx, block.Fields, block.FieldPath("env"), parent)
			if err != nil {
				failures = append(failures, err)
				continue
			}
		}
		persisted, files := make(scope), make(scope)
		failures = append(failures, a.authoredDisplay(block, bindings))
		failures = append(failures, a.authoredWorkflowInputs(ctx, block, bindings))
		promptDone := block.Path != ""
		for _, step := range block.Steps {
			if ctx.Err() != nil {
				return a.result, errors.Join(append(failures, ctx.Err())...)
			}
			if step.Post && !promptDone {
				failures = append(failures, a.authoredPrompt(document, bindings, persisted))
				promptDone = true
			}
			fields, err := authoredRunFields(step, block, document)
			if err != nil {
				failures = append(failures, err)
				continue
			}
			failures = append(failures, a.step(fields, step.Path, bindings, persisted, files))
		}
		if !promptDone {
			failures = append(failures, a.authoredPrompt(document, bindings, persisted))
		}
		if block.Path != "" {
			outputBindings := maps.Clone(bindings)
			maps.Copy(outputBindings, persisted)
			failures = append(failures, a.authoredOutputs(ctx, block, outputBindings))
		}
	}
	return a.result, errors.Join(failures...)
}

func analyzeAuthoredJavaScript(ctx context.Context, document *aw.Document, rule Rule, workflowContext bool) (Result, error) {
	result, err := analyzeAuthoredWithContext(ctx, document, rule, workflowContext)
	// A JS-disabled baseline removes ordinary shell/prompt duplicates without
	// dropping shell findings whose taint came from JavaScript stdout or files.
	baselineRule := rule
	baselineRule.JavaScript = nil
	baseline, baselineErr := analyzeAuthoredDocumentWithContext(ctx, document, baselineRule, workflowContext)
	result.Findings = slices.DeleteFunc(result.Findings, func(finding Finding) bool {
		return slices.ContainsFunc(baseline.Findings, func(previous Finding) bool {
			return finding.Site == previous.Site && slices.Equal(finding.Sources, previous.Sources)
		})
	})
	result.Gaps = slices.DeleteFunc(result.Gaps, func(gap CoverageGap) bool {
		return slices.Contains(baseline.Gaps, gap)
	})
	return result, errors.Join(err, baselineErr)
}

func (a *analyzer) authoredWorkflowInputs(ctx context.Context, block aw.ExecutionBlock, bindings scope) error {
	if block.Path == "" || block.Fields["uses"] == nil {
		return nil
	}
	uses, err := optionalField[string](block.Fields, "uses", block.Path)
	if err != nil {
		return err
	}
	inputs, err := optionalField[map[string]any](block.Fields, "with", block.Path)
	if err != nil {
		return err
	}
	for _, key := range slices.Sorted(maps.Keys(inputs)) {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch inputs[key].(type) {
		case nil, string, bool, int, int64, uint64, float64:
		default:
			return fmt.Errorf("AW workflow input must be a scalar at %s", block.FieldPath("with"))
		}
		path := block.FieldPath("with") + "." + key
		sources, err := a.text(fmt.Sprint(inputs[key]), path, bindings)
		if err != nil {
			return err
		}
		a.emit(Site{Kind: WorkflowInput, Path: path, Uses: uses, Input: key}, sources)
	}
	return nil
}

func (a *analyzer) authoredDisplay(block aw.ExecutionBlock, bindings scope) error {
	var failures []error
	for _, key := range []string{"name", "run-name"} {
		text, err := optionalField[string](block.Fields, key, block.Path)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		sources, err := a.text(text, block.FieldPath(key), bindings)
		failures = append(failures, err)
		a.emit(Site{Kind: Display, Path: block.FieldPath(key)}, sources)
	}
	return errors.Join(failures...)
}

func (a *analyzer) authoredOutputs(ctx context.Context, block aw.ExecutionBlock, bindings scope) error {
	outputs, err := optionalField[map[string]any](block.Fields, "outputs", block.Path)
	if err != nil {
		return err
	}
	for _, key := range slices.Sorted(maps.Keys(outputs)) {
		if err := ctx.Err(); err != nil {
			return err
		}
		text, ok := outputs[key].(string)
		if !ok {
			return fmt.Errorf("AW job output must be an expression string at %s", block.FieldPath("outputs"))
		}
		path := block.FieldPath("outputs") + "." + key
		sources, err := a.text(text, path, bindings)
		if err != nil {
			return err
		}
		a.emit(Site{Kind: Output, Path: path}, sources)
	}
	return nil
}

func (a *analyzer) authoredEnvironment(ctx context.Context, fields map[string]any, path string, parent scope) (scope, error) {
	bindings := maps.Clone(parent)
	if bindings == nil {
		bindings = make(scope)
	}
	if fields["env"] == nil {
		return bindings, nil
	}
	env, ok := fields["env"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("AW environment must be a mapping at %s", path)
	}
	for _, key := range slices.Sorted(maps.Keys(env)) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		switch env[key].(type) {
		case nil, string, bool, int, int64, uint64, float64:
		default:
			return nil, fmt.Errorf("AW environment binding must be a scalar at %s.%s", path, key)
		}
		sources, err := a.text(fmt.Sprint(env[key]), path+"."+key, parent)
		if err != nil {
			return nil, err
		}
		bindings["env."+strings.ToLower(key)] = sources
		bindings["shell."+key] = sources
	}
	return bindings, nil
}

func authoredRunFields(step aw.ExecutionStep, block aw.ExecutionBlock, document *aw.Document) (map[string]any, error) {
	rootDefaults, err := optionalField[map[string]any](document.Frontmatter, "defaults", "workflow")
	if err != nil {
		return nil, err
	}
	localDefaults, err := optionalField[map[string]any](block.Fields, "defaults", block.Path)
	if err != nil {
		return nil, err
	}
	fields := maps.Clone(step.Fields)
	if _, exists := fields["shell"]; !exists {
		fields["shell"] = inheritedRunSetting("shell", runDefaults(localDefaults), runDefaults(rootDefaults), runnerShell(block.Fields["runs-on"]))
	}
	if _, exists := fields["working-directory"]; !exists {
		directory := inheritedRunSetting("working-directory", runDefaults(localDefaults), runDefaults(rootDefaults), "")
		if directory == unknownRunSetting {
			return nil, fmt.Errorf("AW working directory could not be modeled at %s", step.Path)
		}
		if directory != "" {
			fields["working-directory"] = directory
		}
	}
	return fields, nil
}

func (a *analyzer) authoredPrompt(document *aw.Document, bindings, persisted scope) error {
	prompt := maps.Clone(bindings)
	maps.Copy(prompt, persisted)
	sources, err := a.text(document.Markdown, "markdown", prompt)
	if err != nil {
		return err
	}
	a.emit(Site{Kind: Prompt, Path: "markdown"}, sources)
	return nil
}
