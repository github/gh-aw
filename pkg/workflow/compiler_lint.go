package workflow

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	workflowlint "github.com/github/gh-aw/pkg/workflow/lint"
)

func (c *Compiler) lintCompiledWorkflow(data *WorkflowData, content, sourcePath string) error {
	if !c.lint && data.Decision == nil {
		return nil
	}
	workflowLog.Print("Linting generated Actions graph and authored AW document")
	graph, err := actions.Parse([]byte(content))
	if err != nil {
		return formatCompilerError(sourcePath, "error", "cannot parse generated Actions AST for linting", nil)
	}
	if err := validateDecisionQuestions(data, graph, sourcePath); err != nil {
		return err
	}
	if !c.lint {
		return nil
	}
	generatedActionRefs := compilerGeneratedActionRefs(data)
	// An explicit --action-tag is a caller-selected reference override. Do not
	// let compiler ownership downgrade mutable fallback refs; callers must opt in
	// with --allow-action-refs when the override cannot be resolved to a SHA.
	if c.actionTag != "" {
		generatedActionRefs = nil
	}
	diagnostics, err := workflowlint.Default().Run(c.ctx, workflowlint.Input{
		Workflow: graph, Document: c.lintPolicyDocument(data), SourcePath: sourcePath,
		AllowActionRefs:     c.allowActionRefs,
		SafeOutputJobs:      safeOutputLintJobs(data),
		GeneratedActionRefs: generatedActionRefs,
	})
	var failures []error
	if err != nil {
		failures = append(failures, fmt.Errorf("workflow lint analysis failed: %w", err))
	}
	checkouts, checkoutErr := c.artipackedCheckoutLintPaths(graph)
	if checkoutErr != nil {
		failures = append(failures, checkoutErr)
	}
	for _, diagnostic := range diagnostics {
		_, exemptCheckout := checkouts[diagnostic.Path]
		if diagnostic.RuleID == "artipacked" && diagnostic.Severity == workflowlint.Warning && exemptCheckout {
			continue
		}
		if (diagnostic.RuleID == "secret-leak" || diagnostic.RuleID == "grader-file-io") && diagnostic.Severity == workflowlint.Warning {
			continue
		}
		message := fmt.Sprintf("[%s] %s at %s (script line %d)",
			diagnostic.RuleID, diagnostic.Message, diagnostic.Path, diagnostic.Line)
		if diagnostic.ThreatRuleID != "" {
			message = diagnostic.ThreatRuleID + ": " + message
			var suppressions []ThreatDetectionSuppression
			if data.ParsedFrontmatter != nil {
				suppressions = data.ParsedFrontmatter.ThreatDetectionSuppressions
			}
			if isThreatDetectionRuleSuppressed(suppressions, diagnostic.ThreatRuleID, time.Now()) {
				continue
			}
		}
		if diagnostic.Severity == workflowlint.Error {
			failures = append(failures, errors.New(message))
		} else {
			if _, err := fmt.Fprintln(os.Stderr, formatCompilerMessage(sourcePath, "warning", message)); err != nil {
				failures = append(failures, fmt.Errorf("write workflow lint warning: %w", err))
			}
			c.IncrementWarningCount()
		}
	}
	if err := errors.Join(failures...); err != nil {
		return formatCompilerError(sourcePath, "error", err.Error(), err)
	}
	return nil
}

func compilerGeneratedActionRefs(data *WorkflowData) map[string]struct{} {
	generated := maps.Clone(data.generatedActionRefs)
	// Authored pin failures must not inherit a matching compiler-generated fallback.
	// Authored pre-/post-/agent steps are pinned before rendering; unresolved
	// literal collisions are covered here without re-parsing their YAML strings.
	for _, failure := range data.ActionResolutionFailures {
		if failure.compilerGenerated {
			continue
		}
		for reference := range generated {
			if extractActionRepo(reference) == failure.Repo &&
				extractActionVersion(reference) == failure.Ref {
				delete(generated, reference)
			}
		}
	}
	for _, value := range data.Jobs {
		if job, ok := value.(map[string]any); ok {
			removeAuthoredActionRef(generated, job)
			for _, field := range []string{"steps", "pre-steps", "post-steps"} {
				removeAuthoredStepActionRefs(generated, job[field])
			}
		}
	}
	for _, step := range data.OnSteps {
		removeAuthoredActionRef(generated, step)
	}
	if data.SecretMasking != nil {
		for _, step := range data.SecretMasking.Steps {
			removeAuthoredActionRef(generated, step)
		}
	}
	if data.SafeOutputs != nil {
		removeAuthoredStepActionRefs(generated, data.SafeOutputs.Steps)
		for _, action := range data.SafeOutputs.Actions {
			reference := action.ResolvedRef
			if reference == "" {
				reference = action.Uses
			}
			reference, _, _ = strings.Cut(reference, " # ")
			delete(generated, reference)
		}
		for _, job := range data.SafeOutputs.Jobs {
			removeAuthoredStepActionRefs(generated, job.Steps)
		}
		if detection := data.SafeOutputs.ThreatDetection; detection != nil {
			removeAuthoredStepActionRefs(generated, detection.Steps)
			removeAuthoredStepActionRefs(generated, detection.PostSteps)
		}
	}
	return generated
}

func removeAuthoredStepActionRefs(generated map[string]struct{}, value any) {
	if steps, ok := value.([]any); ok {
		for _, value := range steps {
			if step, ok := value.(map[string]any); ok {
				removeAuthoredActionRef(generated, step)
			}
		}
	}
}

func removeAuthoredActionRef(generated map[string]struct{}, value map[string]any) {
	if uses, ok := value["uses"].(string); ok {
		delete(generated, uses)
	}
}

func (c *Compiler) lintPolicyDocument(data *WorkflowData) *aw.Document {
	if data.SourceDocument == nil {
		return nil
	}
	document := *data.SourceDocument
	document.Frontmatter = data.SourceDocument.FrontmatterResult().Frontmatter
	document.Frontmatter["strict"] = c.effectiveStrictMode(data.RawFrontmatter)
	return &document
}

func safeOutputLintJobs(data *WorkflowData) []string {
	var jobs []string
	if data.SafeOutputs != nil {
		for name := range data.SafeOutputs.Jobs {
			jobs = append(jobs, stringutil.NormalizeSafeOutputIdentifier(name))
		}
	}
	slices.Sort(jobs)
	return jobs
}
