package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/typeutil"
	"github.com/goccy/go-yaml"
)

var compilerActivationJobLog = logger.New("workflow:compiler_activation_job")

const (
	activationSymlinkAgentDir  = constants.GithubDir + "agents"
	activationSymlinkSkillDir  = constants.GithubDir + "skills"
	activationSymlinkPromptDir = constants.GithubDir + "prompts"
)

const pullRequestBaseSHAExpression = "(github.event_name == 'pull_request' || github.event_name == 'pull_request_review' || github.event_name == 'pull_request_review_comment') && github.event.pull_request != null && github.event.pull_request.base.sha"

var activationMetadataTriggerFields = map[string]struct{}{
	"reaction":       {},
	"status-comment": {},
	"command":        {},
	"slash_command":  {},
	"label_command":  {},
	"stop-after":     {},
	"github-token":   {},
	"github-app":     {},
}

// buildActivationJob creates the activation job that handles timestamp checking, reactions, and locking.
// This job depends on the pre-activation job if it exists, and runs before the main agent job.
func (c *Compiler) buildActivationJob(data *WorkflowData, preActivationJobCreated bool, workflowRunRepoSafety string, lockFilename string) (*Job, error) {
	ctx, err := c.newActivationJobBuildContext(data, preActivationJobCreated, workflowRunRepoSafety, lockFilename)
	if err != nil {
		return nil, fmt.Errorf("failed to create activation job build context: %w", err)
	}
	if !c.effectiveStrictMode(data.RawFrontmatter) {
		ctx.steps = append(ctx.steps, buildPolicyStrictEnforcementStep()...)
	}

	if err := c.addActivationFeedbackAndValidationSteps(ctx); err != nil {
		return nil, fmt.Errorf("failed to add activation feedback and validation steps: %w", err)
	}
	if err := c.addActivationRepositoryAndOutputSteps(ctx); err != nil {
		return nil, fmt.Errorf("failed to add activation repository and output steps: %w", err)
	}
	c.addActivationSkillInstallSteps(ctx)
	if err := c.addActivationCommandAndLabelOutputs(ctx); err != nil {
		return nil, fmt.Errorf("failed to add activation command and label outputs: %w", err)
	}
	ctx.steps = append(ctx.steps, buildRuntimeFeaturesSummaryStep()...)

	c.configureActivationNeedsAndCondition(ctx)
	compilerActivationJobLog.Print("Generating prompt in activation job")
	c.generatePromptInActivationJob(&ctx.steps, data, preActivationJobCreated, ctx.customJobsBeforeActivation)
	c.addActivationArtifactUploadStep(ctx)
	if len(ctx.steps) == 0 {
		ctx.steps = append(ctx.steps, "      - run: echo \"Activation success\"\n")
	}

	if c.actionMode.IsScript() {
		ctx.steps = append(ctx.steps, c.generateScriptModeCleanupStep())
	}

	permissions, err := c.buildActivationPermissions(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to build activation permissions: %w", err)
	}

	return &Job{
		Name:                       string(constants.ActivationJobName),
		If:                         ctx.activationCondition,
		HasWorkflowRunSafetyChecks: workflowRunRepoSafety != "",
		RunsOn:                     c.formatFrameworkJobRunsOn(data),
		Permissions:                permissions,
		Environment:                c.buildActivationEnvironment(ctx),
		Env:                        buildDailyAICActivationJobEnv(data),
		Steps:                      ctx.steps,
		Outputs:                    ctx.outputs,
		Needs:                      ctx.activationNeeds,
	}, nil
}

func addActivationInteractionPermissions(
	perms *Permissions,
	options activationInteractionPermissionsOptions,
) {
	if perms == nil {
		return
	}
	permsMap := make(map[PermissionScope]PermissionLevel)
	addActivationInteractionPermissionsMap(permsMap, options)
	for scope, level := range permsMap {
		perms.Set(scope, level)
	}
}

type activationInteractionPermissionsOptions struct {
	onSection                         string
	hasReaction                       bool
	reactionIncludesIssues            bool
	reactionIncludesPullRequests      bool
	reactionIncludesDiscussions       bool
	hasStatusComment                  bool
	statusCommentIncludesIssues       bool
	statusCommentIncludesPullRequests bool
	statusCommentIncludesDiscussions  bool
}

func addActivationInteractionPermissionsMap(
	permsMap map[PermissionScope]PermissionLevel,
	options activationInteractionPermissionsOptions,
) {
	if !options.hasReaction && !options.hasStatusComment {
		return
	}

	// Fallback for unit tests or synthetic WorkflowData instances that do not populate the "on" section.
	// Real compiled workflows always have a populated trigger section.
	if options.onSection == "" {
		compilerActivationJobLog.Print("Empty on section while computing activation permissions; using broad fallback permissions")
		addBroadActivationInteractionPermissions(permsMap, options)
		return
	}

	eventSet, eventSetParsed := activationEventSet(options.onSection)
	if !eventSetParsed {
		compilerActivationJobLog.Print("Unable to parse activation trigger events while computing permissions; using broad fallback permissions")
		addBroadActivationInteractionPermissions(permsMap, options)
		return
	}

	hasIssuesEvent := setutil.Contains(eventSet, "issues")
	hasIssueCommentEvent := setutil.Contains(eventSet, "issue_comment")
	hasPullRequestEvent := setutil.Contains(eventSet, "pull_request")
	hasPullRequestReviewCommentEvent := setutil.Contains(eventSet, "pull_request_review_comment")
	hasDiscussionEvent := setutil.Contains(eventSet, "discussion")
	hasDiscussionCommentEvent := setutil.Contains(eventSet, "discussion_comment")

	if options.hasReaction {
		// Reactions on issues, issue comments, and pull requests use issues endpoints.
		needsIssuesWriteForIssueEvents := options.reactionIncludesIssues && (hasIssuesEvent || hasIssueCommentEvent)
		needsIssuesWriteForPullRequestEvents := options.reactionIncludesPullRequests && hasPullRequestEvent
		needsIssuesWriteForReaction := needsIssuesWriteForIssueEvents || needsIssuesWriteForPullRequestEvents
		if needsIssuesWriteForReaction {
			permsMap[PermissionIssues] = PermissionWrite
		}
		// Reactions on pull requests and PR review comments require pull-requests: write.
		// issue_comment events also fire for PR comments (slash_command with events:[pull_request_comment]
		// compiles to issue_comment), so pull-requests: write is also needed when issue_comment is present.
		if options.reactionIncludesPullRequests && (hasPullRequestEvent || hasPullRequestReviewCommentEvent || hasIssueCommentEvent) {
			permsMap[PermissionPullRequests] = PermissionWrite
		}
		// Reactions on discussions use GraphQL discussion APIs.
		if options.reactionIncludesDiscussions && (hasDiscussionEvent || hasDiscussionCommentEvent) {
			permsMap[PermissionDiscussions] = PermissionWrite
		}
	}

	if options.hasStatusComment {
		// Status comments for issue and pull request related events use issue comment endpoints.
		if (options.statusCommentIncludesIssues && (hasIssuesEvent || hasIssueCommentEvent)) ||
			(options.statusCommentIncludesPullRequests && (hasPullRequestEvent || hasPullRequestReviewCommentEvent)) {
			permsMap[PermissionIssues] = PermissionWrite
		}
		// Status comments for discussions use discussion comment APIs and can be disabled via frontmatter.
		if options.statusCommentIncludesDiscussions && (hasDiscussionEvent || hasDiscussionCommentEvent) {
			permsMap[PermissionDiscussions] = PermissionWrite
		}
	}
}

func addBroadActivationInteractionPermissions(
	permsMap map[PermissionScope]PermissionLevel,
	options activationInteractionPermissionsOptions,
) {
	if !options.hasReaction && !options.hasStatusComment {
		return
	}

	needsIssuesWriteForReaction := options.hasReaction && (options.reactionIncludesIssues || options.reactionIncludesPullRequests)
	needsIssuesWriteForStatusComment := options.hasStatusComment &&
		(options.statusCommentIncludesIssues || options.statusCommentIncludesPullRequests)
	if needsIssuesWriteForReaction || needsIssuesWriteForStatusComment {
		permsMap[PermissionIssues] = PermissionWrite
	}
	if options.hasReaction && options.reactionIncludesPullRequests {
		permsMap[PermissionPullRequests] = PermissionWrite
	}
	if (options.hasReaction && options.reactionIncludesDiscussions) ||
		(options.hasStatusComment && options.statusCommentIncludesDiscussions) {
		permsMap[PermissionDiscussions] = PermissionWrite
	}
}

func shouldIncludeIssueReactions(data *WorkflowData) bool {
	if data == nil || data.ReactionIssues == nil {
		return true
	}
	return *data.ReactionIssues
}

func shouldIncludePullRequestReactions(data *WorkflowData) bool {
	if data == nil || data.ReactionPullRequests == nil {
		return true
	}
	return *data.ReactionPullRequests
}

func shouldIncludeDiscussionReactions(data *WorkflowData) bool {
	if data == nil || data.ReactionDiscussions == nil {
		return true
	}
	return *data.ReactionDiscussions
}

func shouldIncludeIssueStatusComments(data *WorkflowData) bool {
	if data == nil || data.StatusCommentIssues == nil {
		return true
	}
	return *data.StatusCommentIssues
}

func shouldIncludePullRequestStatusComments(data *WorkflowData) bool {
	if data == nil || data.StatusCommentPullRequests == nil {
		return true
	}
	return *data.StatusCommentPullRequests
}

func shouldIncludeDiscussionStatusComments(data *WorkflowData) bool {
	if data == nil || data.StatusCommentDiscussions == nil {
		return true
	}
	return *data.StatusCommentDiscussions
}

func activationEventSet(onSection string) (map[string]struct {
}, bool) {
	events := make(map[string]struct {
	})
	var onData map[string]any
	if err := yaml.Unmarshal([]byte(onSection), &onData); err != nil {
		compilerActivationJobLog.Printf("Failed to parse on section for activation permission scoping: %v", err)
		return events, false
	}

	onValue, hasOn := onData["on"]
	if !hasOn {
		compilerActivationJobLog.Print("No top-level on key found while parsing activation permission events")
		return events, false
	}

	switch v := onValue.(type) {
	case string:
		events[v] = struct {
		}{}
	case []any:
		for _, item := range v {
			if eventName, ok := item.(string); ok {
				events[eventName] = struct {
				}{}
			}
		}
	case map[string]any:
		for eventName := range v {
			if isActivationMetadataTriggerField(eventName) {
				continue
			}
			events[eventName] = struct {
			}{}
		}
	default:
		compilerActivationJobLog.Printf("Unsupported on section type for activation permission scoping: %T", onValue)
		return events, false
	}

	return events, true
}

func isActivationMetadataTriggerField(eventName string) bool {
	_, isMetadataField := activationMetadataTriggerFields[eventName]
	return isMetadataField
}

// buildCentralizedCommandOnSection builds a synthetic "on" YAML section from command events.
// Centralized slash_command workflows compile to workflow_dispatch, so their activation job
// does not expose the original event triggers. This helper reconstructs the equivalent GitHub
// event names to allow addActivationInteractionPermissionsMap to compute the right permissions.
func buildCentralizedCommandOnSection(commandEvents []string) string {
	filteredEvents := FilterCommentEvents(commandEvents)
	// Map to actual GitHub event names and deduplicate.
	eventSet := make(map[string]struct {
	})
	for _, mapping := range filteredEvents {
		eventSet[GetActualGitHubEventName(mapping.EventName)] = struct {
		}{}
	}
	if len(eventSet) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("on:\n")
	// Derive ordering from GetAllCommentEvents to stay consistent with the rest of the codebase.
	seen := make(map[string]struct {
	})
	for _, mapping := range GetAllCommentEvents() {
		name := GetActualGitHubEventName(mapping.EventName)
		if setutil.Contains(eventSet, name) && !setutil.Contains(seen, name) {
			seen[name] = struct {
			}{}
			b.WriteString("  " + name + ":\n    types: [created]\n")
		}
	}
	return b.String()
}

// generatePromptInActivationJob generates the prompt creation steps and adds them to the activation job
// This creates the prompt.txt file that will be uploaded as an artifact and downloaded by the agent job
// beforeActivationJobs is the list of custom job names that run before (i.e., are dependencies of) activation.
// Passing nil or an empty slice means no custom jobs run before activation; expressions referencing any
// custom job will be filtered out of the substitution step to avoid actionlint errors.
func (c *Compiler) generatePromptInActivationJob(steps *[]string, data *WorkflowData, preActivationJobCreated bool, beforeActivationJobs []string) {
	compilerActivationJobLog.Print("Generating prompt steps in activation job")

	// Use a string builder to collect the YAML
	var yaml strings.Builder

	// Call the existing generatePrompt method to get all the prompt steps
	c.generatePrompt(&yaml, data, preActivationJobCreated, beforeActivationJobs)

	// Append the generated YAML content as a single string to steps
	yamlContent := yaml.String()
	*steps = append(*steps, yamlContent)

	compilerActivationJobLog.Print("Prompt generation steps added to activation job")
}

// generateResolveHostRepoStep generates a step that resolves the platform (host) repository
// for the activation job checkout using the job.workflow_* context fields.
//
// job.workflow_repository provides the owner/repo of the currently executing workflow file,
// correctly identifying the platform repo in all relay patterns (cross-repo workflow_call,
// event-driven relays like on: issue_comment, on: push, and cross-org scenarios).
//
// The step emits two distinct ref outputs:
//   - target_checkout_ref: the immutable commit SHA from job.workflow_sha, used by
//     actions/checkout to pin the activation checkout to the exact executing revision.
//   - target_ref: the branch/tag ref parsed from job.workflow_ref (e.g. refs/heads/main),
//     used by dispatch_workflow safe outputs as the dispatch ref. The GitHub workflow
//     dispatch API only accepts branch/tag refs, not commit SHAs.
func (c *Compiler) generateResolveHostRepoStep(data *WorkflowData) string {
	var step strings.Builder
	step.WriteString("      - name: Resolve host repo for activation checkout\n")
	step.WriteString("        id: resolve-host-repo\n")
	fmt.Fprintf(&step, "        uses: %s\n", getCachedActionPin("actions/github-script", data))
	step.WriteString("        env:\n")
	step.WriteString("          JOB_WORKFLOW_REPOSITORY: ${{ job.workflow_repository }}\n")
	step.WriteString("          JOB_WORKFLOW_SHA: ${{ job.workflow_sha }}\n")
	step.WriteString("          JOB_WORKFLOW_REF: ${{ job.workflow_ref }}\n")
	step.WriteString("          JOB_WORKFLOW_FILE_PATH: ${{ job.workflow_file_path }}\n")
	step.WriteString("        with:\n")
	step.WriteString("          script: |\n")
	step.WriteString(generateGitHubScriptWithRequire("resolve_host_repo.cjs"))
	return step.String()
}

// generateCheckoutGitHubFolderForActivation generates the checkout step for .github and .agents folders
// specifically for the activation job. Unlike generateCheckoutGitHubFolder, this method doesn't skip
// the checkout when the agent job will have a full repository checkout, because the activation job
// runs before the agent job and needs independent access to workflow files for runtime imports during
// prompt generation.
func (c *Compiler) generateCheckoutGitHubFolderForActivation(data *WorkflowData) []string {
	// Check if action-tag is specified - if so, skip checkout
	if activationCheckoutDisabledByActionTag(data) {
		compilerActivationJobLog.Print("Skipping .github checkout in activation: action-tag specified")
		return nil
	}

	// Note: We don't check data.Permissions for contents read access here because
	// the activation job ALWAYS gets contents:read added to its permissions (see buildActivationJob
	// around line 720). The workflow's original permissions may not include contents:read,
	// but the activation job will always have it for GitHub API access and runtime imports.
	// The agent job uses only the user-specified permissions (no automatic contents:read augmentation).

	// For workflow_call triggers, checkout the callee (platform) repository using the target_repo
	// and target_checkout_ref outputs from the resolve-host-repo step. That step uses
	// job.workflow_repository and job.workflow_sha to identify the platform repo and pin to the
	// exact commit, correctly handling all relay patterns including cross-repo and cross-org scenarios.
	// (target_checkout_ref carries the SHA; target_ref carries the dispatch-compatible branch/tag ref.)
	//
	// Skip when inlined-imports is enabled: content is embedded at compile time and no
	// runtime-import macros are used, so the callee's .md files are not needed at runtime.
	extraPaths := c.activationSparseCheckoutExtraPaths(data)

	cm := NewCheckoutManager(nil)
	activationToken := c.resolveActivationToken(data)
	if data != nil && hasWorkflowCallTrigger(data.On) && !data.InlinedImports {
		compilerActivationJobLog.Print("Adding cross-repo-aware .github checkout for workflow_call trigger")
		cm.SetCrossRepoTargetRepo("${{ steps.resolve-host-repo.outputs.target_repo }}")
		cm.SetCrossRepoTargetRef("${{ steps.resolve-host-repo.outputs.target_checkout_ref }}")
		checkoutRef := cm.GetCrossRepoTargetRef()
		if prCheckoutRef := activationCheckoutRef(data, "steps.resolve-host-repo.outputs.target_checkout_ref"); prCheckoutRef != "" {
			checkoutRef = prCheckoutRef
		}
		checkoutSteps := cm.GenerateGitHubFolderCheckoutStep(
			cm.GetCrossRepoTargetRepo(),
			checkoutRef,
			activationToken,
			c.getActionPin,
			extraPaths...,
		)
		// When no custom token is configured, GITHUB_TOKEN is scoped to the calling
		// repository and cannot read a private callee repository in cross-repo invocations
		// (e.g. nbcnews/tvOS-App calling nbcnews/.github). Add an if: condition so the
		// checkout is only attempted for same-repo invocations where GITHUB_TOKEN works.
		// For cross-repo scenarios, users can enable the checkout by configuring
		// activation-github-token or activation-github-app in the workflow frontmatter.
		if activationTokenMayUseGitHubToken(data) {
			compilerActivationJobLog.Print("Activation token may use GITHUB_TOKEN — restricting cross-repo checkout to same-repo invocations")
			checkoutSteps = addSameRepoIfConditionToSteps(checkoutSteps)
		}
		return checkoutSteps
	}

	// For activation job, sparse checkout .github, .agents, and engine-specific config directories
	// (plus actions/setup in dev mode). Root instruction files are excluded as they are not needed
	// during activation. sparse-checkout-cone-mode: true ensures subdirectories are recursively included.
	compilerActivationJobLog.Print("Adding .github, .agents, and engine-specific dirs to sparse checkout for activation job")
	return cm.GenerateGitHubFolderCheckoutStep("", activationCheckoutRef(data, "github.sha"), activationToken, c.getActionPin, extraPaths...)
}

// activationCheckoutDisabledByActionTag reports whether action-tag supplies setup
// assets outside the workspace, making the activation sparse checkout unnecessary.
func activationCheckoutDisabledByActionTag(data *WorkflowData) bool {
	if data == nil || data.Features == nil {
		return false
	}
	actionTagVal, exists := data.Features["action-tag"]
	actionTagStr, ok := actionTagVal.(string)
	return exists && ok && actionTagStr != ""
}

// activationSparseCheckoutExtraPaths returns additional activation-job sparse checkout
// paths beyond the default .github and .agents directories.
func (c *Compiler) activationSparseCheckoutExtraPaths(data *WorkflowData) []string {
	var extraPaths []string
	if c.actionMode.IsDev() {
		compilerActivationJobLog.Print("Dev mode: adding actions/setup to sparse-checkout to preserve local action post step")
		extraPaths = append(extraPaths, "actions/setup")
	}

	defaultSparseCheckoutDirs := map[string]struct{}{".github": {}, ".agents": {}}
	for _, folder := range c.engineRegistry.GetAllAgentManifestFolders() {
		if !setutil.Contains(defaultSparseCheckoutDirs, folder) {
			extraPaths = append(extraPaths, folder)
		}
	}
	for _, folder := range localSkillSparseCheckoutTopLevelDirs(data) {
		if !setutil.Contains(defaultSparseCheckoutDirs, folder) {
			extraPaths = append(extraPaths, folder)
		}
	}
	if data != nil {
		for _, folder := range data.AmbientFolders {
			if !setutil.Contains(defaultSparseCheckoutDirs, folder) {
				extraPaths = append(extraPaths, folder)
			}
		}
	}
	compilerActivationJobLog.Printf("Adding %d engine-specific dirs to sparse-checkout: %v", len(extraPaths), extraPaths)

	repoRoot := c.gitRoot
	if repoRoot == "" {
		if cwd, err := os.Getwd(); err == nil {
			repoRoot = cwd
		}
	}
	return resolveSymlinkExtraPaths(repoRoot, extraPaths)
}

// activationCheckoutRef returns the ref expression for activation sparse checkout.
// It returns an empty string when the workflow has no pull-request event whose payload
// includes a pull request. fallbackExpression must be a raw GitHub Actions expression
// without a ${{ }} wrapper.
func activationCheckoutRef(data *WorkflowData, fallbackExpression string) string {
	if data == nil || !onSectionHasAnyTrigger(data.On, "pull_request", "pull_request_review", "pull_request_review_comment") {
		return ""
	}
	return fmt.Sprintf("${{ %s || %s }}", pullRequestBaseSHAExpression, fallbackExpression)
}

// onSectionHasAnyTrigger reports whether a rendered on: YAML section contains a trigger.
// Structured parsing provides exact key matching for security-sensitive triggers,
// unlike the legacy substring check used by hasWorkflowCallTrigger.
func onSectionHasAnyTrigger(onSection string, triggers ...string) bool {
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(onSection), &parsed); err != nil {
		return false
	}
	if onValue, ok := parsed["on"]; ok {
		for _, trigger := range triggers {
			if frontmatterHasTrigger(onValue, trigger) {
				return true
			}
		}
		return false
	}
	for _, trigger := range triggers {
		if frontmatterHasTrigger(parsed, trigger) {
			return true
		}
	}
	return false
}

func localSkillSparseCheckoutTopLevelDirs(data *WorkflowData) []string {
	if data == nil {
		return nil
	}
	refs := append([]SkillReference(nil), data.SkillReferences...)
	if len(refs) == 0 && len(data.Skills) > 0 {
		refs = make([]SkillReference, 0, len(data.Skills))
		for _, skill := range data.Skills {
			skill = strings.TrimSpace(skill)
			if skill == "" {
				continue
			}
			refs = append(refs, SkillReference{Skill: skill})
		}
	}
	if len(refs) == 0 {
		return nil
	}

	seen := map[string]struct{}{}
	result := make([]string, 0, len(refs))
	for _, ref := range refs {
		spec := strings.TrimSpace(ref.Skill)
		if !parseSkillRefSpec(spec).isLocal {
			continue
		}
		normalized := strings.TrimPrefix(strings.ReplaceAll(spec, "\\", "/"), "./")
		if normalized == "" {
			continue
		}
		parts := strings.Split(normalized, "/")
		invalid := false
		topLevel := ""
		for i, part := range parts {
			if part == "" || part == "." || part == ".." {
				invalid = true
				break
			}
			if i == 0 {
				topLevel = part
			}
		}
		if invalid {
			continue
		}

		if topLevel == "" {
			continue
		}
		if _, ok := seen[topLevel]; ok {
			continue
		}
		seen[topLevel] = struct{}{}
		result = append(result, topLevel)
	}

	return result
}

// addSameRepoIfConditionToSteps injects an if: condition into each step that restricts
// execution to same-repo workflow_call invocations. This prevents checkout steps from
// failing when GITHUB_TOKEN cannot read a private callee repository in cross-repo scenarios.
func addSameRepoIfConditionToSteps(steps []string) []string {
	const sameRepoCondition = "steps.resolve-host-repo.outputs.target_repo == github.repository"
	result := make([]string, 0, len(steps))
	for _, step := range steps {
		result = append(result, injectIfConditionAfterName(step, sameRepoCondition))
	}
	return result
}

// activationTokenMayUseGitHubToken reports whether activation authentication can
// resolve to the repository-scoped GITHUB_TOKEN at runtime.
func activationTokenMayUseGitHubToken(data *WorkflowData) bool {
	if data.ActivationGitHubApp != nil {
		return data.ActivationGitHubApp.shouldIgnoreMissingKey()
	}
	return data.ActivationGitHubToken == "" || data.ActivationGitHubToken == "${{ secrets.GITHUB_TOKEN }}"
}

// injectIfConditionAfterName inserts an "if:" field immediately after the "- name:"
// line of a YAML step string. The field indentation is derived from the step's existing
// content so this remains stable if the step formatter changes indentation.
// Returns the step unchanged if a "- name:" line cannot be found, and is idempotent
// (does nothing if an "if:" field is already present).
func injectIfConditionAfterName(step, condition string) string {
	lines := strings.Split(step, "\n")

	// Find the "- name:" line
	nameLineIdx := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "- name:") {
			nameLineIdx = i
			break
		}
	}
	if nameLineIdx < 0 || nameLineIdx >= len(lines) {
		compilerActivationJobLog.Printf("Warning: could not inject if-condition %q — step has no '- name:' line: %q", condition, step)
		return step
	}

	// Idempotency: don't inject if an "if:" field is already present
	for _, line := range lines[nameLineIdx+1:] {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "if:") {
			return step
		}
	}

	// Derive the field indentation from the first non-empty line after "- name:"
	fieldIndent := ""
	for _, line := range lines[nameLineIdx+1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fieldIndent = line[:len(line)-len(strings.TrimLeft(line, " "))]
		break
	}
	if fieldIndent == "" {
		// Fall back: indent = name-line indent + 2 spaces
		nameLine := lines[nameLineIdx]
		nameIndent := nameLine[:len(nameLine)-len(strings.TrimLeft(nameLine, " "))]
		fieldIndent = nameIndent + "  "
	}

	newLines := make([]string, 0, typeutil.SafeAllocationCapacity(len(lines), 1))
	newLines = append(newLines, lines[:nameLineIdx+1]...)
	newLines = append(newLines, fieldIndent+"if: "+condition)
	newLines = append(newLines, lines[nameLineIdx+1:]...)
	return strings.Join(newLines, "\n")
}

// resolveSymlinkExtraPaths inspects well-known .github sub-paths that are commonly
// symlinked (e.g. .github/agents -> ../.ai/agents). When a path is a symlink, the
// resolved target directory (relative to the repo root) is appended to extraPaths so
// that the sparse-checkout step fetches the target files, not just the dangling symlink
// blob. Symlink targets that escape the repository root are silently ignored to prevent
// path traversal. Already-present paths are not duplicated.
// repoRoot must be an absolute path to the repository root; when empty, the function is a no-op.
func resolveSymlinkExtraPaths(repoRoot string, extraPaths []string) []string {
	if repoRoot == "" {
		return extraPaths
	}
	candidates := []string{
		activationSymlinkAgentDir,
		activationSymlinkSkillDir,
		activationSymlinkPromptDir,
	}
	existing := make(map[string]struct{}, len(extraPaths))
	for _, p := range extraPaths {
		existing[p] = struct{}{}
	}
	for _, candidate := range candidates {
		absCandidate := filepath.Join(repoRoot, candidate)
		info, err := os.Lstat(absCandidate)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			continue // not a symlink or doesn't exist
		}
		target, err := os.Readlink(absCandidate)
		if err != nil {
			continue
		}
		// Resolve target to an absolute path.
		// When the symlink target is absolute (e.g. /etc/passwd), use it directly.
		// When relative, resolve it against the directory that contains the symlink.
		var absResolved string
		if filepath.IsAbs(target) {
			absResolved = filepath.Clean(target)
		} else {
			absResolved = filepath.Clean(filepath.Join(filepath.Dir(absCandidate), target))
		}
		rel, err := filepath.Rel(repoRoot, absResolved)
		if err != nil {
			continue
		}
		// Reject paths that escape the repository root using a segment-aware check.
		// strings.HasPrefix would incorrectly reject names like "..foo".
		firstSeg := rel
		if i := strings.IndexByte(rel, filepath.Separator); i >= 0 {
			firstSeg = rel[:i]
		}
		if firstSeg == ".." {
			compilerActivationJobLog.Printf("Ignoring symlink %s -> %s: resolved path %q escapes the repository root", candidate, target, rel)
			continue
		}
		// Normalize to forward slashes for use in YAML sparse-checkout paths.
		rel = filepath.ToSlash(rel)
		if _, alreadyPresent := existing[rel]; alreadyPresent {
			continue
		}
		compilerActivationJobLog.Printf("Symlink detected: %s -> %s (adding %s to sparse checkout)", candidate, target, rel)
		extraPaths = append(extraPaths, rel)
		existing[rel] = struct{}{}
	}
	return extraPaths
}
