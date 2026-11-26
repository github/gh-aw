package lint

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func AllowedLabelsRule() Rule {
	return Rule{
		ID: "allowed-labels", Description: "Keep safe-output label allowlists meaningfully scoped",
		ThreatRuleID: "CTR-015",
		Severity:     Error, Targets: AWAST, Check: checkAllowedLabels,
	}
}

func checkAllowedLabels(ctx context.Context, input Input) ([]Diagnostic, error) {
	outputs, err := authoredPolicyMapping(ctx, input, "safe-outputs")
	if err != nil {
		return nil, err
	}
	var diagnostics []Diagnostic
	var failures []error
	for _, handler := range []string{"create-issue", "create-discussion", "update-discussion", "create-pull-request"} {
		if err := ctx.Err(); err != nil {
			return diagnostics, errors.Join(append(failures, err)...)
		}
		path := "safe-outputs." + handler
		// `handler: true` or `false` only enables or disables the handler; it has no allowed-labels.
		if _, isBool := outputs[handler].(bool); isBool {
			continue
		}
		config, err := policyMapping(outputs[handler], path)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		value, exists := config["allowed-labels"]
		if !exists {
			continue
		}
		labels, err := policyStrings(ctx, value, path+".allowed-labels")
		failures = append(failures, err)
		for _, label := range labels {
			if err := ctx.Err(); err != nil {
				return diagnostics, errors.Join(append(failures, err)...)
			}
			if strings.TrimSpace(label) == "*" {
				diagnostics = append(diagnostics, Diagnostic{
					Path:    path + ".allowed-labels",
					Message: "A bare wildcard matches every allowed label; use explicit label names or narrower patterns such as team-*",
				})
				break
			}
		}
	}
	return diagnostics, errors.Join(failures...)
}

func PushTargetRule() Rule {
	return Rule{
		ID: "push-target", Description: "Constrain wildcard pull-request branch push targets",
		ThreatRuleID: "CTR-012",
		Severity:     Warning, Targets: AWAST, Check: checkPushTarget,
	}
}

func checkPushTarget(ctx context.Context, input Input) ([]Diagnostic, error) {
	config, err := authoredPolicyMapping(ctx, input, "safe-outputs", "push-to-pull-request-branch")
	if err != nil || config == nil {
		return nil, err
	}
	if target, ok := config["target"].(string); !ok || target != "*" {
		return nil, nil
	}
	constrained, err := pushPolicyConstraints(ctx, config)
	if err != nil || constrained {
		return nil, err
	}
	// Imported configuration can supply the missing constraints. Without a
	// resolved source graph their absence is not evidence of an unsafe target.
	pending, err := authoredImportsPending(input)
	if err != nil || pending {
		return nil, err
	}
	return []Diagnostic{{
		Path:    "safe-outputs.push-to-pull-request-branch.target",
		Message: "Wildcard pull-request pushes have no author, title, or label constraint; configure required-author, required-title-prefix, or required-labels",
	}}, nil
}

func pushPolicyConstraints(ctx context.Context, config map[string]any) (bool, error) {
	constrained := false
	if value, exists := config["required-author"]; exists {
		text, ok := value.(string)
		if !ok {
			return false, errors.New("push-target requires a literal string at safe-outputs.push-to-pull-request-branch.required-author")
		}
		constrained = constrained || strings.TrimSpace(text) != ""
	}
	for _, key := range []string{"required-title-prefix", "title-prefix"} {
		if value, exists := config[key]; exists {
			text, ok := value.(string)
			if !ok {
				return false, fmt.Errorf("push-target requires a literal string at safe-outputs.push-to-pull-request-branch.%s", key)
			}
			constrained = constrained || text != ""
		}
	}
	for _, key := range []string{"required-labels", "labels"} {
		value, exists := config[key]
		if !exists {
			continue
		}
		if expression, ok := value.(string); ok && strings.HasPrefix(strings.TrimSpace(expression), "${{") && strings.HasSuffix(strings.TrimSpace(expression), "}}") {
			constrained = true
			continue
		}
		labels, err := policyStrings(ctx, value, "safe-outputs.push-to-pull-request-branch."+key)
		if err != nil {
			return false, err
		}
		constrained = constrained || len(labels) > 0
	}
	return constrained, ctx.Err()
}
