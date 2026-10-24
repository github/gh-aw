package workflow

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/github/gh-aw/pkg/workflow/actions"
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
	document := data.SourceDocument
	if document != nil {
		policyDocument := *document
		policyDocument.Frontmatter = document.FrontmatterResult().Frontmatter
		policyDocument.Frontmatter["strict"] = c.effectiveStrictMode(data.RawFrontmatter)
		document = &policyDocument
	}
	diagnostics, err := workflowlint.Default().Run(c.ctx, workflowlint.Input{
		Workflow: graph, Document: document, SourcePath: sourcePath,
		AllowActionRefs: c.allowActionRefs,
	})
	var failures []error
	if err != nil {
		failures = append(failures, fmt.Errorf("workflow lint analysis failed: %w", err))
	}
	for _, diagnostic := range diagnostics {
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
