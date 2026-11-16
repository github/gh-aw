package lint

import (
	"context"
	"errors"
	"slices"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow/actions"
)

// untrustedRolesTrustedOnlyEvents can only be fired by actors that already have
// repository write access, so on.roles: all does not widen agent admission.
var untrustedRolesTrustedOnlyEvents = []string{"merge_group", "push", "schedule", "workflow_dispatch"}

// untrustedRolesOnSettings are gh-aw admission and activation settings in the
// on: section; they are not GitHub events. Unknown keys are treated as events
// so new triggers fail closed.
var untrustedRolesOnSettings = []string{
	"admission", "allow-bot-authored-trigger-comment", "bots", "cooldown", "github-app", "github-token",
	"labels", "manual-approval", "needs", "permissions", "reaction", "report-blocked-version",
	"restore-memory", "roles", "skip-author-associations", "skip-bots", "skip-if-check-failing",
	"skip-if-match", "skip-if-no-match", "skip-roles", "stale-check", "status-comment", "steps", "stop-after",
}

func UntrustedRolesDetectionRule() Rule {
	return Rule{
		ID: "untrusted-roles-detection", Description: "Keep threat detection enabled when on.roles: all admits untrusted actors",
		ThreatRuleID: "CTR-005",
		Severity:     Warning, Targets: AWAST, Check: checkUntrustedRolesDetection,
	}
}

func checkUntrustedRolesDetection(ctx context.Context, input Input) ([]Diagnostic, error) {
	if err := integrityInput(ctx, input); err != nil {
		return nil, err
	}
	if input.Document == nil {
		return nil, nil
	}
	on, ok := input.Document.Frontmatter["on"].(map[string]any)
	if !ok {
		return nil, nil
	}
	raw, specified := on["roles"]
	if !specified {
		return nil, nil
	}
	var roles []string
	var err error
	if role, scalar := raw.(string); scalar {
		roles = []string{role}
	} else {
		roles, err = policyStrings(ctx, raw, "on.roles")
	}
	if !slices.Contains(roles, "all") || !hasUntrustedTrigger(on) {
		return nil, err
	}
	disabled, detectionErr := untrustedRolesDetectionDisabled(input)
	if detectionErr != nil || !disabled {
		return nil, errors.Join(err, detectionErr)
	}
	severity, severityErr := sourcePolicySeverity(input, "untrusted-roles-detection")
	if severityErr != nil {
		return nil, errors.Join(err, severityErr)
	}
	message := "roles: all lets any GitHub user trigger the agent while safe-outputs.threat-detection is disabled, so prompt-injected agent output is published without inspection; " +
		"remove threat-detection: false or restrict on.roles to trusted repository roles"
	if !authoredNVXRuntime(input) {
		message += ", and run the agent in the NVX microVM sandbox (sandbox.agent.runtime: nvx) because the default container sandbox shares the runner's /tmp and user"
	}
	return []Diagnostic{{Severity: severity, Path: "on.roles", Message: message}}, err
}

func hasUntrustedTrigger(on map[string]any) bool {
	for key := range on {
		if slices.Contains(untrustedRolesOnSettings, key) || slices.Contains(untrustedRolesTrustedOnlyEvents, key) {
			continue
		}
		return true
	}
	return false
}

// untrustedRolesDetectionDisabled prefers the compiled graph, which reflects
// imported safe-outputs, and falls back to explicit authored disablement.
func untrustedRolesDetectionDisabled(input Input) (bool, error) {
	if input.Workflow != nil {
		if hasWorkflowJob(input.Workflow, string(constants.DetectionJobName)) {
			return false, nil
		}
		if hasWorkflowJob(input.Workflow, string(constants.SafeOutputsJobName)) {
			return true, nil
		}
	}
	outputs, err := policyMapping(input.Document.Frontmatter["safe-outputs"], "safe-outputs")
	if err != nil || outputs == nil {
		return false, err
	}
	if detection, ok := outputs["threat-detection"].(map[string]any); ok {
		if enabled, literal := detection["enabled"].(bool); literal && !enabled {
			return true, nil
		}
	}
	return customJobDetectionDisabled(Input{Document: input.Document})
}

func hasWorkflowJob(workflow *actions.Workflow, id string) bool {
	return slices.ContainsFunc(workflow.Jobs, func(job *actions.Job) bool {
		return job != nil && string(job.ID) == id
	})
}

func authoredNVXRuntime(input Input) bool {
	sandbox, ok := input.Document.Frontmatter["sandbox"].(map[string]any)
	if !ok {
		return false
	}
	agent, ok := sandbox["agent"].(map[string]any)
	if !ok {
		return false
	}
	runtime, ok := agent["runtime"].(string)
	return ok && runtime == "nvx"
}
