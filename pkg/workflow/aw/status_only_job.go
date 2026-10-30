package aw

import (
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
	"github.com/rhysd/actionlint"
)

var statusOnlyExpressionPattern = actionslanguageservice.Pattern{}

// StatusOnlyViolation describes one rejected status-only job reference.
type StatusOnlyViolation struct {
	Path    string
	Message string
}

type statusOnlyJobValidator struct {
	jobID            string
	allowedOutputJob map[string]struct{}
	violations       []StatusOnlyViolation
	seen             map[string]struct{}
}

// IsStatusOnlyJob reports whether an authored custom job opted to continue after detection errors.
func IsStatusOnlyJob(config map[string]any) bool {
	gate, ok := config["detection-gate"].(string)
	return ok && gate == "status-only"
}

// HasSuccessfulDetectionCondition reports whether a job's condition requires
// detection success on every branch, allowing skipped detection for conditional gates.
func HasSuccessfulDetectionCondition(condition string) bool {
	if strings.TrimSpace(condition) == "" {
		return true
	}
	_, node, err := ParseActionsExpression(condition)
	return err == nil && statusOnlyDetectionSuccessRequired(node)
}

// ValidateStatusOnlyJob checks that a status-only job cannot access agent data.
func ValidateStatusOnlyJob(jobID string, config map[string]any, allowedOutputJobs map[string]struct{}) []StatusOnlyViolation {
	allowed := make(map[string]struct{}, len(allowedOutputJobs))
	for id := range allowedOutputJobs {
		allowed[strings.ToLower(id)] = struct{}{}
	}

	validator := statusOnlyJobValidator{
		jobID:            jobID,
		allowedOutputJob: allowed,
		seen:             make(map[string]struct{}),
	}
	validator.inspect(config, "jobs."+jobID, false, false, false)
	return validator.violations
}

func (validator *statusOnlyJobValidator) addViolation(path, message string) {
	key := path + "\x00" + message
	if _, exists := validator.seen[key]; exists {
		return
	}
	validator.seen[key] = struct{}{}
	validator.violations = append(validator.violations, StatusOnlyViolation{Path: path, Message: message})
}

func (validator *statusOnlyJobValidator) inspect(value any, path string, inEnv, inRun, inIf bool) {
	switch value := value.(type) {
	case map[string]any:
		validator.inspectMap(value, path, inEnv, inRun)
	case []any:
		for index, item := range value {
			validator.inspect(item, fmt.Sprintf("%s[%d]", path, index), inEnv, inRun, inIf)
		}
	case string:
		validator.inspectString(value, path, inEnv, inRun, inIf)
	}
}

func (validator *statusOnlyJobValidator) inspectMap(value map[string]any, path string, inEnv, inRun bool) {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		childPath := path + "." + key
		validator.inspectMapKey(value, path, key, childPath, inEnv)
		validator.inspect(value[key], childPath, inEnv || strings.EqualFold(key, "env"), inRun || strings.EqualFold(key, "run"), strings.EqualFold(key, "if"))
	}
}

func (validator *statusOnlyJobValidator) inspectMapKey(value map[string]any, path, key, childPath string, inEnv bool) {
	if strings.EqualFold(key, "uses") {
		if uses, ok := value[key].(string); ok {
			if path == "jobs."+validator.jobID && uses != "" {
				validator.addViolation(childPath, "status-only jobs cannot call an opaque reusable workflow")
			}
			if strings.Contains(strings.ToLower(uses), "actions/download-artifact") {
				validator.addViolation(childPath, "status-only jobs cannot use actions/download-artifact")
			}
		}
	}
	lowerKey := strings.ToLower(key)
	if inEnv && (strings.Contains(lowerKey, "gh_aw_agent_output") ||
		strings.Contains(lowerKey, "gh_aw_safe_output") ||
		strings.Contains(lowerKey, "gh_aw_safeoutput")) {
		validator.addViolation(childPath, "status-only jobs cannot reference GH_AW_AGENT_OUTPUT or agent/safe-output files")
	}
}

func (validator *statusOnlyJobValidator) inspectString(value, path string, inEnv, inRun, inIf bool) {
	if inEnv || inRun {
		lower := strings.ToLower(value)
		if strings.Contains(lower, "gh_aw_agent_output") ||
			strings.Contains(lower, "gh_aw_safe_output") ||
			strings.Contains(lower, "gh_aw_safeoutput") ||
			strings.Contains(lower, "gh-aw/safeoutputs") ||
			strings.Contains(lower, "gh-aw/safe-outputs") ||
			strings.Contains(lower, "gh-aw/agent_output") ||
			strings.Contains(lower, "agent_output.json") ||
			strings.Contains(lower, "safe_outputs.ndjson") {
			validator.addViolation(path, "status-only jobs cannot reference GH_AW_AGENT_OUTPUT or agent/safe-output files")
		}
	}

	matches := statusOnlyExpressionPattern.FindAllString(value, -1)
	for _, match := range matches {
		validator.inspectExpression(match, path)
	}
	if strings.Contains(value, "${{") && len(matches) == 0 {
		validator.addViolation(path, "status-only job contains an expression that cannot be inspected safely")
	}
	if inIf && !strings.Contains(value, "${{") && strings.Contains(strings.ToLower(value), "needs") {
		validator.inspectExpression(value, path)
	}
}

func (validator *statusOnlyJobValidator) inspectExpression(expression, path string) {
	inspectStatusOnlyExpression(expression, path, validator.allowedOutputJob, validator.addViolation)
}

func inspectStatusOnlyExpression(expression, path string, allowed map[string]struct{}, addViolation func(string, string)) {
	_, node, err := ParseActionsExpression(expression)
	if err != nil {
		addViolation(path, "status-only job contains an expression that cannot be inspected safely")
		return
	}
	badReferences := make(map[string]struct{})
	actionlint.VisitExprNode(node, func(child, _ actionlint.ExprNode, entering bool) {
		if !entering {
			return
		}
		parts, ok := statusOnlyExpressionPath(child)
		jobID, outputsReference := statusOnlyOutputJobID(parts)
		if !ok || !outputsReference {
			return
		}
		jobID = strings.ToLower(jobID)
		if _, ok := allowed[jobID]; ok {
			return
		}
		badReferences["needs."+jobID+".outputs"] = struct{}{}
	})
	if len(badReferences) == 0 {
		return
	}
	references := make([]string, 0, len(badReferences))
	for reference := range badReferences {
		references = append(references, reference)
	}
	slices.Sort(references)
	for _, reference := range references {
		addViolation(path, fmt.Sprintf("status-only jobs cannot read %s; only outputs of detection-gated compiler-owned jobs are permitted", reference))
	}
}

func statusOnlyDetectionSuccessRequired(node actionlint.ExprNode) bool {
	switch node := node.(type) {
	case *actionlint.LogicalOpNode:
		if node.Kind.String() == "&&" {
			return statusOnlyDetectionSuccessRequired(node.Left) || statusOnlyDetectionSuccessRequired(node.Right)
		}
		return statusOnlyDetectionSuccessRequired(node.Left) && statusOnlyDetectionSuccessRequired(node.Right)
	case *actionlint.FuncCallNode:
		return strings.EqualFold(node.Callee, "success") && len(node.Args) == 0
	case *actionlint.CompareOpNode:
		if node.Kind.String() != "==" {
			return false
		}
		return statusOnlyDetectionResultReference(node.Left) && statusOnlyDetectionSuccessLiteral(node.Right) ||
			statusOnlyDetectionResultReference(node.Right) && statusOnlyDetectionSuccessLiteral(node.Left)
	}
	return false
}

func statusOnlyDetectionResultReference(node actionlint.ExprNode) bool {
	parts, ok := statusOnlyExpressionPath(node)
	return ok && len(parts) == 3 && strings.EqualFold(strings.Join(parts, "."), "needs.detection.result")
}

func statusOnlyOutputJobID(parts []string) (string, bool) {
	if len(parts) < 3 {
		return "", false
	}
	root, jobID, field := "", "", ""
	for index, part := range parts {
		switch index {
		case 0:
			root = part
		case 1:
			jobID = part
		case 2:
			field = part
		}
	}
	return jobID, strings.EqualFold(root, "needs") && strings.EqualFold(field, "outputs")
}

func statusOnlyDetectionSuccessLiteral(node actionlint.ExprNode) bool {
	value, ok := node.(*actionlint.StringNode)
	return ok && (strings.EqualFold(value.Value, "success") || strings.EqualFold(value.Value, "skipped"))
}

func statusOnlyExpressionPath(node actionlint.ExprNode) ([]string, bool) {
	switch node := node.(type) {
	case *actionlint.VariableNode:
		return []string{node.Name}, true
	case *actionlint.ObjectDerefNode:
		parts, ok := statusOnlyExpressionPath(node.Receiver)
		if !ok {
			return nil, false
		}
		return append(parts, node.Property), true
	case *actionlint.IndexAccessNode:
		parts, ok := statusOnlyExpressionPath(node.Operand)
		if !ok {
			return nil, false
		}
		if index, ok := node.Index.(*actionlint.StringNode); ok {
			return append(parts, index.Value), true
		}
		return append(parts, "*"), true
	default:
		return nil, false
	}
}
