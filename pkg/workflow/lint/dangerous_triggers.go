package lint

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
		triggerOn, found, err := dangerousTriggerDeclaration(ctx, input, on, name)
		if err != nil {
			return diagnostics, err
		}
		if !found {
			continue
		}
		source, path, err := dangerousTriggerOrigin(ctx, input, triggerOn, name)
		if err != nil {
			return diagnostics, err
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
				Source: source, ThreatRuleID: "CTR-008", Path: path,
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
		remediation := ""
		if filtering == "GitHub integrity filtering overridden by lockdown" {
			remediation = " Disable tools.github.lockdown before applying approved/merged filtering."
		}
		diagnostics = append(diagnostics, Diagnostic{Source: source, Path: path,
			Message: fmt.Sprintf("%s admits untrusted comment or review content with %s.%s See https://github.github.com/gh-aw/reference/workflow-linters/#privileged-triggers", name, filtering, remediation),
		})
	}
	return diagnostics, nil
}

func dangerousTriggerDeclaration(ctx context.Context, input Input, on any, name string) (any, bool, error) {
	if input.Workflow != nil && (name == "slash_command" || name == "command") {
		if input.Document == nil {
			return nil, false, nil
		}
		on = input.Document.Frontmatter["on"]
		trigger, found, err := authoredTrigger(ctx, on, name)
		if err != nil || !found {
			return nil, false, err
		}
		settings, mapped := trigger.(map[string]any)
		strategy, configured := settings["strategy"].(string)
		return on, mapped && configured && strings.EqualFold(strings.TrimSpace(strategy), "centralized"), nil
	}
	_, found, err := authoredTrigger(ctx, on, name)
	return on, found, err
}

func dangerousTriggerOrigin(ctx context.Context, input Input, on any, name string) (ASTTargets, string, error) {
	path := "on"
	if _, mapping := on.(map[string]any); mapping {
		path += "." + name
	}
	if input.Document == nil {
		return ActionsAST, path, nil
	}
	authoredOn := input.Document.Frontmatter["on"]
	if _, found, err := authoredTrigger(ctx, authoredOn, name); err != nil {
		return 0, "", err
	} else if found {
		if _, mapping := authoredOn.(map[string]any); mapping {
			return AWAST, "on." + name, nil
		}
		return AWAST, "on", nil
	}
	if name == "issue_comment" || name == "pull_request_review" || name == "pull_request_review_comment" {
		for _, command := range []string{"slash_command", "command"} {
			trigger, found, err := authoredTrigger(ctx, authoredOn, command)
			if err != nil {
				return 0, "", err
			}
			if !found {
				continue
			}
			settings, mapped := trigger.(map[string]any)
			strategy, configured := settings["strategy"].(string)
			if mapped && configured && strings.EqualFold(strings.TrimSpace(strategy), "centralized") {
				continue
			}
			events := settings["events"]
			if events == nil {
				events = []string{}
			}
			if event, scalar := events.(string); scalar {
				events = []string{event}
			}
			names, err := policyStrings(ctx, events, "on."+command+".events")
			if err != nil {
				return 0, "", err
			}
			if len(names) == 0 {
				names = []string{"issue_comment", "pull_request_review_comment"}
			}
			for _, event := range names {
				if event == name || event == "*" || (event == "pull_request_comment" && name == "issue_comment") {
					return AWAST, "on." + command, nil
				}
			}
		}
	}
	return ActionsAST, path, nil
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
	switch tools["github"].(type) {
	case bool, string:
		return "", nil
	}
	github, err := policyMapping(tools["github"], "tools.github")
	if err != nil {
		return "", err
	}
	filtering := ""
	if value, exists := github["min-integrity"]; exists {
		switch value {
		case "approved", "merged":
		case "none", "unapproved":
			filtering = "a GitHub integrity threshold that admits untrusted content"
		default:
			return "", errors.New("dangerous-triggers requires tools.github.min-integrity to be none, unapproved, approved, or merged")
		}
	}
	if value, exists := github["lockdown"]; exists {
		lockdown, ok := value.(bool)
		if !ok {
			return "", errors.New("dangerous-triggers requires tools.github.lockdown to be a boolean")
		}
		if lockdown {
			for _, field := range []string{"allowed-repos", "repos", "min-integrity", "blocked-users", "trusted-users", "approval-labels"} {
				value, configured := github[field]
				if value == nil {
					continue
				}
				if field == "blocked-users" || field == "trusted-users" || field == "approval-labels" {
					switch entries := value.(type) {
					case string:
						configured = entries != ""
					case []any:
						configured = len(entries) > 0
					case []string:
						configured = len(entries) > 0
					}
				}
				if configured {
					return "GitHub integrity filtering overridden by lockdown", nil
				}
			}
		}
	}
	return filtering, nil
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
