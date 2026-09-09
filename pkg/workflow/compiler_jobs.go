package workflow

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var compilerJobsLog = logger.New("workflow:compiler_jobs")

// This file contains job building functions extracted from compiler.go
// These functions are responsible for constructing the various jobs that make up
// a compiled agentic workflow, including activation, main, safe outputs, and custom jobs.

func (c *Compiler) isActivationJobNeeded() bool {
	// Activation job is always needed to perform the timestamp check
	// It also handles:
	// 1. Command is configured (for team member checking)
	// 2. Text output is needed (for compute-text action)
	// 3. If condition is specified (to handle runtime conditions)
	// 4. Permission checks are needed (consolidated team member validation)
	return true
}

// ensureDriveMemoryJobPermissions grants the preview drive and OIDC permissions only
// to jobs containing compiler-generated drive-memory steps.
func (c *Compiler) ensureDriveMemoryJobPermissions(data *WorkflowData) {
	if data == nil || data.DriveMemoryConfig == nil || len(data.DriveMemoryConfig.Drives) == 0 {
		return
	}
	for _, job := range c.jobManager.GetAllJobs() {
		hasCheckout := false
		hasCommit := false
		for _, step := range job.Steps {
			hasCheckout = hasCheckout || strings.Contains(step, "actions/gh-drives-preview/checkout@")
			hasCommit = hasCommit || strings.Contains(step, "actions/gh-drives-preview/commit@")
		}
		if !hasCheckout {
			continue
		}
		perms := NewPermissionsParser(job.Permissions).ToPermissions()
		perms.Set(PermissionContents, PermissionRead)
		perms.Set(PermissionIdToken, PermissionWrite)
		if hasCommit {
			perms.Set(PermissionDrives, PermissionWrite)
		} else {
			perms.Set(PermissionDrives, PermissionRead)
		}
		job.Permissions = perms.RenderToYAML()
	}
}

// referencesCustomJobOutputs checks if a condition string references custom jobs.
// Returns true if the condition contains "needs.<customJobName>." patterns, which includes
// both outputs (needs.job.outputs.*) and results (needs.job.result).
func (c *Compiler) referencesCustomJobOutputs(condition string, customJobs map[string]any) bool {
	compilerJobsLog.Printf("Checking if condition references custom job outputs: custom_job_count=%d", len(customJobs))
	if condition == "" || customJobs == nil {
		return false
	}

	for jobName := range customJobs {
		// Check for patterns like "needs.ast_grep.outputs" or "needs.ast_grep.result"
		if strings.Contains(condition, fmt.Sprintf("needs.%s.", jobName)) {
			compilerJobsLog.Printf("Found reference to custom job %s in condition", jobName)
			return true
		}
	}
	compilerJobsLog.Print("No custom job references found in condition")
	return false
}

// jobDependsOnPreActivation checks if a job config has pre_activation as a dependency.
func jobDependsOnPreActivation(jobConfig map[string]any) bool {
	if needs, hasNeeds := jobConfig["needs"]; hasNeeds {
		if needsList, ok := needs.([]any); ok {
			for _, need := range needsList {
				if needStr, ok := need.(string); ok && needStr == string(constants.PreActivationJobName) {
					return true
				}
			}
		} else if needStr, ok := needs.(string); ok && needStr == string(constants.PreActivationJobName) {
			return true
		}
	}
	return false
}

// jobDependsOnActivation checks if a job config has activation as a dependency.
// Jobs that depend on activation run AFTER activation, not before it.
func jobDependsOnActivation(jobConfig map[string]any) bool {
	if needs, hasNeeds := jobConfig["needs"]; hasNeeds {
		if needsList, ok := needs.([]any); ok {
			for _, need := range needsList {
				if needStr, ok := need.(string); ok && needStr == string(constants.ActivationJobName) {
					return true
				}
			}
		} else if needStr, ok := needs.(string); ok && needStr == string(constants.ActivationJobName) {
			return true
		}
	}
	return false
}

// jobDependsOnAgent checks if a job config has agent as a dependency.
// Jobs that depend on agent should run AFTER the agent job, not before it.
// The jobConfig parameter is expected to be a map representing the job's YAML configuration,
// where "needs" can be either a string (single dependency) or []any (multiple dependencies).
// Returns false if "needs" is missing, malformed, or doesn't contain the agent job.
func jobDependsOnAgent(jobConfig map[string]any) bool {
	if needs, hasNeeds := jobConfig["needs"]; hasNeeds {
		if needsList, ok := needs.([]any); ok {
			for _, need := range needsList {
				if needStr, ok := need.(string); ok && needStr == string(constants.AgentJobName) {
					return true
				}
			}
		} else if needStr, ok := needs.(string); ok && needStr == string(constants.AgentJobName) {
			return true
		}
	}
	return false
}

// getCustomJobsDependingOnPreActivation returns custom job names that explicitly depend on pre_activation
// but NOT on activation. These jobs run after pre_activation but before activation, and activation
// should depend on them. Jobs that also depend on activation cannot run before activation.
func (c *Compiler) getCustomJobsDependingOnPreActivation(customJobs map[string]any) []string {
	compilerJobsLog.Printf("Finding custom jobs depending on pre_activation: total_custom_jobs=%d", len(customJobs))
	deps := sliceutil.FilterMapKeys(customJobs, func(jobName string, jobConfig any) bool {
		if configMap, ok := jobConfig.(map[string]any); ok {
			// Must depend on pre_activation AND must NOT depend on activation
			return jobDependsOnPreActivation(configMap) && !jobDependsOnActivation(configMap)
		}
		return false
	})
	sort.Strings(deps)
	compilerJobsLog.Printf("Found %d custom jobs depending on pre_activation: %v", len(deps), deps)
	return deps
}

// getReferencedCustomJobs returns custom job names that are referenced in the given content.
// It looks for patterns like "needs.<jobName>." or "${{ needs.<jobName>." in the content.
func (c *Compiler) getReferencedCustomJobs(content string, customJobs map[string]any) []string {
	if content == "" || customJobs == nil {
		return nil
	}
	compilerJobsLog.Printf("Searching for custom job references in content: content_length=%d, custom_job_count=%d", len(content), len(customJobs))
	// Check for patterns like "needs.job_name." which covers:
	// - needs.job_name.outputs.X
	// - ${{ needs.job_name.outputs.X }}
	// - needs.job_name.result
	refs := sliceutil.FilterMapKeys(customJobs, func(jobName string, _ any) bool {
		return strings.Contains(content, fmt.Sprintf("needs.%s.", jobName))
	})
	sort.Strings(refs)
	if len(refs) > 0 {
		compilerJobsLog.Printf("Found %d custom job references: %v", len(refs), refs)
	}
	return refs
}

// getCustomJobsReferencedInPromptWithNoActivationDep returns custom jobs whose outputs are referenced
// in the markdown body content but have no explicit needs (and therefore no activation dependency).
// These jobs need to run before the activation job so their outputs are available when the
// activation job builds the prompt. Without this, activation's prompt-building steps would reference
// those job outputs before the jobs have run, causing actionlint errors and empty substitutions.
//
// Only jobs with NO explicit needs are returned - jobs that explicitly depend on activation/pre_activation/etc.
// are excluded because they either already run before activation or cannot run before it.
func (c *Compiler) getCustomJobsReferencedInPromptWithNoActivationDep(data *WorkflowData) []string {
	if data == nil || data.Jobs == nil {
		return nil
	}

	promptContent := data.MarkdownContent
	if runtimeImportMarkdown := c.collectRuntimeImportMarkdownForCompilerAnalysis(data); runtimeImportMarkdown != "" {
		promptContent += "\n" + runtimeImportMarkdown
	}

	referencedJobs := c.getReferencedCustomJobs(promptContent, data.Jobs)
	var result []string
	for _, jobName := range referencedJobs {
		jobConfig, ok := data.Jobs[jobName].(map[string]any)
		if !ok {
			continue
		}
		// Only include jobs with no explicit needs - those get activation auto-added normally.
		// Jobs with explicit needs either already run before activation (pre_activation dependency)
		// or explicitly depend on activation/agent and must run after.
		if _, hasNeeds := jobConfig["needs"]; hasNeeds {
			continue
		}
		result = append(result, jobName)
		compilerJobsLog.Printf("Found custom job '%s' referenced in markdown body with no explicit needs: will run before activation", jobName)
	}
	return result
}

// getEngineEnvReferencedCustomJobsWithNoExplicitNeeds returns custom job names referenced
// by activation-rendered engine.env values via needs.<job>.outputs.* that have no explicit
// needs declaration.
// These jobs must run before activation so their outputs are available in activation steps
// (e.g. secret validation uses engine.env overrides at activation time).
//
// Only jobs with NO explicit needs are returned, matching the same filter applied to
// markdown-body-referenced jobs. Jobs with explicit needs either already run before activation
// (pre_activation dependency, picked up by getCustomJobsDependingOnPreActivation) or explicitly
// depend on activation/agent and therefore cannot be activation prerequisites.
func (c *Compiler) getEngineEnvReferencedCustomJobsWithNoExplicitNeeds(data *WorkflowData) []string {
	if data == nil || data.EngineConfig == nil || len(data.EngineConfig.Env) == 0 || data.Jobs == nil {
		return nil
	}

	activationRenderedEnvValues := c.getActivationRenderedEngineEnvValues(data)
	if len(activationRenderedEnvValues) == 0 {
		return nil
	}

	var engineEnvBuilder strings.Builder
	for _, envValue := range activationRenderedEnvValues {
		engineEnvBuilder.WriteByte('\n')
		engineEnvBuilder.WriteString(envValue)
	}
	referencedJobs := c.getReferencedCustomJobs(engineEnvBuilder.String(), data.Jobs)
	var result []string
	for _, jobName := range referencedJobs {
		jobConfig, ok := data.Jobs[jobName].(map[string]any)
		if !ok {
			continue
		}
		// Only include jobs with no explicit needs - those get activation auto-added normally.
		// Jobs with explicit needs either already run before activation (pre_activation dependency)
		// or explicitly depend on activation/agent and must run after.
		if _, hasNeeds := jobConfig["needs"]; hasNeeds {
			continue
		}
		result = append(result, jobName)
		compilerJobsLog.Printf("Found custom job '%s' referenced in engine.env with no explicit needs: will run before activation", jobName)
	}
	sort.Strings(result)
	return result
}

// getActivationRenderedEngineEnvValues returns the subset of engine.env values that are
// rendered in activation steps: required secret validation env vars and COPILOT_GITHUB_TOKEN
// (used by OAuth token checks).
func (c *Compiler) getActivationRenderedEngineEnvValues(data *WorkflowData) []string {
	if data == nil || data.EngineConfig == nil || len(data.EngineConfig.Env) == 0 {
		return nil
	}

	envKeys := map[string]struct{}{
		constants.CopilotGitHubToken: {},
	}
	engineID := strings.ToLower(resolveActivationEngineID(data))
	if engine, err := c.engineRegistry.GetEngine(engineID); err == nil {
		for _, secretName := range engine.GetRequiredSecretNames(data) {
			envKeys[secretName] = struct{}{}
		}
	}

	keyList := make([]string, 0, len(envKeys))
	for key := range envKeys {
		keyList = append(keyList, key)
	}
	sort.Strings(keyList)

	values := make([]string, 0, len(keyList))
	for _, key := range keyList {
		if value, ok := data.EngineConfig.Env[key]; ok {
			values = append(values, value)
		}
	}

	return values
}

// buildJobs creates all jobs for the workflow and adds them to the job manager.
// This function orchestrates the building of all job types by delegating to focused helper functions.
func (c *Compiler) buildJobs(data *WorkflowData, markdownPath string) error {
	compilerJobsLog.Printf("Building jobs for workflow: %s", markdownPath)

	// Use the already-parsed frontmatter from WorkflowData; consumers below only
	// inspect the processed "on" field for activation decisions.
	frontmatter := data.RawFrontmatter
	lockFilename := filepath.Base(stringutil.MarkdownToLockFile(markdownPath))

	c.resolveSafeOutputActionSchemas(data, markdownPath)

	activationJobCreated, err := c.buildActivationAndMainJobs(data, frontmatter, lockFilename)
	if err != nil {
		return err
	}

	if err := c.buildSafeOutputsAndEvalsJobs(data, markdownPath); err != nil {
		return err
	}

	if err := c.applyBuiltinJobPreSteps(data); err != nil {
		return fmt.Errorf("built-in job pre-steps could not be applied: %w. Check that pre-steps is an array of valid step objects", err)
	}

	if err := c.buildFrontmatterCustomJobs(data, activationJobCreated); err != nil {
		return err
	}

	if err := c.buildMemoryManagementJobs(data); err != nil {
		return err
	}

	if err := c.finalizeBuiltJobs(data); err != nil {
		return err
	}

	compilerJobsLog.Print("Successfully built all jobs for workflow")
	return nil
}

func (c *Compiler) resolveSafeOutputActionSchemas(data *WorkflowData, markdownPath string) {
	// Resolve custom safe-output actions early so that tool schemas (derived from action.yml)
	// are available when buildMainJobWrapper → generateMCPSetup → generateToolsMetaJSON →
	// generateDynamicTools runs. Without this early resolution the dynamic_tools entry for
	// each action tool would have an empty schema because Inputs/ActionDescription are nil.
	if data.SafeOutputs != nil && len(data.SafeOutputs.Actions) > 0 {
		c.resolveAllActions(data, markdownPath)
	}
}

// buildActivationAndMainJobs builds the activation chain and the primary agent job.
func (c *Compiler) buildActivationAndMainJobs(data *WorkflowData, frontmatter map[string]any, lockFilename string) (bool, error) {
	_, activationJobCreated, err := c.buildPreActivationAndActivationJobs(data, frontmatter, lockFilename)
	if err != nil {
		return false, err
	}

	if err := c.buildMainJobWrapper(data, activationJobCreated); err != nil {
		return false, err
	}

	return activationJobCreated, nil
}

// buildSafeOutputsAndEvalsJobs builds optional post-agent evaluation/output jobs.
func (c *Compiler) buildSafeOutputsAndEvalsJobs(data *WorkflowData, markdownPath string) error {
	if err := c.buildSafeOutputsJobs(data, string(constants.AgentJobName), markdownPath); err != nil {
		return fmt.Errorf("safe outputs jobs could not be built: %w. Check the safe-outputs configuration for valid job types", err)
	}

	if err := c.buildEvalsJobWrapper(data); err != nil {
		return err
	}

	return nil
}

// buildEvalsJobWrapper builds and adds the BinEval evals job if evals are declared in frontmatter.
func (c *Compiler) buildEvalsJobWrapper(data *WorkflowData) error {
	evalsJob, err := c.buildEvalsJob(data)
	if err != nil {
		return fmt.Errorf("evals job could not be built: %w. Check that the evals frontmatter section is a valid object", err)
	}
	if evalsJob == nil {
		return nil
	}
	if err := c.jobManager.AddJob(evalsJob); err != nil {
		return fmt.Errorf("evals job could not be added: %w. Check that no other job in the workflow reuses its name", err)
	}
	return nil
}

// buildFrontmatterCustomJobs builds additional jobs declared in the workflow frontmatter.
func (c *Compiler) buildFrontmatterCustomJobs(data *WorkflowData, activationJobCreated bool) error {
	if len(data.Jobs) > 0 {
		compilerJobsLog.Printf("Building %d custom jobs from frontmatter", len(data.Jobs))
	}
	if err := c.buildCustomJobs(data, activationJobCreated); err != nil {
		return fmt.Errorf("custom jobs could not be built: %w. Check the jobs section in frontmatter for valid job definitions", err)
	}
	return nil
}

// finalizeBuiltJobs applies final dependency, permission, and token-reference passes.
func (c *Compiler) finalizeBuiltJobs(data *WorkflowData) error {
	if err := c.applyBuiltinJobAugmentations(data); err != nil {
		return fmt.Errorf("built-in job needs augmentations could not be applied: %w. Check that jobs referenced in needs actually exist in the workflow", err)
	}

	if err := c.ensureConclusionIsLastJob(); err != nil {
		return err
	}

	c.ensureOTLPOIDCJobPermissions(data)
	c.ensureDriveMemoryJobPermissions(data)

	if err := c.validateSafeOutputStepTokenReferences(data); err != nil {
		return err
	}

	return nil
}

// ensureOTLPOIDCJobPermissions grants id-token: write to every job that contains the
// OTLP OIDC token mint step. core.getIDToken() fails when the job-level permissions
// block omits id-token: write, even if the workflow-level block grants it.
func (c *Compiler) ensureOTLPOIDCJobPermissions(data *WorkflowData) {
	if data == nil || !hasOTLPGitHubOIDCAuth(data.ParsedFrontmatter, data.RawFrontmatter) {
		return
	}
	for _, job := range c.jobManager.GetAllJobs() {
		if !jobStepsMintOTLPOIDCToken(job) {
			continue
		}
		perms := NewPermissionsParser(job.Permissions).ToPermissions()
		if level, exists := perms.Get(PermissionIdToken); exists && level == PermissionWrite {
			continue
		}
		perms.Set(PermissionIdToken, PermissionWrite)
		job.Permissions = perms.RenderToYAML()
		compilerJobsLog.Printf("Granted id-token: write to job %s for OTLP OIDC token mint", job.Name)
	}
}

// jobStepsMintOTLPOIDCToken reports whether any step in the job is the OTLP OIDC mint step.
func jobStepsMintOTLPOIDCToken(job *Job) bool {
	if job == nil {
		return false
	}
	for _, step := range job.Steps {
		if strings.Contains(step, "id: "+otlpOIDCMintStepID+"\n") {
			return true
		}
	}
	return false
}

// buildPreActivationAndActivationJobs builds the pre-activation and activation jobs if needed.
// Returns whether each job was created.
func (c *Compiler) buildPreActivationAndActivationJobs(data *WorkflowData, frontmatter map[string]any, lockFilename string) (preActivationJobCreated bool, activationJobCreated bool, err error) {
	data.Cooldown, err = extractCooldown(frontmatter)
	if err != nil {
		return
	}

	// Determine if permission checks or stop-time checks are needed
	needsPermissionCheck := c.needsRoleCheck(data, frontmatter)
	hasStopTime := data.StopTime != ""
	hasSkipIfMatch := data.SkipIfMatch != nil
	hasSkipIfNoMatch := data.SkipIfNoMatch != nil
	hasSkipRoles := len(data.SkipRoles) > 0
	hasSkipBots := len(data.SkipBots) > 0
	hasSkipAuthorAssociations := len(data.SkipAuthorAssociations) > 0
	hasCommandTrigger := len(data.Command) > 0
	hasRateLimit := data.RateLimit != nil
	hasCooldown := data.Cooldown > 0
	hasOnSteps := len(data.OnSteps) > 0
	hasOnNeeds := len(data.OnNeeds) > 0
	hasLabelNames := len(data.LabelNames) > 0
	compilerJobsLog.Printf("Job configuration: needsPermissionCheck=%v, hasStopTime=%v, hasSkipIfMatch=%v, hasSkipIfNoMatch=%v, hasSkipRoles=%v, hasSkipBots=%v, hasSkipAuthorAssociations=%v, hasCommand=%v, hasRateLimit=%v, hasCooldown=%v, hasOnSteps=%v, hasOnNeeds=%v, hasLabelNames=%v", needsPermissionCheck, hasStopTime, hasSkipIfMatch, hasSkipIfNoMatch, hasSkipRoles, hasSkipBots, hasSkipAuthorAssociations, hasCommandTrigger, hasRateLimit, hasCooldown, hasOnSteps, hasOnNeeds, hasLabelNames)

	// Build pre-activation job if needed. The job combines:
	//   - membership checks, stop-time validation, skip-if-match/no-match checks
	//   - skip-roles/bots checks, rate limit check, command position check
	//   - on.steps injection, label-names filter
	if needsPermissionCheck || hasStopTime || hasSkipIfMatch || hasSkipIfNoMatch || hasSkipRoles || hasSkipBots || hasSkipAuthorAssociations || hasCommandTrigger || hasRateLimit || hasCooldown || hasOnSteps || hasOnNeeds || hasLabelNames {
		compilerJobsLog.Print("Building pre-activation job")
		preActivationJob, err := c.buildPreActivationJob(data, needsPermissionCheck)
		if err != nil {
			return false, false, fmt.Errorf("%s job could not be built: %w. Check the activation-related frontmatter fields (roles, if, stop-time, etc.)", constants.PreActivationJobName, err)
		}
		if err := c.jobManager.AddJob(preActivationJob); err != nil {
			return false, false, fmt.Errorf("%s job could not be added: %w. Check that no other job in the workflow reuses its name", constants.PreActivationJobName, err)
		}
		compilerJobsLog.Printf("Successfully added pre-activation job: %s", constants.PreActivationJobName)
		preActivationJobCreated = true
	}

	// Determine if we need to add workflow_run repository safety check
	var workflowRunRepoSafety string
	if c.hasWorkflowRunTrigger(frontmatter) {
		workflowRunRepoSafety = c.buildWorkflowRunRepoSafetyCondition()
		compilerJobsLog.Print("Adding workflow_run repository safety check")
	}

	// Build activation job if needed (preamble job that handles runtime conditions)
	if c.isActivationJobNeeded() {
		compilerJobsLog.Print("Building activation job")
		activationJob, err := c.buildActivationJob(data, preActivationJobCreated, workflowRunRepoSafety, lockFilename)
		if err != nil {
			return preActivationJobCreated, false, fmt.Errorf("activation job could not be built: %w. Check the workflow triggers and activation-related frontmatter fields", err)
		}
		if err := c.jobManager.AddJob(activationJob); err != nil {
			return preActivationJobCreated, false, fmt.Errorf("activation job could not be added: %w. Check that no other job in the workflow reuses its name", err)
		}
		compilerJobsLog.Print("Successfully added activation job")
		activationJobCreated = true
	}

	return preActivationJobCreated, activationJobCreated, nil
}

// buildMainJobWrapper builds the main workflow job and adds it to the job manager.
func (c *Compiler) buildMainJobWrapper(data *WorkflowData, activationJobCreated bool) error {
	compilerJobsLog.Print("Building main job")
	mainJob, err := c.buildMainJob(data, activationJobCreated)
	if err != nil {
		return fmt.Errorf("main job could not be built: %w. Check the engine and steps configuration in frontmatter", err)
	}
	if err := c.jobManager.AddJob(mainJob); err != nil {
		return fmt.Errorf("main job could not be added: %w. Check that no other job in the workflow reuses its name", err)
	}
	compilerJobsLog.Printf("Successfully added main job: %s", string(constants.AgentJobName))
	return nil
}

// buildMemoryManagementJobs builds memory management jobs (push_repo_memory and update_cache_memory).
// These jobs handle artifact-based memory persistence to git branches and GitHub Actions cache.
func (c *Compiler) buildMemoryManagementJobs(data *WorkflowData) error {
	threatDetectionEnabledForSafeJobs := IsDetectionJobEnabled(data.SafeOutputs)

	// Build push_repo_memory job if repo-memory is configured
	pushRepoMemoryJobName, err := c.buildPushRepoMemoryJobWrapper(data, threatDetectionEnabledForSafeJobs)
	if err != nil {
		return err
	}

	// Build update_cache_memory job if cache-memory is configured and threat detection is enabled
	updateCacheMemoryJobName, err := c.buildUpdateCacheMemoryJobWrapper(data, threatDetectionEnabledForSafeJobs)
	if err != nil {
		return err
	}

	if _, err := c.buildUpdateDriveMemoryJobWrapper(data, threatDetectionEnabledForSafeJobs); err != nil {
		return err
	}

	// Build push_experiments_state job when experiment storage is "repo"
	pushExperimentsJobName, err := c.buildPushExperimentsStateJobWrapper(data)
	if err != nil {
		return err
	}

	// Build push_evals_state job when evals are configured
	pushEvalsJobName, err := c.buildPushEvalsStateJobWrapper(data)
	if err != nil {
		return err
	}

	// Update conclusion job dependencies
	if err := c.updateConclusionJobDependencies(pushRepoMemoryJobName, updateCacheMemoryJobName, pushExperimentsJobName, pushEvalsJobName); err != nil {
		return err
	}

	return nil
}

// buildPushRepoMemoryJobWrapper builds the push_repo_memory job if repo-memory is configured.
// Returns the job name if created, empty string otherwise.
func (c *Compiler) buildPushRepoMemoryJobWrapper(data *WorkflowData, threatDetectionEnabled bool) (string, error) {
	if data.RepoMemoryConfig == nil || len(data.RepoMemoryConfig.Memories) == 0 {
		return "", nil
	}

	compilerJobsLog.Print("Building push_repo_memory job")
	pushRepoMemoryJob, err := c.buildPushRepoMemoryJob(data, threatDetectionEnabled)
	if err != nil {
		return "", fmt.Errorf("push_repo_memory job could not be built: %w. Check the repo-memory configuration in frontmatter", err)
	}

	if pushRepoMemoryJob == nil {
		return "", nil
	}

	// Add detection dependency if threat detection is enabled
	// The detection job runs after the agent job; push_repo_memory depends on detection
	// and its condition checks needs.detection.result == 'success'

	if err := c.jobManager.AddJob(pushRepoMemoryJob); err != nil {
		return "", fmt.Errorf("push_repo_memory job could not be added: %w. Check that no other job in the workflow reuses its name", err)
	}

	compilerJobsLog.Printf("Successfully added push_repo_memory job: %s", pushRepoMemoryJob.Name)
	return pushRepoMemoryJob.Name, nil
}

// buildUpdateCacheMemoryJobWrapper builds the update_cache_memory job if cache-memory is configured.
// Returns the job name if created, empty string otherwise.
func (c *Compiler) buildUpdateCacheMemoryJobWrapper(data *WorkflowData, threatDetectionEnabled bool) (string, error) {
	if data.CacheMemoryConfig == nil || len(data.CacheMemoryConfig.Caches) == 0 {
		return "", nil
	}

	if !threatDetectionEnabled {
		return "", nil
	}

	compilerJobsLog.Print("Building update_cache_memory job")
	updateCacheMemoryJob, err := c.buildUpdateCacheMemoryJob(data, threatDetectionEnabled)
	if err != nil {
		return "", fmt.Errorf("update_cache_memory job could not be built: %w. Check the cache-memory configuration in frontmatter", err)
	}

	if updateCacheMemoryJob == nil {
		return "", nil
	}

	if err := c.jobManager.AddJob(updateCacheMemoryJob); err != nil {
		return "", fmt.Errorf("update_cache_memory job could not be added: %w. Check that no other job in the workflow reuses its name", err)
	}

	compilerJobsLog.Printf("Successfully added update_cache_memory job: %s", updateCacheMemoryJob.Name)
	return updateCacheMemoryJob.Name, nil
}

func (c *Compiler) buildUpdateDriveMemoryJobWrapper(data *WorkflowData, threatDetectionEnabled bool) (string, error) {
	if data.DriveMemoryConfig == nil || len(data.DriveMemoryConfig.Drives) == 0 || !threatDetectionEnabled {
		return "", nil
	}
	job, err := c.buildUpdateDriveMemoryJob(data, threatDetectionEnabled)
	if err != nil {
		return "", fmt.Errorf("failed to build update_drive_memory job: %w", err)
	}
	if job == nil {
		return "", nil
	}
	if err := c.jobManager.AddJob(job); err != nil {
		return "", fmt.Errorf("failed to add update_drive_memory job: %w", err)
	}
	return job.Name, nil
}

// buildPushExperimentsStateJobWrapper builds the push_experiments_state job when experiments
// use repo-based storage.  Returns the job name if created, empty string otherwise.
func (c *Compiler) buildPushExperimentsStateJobWrapper(data *WorkflowData) (string, error) {
	if len(data.Experiments) == 0 || data.ExperimentsStorage != ExperimentsStorageRepo {
		return "", nil
	}

	compilerJobsLog.Print("Building push_experiments_state job")
	job, err := c.buildPushExperimentsStateJob(data)
	if err != nil {
		return "", fmt.Errorf("push_experiments_state job could not be built: %w. Check the experiments configuration in frontmatter", err)
	}
	if job == nil {
		return "", nil
	}

	if err := c.jobManager.AddJob(job); err != nil {
		return "", fmt.Errorf("push_experiments_state job could not be added: %w. Check that no other job in the workflow reuses its name", err)
	}

	compilerJobsLog.Printf("Successfully added push_experiments_state job: %s", job.Name)
	return job.Name, nil
}

// buildPushEvalsStateJobWrapper builds the push_evals_state job when evals are configured.
// Returns the job name if created, empty string otherwise.
func (c *Compiler) buildPushEvalsStateJobWrapper(data *WorkflowData) (string, error) {
	if data.Evals == nil || !data.Evals.HasEvals() {
		return "", nil
	}

	compilerJobsLog.Print("Building push_evals_state job")
	job, err := c.buildPushEvalsStateJob(data)
	if err != nil {
		return "", fmt.Errorf("push_evals_state job could not be built: %w. Check the evals configuration in frontmatter", err)
	}
	if job == nil {
		return "", nil
	}

	if err := c.jobManager.AddJob(job); err != nil {
		return "", fmt.Errorf("push_evals_state job could not be added: %w. Check that no other job in the workflow reuses its name", err)
	}

	compilerJobsLog.Printf("Successfully added push_evals_state job: %s", job.Name)
	return job.Name, nil
}

// updateConclusionJobDependencies updates the conclusion job to depend on memory management jobs if they exist.
func (c *Compiler) updateConclusionJobDependencies(pushRepoMemoryJobName, updateCacheMemoryJobName, pushExperimentsJobName, pushEvalsJobName string) error {
	conclusionJob, exists := c.jobManager.GetJob("conclusion")
	if !exists {
		return nil
	}

	if pushRepoMemoryJobName != "" {
		conclusionJob.Needs = append(conclusionJob.Needs, pushRepoMemoryJobName)
		compilerJobsLog.Printf("Added push_repo_memory dependency to conclusion job")
	}

	if updateCacheMemoryJobName != "" {
		conclusionJob.Needs = append(conclusionJob.Needs, updateCacheMemoryJobName)
		compilerJobsLog.Printf("Added update_cache_memory dependency to conclusion job")
	}

	if pushExperimentsJobName != "" {
		conclusionJob.Needs = append(conclusionJob.Needs, pushExperimentsJobName)
		compilerJobsLog.Printf("Added push_experiments_state dependency to conclusion job")
	}

	if pushEvalsJobName != "" {
		conclusionJob.Needs = append(conclusionJob.Needs, pushEvalsJobName)
		compilerJobsLog.Printf("Added push_evals_state dependency to conclusion job")
	}

	return nil
}

// ensureConclusionIsLastJob ensures the conclusion job depends on ALL other workflow jobs,
// making it truly the last job to execute. This is a final-pass check that catches any
// custom or auto-generated jobs that were not explicitly added to conclusion's needs
// during the incremental build process (e.g. user-defined post-agent jobs).
//
// Jobs intentionally excluded:
//   - "conclusion" itself
//   - "pre_activation" / "pre-activation" – runs at the very start, before activation
func (c *Compiler) ensureConclusionIsLastJob() error {
	conclusionJob, exists := c.jobManager.GetJob("conclusion")
	if !exists {
		return nil
	}

	// Build a set of already-listed needs for O(1) lookup
	currentNeeds := make(map[string]struct {
	}, len(conclusionJob.Needs))
	for _, need := range conclusionJob.Needs {
		currentNeeds[need] = struct {
		}{}
	}

	// Jobs that must never appear in conclusion's needs
	exclude := map[string]struct {
	}{
		"conclusion":                           {},
		string(constants.PreActivationJobName): {},
		"pre-activation":                       {},
	}

	// Iterate over all jobs in alphabetical order for deterministic output
	allJobs := c.jobManager.GetAllJobs()
	jobNames := sliceutil.SortedKeys(allJobs)

	for _, jobName := range jobNames {
		if setutil.Contains(exclude, jobName) || setutil.Contains(currentNeeds, jobName) {
			continue
		}
		conclusionJob.Needs = append(conclusionJob.Needs, jobName)
		compilerJobsLog.Printf("ensureConclusionIsLastJob: added %s to conclusion needs", jobName)
	}

	return nil
}

// buildSafeOutputsJobs is now in compiler_safe_output_jobs.go
// buildPreActivationJob, buildActivationJob, and buildMainJob are now in compiler_activation_jobs.go

// extractJobsFromFrontmatter extracts job configuration from frontmatter
// This now uses the structured extraction helper for consistency
func (c *Compiler) extractJobsFromFrontmatter(frontmatter map[string]any) map[string]any {
	return ExtractMapField(frontmatter, "jobs")
}

// shouldAddCheckoutStep returns true if the workflow requires a checkout step.
// The repository checkout is needed in the agent job to access workflow files,
// custom agent files, and other repository content.
//
// The checkout step is only skipped when:
//   - Custom steps already contain a checkout action
//   - checkout: false is set in the workflow frontmatter
//   - permissions.contents: none is set (target-only checkout: the workflow does
//     not need its own repository content, but other configured checkout entries
//     such as a target-repo checkout are still generated)
//
// Otherwise, checkout is always added to ensure the agent has access to the repository.
func (c *Compiler) shouldAddCheckoutStep(data *WorkflowData) bool {
	// If checkout was explicitly disabled via checkout: false, skip it
	if data.CheckoutDisabled {
		workflowLog.Print("Skipping checkout step: checkout disabled via checkout: false")
		return false
	}

	// If permissions.contents: none is set, skip only the default (workflow-repository)
	// checkout step; other configured checkouts (e.g. a target-only sidecar checkout)
	// are still generated separately.
	if data.CheckoutSkipDefault {
		workflowLog.Print("Skipping default checkout step: permissions.contents is none (target-only checkout)")
		return false
	}

	// If custom steps already contain checkout, don't add another one
	if data.CustomSteps != "" && ContainsCheckout(data.CustomSteps) {
		workflowLog.Print("Skipping checkout step: custom steps already contain checkout")
		return false
	}

	// Always add checkout to ensure agent has repository access
	workflowLog.Print("Adding checkout step: agent job requires repository access")
	return true
}
