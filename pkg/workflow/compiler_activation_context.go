package workflow

import (
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

// compiler_activation_context contains activation job build context initialization and engine-resolution helpers.

// activationJobBuildContext carries mutable state while composing the activation job.
// It is created once by newActivationJobBuildContext, then incrementally mutated by
// helper methods in buildActivationJob, and discarded after the final Job is assembled.
type activationJobBuildContext struct {
	data                     *WorkflowData
	preActivationJob         bool
	workflowRunRepoSafety    string
	lockFilename             string
	steps                    []string
	outputs                  map[string]string
	engine                   CodingAgentEngine
	hasReaction              bool
	reactionIssues           bool
	reactionPullRequests     bool
	reactionDiscussions      bool
	hasStatusComment         bool
	statusCommentIssues      bool
	statusCommentPRs         bool
	statusCommentDiscussions bool
	hasLabelCommand          bool
	shouldRemoveLabel        bool
	filteredLabelEvents      []string
	needsAppTokenForAccess   bool

	customJobsBeforeActivation []string
	activationNeeds            []string
	activationCondition        string

	// activationAllScripts holds the `run` scripts extracted from jobs.activation setup,
	// pre, and activation steps, cached to avoid repeated extraction.
	activationAllScripts []string
	// activationInferredPerms holds the permissions inferred from activationAllScripts,
	// cached here to avoid repeated inference.
	activationInferredPerms map[PermissionScope]PermissionLevel
}

// resolveActivationEngineID resolves the workflow engine for activation-time paths,
// defaulting to the repository-wide default engine when frontmatter leaves it unset.
// This keeps skill installation and activation artifact uploads on the same engine-specific directory.
func resolveActivationEngineID(workflowData *WorkflowData) string {
	engineID := strings.TrimSpace(ResolveEngineID(workflowData))
	if engineID == "" {
		return string(constants.DefaultEngine)
	}
	return engineID
}

// newActivationJobBuildContext initializes activation-job state with setup, aw_info, and base outputs.
func (c *Compiler) newActivationJobBuildContext(
	data *WorkflowData,
	preActivationJobCreated bool,
	workflowRunRepoSafety string,
	lockFilename string,
) (*activationJobBuildContext, error) {
	compilerActivationJobLog.Printf("Initializing activation job build context: pre_activation=%t, lock=%s", preActivationJobCreated, lockFilename)
	setupActionRef := c.resolveActionReference("./actions/setup", data)
	if setupActionRef == "" {
		compilerActivationJobLog.Print("Failed to resolve setup action reference for activation job")
		return nil, errors.New("failed to resolve setup action reference; ensure ./actions/setup exists and is accessible")
	}

	ctx := newActivationBuildContext(data, preActivationJobCreated, workflowRunRepoSafety, lockFilename)
	if err := cacheActivationPreStepPermissions(ctx); err != nil {
		return nil, err
	}
	c.addActivationSetupAndWorkflowCallSteps(ctx, setupActionRef)

	engine, err := c.getAgenticEngine(data.AI)
	if err != nil {
		return nil, fmt.Errorf("failed to get agentic engine: %w", err)
	}
	c.addActivationEngineOutputs(ctx, engine)

	return ctx, nil
}

func newActivationBuildContext(data *WorkflowData, preActivationJobCreated bool, workflowRunRepoSafety, lockFilename string) *activationJobBuildContext {
	ctx := &activationJobBuildContext{
		data:                     data,
		preActivationJob:         preActivationJobCreated,
		workflowRunRepoSafety:    workflowRunRepoSafety,
		lockFilename:             lockFilename,
		outputs:                  map[string]string{},
		hasReaction:              data.AIReaction != "" && data.AIReaction != "none",
		reactionIssues:           shouldIncludeIssueReactions(data),
		reactionPullRequests:     shouldIncludePullRequestReactions(data),
		reactionDiscussions:      shouldIncludeDiscussionReactions(data),
		hasStatusComment:         data.StatusComment != nil && *data.StatusComment,
		statusCommentIssues:      shouldIncludeIssueStatusComments(data),
		statusCommentPRs:         shouldIncludePullRequestStatusComments(data),
		statusCommentDiscussions: shouldIncludeDiscussionStatusComments(data),
		hasLabelCommand:          len(data.LabelCommand) > 0,
		filteredLabelEvents:      FilterLabelCommandEvents(data.LabelCommandEvents),
		needsAppTokenForAccess:   data.ActivationGitHubApp != nil && !data.StaleCheckDisabled,
	}
	ctx.shouldRemoveLabel = ctx.hasLabelCommand && data.LabelCommandRemoveLabel
	return ctx
}

func cacheActivationPreStepPermissions(ctx *activationJobBuildContext) error {
	// Cache scripts from setup/pre-steps and inferred permissions once to avoid redundant
	// extraction and inference calls in buildActivationPermissions and
	// addActivationFeedbackAndValidationSteps.
	// Activation setup/pre/regular steps are injected into the built-in activation job;
	// scan their scripts so permission inference stays aligned with emitted steps.
	activationJobName := string(constants.ActivationJobName)
	ctx.activationAllScripts = extractRunScriptsFromJobSection(ctx.data.Jobs, activationJobName, "setup-steps")
	ctx.activationAllScripts = append(ctx.activationAllScripts, extractRunScriptsFromJobSection(ctx.data.Jobs, activationJobName, "pre-steps")...)
	ctx.activationAllScripts = append(ctx.activationAllScripts, extractRunScriptsFromJobSection(ctx.data.Jobs, activationJobName, "steps")...)
	if len(ctx.activationAllScripts) > 0 {
		inferredPerms, err := inferPermissionsFromShellScripts(ctx.activationAllScripts)
		if err != nil {
			return err
		}
		ctx.activationInferredPerms = inferredPerms
	}
	return nil
}

func (c *Compiler) addActivationSetupAndWorkflowCallSteps(ctx *activationJobBuildContext, setupActionRef string) {
	ctx.steps = append(ctx.steps, c.generateCheckoutActionsFolder(ctx.data)...)
	activationSetupTraceID, activationSetupParentSpanID := buildActivationSetupParentSpans(ctx.preActivationJob)
	enableArtifactClient := hasMaxDailyAICGuardrail(ctx.data)
	artifactClientCondition := ""
	if enableArtifactClient {
		artifactClientCondition = maxDailyAICreditsConfiguredIfExpr
	}
	ctx.steps = append(ctx.steps, c.generateSetupStepWithArtifactClientCondition(
		ctx.data,
		setupActionRef,
		SetupActionDestination,
		enableArtifactClient,
		activationSetupTraceID,
		activationSetupParentSpanID,
		artifactClientCondition,
	)...)
	ctx.outputs["setup-trace-id"] = "${{ steps.setup.outputs.trace-id }}"
	ctx.outputs["setup-span-id"] = "${{ steps.setup.outputs.span-id }}"
	ctx.outputs["setup-parent-span-id"] = "${{ steps.setup.outputs.parent-span-id || steps.setup.outputs.span-id }}"
	c.addActivationWorkflowCallResolutionSteps(ctx)
}

func buildActivationSetupParentSpans(preActivationJobCreated bool) (traceID string, parentSpanID string) {
	if !preActivationJobCreated {
		return "", ""
	}
	return fmt.Sprintf("${{ needs.%s.outputs.setup-trace-id }}", constants.PreActivationJobName),
		setupParentSpanNeedsExpr(constants.PreActivationJobName)
}

func (c *Compiler) addActivationWorkflowCallResolutionSteps(ctx *activationJobBuildContext) {
	if isOTLPHeadersPresent(ctx.data) {
		ctx.steps = append(ctx.steps, generateOTLPHeadersMaskStep())
	}
	if isOTLPAttributesPresent(ctx.data) {
		ctx.steps = append(ctx.steps, generateOTLPAttributesMaskStep())
	}
	if hasWorkflowCallTrigger(ctx.data.On) && !ctx.data.InlinedImports {
		compilerActivationJobLog.Print("Adding resolve-host-repo step for workflow_call trigger")
		ctx.steps = append(ctx.steps, c.generateResolveHostRepoStep(ctx.data))
	}
	if hasWorkflowCallTrigger(ctx.data.On) {
		compilerActivationJobLog.Print("Adding artifact prefix computation step for workflow_call trigger")
		ctx.steps = append(ctx.steps, generateArtifactPrefixStep()...)
		ctx.outputs[constants.ArtifactPrefixOutputName] = "${{ steps.artifact-prefix.outputs.prefix }}"
	}
}

func (c *Compiler) addActivationEngineOutputs(ctx *activationJobBuildContext, engine CodingAgentEngine) {
	ctx.engine = engine
	compilerActivationJobLog.Print("Generating aw_info step in activation job")
	var awInfoYAML strings.Builder
	c.generateCreateAwInfo(&awInfoYAML, ctx.data, engine)
	ctx.steps = append(ctx.steps, awInfoYAML.String())
	ctx.outputs["engine_id"] = "${{ steps.generate_aw_info.outputs.engine_id }}"
	ctx.outputs["model"] = "${{ steps.generate_aw_info.outputs.model }}"
	ctx.outputs["lockdown_check_failed"] = "${{ steps.generate_aw_info.outputs.lockdown_check_failed == 'true' }}"
	ctx.outputs["oauth_token_check_failed"] = "${{ steps.check-oauth-tokens.outputs.oauth_token_check_failed == 'true' }}"
	if !ctx.data.StaleCheckDisabled {
		ctx.outputs["stale_lock_file_failed"] = "${{ steps.check-lock-file.outputs.stale_lock_file_failed == 'true' }}"
	}
	if hasWorkflowCallTrigger(ctx.data.On) && !ctx.data.InlinedImports {
		ctx.outputs["target_repo"] = "${{ steps.resolve-host-repo.outputs.target_repo }}"
		ctx.outputs["target_repo_name"] = "${{ steps.resolve-host-repo.outputs.target_repo_name }}"
		// target_ref: dispatch-compatible branch/tag ref (e.g. refs/heads/main) parsed from
		// job.workflow_ref. Used by dispatch_workflow safe outputs as the `ref` argument to
		// createWorkflowDispatch. The GitHub workflow dispatch API does not accept commit SHAs.
		ctx.outputs["target_ref"] = "${{ steps.resolve-host-repo.outputs.target_ref }}"
		// target_checkout_ref: immutable commit SHA from job.workflow_sha. Used by actions/checkout
		// in the activation job to pin to the exact executing revision.
		ctx.outputs["target_checkout_ref"] = "${{ steps.resolve-host-repo.outputs.target_checkout_ref }}"
	}
}
