package workflow

import (
	"fmt"
	"slices"
	"strings"
)

// compiler_activation_permissions contains activation permission calculation and app-token minting gates.

func (c *Compiler) maybeAddActivationAppTokenMintStep(ctx *activationJobBuildContext) {
	if !activationJobNeedsAppToken(ctx) {
		compilerActivationJobLog.Print("Skipping activation app-token mint step (no reaction/status-comment/label/access/guardrail trigger requires it)")
		return
	}
	compilerActivationJobLog.Print("Adding activation app-token mint step")
	appPerms := buildActivationAppTokenPermissions(ctx)
	ctx.steps = append(ctx.steps, c.buildActivationAppTokenMintStep(ctx.data.ActivationGitHubApp, appPerms)...)
	ctx.outputs["activation_app_token_minting_failed"] = "${{ steps.activation-app-token.outcome == 'failure' }}"
}

// activationJobNeedsAppToken gates app-token minting and must stay in sync with
// buildActivationAppTokenPermissions. Any new trigger added here (reaction,
// status-comment, remove-label, repo-access, guardrail) must also add the
// corresponding permission grants there; drift causes either unnecessary minting
// or runtime 403s from missing scopes. TestActivationJobNeedsAppToken locks the
// gate behavior for these triggers.
func activationJobNeedsAppToken(ctx *activationJobBuildContext) bool {
	if ctx.data.ActivationGitHubApp == nil {
		return false
	}
	return ctx.hasReaction ||
		ctx.hasStatusComment ||
		ctx.shouldRemoveLabel ||
		ctx.needsAppTokenForAccess ||
		hasMaxDailyAICGuardrail(ctx.data)
}

func buildActivationAppTokenPermissions(ctx *activationJobBuildContext) *Permissions {
	appPerms := NewPermissions()
	addActivationAppInteractionPermissions(appPerms, ctx)
	addActivationAppLabelAndGuardrailPermissions(appPerms, ctx)
	addActivationAppInferredPermissions(appPerms, ctx)
	return appPerms
}

func addActivationAppInteractionPermissions(appPerms *Permissions, ctx *activationJobBuildContext) {
	addActivationInteractionPermissions(
		appPerms,
		activationInteractionPermissionsOptions{
			onSection:                         ctx.data.On,
			hasReaction:                       ctx.hasReaction,
			reactionIncludesIssues:            ctx.reactionIssues,
			reactionIncludesPullRequests:      ctx.reactionPullRequests,
			reactionIncludesDiscussions:       ctx.reactionDiscussions,
			hasStatusComment:                  ctx.hasStatusComment,
			statusCommentIncludesIssues:       ctx.statusCommentIssues,
			statusCommentIncludesPullRequests: ctx.statusCommentPRs,
			statusCommentIncludesDiscussions:  ctx.statusCommentDiscussions,
		},
	)
	if ctx.data.CommandCentralized && (ctx.hasReaction || ctx.hasStatusComment) {
		syntheticOn := buildCentralizedCommandOnSection(ctx.data.CommandEvents)
		if syntheticOn != "" {
			addActivationInteractionPermissions(
				appPerms,
				activationInteractionPermissionsOptions{
					onSection:                         syntheticOn,
					hasReaction:                       ctx.hasReaction,
					reactionIncludesIssues:            ctx.reactionIssues,
					reactionIncludesPullRequests:      ctx.reactionPullRequests,
					reactionIncludesDiscussions:       ctx.reactionDiscussions,
					hasStatusComment:                  ctx.hasStatusComment,
					statusCommentIncludesIssues:       ctx.statusCommentIssues,
					statusCommentIncludesPullRequests: ctx.statusCommentPRs,
					statusCommentIncludesDiscussions:  ctx.statusCommentDiscussions,
				},
			)
		}
	}
	if hasWorkflowCallTrigger(ctx.data.On) && (ctx.hasReaction || ctx.hasStatusComment) {
		addActivationInteractionPermissions(
			appPerms,
			activationInteractionPermissionsOptions{
				hasReaction:                       ctx.hasReaction,
				reactionIncludesIssues:            ctx.reactionIssues,
				reactionIncludesPullRequests:      ctx.reactionPullRequests,
				reactionIncludesDiscussions:       ctx.reactionDiscussions,
				hasStatusComment:                  ctx.hasStatusComment,
				statusCommentIncludesIssues:       ctx.statusCommentIssues,
				statusCommentIncludesPullRequests: ctx.statusCommentPRs,
				statusCommentIncludesDiscussions:  ctx.statusCommentDiscussions,
			},
		)
	}
}

func addActivationAppLabelAndGuardrailPermissions(appPerms *Permissions, ctx *activationJobBuildContext) {
	if ctx.shouldRemoveLabel {
		if slices.Contains(ctx.filteredLabelEvents, "issues") || slices.Contains(ctx.filteredLabelEvents, "pull_request") {
			appPerms.Set(PermissionIssues, PermissionWrite)
		}
		if slices.Contains(ctx.filteredLabelEvents, "discussion") {
			appPerms.Set(PermissionDiscussions, PermissionWrite)
		}
	}
	if ctx.needsAppTokenForAccess {
		appPerms.Set(PermissionContents, PermissionRead)
	}
	if hasMaxDailyAICGuardrail(ctx.data) {
		appPerms.Set(PermissionActions, PermissionRead)
	}
}

func addActivationAppInferredPermissions(appPerms *Permissions, ctx *activationJobBuildContext) {
	for scope, level := range ctx.activationInferredPerms {
		if IsGitHubAppOnlyScope(scope) {
			appPerms.Set(scope, level)
		}
	}
}

// buildActivationPermissions builds activation job permissions from workflow features and selected interactions.
// Returns an error if any activation job step section contains write gh CLI commands that would require write permissions.
func (c *Compiler) buildActivationPermissions(ctx *activationJobBuildContext) (string, error) {
	permsMap := c.buildActivationBasePermissions(ctx)
	c.addCentralizedCommandActivationPermissions(permsMap, ctx)
	c.addWorkflowCallActivationPermissions(permsMap, ctx)
	c.addActivationLabelPermissions(permsMap, ctx)
	if err := c.addActivationScriptPermissions(permsMap, ctx); err != nil {
		return "", err
	}
	compilerActivationJobLog.Printf("Computed activation job permissions across %d scope(s)", len(permsMap))
	return NewPermissionsFromMap(permsMap).RenderToYAML(), nil
}

func (c *Compiler) buildActivationBasePermissions(ctx *activationJobBuildContext) map[PermissionScope]PermissionLevel {
	permsMap := map[PermissionScope]PermissionLevel{
		PermissionContents: PermissionRead,
	}
	if !ctx.data.StaleCheckDisabled || hasMaxDailyAICGuardrail(ctx.data) || operationalValueGraderEnabled(ctx.data) {
		permsMap[PermissionActions] = PermissionRead
	}
	if isSteeringIssueEnabled(ctx.data) {
		permsMap[PermissionIssues] = PermissionWrite
	}
	if c.activationBlockedVersionIssueEnabled(ctx) {
		permsMap[PermissionIssues] = PermissionWrite
	}
	addActivationInteractionPermissionsMap(permsMap, activationInteractionPermissionsOptions{
		onSection:                         ctx.data.On,
		hasReaction:                       ctx.hasReaction,
		reactionIncludesIssues:            ctx.reactionIssues,
		reactionIncludesPullRequests:      ctx.reactionPullRequests,
		reactionIncludesDiscussions:       ctx.reactionDiscussions,
		hasStatusComment:                  ctx.hasStatusComment,
		statusCommentIncludesIssues:       ctx.statusCommentIssues,
		statusCommentIncludesPullRequests: ctx.statusCommentPRs,
		statusCommentIncludesDiscussions:  ctx.statusCommentDiscussions,
	})
	// When observability.otlp.github-app is configured without app-id/private-key
	// credentials, id-token: write is needed so the activation job can mint the OTLP
	// OIDC token via core.getIDToken(audience) (mirrors threat_detection_job.go).
	if hasOTLPGitHubOIDCAuth(ctx.data.ParsedFrontmatter, ctx.data.RawFrontmatter) {
		permsMap[PermissionIdToken] = PermissionWrite
	}
	return permsMap
}

// activationBlockedVersionIssueEnabled reports whether the activation-stage
// blocked-version check may create/update a notification issue, and is used
// to conservatively grant issues: write at compile time. It reuses
// conclusionReportFailureAsIssueEnabled, which only detects a literal "false"
// for safe-outputs.report-failure-as-issue. It intentionally does NOT honor
// that setting's category-filter arrays (ReportFailureAsIssueCategories /
// ReportFailureAsIssueExcludedCategories, e.g. report-failure-as-issue:
// ["!blocked_version"]): those categories describe failures detected by the
// downstream conclusion job and are not available this early in the
// workflow. A workflow that only wants to suppress blocked-version
// notifications must set report-failure-as-issue to a literal false, or set
// on.report-blocked-version: false (see ReportBlockedVersionDisabled), which
// is a narrower toggle that only affects this notification and does not
// disable check-for-updates or its hard failure.
func (c *Compiler) activationBlockedVersionIssueEnabled(ctx *activationJobBuildContext) bool {
	return !ctx.data.UpdateCheckDisabled && !ctx.data.ReportBlockedVersionDisabled && IsReleasedVersion(c.version) && conclusionReportFailureAsIssueEnabled(ctx.data)
}

// activationBlockedVersionReportAsIssueValue returns the templatable
// report-failure-as-issue value to embed in the blocked-version check step's
// environment. Unlike activationBlockedVersionIssueEnabled (used to
// conservatively grant issues: write at compile time), this preserves runtime
// expressions (e.g. "${{ inputs.report-failure-as-issue }}") instead of
// collapsing them to a compile-time boolean, so the value is only resolved
// once GitHub Actions evaluates the expression at runtime. Defaults to "true"
// when report-failure-as-issue is unset. When on.report-blocked-version: false
// is set in frontmatter, this always returns a literal "false", overriding any
// report-failure-as-issue value, since that flag is a dedicated off-switch for
// this notification.
func activationBlockedVersionReportAsIssueValue(ctx *activationJobBuildContext) *string {
	if ctx.data.ReportBlockedVersionDisabled {
		v := "false"
		return &v
	}
	if ctx.data.SafeOutputs == nil || ctx.data.SafeOutputs.ReportFailureAsIssue == nil {
		v := "true"
		return &v
	}
	v := ctx.data.SafeOutputs.ReportFailureAsIssue.String()
	return &v
}

func (c *Compiler) addCentralizedCommandActivationPermissions(permsMap map[PermissionScope]PermissionLevel, ctx *activationJobBuildContext) {
	// For centralized slash_command workflows, the compiled "on" section only contains
	// workflow_dispatch, so addActivationInteractionPermissionsMap above cannot detect the
	// original event types and skips write permissions. Supplement with a synthetic section
	// built from the declared command events so reactions and status-comments work correctly.
	if ctx.data.CommandCentralized && (ctx.hasReaction || ctx.hasStatusComment) {
		syntheticOn := buildCentralizedCommandOnSection(ctx.data.CommandEvents)
		if syntheticOn != "" {
			addActivationInteractionPermissionsMap(permsMap, activationInteractionPermissionsOptions{
				onSection:                         syntheticOn,
				hasReaction:                       ctx.hasReaction,
				reactionIncludesIssues:            ctx.reactionIssues,
				reactionIncludesPullRequests:      ctx.reactionPullRequests,
				reactionIncludesDiscussions:       ctx.reactionDiscussions,
				hasStatusComment:                  ctx.hasStatusComment,
				statusCommentIncludesIssues:       ctx.statusCommentIssues,
				statusCommentIncludesPullRequests: ctx.statusCommentPRs,
				statusCommentIncludesDiscussions:  ctx.statusCommentDiscussions,
			})
		}
	}
}

// addWorkflowCallActivationPermissions supplements the activation job's permission map when the
// workflow is triggered via workflow_call (i.e. it is used as a reusable workflow).
//
// At compile time it is impossible to know which GitHub event will fire in the *calling* workflow,
// so the compiler cannot restrict permissions to a specific event type (e.g. "issues" or
// "pull_request"). Instead it falls back to the broad permission set: all permission scopes that
// the configured reactions / status-comments could ever need are granted, respecting the per-type
// opt-out flags (reaction.issues, reaction.pull-requests, etc.).
//
// Because the caller event type is unknown at compile time, this path always uses the broad
// fallback (addBroadActivationInteractionPermissions) instead of event-aware trigger parsing.
func (c *Compiler) addWorkflowCallActivationPermissions(permsMap map[PermissionScope]PermissionLevel, ctx *activationJobBuildContext) {
	if !hasWorkflowCallTrigger(ctx.data.On) {
		return
	}
	if !ctx.hasReaction && !ctx.hasStatusComment {
		return
	}
	compilerActivationJobLog.Print("workflow_call trigger detected; applying broad interaction permissions for reactions/status-comments")
	addBroadActivationInteractionPermissions(permsMap, activationInteractionPermissionsOptions{
		hasReaction:                       ctx.hasReaction,
		reactionIncludesIssues:            ctx.reactionIssues,
		reactionIncludesPullRequests:      ctx.reactionPullRequests,
		reactionIncludesDiscussions:       ctx.reactionDiscussions,
		hasStatusComment:                  ctx.hasStatusComment,
		statusCommentIncludesIssues:       ctx.statusCommentIssues,
		statusCommentIncludesPullRequests: ctx.statusCommentPRs,
		statusCommentIncludesDiscussions:  ctx.statusCommentDiscussions,
	})
}

func (c *Compiler) addActivationLabelPermissions(permsMap map[PermissionScope]PermissionLevel, ctx *activationJobBuildContext) {
	if ctx.data.LockForAgent {
		permsMap[PermissionIssues] = PermissionWrite
	}
	if ctx.shouldRemoveLabel && ctx.data.ActivationGitHubApp == nil {
		if slices.Contains(ctx.filteredLabelEvents, "issues") || slices.Contains(ctx.filteredLabelEvents, "pull_request") {
			permsMap[PermissionIssues] = PermissionWrite
		}
		if slices.Contains(ctx.filteredLabelEvents, "discussion") {
			permsMap[PermissionDiscussions] = PermissionWrite
		}
	}
}

func (c *Compiler) addActivationScriptPermissions(permsMap map[PermissionScope]PermissionLevel, ctx *activationJobBuildContext) error {
	// Infer permissions required by gh CLI calls in jobs.activation step sections
	// (pre-steps, steps, post-steps). This ensures that user-defined steps that call
	// `gh pr diff`, `gh issue view`, etc. get the permissions they need without requiring
	// manual permission declarations.
	// Scripts and inferred permissions are cached in ctx to avoid redundant computation.
	if len(ctx.activationAllScripts) > 0 {
		// Detect write commands first — these are not permitted in the activation job
		// because it intentionally operates with read-only permissions.
		writeCmds, err := detectWriteCommandsInShellScripts(ctx.activationAllScripts)
		if err != nil {
			return err
		}
		if len(writeCmds) > 0 {
			compilerActivationJobLog.Printf("Rejecting activation job: %d write gh command(s) detected in activation step scripts", len(writeCmds))
			return fmt.Errorf(
				"activation job uses write gh command(s) [%s]; write operations are not permitted in activation job steps because the activation job runs with read-only permissions. Move write operations to the agent job steps or use safe-outputs. See: https://github.github.com/gh-aw/reference/safe-outputs/",
				strings.Join(writeCmds, ", "),
			)
		}
		for scope, level := range ctx.activationInferredPerms {
			if _, exists := permsMap[scope]; !exists {
				permsMap[scope] = level
			}
		}
	}
	return nil
}
