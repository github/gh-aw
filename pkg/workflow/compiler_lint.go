package workflow

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"time"

	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	workflowlint "github.com/github/gh-aw/pkg/workflow/lint"
)

func (c *Compiler) lintCompiledWorkflow(data *WorkflowData, content, sourcePath string) error {
	if !c.lint {
		return nil
	}
	workflowLog.Print("Linting generated Actions graph and authored AW document")
	graph, err := actions.Parse([]byte(content))
	if err != nil {
		return formatCompilerError(sourcePath, "error", "cannot parse generated Actions AST for linting", nil)
	}
	diagnostics, err := workflowlint.Default().Run(c.ctx, workflowlint.Input{
		Workflow: graph, Document: c.lintPolicyDocument(data), SourcePath: sourcePath,
		AllowActionRefs:     c.allowActionRefs,
		SafeOutputJobs:      safeOutputLintJobs(data),
		GeneratedActionRefs: compilerGeneratedActionRefs(data),
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
	for _, failure := range data.ActionResolutionFailures {
		for reference := range generated {
			if gitutil.ExtractBaseRepo(extractActionRepo(reference)) == gitutil.ExtractBaseRepo(failure.Repo) &&
				extractActionVersion(reference) == failure.Ref {
				delete(generated, reference)
			}
		}
	}
	removeAuthoredActionRefs(generated, data.Jobs)
	return generated
}

func removeAuthoredActionRefs(generated map[string]struct{}, value any) {
	switch value := value.(type) {
	case map[string]any:
		if uses, ok := value["uses"].(string); ok {
			delete(generated, uses)
		}
		for _, child := range value {
			removeAuthoredActionRefs(generated, child)
		}
	case []any:
		for _, child := range value {
			removeAuthoredActionRefs(generated, child)
		}
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
