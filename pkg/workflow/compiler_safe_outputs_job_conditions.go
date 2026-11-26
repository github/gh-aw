package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/setutil"
)

// getSafeOutputsHeadApp returns the first non-nil HeadGitHubApp config from
// create-pull-request or push-to-pull-request-branch handlers, used to generate
// the safe-outputs-head-app-token step.
func getSafeOutputsHeadApp(safeOutputs *SafeOutputsConfig) *GitHubAppConfig {
	if safeOutputs == nil {
		return nil
	}
	if safeOutputs.CreatePullRequests != nil && safeOutputs.CreatePullRequests.HeadGitHubApp != nil {
		return safeOutputs.CreatePullRequests.HeadGitHubApp
	}
	if safeOutputs.PushToPullRequestBranch != nil && safeOutputs.PushToPullRequestBranch.HeadGitHubApp != nil {
		return safeOutputs.PushToPullRequestBranch.HeadGitHubApp
	}
	return nil
}

// getSafeOutputsHeadRepoSlug returns the HeadRepoSlug associated with the configured
// head-github-app, used to scope the minted token to the correct fork repository.
func getSafeOutputsHeadRepoSlug(safeOutputs *SafeOutputsConfig) string {
	if safeOutputs == nil {
		return ""
	}
	if safeOutputs.CreatePullRequests != nil && safeOutputs.CreatePullRequests.HeadGitHubApp != nil {
		return safeOutputs.CreatePullRequests.HeadRepoSlug
	}
	if safeOutputs.PushToPullRequestBranch != nil && safeOutputs.PushToPullRequestBranch.HeadGitHubApp != nil {
		return safeOutputs.PushToPullRequestBranch.HeadRepoSlug
	}
	return ""
}

// headRepoNameFromSlug extracts the repository name (without owner) from an "owner/repo"
// slug for use as the fallback repositories value in app token minting.
// Returns an empty string when the slug is absent, cannot be parsed, or contains an expression.
// The expression guard uses "${{" as a conservative prefix; standard GitHub Actions expressions
// always begin with exactly this prefix so this check is sufficient in practice.
func headRepoNameFromSlug(slug string) string {
	if slug == "" {
		return ""
	}
	parts := strings.SplitN(slug, "/", 2)
	if len(parts) == 2 && !strings.Contains(parts[1], "${{") { //nolint:uncheckedsliceindex // The length check guards the indexed access.
		return parts[1] //nolint:uncheckedsliceindex // The length check above guards the indexed access.
	}
	return ""
}

// messagesContainPreActivationRef reports whether any message template in cfg
// contains a reference to a needs.pre_activation.outputs.* expression.
// When true, the safe_outputs and conclusion jobs must declare pre_activation
// in their needs so that GitHub Actions can resolve the expression at runtime.
func messagesContainPreActivationRef(cfg *SafeOutputMessagesConfig) bool {
	if cfg == nil {
		return false
	}
	preActivationOutputsRef := "needs." + string(constants.PreActivationJobName) + ".outputs."
	for _, field := range []string{
		cfg.Footer,
		cfg.FooterInstall,
		cfg.FooterWorkflowRecompile,
		cfg.FooterWorkflowRecompileComment,
		cfg.StagedTitle,
		cfg.StagedDescription,
		cfg.ActivationComments,
		cfg.RunStarted,
		cfg.RunSuccess,
		cfg.RunFailure,
		cfg.DetectionFailure,
		cfg.PullRequestCreated,
		cfg.IssueCreated,
		cfg.CommitPushed,
		cfg.AgentFailureIssue,
		cfg.AgentFailureComment,
		cfg.BodyHeader,
	} {
		if strings.Contains(field, preActivationOutputsRef) {
			return true
		}
	}
	return false
}

// buildSafeOutputsJobCondition constructs the "if" expression for the safe_outputs job.
// The job runs when the agent job completed (not skipped) and, when threat detection is enabled,
// when the detection job passed.
func buildSafeOutputsJobCondition(data *WorkflowData, threatDetectionEnabled bool) ConditionNode {
	agentNotSkipped := BuildAnd(
		&NotNode{Child: BuildFunctionCall("cancelled")},
		BuildNotEquals(
			BuildPropertyAccess(fmt.Sprintf("needs.%s.result", constants.AgentJobName)),
			BuildStringLiteral("skipped"),
		),
	)
	agentNotSkipped = requireSafeOutputsAgentSuccess(agentNotSkipped, data.SafeOutputs)
	if workQueueRequiresAssignment(data) {
		agentNotSkipped = BuildAnd(agentNotSkipped, workQueueAssignmentOpenCondition())
	}
	if IsConditionalDetection(data.SafeOutputs) {
		// When detection is expression-controlled, the detection job may be skipped at runtime
		// (expression evaluated to false). Use always() to prevent safe_outputs from being
		// skipped due to a skipped dependency, and accept both success and skipped results.
		return BuildAnd(
			BuildAnd(BuildFunctionCall("always"), agentNotSkipped),
			buildDetectionPassedCondition(),
		)
	}
	if threatDetectionEnabled {
		return BuildAnd(agentNotSkipped, buildDetectionSuccessCondition())
	}
	return agentNotSkipped
}

// buildSafeOutputsJobNeeds returns the ordered list of job names that safe_outputs depends on.
func (c *Compiler) buildSafeOutputsJobNeeds(data *WorkflowData, mainJobName string, threatDetectionEnabled bool) []string {
	// safe_outputs depends on agent; when threat detection is enabled it also
	// depends on the detection job (so that detection_success is available).
	needs := []string{mainJobName}
	if threatDetectionEnabled {
		needs = append(needs, string(constants.DetectionJobName))
		consolidatedSafeOutputsJobLog.Print("Added detection job dependency to safe_outputs job")
	}
	// Always add activation job dependency to get the trace-id for OTLP correlation,
	// and also when needed for other reasons:
	// - create_pull_request or push_to_pull_request_branch (need the activation artifact)
	// - lock-for-agent (need the activation lock)
	// - workflow_call trigger (need needs.activation.outputs.target_repo for cross-repo token/dispatch)
	needs = append(needs, string(constants.ActivationJobName))
	// Add unlock job dependency if lock-for-agent is enabled
	// This ensures the issue is unlocked before safe outputs run
	if data.LockForAgent {
		needs = append(needs, "unlock")
		consolidatedSafeOutputsJobLog.Print("Added unlock job dependency to safe_outputs job")
	}
	seenNeeds := make(map[string]struct{}, len(needs))
	for _, need := range needs {
		seenNeeds[need] = struct{}{}
	}
	if data.SafeOutputs != nil {
		for _, need := range data.SafeOutputs.Needs {
			if setutil.Contains(seenNeeds, need) {
				continue
			}
			needs = append(needs, need)
			seenNeeds[need] = struct{}{}
			consolidatedSafeOutputsJobLog.Printf("Added explicit safe-outputs needs dependency to safe_outputs job: %s", need)
		}
	}
	// If any message template references needs.pre_activation.outputs.*, add pre_activation
	// as a dependency so that GitHub Actions can resolve the expression at runtime.
	if data.SafeOutputs != nil && messagesContainPreActivationRef(data.SafeOutputs.Messages) {
		if _, exists := c.jobManager.GetJob(string(constants.PreActivationJobName)); exists {
			preActName := string(constants.PreActivationJobName)
			if !setutil.Contains(seenNeeds, preActName) {
				needs = append(needs, preActName)
				seenNeeds[preActName] = struct{}{} // keep map consistent with all other appends
				consolidatedSafeOutputsJobLog.Print("Added pre_activation dependency to safe_outputs job (messages reference pre_activation outputs)")
			}
		}
	}
	return needs
}

// headSHAExpressionForTrigger returns the GitHub Actions expression for the PR head SHA
// based on the workflow's `on:` field. Returns an empty string if the trigger type does
// not carry a directly accessible PR head SHA (e.g. push, schedule, issues).
//
// The returned expression is injected as GH_AW_HEAD_SHA in the safe_outputs job so that
// PR-review handlers automatically pin their reviews to the commit that was in place when
// the workflow triggered, preventing attribution drift if new commits land during the run.
func headSHAExpressionForTrigger(onField any) string {
	switch v := onField.(type) {
	case map[string]any:
		if _, ok := v["workflow_run"]; ok {
			return "${{ github.event.workflow_run.head_sha }}"
		}
		if _, ok := v["pull_request"]; ok {
			return "${{ github.event.pull_request.head.sha }}"
		}
		if _, ok := v["pull_request_target"]; ok {
			return "${{ github.event.pull_request.head.sha }}"
		}
	case string:
		switch v {
		case "workflow_run":
			return "${{ github.event.workflow_run.head_sha }}"
		case "pull_request", "pull_request_target":
			return "${{ github.event.pull_request.head.sha }}"
		}
	}
	return ""
}

// resolveSafeOutputsEnvironment resolves the effective GitHub deployment environment for
// safe-output jobs. If safe-outputs.environment is explicitly set, it takes precedence.
// Otherwise the top-level environment: field is propagated so that environment-scoped
// secrets are accessible in all safe-output jobs.
func resolveSafeOutputsEnvironment(data *WorkflowData) string {
	if data.SafeOutputs != nil && data.SafeOutputs.Environment != "" {
		return data.SafeOutputs.Environment
	}
	return data.Environment
}
