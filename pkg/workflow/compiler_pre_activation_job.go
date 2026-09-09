package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/stringutil"
)

var compilerActivationJobsLog = logger.New("workflow:compiler_activation_jobs")

// buildPreActivationJob creates a unified pre-activation job that combines membership checks and stop-time validation.
// This job exposes a single "activated" output that indicates whether the workflow should proceed.
func (c *Compiler) buildPreActivationJob(data *WorkflowData, needsPermissionCheck bool) (*Job, error) {
	compilerActivationJobsLog.Printf("Building pre-activation job: needsPermissionCheck=%v, hasStopTime=%v", needsPermissionCheck, data.StopTime != "")

	// Extract custom steps and outputs from jobs.pre-activation if present.
	customSteps, customOutputs, err := c.extractPreActivationCustomFields(data.Jobs)
	if err != nil {
		return nil, fmt.Errorf("failed to extract pre-activation custom fields (check that jobs.pre_activation and jobs.activation only use 'steps', 'outputs', and 'pre-steps' fields): %w", err)
	}

	setupActionRef := c.resolveActionReference("./actions/setup", data)
	if setupActionRef == "" {
		return nil, errors.New("setup action reference is required but could not be resolved")
	}

	steps, permissions := c.buildPreActivationPermissions(data, setupActionRef)
	steps = c.buildPreActivationCheckSteps(data, steps, needsPermissionCheck)

	// Emit a single unified GitHub App token mint step if on.github-app is configured
	// and any skip-if check is present. Both checks share the same minted token.
	hasSkipIfCheck := data.SkipIfMatch != nil || data.SkipIfNoMatch != nil
	if hasSkipIfCheck && data.ActivationGitHubApp != nil {
		steps = append(steps, c.buildPreActivationAppTokenMintStep(data.ActivationGitHubApp)...)
	}

	// Resolve the token expression to use for skip-if checks (app token > custom token > default).
	skipIfToken := c.resolvePreActivationSkipIfToken(data)
	steps = c.buildPreActivationSkipIfQuerySteps(data, steps, skipIfToken)
	steps = c.buildPreActivationSkipIfCheckFailingStep(data, steps)
	steps = c.buildPreActivationRolesBotsCmdSteps(data, steps)
	steps = c.buildPreActivationMemoryRestoreSteps(data, steps)
	steps, onStepIDs, err := c.injectPreActivationOnSteps(data, steps, customSteps)
	if err != nil {
		return nil, err
	}

	// Generate the activated output expression using expression builders.
	activatedExpression, err := buildPreActivationActivatedExpression(data, buildPreActivationActivatedConditions(data, needsPermissionCheck))
	if err != nil {
		return nil, err
	}

	jobIfCondition := c.applyPreActivationIfConditionGuards(data, needsPermissionCheck, c.buildPreActivationBaseIfCondition(data))
	if c.actionMode.IsScript() {
		steps = append(steps, c.generateScriptModeCleanupStep())
	}

	runsOn := c.formatFrameworkJobRunsOn(data)
	if data.OnRestoreMemory && data.DriveMemoryConfig != nil && len(data.DriveMemoryConfig.Drives) > 0 {
		runsOn = "runs-on: ubuntu-latest"
	}
	return &Job{
		Name:        string(constants.PreActivationJobName),
		If:          jobIfCondition,
		RunsOn:      runsOn,
		Environment: c.indentYAMLLines(resolveSafeOutputsEnvironment(data), "    "),
		Permissions: permissions,
		Steps:       steps,
		Outputs:     buildPreActivationJobOutputs(data, activatedExpression, onStepIDs, customOutputs),
		Needs:       sliceutil.Deduplicate(data.OnNeeds),
	}, nil
}

func (c *Compiler) buildPreActivationPermissions(data *WorkflowData, setupActionRef string) ([]string, string) {
	// Add setup step to copy activation scripts (required - no inline fallback).
	// For dev mode (local action path), checkout the actions folder first.
	// This requires contents: read permission.
	steps := c.generateCheckoutActionsFolder(data)
	needsContentsRead := (c.actionMode.IsDev() || c.actionMode.IsScript()) && len(steps) > 0

	// Pre-activation job doesn't need project support (no safe outputs processed here).
	// Pre-activation generates the root trace ID; activation will reuse it via setup-trace-id output.
	steps = append(steps, c.generateSetupStep(data, setupActionRef, SetupActionDestination, false, "", "")...)

	var perms *Permissions
	if needsContentsRead {
		perms = NewPermissionsContentsRead()
	}
	// Add actions: read permission if run history checks are configured.
	if data.RateLimit != nil || data.Cooldown > 0 {
		if perms == nil {
			perms = NewPermissions()
		}
		perms.Set(PermissionActions, PermissionRead)
	}
	// Auto-grant pull-requests: read when label_command uses decentralized strategy
	// with pull_request events. The check_membership.cjs script calls the pulls API
	// to verify PR provenance, which requires pull-requests: read.
	if data.LabelCommandDecentralized && slices.Contains(FilterLabelCommandEvents(data.LabelCommandEvents), "pull_request") {
		if perms == nil {
			perms = NewPermissions()
		}
		perms.Set(PermissionPullRequests, PermissionRead)
	}
	// Merge on.permissions into the pre-activation job permissions.
	// on.permissions lets users declare extra scopes required by their on.steps steps.
	if data.OnPermissions != nil {
		if perms == nil {
			perms = NewPermissions()
		}
		perms.Merge(data.OnPermissions)
	}
	if perms == nil {
		return steps, ""
	}
	return steps, perms.RenderToYAML()
}

func (c *Compiler) buildPreActivationCheckSteps(data *WorkflowData, steps []string, needsPermissionCheck bool) []string {
	// For command workflows, run command position check before the membership check.
	// check_membership uses an if: condition that requires command_position_ok == 'true',
	// so the command check must execute first. This prevents a confusing "access denied"
	// warning from appearing when an issue is opened without the slash command.
	if len(data.Command) > 0 {
		steps = c.appendPreActivationCommandPositionStep(data, steps)
	}
	if needsPermissionCheck {
		steps = c.generateMembershipCheck(data, steps)
	}
	if data.RateLimit != nil {
		steps = c.generateRateLimitCheck(data, steps)
	}
	if data.Cooldown > 0 {
		steps = c.generateCooldownCheck(data, steps)
	}
	if data.StopTime == "" {
		return steps
	}

	compilerActivationJobsLog.Printf("Adding stop-time check step: stop_time=%s", data.StopTime)
	steps = append(steps, "      - name: Check stop-time limit\n")
	steps = append(steps, fmt.Sprintf("        id: %s\n", constants.CheckStopTimeStepID))
	steps = append(steps, fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)))
	steps = append(steps, "        env:\n")
	steps = append(steps, fmt.Sprintf("          GH_AW_STOP_TIME: %q\n", stringutil.StripANSI(data.StopTime)))
	steps = append(steps, fmt.Sprintf("          GH_AW_WORKFLOW_NAME: %q\n", data.Name))
	steps = append(steps, "        with:\n")
	steps = append(steps, "          script: |\n")
	return append(steps, generateGitHubScriptWithRequire("check_stop_time.cjs"))
}

func (c *Compiler) buildPreActivationSkipIfQuerySteps(data *WorkflowData, steps []string, skipIfToken string) []string {
	if data.SkipIfMatch != nil {
		compilerActivationJobsLog.Printf("Adding skip-if-match check step: query=%s, max=%d", data.SkipIfMatch.Query, data.SkipIfMatch.Max)
		steps = c.appendPreActivationSkipIfMatchStep(data, steps, skipIfToken)
	}
	if data.SkipIfNoMatch != nil {
		compilerActivationJobsLog.Printf("Adding skip-if-no-match check step: query=%s, min=%d", data.SkipIfNoMatch.Query, data.SkipIfNoMatch.Min)
		steps = c.appendPreActivationSkipIfNoMatchStep(data, steps, skipIfToken)
	}
	return steps
}

func (c *Compiler) appendPreActivationSkipIfMatchStep(data *WorkflowData, steps []string, skipIfToken string) []string {
	steps = append(steps, "      - name: Check skip-if-match query\n")
	steps = append(steps, fmt.Sprintf("        id: %s\n", constants.CheckSkipIfMatchStepID))
	steps = append(steps, fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)))
	steps = append(steps, "        env:\n")
	steps = append(steps, fmt.Sprintf("          GH_AW_SKIP_QUERY: %q\n", data.SkipIfMatch.Query))
	steps = append(steps, fmt.Sprintf("          GH_AW_WORKFLOW_NAME: %q\n", data.Name))
	steps = append(steps, fmt.Sprintf("          GH_AW_SKIP_MAX_MATCHES: \"%d\"\n", data.SkipIfMatch.Max))
	if data.SkipIfMatch.Scope != "" {
		steps = append(steps, fmt.Sprintf("          GH_AW_SKIP_SCOPE: %q\n", data.SkipIfMatch.Scope))
	}
	steps = append(steps, "        with:\n")
	if skipIfToken != "" {
		steps = append(steps, fmt.Sprintf("          github-token: %s\n", skipIfToken))
	}
	steps = append(steps, "          script: |\n")
	return append(steps, generateGitHubScriptWithRequire("check_skip_if_match.cjs"))
}

func (c *Compiler) appendPreActivationSkipIfNoMatchStep(data *WorkflowData, steps []string, skipIfToken string) []string {
	steps = append(steps, "      - name: Check skip-if-no-match query\n")
	steps = append(steps, fmt.Sprintf("        id: %s\n", constants.CheckSkipIfNoMatchStepID))
	steps = append(steps, fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)))
	steps = append(steps, "        env:\n")
	steps = append(steps, fmt.Sprintf("          GH_AW_SKIP_QUERY: %q\n", data.SkipIfNoMatch.Query))
	steps = append(steps, fmt.Sprintf("          GH_AW_WORKFLOW_NAME: %q\n", data.Name))
	steps = append(steps, fmt.Sprintf("          GH_AW_SKIP_MIN_MATCHES: \"%d\"\n", data.SkipIfNoMatch.Min))
	if data.SkipIfNoMatch.Scope != "" {
		steps = append(steps, fmt.Sprintf("          GH_AW_SKIP_SCOPE: %q\n", data.SkipIfNoMatch.Scope))
	}
	steps = append(steps, "        with:\n")
	if skipIfToken != "" {
		steps = append(steps, fmt.Sprintf("          github-token: %s\n", skipIfToken))
	}
	steps = append(steps, "          script: |\n")
	return append(steps, generateGitHubScriptWithRequire("check_skip_if_no_match.cjs"))
}

func (c *Compiler) buildPreActivationSkipIfCheckFailingStep(data *WorkflowData, steps []string) []string {
	if data.SkipIfCheckFailing == nil {
		return steps
	}

	compilerActivationJobsLog.Printf("Adding skip-if-check-failing check step: include=%v, exclude=%v", data.SkipIfCheckFailing.Include, data.SkipIfCheckFailing.Exclude)
	steps = append(steps, "      - name: Check skip-if-check-failing\n")
	steps = append(steps, fmt.Sprintf("        id: %s\n", constants.CheckSkipIfCheckFailingStepID))
	steps = append(steps, fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)))

	cfg := data.SkipIfCheckFailing
	if len(cfg.Include) > 0 || len(cfg.Exclude) > 0 || cfg.Branch != "" || cfg.AllowPending {
		steps = append(steps, "        env:\n")
		if len(cfg.Include) > 0 {
			includeJSON, _ := json.Marshal(cfg.Include) //nolint:jsonmarshalignoredeerror // marshaling a string slice cannot fail
			steps = append(steps, fmt.Sprintf("          GH_AW_SKIP_CHECK_INCLUDE: %q\n", string(includeJSON)))
		}
		if len(cfg.Exclude) > 0 {
			excludeJSON, _ := json.Marshal(cfg.Exclude) //nolint:jsonmarshalignoredeerror // marshaling a string slice cannot fail
			steps = append(steps, fmt.Sprintf("          GH_AW_SKIP_CHECK_EXCLUDE: %q\n", string(excludeJSON)))
		}
		if cfg.Branch != "" {
			steps = append(steps, fmt.Sprintf("          GH_AW_SKIP_BRANCH: %q\n", cfg.Branch))
		}
		if cfg.AllowPending {
			steps = append(steps, "          GH_AW_SKIP_CHECK_ALLOW_PENDING: \"true\"\n")
		}
	}
	steps = append(steps, "        with:\n")
	steps = append(steps, "          script: |\n")
	return append(steps, generateGitHubScriptWithRequire("check_skip_if_check_failing.cjs"))
}

func (c *Compiler) buildPreActivationRolesBotsCmdSteps(data *WorkflowData, steps []string) []string {
	if len(data.SkipRoles) > 0 {
		steps = c.appendPreActivationSkipRolesStep(data, steps)
	}
	if len(data.SkipBots) > 0 {
		steps = c.appendPreActivationSkipBotsStep(data, steps)
	}
	// Command position step is added in buildPreActivationCheckSteps (before check_membership)
	// for command workflows so that check_membership can be made conditional on it.
	return steps
}

// buildPreActivationMemoryRestoreSteps restores memory stores before on.steps run in pre-activation.
// This is a read-only surface: it restores/loads memory data but does not emit write-back or commit steps.
func (c *Compiler) buildPreActivationMemoryRestoreSteps(data *WorkflowData, steps []string) []string {
	if len(data.OnSteps) == 0 || !data.OnRestoreMemory {
		return steps
	}

	var cacheMemorySteps strings.Builder
	generatePreActivationCacheMemoryRestoreSteps(&cacheMemorySteps, data)
	if cacheMemorySteps.Len() > 0 {
		steps = append(steps, cacheMemorySteps.String())
	}

	var driveMemorySteps strings.Builder
	c.generatePreActivationDriveMemoryRestoreSteps(&driveMemorySteps, data)
	if driveMemorySteps.Len() > 0 {
		steps = append(steps, driveMemorySteps.String())
	}

	var repoMemorySteps strings.Builder
	generateRepoMemorySteps(&repoMemorySteps, data)
	if repoMemorySteps.Len() > 0 {
		steps = append(steps, repoMemorySteps.String())
	}

	if data.CommentMemoryConfig != nil {
		if configLines, ok := c.generateCommentMemoryEarlyConfigLines(data); ok {
			steps = append(steps, strings.Join(configLines, ""))
			var commentMemorySteps strings.Builder
			commentMemorySteps.WriteString("      - name: Prepare comment memory files\n")
			fmt.Fprintf(&commentMemorySteps, "        uses: %s\n", getCachedActionPin("actions/github-script", data))
			commentMemorySteps.WriteString("        with:\n")
			fmt.Fprintf(&commentMemorySteps, "          github-token: %s\n", getEffectiveSafeOutputGitHubToken(data.CommentMemoryConfig.GitHubToken))
			commentMemorySteps.WriteString("          script: |\n")
			commentMemorySteps.WriteString("            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');\n")
			commentMemorySteps.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
			commentMemorySteps.WriteString("            const { main } = require('${{ runner.temp }}/gh-aw/actions/setup_comment_memory_files.cjs');\n")
			commentMemorySteps.WriteString("            await main();\n")
			steps = append(steps, commentMemorySteps.String())
		}
	}

	return steps
}

func (c *Compiler) generatePreActivationDriveMemoryRestoreSteps(builder *strings.Builder, data *WorkflowData) {
	if data.DriveMemoryConfig == nil || len(data.DriveMemoryConfig.Drives) == 0 {
		return
	}
	generateDriveMemorySteps(builder, driveMemoryRestoreOnlyData(data), c.getActionPin)
}

func generatePreActivationCacheMemoryRestoreSteps(builder *strings.Builder, data *WorkflowData) {
	if data.CacheMemoryConfig == nil || len(data.CacheMemoryConfig.Caches) == 0 {
		return
	}

	preActivationData := &WorkflowData{
		ParsedTools: data.ParsedTools,
		SafeOutputs: data.SafeOutputs,
		CacheMemoryConfig: &CacheMemoryConfig{
			Caches: make([]CacheMemoryEntry, len(data.CacheMemoryConfig.Caches)),
		},
	}
	for i, cache := range data.CacheMemoryConfig.Caches {
		cacheCopy := CacheMemoryEntry{
			ID:          cache.ID,
			Key:         cache.Key,
			Description: cache.Description,
			RestoreOnly: true,
			Scope:       cache.Scope,
		}
		if cache.AllowedExtensions != nil {
			cacheCopy.AllowedExtensions = append([]string(nil), cache.AllowedExtensions...)
		}
		if cache.RetentionDays != nil {
			retentionDays := *cache.RetentionDays
			cacheCopy.RetentionDays = &retentionDays
		}
		preActivationData.CacheMemoryConfig.Caches[i] = cacheCopy
	}

	generateCacheMemorySteps(builder, preActivationData)
}

func (c *Compiler) appendPreActivationSkipRolesStep(data *WorkflowData, steps []string) []string {
	steps = append(steps, "      - name: Check skip-roles\n")
	steps = append(steps, fmt.Sprintf("        id: %s\n", constants.CheckSkipRolesStepID))
	steps = append(steps, fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)))
	steps = append(steps, "        env:\n")
	steps = append(steps, fmt.Sprintf("          GH_AW_SKIP_ROLES: %q\n", strings.Join(data.SkipRoles, ",")))
	steps = append(steps, fmt.Sprintf("          GH_AW_WORKFLOW_NAME: %q\n", data.Name))
	steps = append(steps, "        with:\n")
	steps = append(steps, "          github-token: ${{ secrets.GITHUB_TOKEN }}\n")
	steps = append(steps, "          script: |\n")
	return append(steps, generateGitHubScriptWithRequire("check_skip_roles.cjs"))
}

func (c *Compiler) appendPreActivationSkipBotsStep(data *WorkflowData, steps []string) []string {
	steps = append(steps, "      - name: Check skip-bots\n")
	steps = append(steps, fmt.Sprintf("        id: %s\n", constants.CheckSkipBotsStepID))
	steps = append(steps, fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)))
	steps = append(steps, "        env:\n")
	steps = append(steps, fmt.Sprintf("          GH_AW_SKIP_BOTS: %q\n", strings.Join(data.SkipBots, ",")))
	steps = append(steps, fmt.Sprintf("          GH_AW_WORKFLOW_NAME: %q\n", data.Name))
	if data.AllowBotAuthoredTriggerComment {
		steps = append(steps, "          GH_AW_ALLOW_BOT_AUTHORED_TRIGGER_COMMENT: \"true\"\n")
	}
	steps = append(steps, "        with:\n")
	steps = append(steps, "          script: |\n")
	return append(steps, generateGitHubScriptWithRequire("check_skip_bots.cjs"))
}

func (c *Compiler) appendPreActivationCommandPositionStep(data *WorkflowData, steps []string) []string {
	steps = append(steps, "      - name: Check command position\n")
	steps = append(steps, fmt.Sprintf("        id: %s\n", constants.CheckCommandPositionStepID))
	steps = append(steps, fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)))
	steps = append(steps, "        env:\n")
	commandsJSON, _ := json.Marshal(data.Command) //nolint:jsonmarshalignoredeerror // marshaling a string slice cannot fail
	steps = append(steps, fmt.Sprintf("          GH_AW_COMMANDS: %q\n", string(commandsJSON)))
	if data.CommandPlaceholder != "" {
		steps = append(steps, fmt.Sprintf("          GH_AW_COMMAND_PLACEHOLDER: %q\n", data.CommandPlaceholder))
	}
	steps = append(steps, "        with:\n")
	steps = append(steps, "          script: |\n")
	return append(steps, generateGitHubScriptWithRequire("check_command_position.cjs"))
}

func (c *Compiler) injectPreActivationOnSteps(data *WorkflowData, steps, customSteps []string) ([]string, []string, error) {
	// Append custom steps from jobs.pre-activation if present.
	if len(customSteps) > 0 {
		compilerActivationJobsLog.Printf("Adding %d custom steps to pre-activation job", len(customSteps))
		steps = append(steps, customSteps...)
	}

	// Append on.steps if present (injected after other checks).
	var onStepIDs []string
	if len(data.OnSteps) == 0 {
		return steps, onStepIDs, nil
	}

	compilerActivationJobsLog.Printf("Adding %d on.steps to pre-activation job", len(data.OnSteps))
	for i, stepMap := range data.OnSteps {
		stepYAML, err := ConvertStepToYAML(stepMap)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to convert on.steps[%d] to YAML (ensure the step is a valid GitHub Actions step object with 'name', 'uses', or 'run' fields): %w", i, err)
		}
		steps = append(steps, stepYAML)
		if id, ok := stepMap["id"].(string); ok && id != "" {
			onStepIDs = append(onStepIDs, id)
		}
	}
	return steps, onStepIDs, nil
}

func buildPreActivationActivatedConditions(data *WorkflowData, needsPermissionCheck bool) []ConditionNode {
	conditions := buildPreActivationMembershipAndTimeConditions(data, needsPermissionCheck)
	return append(conditions, buildPreActivationSkipAndCommandConditions(data)...)
}

func buildPreActivationMembershipAndTimeConditions(data *WorkflowData, needsPermissionCheck bool) []ConditionNode {
	conditions := appendPreActivationCondition(nil, needsPermissionCheck, constants.CheckMembershipStepID, constants.IsTeamMemberOutput)
	conditions = appendPreActivationCondition(conditions, data.StopTime != "", constants.CheckStopTimeStepID, constants.StopTimeOkOutput)
	conditions = appendPreActivationCondition(conditions, data.RateLimit != nil, constants.CheckRateLimitStepID, constants.RateLimitOkOutput)
	return appendPreActivationCondition(conditions, data.Cooldown > 0, constants.CheckCooldownStepID, constants.CooldownOkOutput)
}

func buildPreActivationSkipAndCommandConditions(data *WorkflowData) []ConditionNode {
	conditions := appendPreActivationCondition(nil, data.SkipIfMatch != nil, constants.CheckSkipIfMatchStepID, constants.SkipCheckOkOutput)
	conditions = appendPreActivationCondition(conditions, data.SkipIfNoMatch != nil, constants.CheckSkipIfNoMatchStepID, constants.SkipNoMatchCheckOkOutput)
	conditions = appendPreActivationCondition(conditions, data.SkipIfCheckFailing != nil, constants.CheckSkipIfCheckFailingStepID, constants.SkipIfCheckFailingOkOutput)
	conditions = appendPreActivationCondition(conditions, len(data.SkipRoles) > 0, constants.CheckSkipRolesStepID, constants.SkipRolesOkOutput)
	conditions = appendPreActivationCondition(conditions, len(data.SkipBots) > 0, constants.CheckSkipBotsStepID, constants.SkipBotsOkOutput)
	return appendPreActivationCondition(conditions, len(data.Command) > 0, constants.CheckCommandPositionStepID, constants.CommandPositionOkOutput)
}

func appendPreActivationCondition(conditions []ConditionNode, enabled bool, stepID constants.StepID, outputName string) []ConditionNode {
	if !enabled {
		return conditions
	}
	return append(conditions, BuildComparison(
		BuildPropertyAccess(fmt.Sprintf("steps.%s.outputs.%s", stepID, outputName)),
		"==",
		BuildStringLiteral("true"),
	))
}

func buildPreActivationActivatedExpression(data *WorkflowData, conditions []ConditionNode) (string, error) {
	activatedNode, err := buildPreActivationActivatedNode(data, conditions)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("${{ %s }}", activatedNode.Render()), nil
}

func buildPreActivationActivatedNode(data *WorkflowData, conditions []ConditionNode) (ConditionNode, error) {
	// Build the final expression.
	if len(conditions) == 0 {
		// Pre-activation was created solely for on.steps injection.
		// The activated output is unconditionally true; the user controls
		// agent execution through their own if: condition referencing the
		// on.steps outputs (e.g., needs.pre_activation.outputs.gate_result).
		if len(data.OnSteps) > 0 || len(data.OnNeeds) > 0 || len(data.SkipAuthorAssociations) > 0 {
			compilerActivationJobsLog.Printf(
				"Pre-activation created with no output checks (on.steps=%d, on.needs=%d, skip-author-associations=%d); activated output is unconditionally true",
				len(data.OnSteps), len(data.OnNeeds), len(data.SkipAuthorAssociations),
			)
			return BuildStringLiteral("true"), nil
		}
		return nil, errors.New("developer error: pre-activation job created without activation checks or custom steps")
	}
	if len(conditions) == 1 {
		return conditions[0], nil
	}

	activatedNode := conditions[0]
	for i := 1; i < len(conditions); i++ {
		activatedNode = BuildAnd(activatedNode, conditions[i])
	}
	return activatedNode, nil
}

func buildPreActivationJobOutputs(data *WorkflowData, activatedExpression string, onStepIDs []string, customOutputs map[string]string) map[string]string {
	outputs := map[string]string{
		"activated":            activatedExpression,
		"setup-trace-id":       "${{ steps.setup.outputs.trace-id }}",
		"setup-span-id":        "${{ steps.setup.outputs.span-id }}",
		"setup-parent-span-id": "${{ steps.setup.outputs.parent-span-id || steps.setup.outputs.span-id }}",
	}
	// Always declare matched_command output so actionlint can resolve the type.
	// For command workflows, reference the check_command_position step output.
	// For non-command workflows, emit an empty string so the output key is defined.
	if len(data.Command) > 0 {
		outputs[constants.MatchedCommandOutput] = fmt.Sprintf("${{ steps.%s.outputs.%s }}", constants.CheckCommandPositionStepID, constants.MatchedCommandOutput)
	} else {
		outputs[constants.MatchedCommandOutput] = "''"
	}
	// Wire on.steps step outcomes as pre-activation outputs.
	// For each step with an id, emit output "<id>_result: ${{ steps.<id>.outcome }}"
	// so users can reference them with: needs.pre_activation.outputs.<id>_result
	// This is done BEFORE merging custom outputs so that explicit user-defined outputs
	// in jobs.pre-activation.outputs take precedence over the auto-wired values.
	if len(onStepIDs) > 0 {
		compilerActivationJobsLog.Printf("Wiring %d on.steps step outcomes as pre-activation outputs", len(onStepIDs))
		for _, id := range onStepIDs {
			outputs[id+"_result"] = fmt.Sprintf("${{ steps.%s.outcome }}", id)
		}
	}
	// Merge custom outputs from jobs.pre-activation if present.
	// Custom outputs are applied last so they take precedence over auto-wired on.steps outputs.
	if len(customOutputs) > 0 {
		compilerActivationJobsLog.Printf("Adding %d custom outputs to pre-activation job", len(customOutputs))
		maps.Copy(outputs, customOutputs)
	}
	return outputs
}

func (c *Compiler) buildPreActivationBaseIfCondition(data *WorkflowData) string {
	// Pre-activation job uses the user's original if condition (data.If).
	// The workflow_run safety check is NOT applied here - it's only on the activation job.
	// Don't include conditions that reference custom job outputs (those belong on the agent job).
	// Also don't include conditions that reference pre_activation outputs - those are outputs of this
	// very job and can only be evaluated by downstream jobs (activation, agent).
	if c.referencesCustomJobOutputs(data.If, data.Jobs) || referencesPreActivationOutputs(data.If) {
		return ""
	}
	return data.If
}

func (c *Compiler) applyPreActivationIfConditionGuards(data *WorkflowData, needsPermissionCheck bool, jobIfCondition string) string {
	// When labels is specified, add a job-level if: condition to the pre-activation job.
	// This causes the entire job to be skipped (gray ⊘) rather than failed (red ❌) when
	// the triggering label does not match, keeping CI dashboards noise-free.
	// workflow_dispatch is always allowed so manual runs are not blocked.
	if len(data.LabelNames) > 0 {
		jobIfCondition = combinePreActivationIfCondition(buildLabelNamesCondition(data.LabelNames), jobIfCondition)
	}
	// For comment-triggered workflows that require permission checks, add an author_association
	// guard to the job-level if: condition. This prevents the job from running at all for
	// unauthorized commenters (skipped/gray ⊘ vs running and then denying inside check_membership).
	// The guard only applies when:
	//   - the workflow has permission checks enabled (needsPermissionCheck == true), AND
	//   - the compiled on: section includes issue_comment or pull_request_review_comment events.
	// Workflows with roles:all opt out of needsPermissionCheck and are intentionally unrestricted.
	//
	// Exceptions — the static guard is skipped and runtime check_membership always runs:
	//   1. Any bot name in data.Bots is a GitHub Actions expression (contains ${{): we cannot
	//      embed the bot identity into a static if: expression. This also applies to bots that
	//      originate from imported shared agentic workflows.
	//   2. The compiled on: section itself contains a GitHub Actions expression (contains ${{):
	//      event detection cannot be performed reliably at compile time.
	if needsPermissionCheck && hasCommentEventInOn(data.On) && !botsContainExpression(data.Bots) && !strings.Contains(data.On, "${{") {
		jobIfCondition = combinePreActivationIfCondition(RenderCondition(buildCommentAuthorAssociationCondition(data.Bots, activeCommentEventsInOn(data.On))), jobIfCondition)
	}
	// Add optional skip-author-associations event guards as a job-level if condition.
	// This compiles to a static expression so skipped runs exit early without pre-activation
	// script execution cost for matching event/association combinations.
	if len(data.SkipAuthorAssociations) > 0 {
		jobIfCondition = combinePreActivationIfCondition(RenderCondition(buildSkipAuthorAssociationsCondition(data.SkipAuthorAssociations)), jobIfCondition)
	}
	return jobIfCondition
}

func combinePreActivationIfCondition(guard, jobIfCondition string) string {
	if jobIfCondition == "" {
		return guard
	}
	return RenderCondition(BuildAnd(
		&ExpressionNode{Expression: guard},
		&ExpressionNode{Expression: jobIfCondition},
	))
}

// buildLabelNamesCondition constructs the GitHub Actions if: expression for labels filtering.
// The generated condition passes when:
//   - the event has no label object (github.event.label == null), which covers
//     workflow_dispatch, push, schedule, and any other non-labeled events, OR
//   - the triggering label name matches any of the specified names.
//
// Using github.event.label == null (rather than checking the name) is semantically
// clearer and handles cases where GitHub Actions evaluates missing nested properties
// as null before coercing to empty string.
func buildLabelNamesCondition(labelNames []string) string {
	// Pass through events without a label payload.
	// github.event.label is null for workflow_dispatch, push, schedule, etc.
	noLabelEvent := ConditionNode(BuildEquals(
		BuildPropertyAccess("github.event.label"),
		BuildNullLiteral(),
	))

	result := noLabelEvent
	for _, name := range labelNames {
		result = BuildOr(result, BuildEquals(
			BuildPropertyAccess("github.event.label.name"),
			BuildStringLiteral(name),
		))
	}

	return result.Render()
}

// hasCommentEventInOn reports whether the rendered on: section includes issue_comment or
// pull_request_review_comment events. These are the events flagged by RGS-004 because
// any GitHub user (including unaffiliated outsiders) can post a comment and trigger the workflow.
// data.On is compiled YAML generated by the compiler, so checking for the event name followed by a
// colon (':') reliably identifies a trigger key without false-positives from embedded strings.
func hasCommentEventInOn(on string) bool {
	return strings.Contains(on, "issue_comment:") || strings.Contains(on, "pull_request_review_comment:")
}

// activeCommentEventsInOn returns the subset of guarded comment event names
// ("issue_comment", "pull_request_review_comment") that are present as trigger
// keys in the rendered on: section. The result is used to emit a precise
// author_association guard that only references events actually in the workflow.
//
// The on: string is compiler-generated YAML whose trigger keys always appear as
// top-level YAML keys followed immediately by a colon (e.g. "issue_comment:\n").
// Matching "<event>:" therefore reliably identifies a trigger key without
// false-positives from embedded user strings or comments — the same pattern used
// by the pre-existing hasCommentEventInOn helper.
func activeCommentEventsInOn(on string) []string {
	var events []string
	if strings.Contains(on, "issue_comment:") {
		events = append(events, "issue_comment")
	}
	if strings.Contains(on, "pull_request_review_comment:") {
		events = append(events, "pull_request_review_comment")
	}
	return events
}

// botsContainExpression reports whether any entry in bots is a GitHub Actions expression
// (i.e. contains "${{"). When true, the static author_association guard must be disabled so
// that check_membership always runs and evaluates the bot list at runtime.
func botsContainExpression(bots []string) bool {
	for _, bot := range bots {
		if strings.Contains(bot, "${{") {
			return true
		}
	}
	return false
}

// generateReportSkipStep generates the "Report skip reason" step for the pre-activation job.
// The step runs with if: always() and writes skip reasons to the GitHub Actions job summary
// extractPreActivationCustomFields extracts custom steps and outputs from jobs.pre-activation field in frontmatter.
// It validates that only steps and outputs fields are present, and errors on any other fields.
// If both jobs.pre-activation and jobs.pre_activation are defined, imports from both.
// Returns (customSteps, customOutputs, error).
func (c *Compiler) extractPreActivationCustomFields(jobs map[string]any) ([]string, map[string]string, error) {
	if jobs == nil {
		return nil, nil, nil
	}

	var customSteps []string
	var customOutputs map[string]string

	// Check both jobs.pre-activation and jobs.pre_activation (users might define both by mistake)
	// Import from both if both are defined
	jobVariants := []string{"pre-activation", string(constants.PreActivationJobName)}

	for _, jobName := range jobVariants {
		configMap, err := validatePreActivationJobConfig(jobs, jobName)
		if err != nil {
			return nil, nil, err
		}
		if configMap == nil {
			continue
		}

		steps, err := extractPreActivationJobSteps(jobName, configMap)
		if err != nil {
			return nil, nil, err
		}
		customSteps = append(customSteps, steps...)

		outputs, err := extractPreActivationJobOutputs(jobName, configMap)
		if err != nil {
			return nil, nil, err
		}
		if len(outputs) > 0 {
			if customOutputs == nil {
				customOutputs = make(map[string]string)
			}
			maps.Copy(customOutputs, outputs)
		}
	}

	return customSteps, customOutputs, nil
}

// validatePreActivationJobConfig returns the config map for a named pre-activation job variant,
// or nil if the variant is absent. It validates the job is a map and contains only allowed fields.
func validatePreActivationJobConfig(jobs map[string]any, jobName string) (map[string]any, error) {
	preActivationJob, exists := jobs[jobName]
	if !exists {
		return nil, nil
	}

	configMap, ok := preActivationJob.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("jobs.%s must be an object, got %T. Example:\njobs:\n  %s:\n    steps:\n      - run: echo hello", jobName, preActivationJob, jobName)
	}

	allowedFields := map[string]struct{}{
		"steps":     {},
		"outputs":   {},
		"pre-steps": {}, // handled by generic built-in pre-steps insertion in compiler_jobs.go
	}
	for field := range configMap {
		if field == "setup-steps" {
			return nil, fmt.Errorf(
				"jobs.%s.setup-steps is not allowed for activation/pre-activation jobs because it can short-circuit protections. Use 'steps', 'outputs', or 'pre-steps' instead. Example:\njobs:\n  %s:\n    steps:\n      - run: echo hello",
				jobName, jobName,
			)
		}
		if !setutil.Contains(allowedFields, field) {
			return nil, fmt.Errorf("jobs.%s: unsupported field '%s'. Only 'steps', 'outputs', and 'pre-steps' are allowed. Example:\njobs:\n  %s:\n    steps:\n      - run: echo hello", jobName, field, jobName)
		}
	}
	return configMap, nil
}

// extractPreActivationJobSteps extracts and converts custom steps from a pre-activation config map.
func extractPreActivationJobSteps(jobName string, configMap map[string]any) ([]string, error) {
	stepsValue, hasSteps := configMap["steps"]
	if !hasSteps {
		return nil, nil
	}

	stepsList, ok := stepsValue.([]any)
	if !ok {
		return nil, fmt.Errorf("jobs.%s.steps must be an array of step objects, got %T. Example:\njobs:\n  %s:\n    steps:\n      - run: echo hello", jobName, stepsValue, jobName)
	}

	var steps []string
	for i, step := range stepsList {
		stepMap, ok := step.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("jobs.%s.steps[%d] must be an object, got %T. Example:\njobs:\n  %s:\n    steps:\n      - run: echo hello", jobName, i, step, jobName)
		}
		stepYAML, err := ConvertStepToYAML(stepMap)
		if err != nil {
			return nil, fmt.Errorf("failed to convert jobs.%s.steps[%d] to YAML (ensure the step is a valid GitHub Actions step object with 'name', 'uses', or 'run' fields): %w", jobName, i, err)
		}
		steps = append(steps, stepYAML)
	}
	compilerActivationJobsLog.Printf("Extracted %d custom steps from jobs.%s", len(steps), jobName)
	return steps, nil
}

// extractPreActivationJobOutputs extracts custom outputs from a pre-activation config map.
func extractPreActivationJobOutputs(jobName string, configMap map[string]any) (map[string]string, error) {
	outputsValue, hasOutputs := configMap["outputs"]
	if !hasOutputs {
		return nil, nil
	}

	outputsMap, ok := outputsValue.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("jobs.%s.outputs must be an object mapping output names to expressions, got %T. Example:\njobs:\n  %s:\n    outputs:\n      result: ${{ steps.my_step.outputs.result }}", jobName, outputsValue, jobName)
	}

	// If the same output key is defined in both variants, the second one (pre_activation) wins.
	result := make(map[string]string, len(outputsMap))
	for key, val := range outputsMap {
		valStr, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("jobs.%s.outputs.%s must be a string, got %T. Example:\njobs:\n  %s:\n    outputs:\n      %s: ${{ steps.my_step.outputs.result }}", jobName, key, val, jobName, key)
		}
		result[key] = valStr
	}
	compilerActivationJobsLog.Printf("Extracted %d custom outputs from jobs.%s", len(result), jobName)
	return result, nil
}

// buildPreActivationAppTokenMintStep generates a single GitHub App token mint step for use
// by all skip-if checks in the pre-activation job. The step ID is "pre-activation-app-token".
// Auth configuration comes from the top-level on.github-app field.
func (c *Compiler) buildPreActivationAppTokenMintStep(app *GitHubAppConfig) []string {
	var steps []string
	tokenStepID := constants.PreActivationAppTokenStepID

	steps = append(steps, "      - name: Generate GitHub App token for skip-if checks\n")
	steps = append(steps, fmt.Sprintf("        id: %s\n", tokenStepID))
	if app.shouldIgnoreMissingKey() {
		guard := buildIgnoreIfMissingCondition(app)
		steps = appendStepEnvAssignments(steps, guard.EnvAssignments)
		if guard.Condition != "" {
			steps = append(steps, fmt.Sprintf("        if: %s\n", guard.Condition))
		}
	}
	steps = append(steps, fmt.Sprintf("        uses: %s\n", getActionPin("actions/create-github-app-token")))
	steps = append(steps, "        with:\n")
	steps = append(steps, fmt.Sprintf("          client-id: %s\n", app.AppID))
	steps = append(steps, fmt.Sprintf("          private-key: %s\n", app.PrivateKey))

	owner := app.Owner
	if owner == "" {
		owner = "${{ github.repository_owner }}"
	}
	steps = append(steps, fmt.Sprintf("          owner: %s\n", owner))

	if len(app.Repositories) == 1 && app.Repositories[0] == "*" {
		// Org-wide access: omit repositories field entirely
	} else if len(app.Repositories) == 1 {
		steps = append(steps, fmt.Sprintf("          repositories: %s\n", app.Repositories[0]))
	} else if len(app.Repositories) > 1 {
		steps = append(steps, "          repositories: |-\n")
		for _, repo := range app.Repositories {
			steps = append(steps, fmt.Sprintf("            %s\n", repo))
		}
	} else {
		steps = append(steps, "          repositories: ${{ github.event.repository.name }}\n")
	}

	steps = append(steps, "          github-api-url: ${{ github.api_url }}\n")

	return steps
}

// resolvePreActivationSkipIfToken returns the GitHub token expression to use for skip-if check
// steps in the pre-activation job. Priority: App token > custom github-token > empty (default).
// When non-empty, callers should emit `with.github-token: <value>` in the step.
func (c *Compiler) resolvePreActivationSkipIfToken(data *WorkflowData) string {
	if data.ActivationGitHubApp != nil {
		if data.ActivationGitHubApp.shouldIgnoreMissingKey() {
			return combineTokenExpressions(
				fmt.Sprintf("${{ steps.%s.outputs.token }}", constants.PreActivationAppTokenStepID),
				"${{ secrets.GITHUB_TOKEN }}",
			)
		}
		return fmt.Sprintf("${{ steps.%s.outputs.token }}", constants.PreActivationAppTokenStepID)
	}
	if data.ActivationGitHubToken != "" {
		return data.ActivationGitHubToken
	}
	return ""
}

// extractOnSteps extracts the 'steps' field from the 'on:' section of frontmatter.
// These steps are injected into the pre-activation job and their step outcome is wired
// as pre-activation outputs so users can reference them with:
//
//	needs.pre_activation.outputs.<id>_result   (contains outcome: success/failure/cancelled/skipped)
//
// Returns nil if on.steps is not configured.
// Returns an error if on.steps is not an array or contains non-object items.
func extractOnSteps(frontmatter map[string]any) ([]map[string]any, error) {
	onValue, exists := frontmatter["on"]
	if !exists || onValue == nil {
		return nil, nil
	}

	onMap, ok := onValue.(map[string]any)
	if !ok {
		return nil, nil
	}

	stepsValue, exists := onMap["steps"]
	if !exists || stepsValue == nil {
		return nil, nil
	}

	stepsList, ok := stepsValue.([]any)
	if !ok {
		return nil, fmt.Errorf("on.steps must be an array of step objects, got %T. Example:\non:\n  steps:\n    - run: echo hello", stepsValue)
	}

	result := make([]map[string]any, 0, len(stepsList))
	for i, step := range stepsList {
		stepMap, ok := step.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("on.steps[%d] must be an object, got %T. Example:\non:\n  steps:\n    - run: echo hello", i, step)
		}
		result = append(result, stepMap)
	}

	return result, nil
}

// extractOnPermissions extracts the 'permissions' field from the 'on:' section of frontmatter.
// These permissions are merged into the pre-activation job permissions, allowing users to declare
// extra scopes required by their on.steps (e.g., issues: read for GitHub API calls).
//
// Returns nil if on.permissions is not configured.
func extractOnPermissions(frontmatter map[string]any) *Permissions {
	onValue, exists := frontmatter["on"]
	if !exists || onValue == nil {
		return nil
	}

	onMap, ok := onValue.(map[string]any)
	if !ok {
		return nil
	}

	permsValue, exists := onMap["permissions"]
	if !exists || permsValue == nil {
		return nil
	}

	parser := NewPermissionsParserFromValue(permsValue)
	return parser.ToPermissions()
}

// extractOnNeeds extracts the 'needs' field from the 'on:' section of frontmatter.
// These dependencies are added to both pre_activation and activation jobs.
//
// Returns nil if on.needs is not configured.
func extractOnNeeds(frontmatter map[string]any) ([]string, error) {
	onValue, exists := frontmatter["on"]
	if !exists || onValue == nil {
		return nil, nil
	}

	onMap, ok := onValue.(map[string]any)
	if !ok {
		return nil, nil
	}

	return parseOnNeedsValues(onMap)
}

// extractOnRestoreMemory extracts the optional 'restore-memory' field from the 'on:' section.
// Default is false when unset.
func extractOnRestoreMemory(frontmatter map[string]any) (bool, error) {
	onValue, exists := frontmatter["on"]
	if !exists || onValue == nil {
		return false, nil
	}

	onMap, ok := onValue.(map[string]any)
	if !ok {
		return false, nil
	}

	restoreMemoryValue, exists := onMap["restore-memory"]
	if !exists || restoreMemoryValue == nil {
		return false, nil
	}

	restoreMemory, ok := restoreMemoryValue.(bool)
	if !ok {
		return false, fmt.Errorf("on.restore-memory must be a boolean, got %T. Example:\non:\n  restore-memory: true", restoreMemoryValue)
	}

	return restoreMemory, nil
}

func parseOnNeedsValues(onMap map[string]any) ([]string, error) {
	if onMap == nil {
		return nil, nil
	}

	needsValue, exists := onMap["needs"]
	if !exists || needsValue == nil {
		return nil, nil
	}

	needsList, ok := needsValue.([]any)
	if !ok {
		return nil, fmt.Errorf("on.needs must be an array of job names, got %T. Example:\non:\n  needs: [\"build\"]", needsValue)
	}

	result := make([]string, 0, len(needsList))
	for i, need := range needsList {
		needStr, ok := need.(string)
		if !ok {
			return nil, fmt.Errorf("on.needs[%d] must be a string job name, got %T. Example:\non:\n  needs: [\"build\"]", i, need)
		}
		result = append(result, needStr)
	}

	return sliceutil.Deduplicate(result), nil
}

// referencesPreActivationOutputs returns true if the condition references the pre_activation job's
// own outputs (e.g., "needs.pre_activation.outputs.foo"). Such conditions cannot be applied to the
// pre_activation job itself (a job cannot reference its own outputs), so they are deferred to
// downstream jobs (activation, agent).
func referencesPreActivationOutputs(condition string) bool {
	if condition == "" {
		return false
	}
	return strings.Contains(condition, "needs."+string(constants.PreActivationJobName)+".outputs.")
}
