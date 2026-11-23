package lint

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/github/gh-aw/pkg/workflow/taint"
	"github.com/rhysd/actionlint"
)

// OverprovisionedSecretsRule reports authored constructs that expose the whole
// secrets context instead of the named secrets a job or step actually needs.
func OverprovisionedSecretsRule() Rule {
	return Rule{
		ID:          "overprovisioned-secrets",
		Description: "Detect secrets: inherit and expressions that materialize the whole secrets context",
		Severity:    Error,
		Targets:     ActionsAST | AWAST,
		Check:       checkOverprovisionedSecrets,
	}
}

const (
	secretsContextMessage = "Expression exposes the entire secrets context; reference only the named secrets this value needs (for example secrets.NAME)"
	remoteInheritMessage  = "Reusable workflow outside this repository receives every caller secret via secrets: inherit; pass only the named secrets it declares"
	localInheritMessage   = "Reusable workflow receives every caller secret via secrets: inherit; pass only the named secrets it declares"
)

func checkOverprovisionedSecrets(ctx context.Context, input Input) ([]Diagnostic, error) {
	if err := integrityInput(ctx, input); err != nil {
		return nil, err
	}
	diagnostics, err := overprovisionedSecretsDiagnostics(ctx, input)
	// Mirror validateSecretsSerializationExpressions: errors require strict AW
	// policy; strict: false and Actions-only input without policy report warnings.
	strict := input.Document != nil
	if strict {
		if value, ok := input.Document.Frontmatter["strict"].(bool); ok {
			strict = value
		}
	}
	if !strict {
		for i := range diagnostics {
			diagnostics[i].Severity = Warning
		}
	}
	return diagnostics, err
}

func overprovisionedSecretsDiagnostics(ctx context.Context, input Input) ([]Diagnostic, error) {
	var diagnostics []Diagnostic
	visit := func(path, text string) error {
		reported := false
		return taint.VisitInterpolations(ctx, text, path, func(_, _, line int, node actionlint.ExprNode) error {
			if reported || !materializesSecretsContext(node) {
				return ctx.Err()
			}
			reported = true
			diagnostic := Diagnostic{Path: path, Message: secretsContextMessage}
			if strings.HasSuffix(path, ".run") || strings.HasSuffix(path, ".with.script") {
				diagnostic.Line = line
			}
			diagnostics = append(diagnostics, diagnostic)
			return ctx.Err()
		})
	}

	if input.Workflow != nil {
		authored, err := overprovisionedSecretsProvenance(input)
		if err != nil {
			return nil, err
		}
		for _, job := range input.Workflow.Jobs {
			if !job.SecretsInherit {
				continue
			}
			// Only exempt call-workflow fan-out jobs positively identified as the
			// compiler's inherit fallback; companion Markdown may omit imported jobs.
			if _, exists := authored[string(job.ID)]; !exists && isCompilerCallWorkflowFallback(input.Workflow, job) {
				continue
			}
			diagnostics = append(diagnostics, inheritDiagnostic("jobs."+string(job.ID)+".secrets", job.Uses))
		}
		root, err := actions.Lower(input.Workflow)
		if err != nil {
			return diagnostics, errors.New("overprovisioned-secrets cannot lower the Actions AST; correct the workflow before linting")
		}
		return diagnostics, walkExpressionScalars(ctx, root, "", visit)
	}

	blocks, err := aw.ExecutionBlocks(ctx, input.Document)
	failures := []error{err}
	// engine.env and sandbox.agent.env are copied into the agent execution step
	// environment but are not execution blocks.
	if input.Document != nil {
		if engine, ok := input.Document.Frontmatter["engine"].(map[string]any); ok {
			failures = append(failures, walkExpressionScalars(ctx, engine["env"], "engine.env", visit))
		}
		if sandbox, ok := input.Document.Frontmatter["sandbox"].(map[string]any); ok {
			if agent, ok := sandbox["agent"].(map[string]any); ok {
				failures = append(failures, walkExpressionScalars(ctx, agent["env"], "sandbox.agent.env", visit))
			}
		}
	}
	for _, block := range blocks {
		if secrets, ok := block.Fields["secrets"].(string); ok && strings.EqualFold(strings.TrimSpace(secrets), "inherit") {
			if uses, ok := block.Fields["uses"].(string); ok {
				diagnostics = append(diagnostics, inheritDiagnostic(block.FieldPath("secrets"), uses))
			}
		}
		// Only runner-interpolated execution fields; prompts and grader code
		// are covered by other rules.
		for _, key := range []string{"env", "with", "secrets", "outputs", "container", "services"} {
			failures = append(failures, walkExpressionScalars(ctx, block.Fields[key], block.FieldPath(key), visit))
		}
		for _, step := range block.Steps {
			failures = append(failures, walkExpressionScalars(ctx, step.Fields, step.Path, visit))
		}
	}
	return diagnostics, errors.Join(failures...)
}

// isCompilerCallWorkflowFallback reports whether job has the exact shape of a
// compiler-generated call-workflow fan-out job: call-<worker> needing
// safe_outputs, gated on call_workflow_name, and calling the local worker file.
func isCompilerCallWorkflowFallback(workflow *actions.Workflow, job *actions.Job) bool {
	worker, local := strings.CutPrefix(job.Uses, "./.github/workflows/")
	if !local || !strings.HasPrefix(string(job.ID), "call-") || !slices.Contains(job.Needs, actions.JobID("safe_outputs")) || job.If == nil {
		return false
	}
	for _, extension := range []string{".lock.yml", ".yml", ".md"} {
		if name, ok := strings.CutSuffix(worker, extension); ok {
			worker = name
			break
		}
	}
	condition, err := actions.RenderExpression(job.If, job.ID, workflow, false)
	if err != nil {
		return false
	}
	condition = strings.TrimSpace(condition)
	for strings.HasPrefix(condition, "(") && strings.HasSuffix(condition, ")") {
		condition = strings.TrimSpace(condition[1 : len(condition)-1])
	}
	return condition == "needs.safe_outputs.outputs.call_workflow_name == '"+strings.ReplaceAll(worker, "'", "''")+"'"
}

func overprovisionedSecretsProvenance(input Input) (map[string]any, error) {
	if input.AuthoredJobs != nil {
		return input.AuthoredJobs, nil
	}
	if input.Document == nil {
		return nil, nil
	}
	authored, err := policyMapping(input.Document.Frontmatter["jobs"], "jobs")
	if err != nil {
		return nil, err
	}
	if authored == nil {
		authored = make(map[string]any)
	}
	return authored, nil
}

func inheritDiagnostic(path, uses string) Diagnostic {
	if strings.HasPrefix(strings.TrimSpace(uses), "./") {
		return Diagnostic{Severity: Warning, Path: path, Message: localInheritMessage}
	}
	return Diagnostic{Path: path, Message: remoteInheritMessage}
}

// materializesSecretsContext reports whether node evaluates the secrets
// context as a whole: any reference other than secrets.NAME or secrets['NAME'].
func materializesSecretsContext(node actionlint.ExprNode) bool {
	found := false
	actionlint.VisitExprNode(node, func(child, parent actionlint.ExprNode, entering bool) {
		if found || !entering {
			return
		}
		variable, ok := child.(*actionlint.VariableNode)
		if !ok || !strings.EqualFold(variable.Name, "secrets") {
			return
		}
		switch parent := parent.(type) {
		case *actionlint.ObjectDerefNode:
			if parent.Receiver == child {
				return
			}
		case *actionlint.IndexAccessNode:
			if _, literal := parent.Index.(*actionlint.StringNode); literal && parent.Operand == child {
				return
			}
		}
		found = true
	})
	return found
}
