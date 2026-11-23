package lint

import (
	"context"
	"errors"
	"fmt"
)

func DangerousTriggersRule() Rule {
	return Rule{
		ID: "dangerous-triggers", Description: "Review privileged target events and comment events with broadened admission and weak integrity filtering",
		ThreatRuleID: "CTR-039", FindingThreatRuleIDs: []string{"CTR-008"},
		Severity: Warning, Targets: ActionsAST | AWAST, Check: checkDangerousTriggers,
	}
}

func checkDangerousTriggers(ctx context.Context, input Input) ([]Diagnostic, error) {
	if err := integrityInput(ctx, input); err != nil {
		return nil, err
	}
	on := executionPolicyOn(input)
	var diagnostics []Diagnostic
	for _, name := range []string{"pull_request_target", "issue_comment", "pull_request_review", "pull_request_review_comment", "slash_command", "command"} {
		if err := ctx.Err(); err != nil {
			return diagnostics, err
		}
		if input.Workflow != nil && (name == "slash_command" || name == "command") {
			continue
		}
		_, found, err := authoredTrigger(ctx, on, name)
		if err != nil {
			return diagnostics, err
		}
		if !found {
			continue
		}
		path := "on"
		if _, mapping := on.(map[string]any); mapping {
			path += "." + name
		}
		if name == "pull_request_target" {
			acknowledged, err := targetTriggerRiskAcknowledged(ctx, input)
			if err != nil {
				return diagnostics, err
			}
			if acknowledged {
				continue
			}
			diagnostics = append(diagnostics, Diagnostic{
				ThreatRuleID: "CTR-008", Path: path,
				Message: "pull_request_target runs in the base repository context with access to secrets and potentially write-capable tokens, even for fork PRs; prefer pull_request when privileged access is unnecessary. Actor authorization and trusted checkout restrictions remain required",
			})
			continue
		}
		filtering, err := commentIntegrityFiltering(input)
		if err != nil {
			return diagnostics, err
		}
		untrusted, err := commentTriggerAdmitsUntrustedActors(ctx, input)
		if err != nil {
			return diagnostics, err
		}
		if filtering == "" || !untrusted {
			continue
		}
		diagnostics = append(diagnostics, Diagnostic{Path: path,
			Message: fmt.Sprintf("%s can deliver untrusted comment or review content from actors outside trusted repository roles with %s; restrict on.roles to admin, maintainer, or write, or configure tools.github.min-integrity: approved or merged. Credential access depends on the event and job configuration: keep comment text out of executable expressions and do not execute untrusted PR code with privileged credentials. Review an explicit GitHub Actions event allowlist in Evaluate mode before enforcement", name, filtering),
		})
	}
	return diagnostics, nil
}

func commentTriggerAdmitsUntrustedActors(ctx context.Context, input Input) (bool, error) {
	roles := input.ResolvedRoles
	if roles == nil {
		if input.Document == nil {
			return true, nil
		}
		on, mapping := input.Document.Frontmatter["on"].(map[string]any)
		if !mapping {
			return false, nil
		}
		raw, specified := on["roles"]
		if !specified {
			return false, nil
		}
		if role, scalar := raw.(string); scalar {
			roles = []string{role}
		} else {
			var err error
			roles, err = policyStrings(ctx, raw, "on.roles")
			if err != nil {
				return false, err
			}
		}
	}
	untrusted := false
	for _, role := range roles {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		switch role {
		case "admin", "maintainer", "maintain", "write":
		case "triage", "read", "all":
			untrusted = true
		default:
			return false, errors.New("dangerous-triggers requires on.roles to contain recognized repository roles")
		}
	}
	return untrusted, nil
}

// This correlates tool-input exposure, not event admission or arbitrary scripts.
// Missing policy is unknown: public-repository defaults and imports may filter it.
func commentIntegrityFiltering(input Input) (string, error) {
	tools := input.ResolvedTools
	if tools == nil {
		if input.Document == nil {
			return "", nil
		}
		var err error
		tools, err = policyMapping(input.Document.Frontmatter["tools"], "tools")
		if err != nil {
			return "", err
		}
	}
	if _, literal := tools["github"].(bool); literal {
		return "", nil
	}
	github, err := policyMapping(tools["github"], "tools.github")
	if err != nil {
		return "", err
	}
	if value, exists := github["lockdown"]; exists {
		lockdown, ok := value.(bool)
		if !ok {
			return "", errors.New("dangerous-triggers requires tools.github.lockdown to be a boolean")
		}
		if lockdown {
			return "GitHub integrity filtering overridden by lockdown", nil
		}
	}
	value, exists := github["min-integrity"]
	if !exists {
		return "", nil
	}
	switch value {
	case "approved", "merged":
		return "", nil
	case "none", "unapproved":
		return "a GitHub integrity threshold that admits untrusted content", nil
	default:
		return "", errors.New("dangerous-triggers requires tools.github.min-integrity to be none, unapproved, approved, or merged")
	}
}

func targetTriggerRiskAcknowledged(ctx context.Context, input Input) (bool, error) {
	if input.Document == nil {
		return false, nil
	}
	trigger, _, err := authoredTrigger(ctx, input.Document.Frontmatter["on"], "pull_request_target")
	if err != nil {
		return false, err
	}
	settings, err := policyMapping(trigger, "on.pull_request_target")
	if err != nil {
		return false, err
	}
	value, exists := settings["acknowledge-risk"]
	if !exists {
		return false, nil
	}
	acknowledged, ok := value.(bool)
	if !ok {
		return false, errors.New("dangerous-triggers requires on.pull_request_target.acknowledge-risk to be a boolean")
	}
	return acknowledged, nil
}
