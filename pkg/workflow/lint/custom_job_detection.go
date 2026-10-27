package lint

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/rhysd/actionlint"
)

func CustomJobDetectionRule() Rule {
	return Rule{
		ID: "custom-job-detection", Description: "Require custom jobs downstream of agent to wait for successful threat detection",
		ThreatRuleID: "CTR-005",
		Severity:     Error, Targets: ActionsAST | AWAST, Check: checkCustomJobDetection,
	}
}

func checkCustomJobDetection(ctx context.Context, input Input) ([]Diagnostic, error) {
	if err := integrityInput(ctx, input); err != nil {
		return nil, err
	}
	disabled, err := customJobDetectionDisabled(input)
	if err != nil || disabled {
		return nil, err
	}
	jobs, graph, err := customJobDetectionJobs(ctx, input)
	if err != nil {
		return nil, err
	}
	allowedOutputJobs := customJobDetectionAllowedOutputs(input, graph)
	var diagnostics []Diagnostic
	for _, id := range sliceutil.SortedKeys(jobs) {
		if err := ctx.Err(); err != nil {
			return diagnostics, err
		}
		fields, err := policyMapping(jobs[id], "jobs."+id)
		if err != nil {
			return diagnostics, err
		}
		if input.Document == nil && constants.IsCompilerOwnedJobName(id) {
			continue
		}
		if _, builtin := constants.KnownBuiltInJobNames[id]; builtin && fields["uses"] == nil && fields["runs-on"] == nil {
			continue
		}
		if !graph.DependsOn(id, string(constants.AgentJobName)) && !graph.DependsOn(id, string(constants.DetectionJobName)) {
			_, hasAgent := graph[string(constants.AgentJobName)]
			if input.Workflow != nil && hasAgent && !graph.DependsOn(string(constants.AgentJobName), id) {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    "jobs." + id + ".needs",
					Message: "Custom jobs must run before agent or after detection; make agent depend on this job or add detection to this job's needs",
				})
			}
			continue
		}
		if graph.DependsOn(string(constants.DetectionJobName), id) {
			diagnostics = append(diagnostics, Diagnostic{
				Path:    "jobs." + id + ".needs",
				Message: "Custom jobs cannot run between agent and detection; move this job before agent or after detection",
			})
			continue
		}
		statusOnly := aw.IsStatusOnlyJob(fields)
		if statusOnly {
			diagnostics = append(diagnostics, customJobDetectionStatusOnlyDiagnostics(id, fields, jobs, graph, allowedOutputJobs)...)
		}
		found, err := customJobDetectionDiagnostics(ctx, fields, "jobs."+id, statusOnly)
		diagnostics = append(diagnostics, found...)
		if err != nil {
			return diagnostics, err
		}
	}
	return diagnostics, nil
}

func customJobDetectionStatusOnlyDiagnostics(id string, fields map[string]any, jobs map[string]any, graph aw.JobDependencyGraph, allowedOutputJobs map[string]struct{}) []Diagnostic {
	var diagnostics []Diagnostic
	for _, dependent := range sliceutil.SortedKeys(jobs) {
		if dependent != id && !constants.IsCompilerOwnedJobName(dependent) && graph.DependsOn(dependent, id) {
			diagnostics = append(diagnostics, Diagnostic{
				Path:    "jobs." + id + ".detection-gate",
				Message: fmt.Sprintf("Jobs that continue after detection errors cannot have dependent custom job %s; apply the same contract to a leaf job instead", dependent),
			})
			break
		}
	}
	for _, violation := range aw.ValidateStatusOnlyJob(id, fields, allowedOutputJobs) {
		diagnostics = append(diagnostics, Diagnostic{
			Path:    violation.Path,
			Message: violation.Message,
		})
	}
	return diagnostics
}

func customJobDetectionAllowedOutputs(input Input, graph aw.JobDependencyGraph) map[string]struct{} {
	allowed := make(map[string]struct{})
	if input.Workflow == nil {
		allowed[string(constants.SafeOutputsJobName)] = struct{}{}
		return allowed
	}
	for _, job := range input.Workflow.Jobs {
		id := string(job.ID)
		condition := ""
		if job.If != nil {
			var err error
			condition, err = actions.RenderExpression(job.If, job.ID, input.Workflow, false)
			if err != nil {
				continue
			}
		}
		if constants.IsCompilerOwnedJobName(id) &&
			graph.DependsOn(id, string(constants.DetectionJobName)) &&
			aw.HasSuccessfulDetectionCondition(condition) {
			allowed[id] = struct{}{}
		}
	}
	return allowed
}

func customJobDetectionJobs(ctx context.Context, input Input) (map[string]any, aw.JobDependencyGraph, error) {
	if input.Workflow != nil {
		return customJobDetectionActions(input)
	}
	jobs, err := policyMapping(input.Document.Frontmatter["jobs"], "jobs")
	if err != nil {
		return nil, nil, err
	}
	for _, id := range sliceutil.SortedKeys(jobs) {
		fields, err := policyMapping(jobs[id], "jobs."+id)
		if err != nil {
			return nil, nil, err
		}
		if _, err := customJobDetectionNeeds(ctx, fields, "jobs."+id); err != nil {
			return nil, nil, err
		}
	}
	return jobs, aw.AuthoredJobDependencies(jobs), nil
}

func customJobDetectionActions(input Input) (map[string]any, aw.JobDependencyGraph, error) {
	jobs := make(map[string]any)
	graph := make(aw.JobDependencyGraph)
	var authored map[string]any
	if input.Document != nil {
		var err error
		authored, err = policyMapping(input.Document.Frontmatter["jobs"], "jobs")
		if err != nil {
			return nil, nil, err
		}
	}
	for _, job := range input.Workflow.Jobs {
		id := string(job.ID)
		graph[id] = []string{}
		var needs []any
		for _, dependency := range job.Needs {
			needs = append(needs, string(dependency))
			graph[id] = append(graph[id], string(dependency))
		}
		// With a companion source, only authored top-level jobs are custom.
		if input.Document != nil {
			if _, exists := authored[id]; !exists {
				continue
			}
		}
		fields := make(map[string]any, len(job.Needs)+1)
		if input.Document != nil {
			if sourceFields, ok := authored[id].(map[string]any); ok {
				maps.Copy(fields, sourceFields)
			}
		}
		fields["needs"] = needs
		if job.If != nil {
			condition, err := actions.RenderExpression(job.If, job.ID, input.Workflow, false)
			if err != nil {
				return nil, nil, fmt.Errorf("cannot inspect jobs.%s.if: correct the Actions condition before linting", id)
			}
			fields["if"] = condition
		}
		jobs[id] = fields
	}
	return jobs, graph, nil
}

func customJobDetectionDisabled(input Input) (bool, error) {
	if input.Workflow != nil {
		hasAgent := slices.ContainsFunc(input.Workflow.Jobs, func(job *actions.Job) bool {
			return string(job.ID) == string(constants.AgentJobName)
		})
		hasDetection := slices.ContainsFunc(input.Workflow.Jobs, func(job *actions.Job) bool {
			return string(job.ID) == string(constants.DetectionJobName)
		})
		return !hasAgent || !hasDetection, nil
	}
	outputs, err := policyMapping(input.Document.Frontmatter["safe-outputs"], "safe-outputs")
	if err != nil {
		return false, err
	}
	detection := outputs["threat-detection"]
	if enabled, ok := detection.(bool); ok {
		return !enabled, nil
	}
	fields, ok := detection.(map[string]any)
	if !ok {
		return false, nil
	}
	engine, literal := fields["engine"].(bool)
	if !literal || engine {
		return false, nil
	}
	for _, key := range []string{"steps", "post-steps"} {
		if fields[key] == nil {
			continue
		}
		steps, valid := fields[key].([]any)
		if !valid {
			return false, fmt.Errorf("custom-job-detection requires a step sequence at safe-outputs.threat-detection.%s", key)
		}
		if len(steps) > 0 {
			return false, nil
		}
	}
	return true, nil
}

func customJobDetectionNeeds(ctx context.Context, fields map[string]any, path string) ([]string, error) {
	if fields["needs"] == nil {
		return nil, nil
	}
	needs, valid, err := workflowRunStrings(ctx, fields["needs"], true)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, fmt.Errorf("custom-job-detection requires literal job IDs at %s.needs; correct the job dependencies before linting", path)
	}
	for _, need := range needs {
		if strings.TrimSpace(need) == "" || strings.Contains(need, "${{") {
			return nil, fmt.Errorf("custom-job-detection requires literal job IDs at %s.needs; correct the job dependencies before linting", path)
		}
	}
	return needs, nil
}

func customJobDetectionDiagnostics(ctx context.Context, fields map[string]any, path string, statusOnly bool) ([]Diagnostic, error) {
	needs, err := customJobDetectionNeeds(ctx, fields, path)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(needs, string(constants.DetectionJobName)) {
		return []Diagnostic{{
			Path:    path + ".needs",
			Message: "Custom jobs downstream of agent must also need detection; add detection to needs and require needs.detection.result == 'success' when using a status override",
		}}, nil
	}
	if statusOnly {
		return nil, nil
	}
	gated, err := customJobDetectionCondition(fields["if"])
	if err != nil {
		return nil, fmt.Errorf("cannot inspect %s.if: expected a valid Actions condition; correct the condition before linting", path)
	}
	if gated {
		return nil, nil
	}
	return []Diagnostic{{
		Path:    path + ".if",
		Message: "Custom job condition can bypass successful threat detection; require needs.detection.result == 'success' on every runnable branch, including with always(), failure(), or cancelled()",
	}}, nil
}

func customJobDetectionCondition(value any) (bool, error) {
	if value == nil {
		return true, nil // Actions implicitly requires success() for all needs.
	}
	if _, ok := value.(bool); ok {
		return true, nil
	}
	text, ok := value.(string)
	if !ok {
		return false, errors.New("invalid job condition")
	}
	_, node, err := aw.ParseActionsExpression(text)
	if err != nil {
		return false, errors.New("invalid job condition")
	}
	statusOverride := false
	actionlint.VisitExprNode(node, func(child, _ actionlint.ExprNode, entering bool) {
		if call, ok := child.(*actionlint.FuncCallNode); ok && entering {
			switch strings.ToLower(call.Callee) {
			case "always", "success", "failure", "cancelled":
				statusOverride = true
			}
		}
	})
	return !statusOverride || detectionSuccessRequired(node), nil
}

// Prove a success guard on every disjunction, not merely its textual presence.
func detectionSuccessRequired(node actionlint.ExprNode) bool {
	switch node := node.(type) {
	case *actionlint.LogicalOpNode:
		if node.Kind.String() == "&&" {
			return detectionSuccessRequired(node.Left) || detectionSuccessRequired(node.Right)
		}
		return detectionSuccessRequired(node.Left) && detectionSuccessRequired(node.Right)
	case *actionlint.FuncCallNode:
		return strings.EqualFold(node.Callee, "success") && len(node.Args) == 0
	case *actionlint.CompareOpNode:
		return node.Kind.String() == "==" &&
			(detectionResultReference(node.Left) && detectionSuccessLiteral(node.Right) ||
				detectionResultReference(node.Right) && detectionSuccessLiteral(node.Left))
	}
	return false
}

func detectionSuccessLiteral(node actionlint.ExprNode) bool {
	value, ok := node.(*actionlint.StringNode)
	return ok && strings.EqualFold(value.Value, "success")
}

func detectionResultReference(node actionlint.ExprNode) bool {
	parts := []string{"result", string(constants.DetectionJobName), "needs"}
	for _, part := range parts {
		switch value := node.(type) {
		case *actionlint.ObjectDerefNode:
			if !strings.EqualFold(value.Property, part) {
				return false
			}
			node = value.Receiver
		case *actionlint.IndexAccessNode:
			key, ok := value.Index.(*actionlint.StringNode)
			if !ok || !strings.EqualFold(key.Value, part) {
				return false
			}
			node = value.Operand
		case *actionlint.VariableNode:
			return part == "needs" && strings.EqualFold(value.Name, part)
		default:
			return false
		}
	}
	return false
}
