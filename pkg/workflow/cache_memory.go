package workflow

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

// generateCacheMemorySteps generates cache setup steps (directory creation, restore, and git init) for the cache-memory configuration.
// Cache-memory provides a simple file share that LLMs can read/write freely.
// Artifact upload is handled separately by generateCacheMemoryArtifactUpload after agent execution.
func generateCacheMemorySteps(builder *strings.Builder, data *WorkflowData) {
	if data.CacheMemoryConfig == nil || len(data.CacheMemoryConfig.Caches) == 0 {
		return
	}

	cacheLog.Printf("Generating cache-memory setup steps for %d caches", len(data.CacheMemoryConfig.Caches))

	builder.WriteString("      # Cache memory file share configuration from frontmatter processed below\n")

	// Use backward-compatible paths only when there's a single cache with ID "default"
	// This maintains compatibility with existing workflows
	useBackwardCompatiblePaths := len(data.CacheMemoryConfig.Caches) == 1 && data.CacheMemoryConfig.Caches[0].ID == "default"

	// Extract GitHub guard policy for integrity-aware cache key generation.
	var githubConfig *GitHubToolConfig
	if data.ParsedTools != nil {
		githubConfig = data.ParsedTools.GitHub
	}
	integrityLevel := cacheIntegrityLevel(githubConfig)
	for i, cache := range data.CacheMemoryConfig.Caches {
		cacheDir := cacheMemoryDirFor(cache.ID)
		restoreStepID := fmt.Sprintf("restore_cache_memory_%d", i)

		// Add step to create cache-memory directory for this cache
		if useBackwardCompatiblePaths {
			// For single default cache, use the original directory for backward compatibility
			builder.WriteString("      - name: Create cache-memory directory\n")
			builder.WriteString("        run: bash \"${RUNNER_TEMP}/gh-aw/actions/create_cache_memory_dir.sh\"\n")
		} else {
			fmt.Fprintf(builder, "      - name: Create cache-memory directory (%s)\n", cache.ID)
			builder.WriteString("        run: |\n")
			fmt.Fprintf(builder, "          mkdir -p %s\n", cacheDir)
		}

		// Use integrity-aware cache key (includes integrity level + policy hash prefix).
		cacheKey := computeIntegrityCacheKey(cache, githubConfig)

		// Ensure run_id suffix is present (computeIntegrityCacheKey guarantees this,
		// but we check again for clarity and safety).
		runIdSuffix := "-${{ github.run_id }}"
		if !strings.HasSuffix(cacheKey, runIdSuffix) {
			cacheKey = cacheKey + runIdSuffix
		}

		// Generate restore keys based on scope
		// - "workflow" (default): Single restore key with workflow ID (secure)
		// - "repo": Two restore keys - with and without workflow ID (allows cross-workflow sharing)
		restoreKeys := buildCacheRestoreKeys(cacheKey, cache.Scope)

		// Step name and action
		// Use actions/cache/restore for restore-only caches or when threat detection is enabled
		// When threat detection is enabled, we only restore the cache and defer saving to a separate job after detection
		// Use actions/cache for normal caches (which auto-saves via post-action)
		threatDetectionEnabled := IsDetectionJobEnabled(data.SafeOutputs)
		useRestoreOnly := cache.RestoreOnly || threatDetectionEnabled

		actionName := "Restore cache-memory file share data"

		if useBackwardCompatiblePaths {
			fmt.Fprintf(builder, "      - name: %s\n", actionName)
		} else {
			fmt.Fprintf(builder, "      - name: %s (%s)\n", actionName, cache.ID)
		}
		fmt.Fprintf(builder, "        id: %s\n", restoreStepID)

		// Use actions/cache/restore@v4 when restore-only or threat detection enabled
		// Use actions/cache@v4 for normal caches
		if useRestoreOnly {
			fmt.Fprintf(builder, "        uses: %s\n", getActionPin("actions/cache/restore"))
		} else {
			fmt.Fprintf(builder, "        uses: %s\n", getActionPin("actions/cache"))
		}
		builder.WriteString("        with:\n")
		fmt.Fprintf(builder, "          key: %s\n", cacheKey)

		// Path - always use the new cache directory format
		fmt.Fprintf(builder, "          path: %s\n", cacheDir)

		builder.WriteString("          restore-keys: |\n")
		for _, key := range restoreKeys {
			fmt.Fprintf(builder, "            %s\n", key)
		}

		// Add git setup step after cache restore.
		// This initialises (or migrates) the git repository used for integrity branching,
		// checks out the current integrity branch, and merges down from higher-integrity branches.
		generateCacheMemoryGitSetupStep(builder, cache, cacheDir, integrityLevel, useBackwardCompatiblePaths)
	}

}

// generateCacheMemoryGitSetupStep emits a pre-agent step that sets up the git-backed integrity
// repository inside the given cache directory. It must run after the cache is restored so that
// any previous git history is available for the merge-down step.
// The step also performs pre-agent security sanitization: it strips execute bits from all
// working-tree files and, when allowed extensions are configured, removes files with
// disallowed extensions before the agent can access them.
func generateCacheMemoryGitSetupStep(builder *strings.Builder, cache CacheMemoryEntry, cacheDir, integrityLevel string, useBackwardCompatiblePaths bool) {
	if useBackwardCompatiblePaths {
		builder.WriteString("      - name: Setup cache-memory git repository\n")
	} else {
		fmt.Fprintf(builder, "      - name: Setup cache-memory git repository (%s)\n", cache.ID)
	}
	builder.WriteString("        env:\n")
	fmt.Fprintf(builder, "          GH_AW_CACHE_DIR: %s\n", cacheDir)
	fmt.Fprintf(builder, "          GH_AW_MIN_INTEGRITY: %s\n", integrityLevel)
	// Pass colon-separated allowed extensions so the setup script can remove disallowed files
	// before the agent runs (pre-agent sanitization). Skip when the list is empty (allow all).
	// Single quotes in the value are escaped ('' in YAML single-quoted scalars) as defense-in-depth,
	// even though isValidFileExtension already rejects values containing single quotes at parse time.
	if len(cache.AllowedExtensions) > 0 {
		escaped := strings.ReplaceAll(strings.Join(cache.AllowedExtensions, ":"), "'", "''")
		fmt.Fprintf(builder, "          GH_AW_ALLOWED_EXTENSIONS: '%s'\n", escaped)
	}
	builder.WriteString("        run: bash \"${RUNNER_TEMP}/gh-aw/actions/setup_cache_memory_git.sh\"\n")
}

// generateCacheMemoryGitCommitSteps emits post-agent steps that commit agent-written changes
// to the current integrity branch. These steps run after agent execution and before artifact
// upload so that the saved tarball always includes up-to-date git history.
func generateCacheMemoryGitCommitSteps(builder *strings.Builder, data *WorkflowData) {
	if data.CacheMemoryConfig == nil || len(data.CacheMemoryConfig.Caches) == 0 {
		return
	}

	cacheLog.Printf("Generating cache-memory git commit steps for %d caches", len(data.CacheMemoryConfig.Caches))

	useBackwardCompatiblePaths := len(data.CacheMemoryConfig.Caches) == 1 && data.CacheMemoryConfig.Caches[0].ID == "default"

	for _, cache := range data.CacheMemoryConfig.Caches {
		// Skip restore-only caches (nothing to commit)
		if cache.RestoreOnly {
			continue
		}

		cacheDir := cacheMemoryDirFor(cache.ID)

		if useBackwardCompatiblePaths {
			builder.WriteString("      - name: Commit cache-memory changes\n")
		} else {
			fmt.Fprintf(builder, "      - name: Commit cache-memory changes (%s)\n", cache.ID)
		}
		// Run even when agent fails so that partial work is still recorded.
		builder.WriteString("        if: always()\n")
		builder.WriteString("        env:\n")
		fmt.Fprintf(builder, "          GH_AW_CACHE_DIR: %s\n", cacheDir)
		builder.WriteString("        run: bash \"${RUNNER_TEMP}/gh-aw/actions/commit_cache_memory_git.sh\"\n")
	}
}

// generateCacheMemoryValidation generates validation steps for cache-memory file types
// This should be called after agent execution to validate files before upload/save
func generateCacheMemoryValidation(builder *strings.Builder, data *WorkflowData) {
	if data.CacheMemoryConfig == nil || len(data.CacheMemoryConfig.Caches) == 0 {
		return
	}

	cacheLog.Printf("Generating cache-memory validation steps for %d caches", len(data.CacheMemoryConfig.Caches))

	// Use backward-compatible paths only when there's a single cache with ID "default"
	useBackwardCompatiblePaths := len(data.CacheMemoryConfig.Caches) == 1 && data.CacheMemoryConfig.Caches[0].ID == "default"

	for _, cache := range data.CacheMemoryConfig.Caches {
		// Skip restore-only caches
		if cache.RestoreOnly {
			continue
		}

		hasFileTypeValidation := len(cache.AllowedExtensions) > 0
		hasCustomValidation := cache.Validation != nil
		if !hasFileTypeValidation && !hasCustomValidation {
			cacheLog.Printf("Skipping validation step for cache %s (empty allowed-extensions and no custom validation)", cache.ID)
			continue
		}

		cacheDir := cacheMemoryDirFor(cache.ID)
		allowedExtsJSON, _ := json.Marshal(cache.AllowedExtensions) //nolint:jsonmarshalignoredeerror // marshaling a string slice cannot fail

		stepName := "Validate cache-memory file types"
		if !useBackwardCompatiblePaths {
			stepName = fmt.Sprintf("Validate cache-memory file types (%s)", cache.ID)
		}
		if hasCustomValidation {
			stepName = strings.Replace(stepName, "file types", "file types and domain content", 1)
		}
		fmt.Fprintf(builder, "      - name: %s\n", stepName)
		fmt.Fprintf(builder, "        id: %s\n", cacheMemoryValidationStepID(cache.ID))
		builder.WriteString("        if: always()\n")
		fmt.Fprintf(builder, "        uses: %s\n", getCachedActionPin("actions/github-script", data))
		builder.WriteString("        env:\n")
		fmt.Fprintf(builder, "          MEMORY_DIR: %s\n", cacheDir)
		fmt.Fprintf(builder, "          MEMORY_ID: %s\n", cache.ID)
		fmt.Fprintf(builder, "          ALLOWED_EXTENSIONS: '%s'\n", allowedExtsJSON)
		if cache.Validation != nil {
			fmt.Fprintf(builder, "          VALIDATION_SCRIPT_B64: %s\n", memoryValidationScriptBase64(cache.Validation))
			fmt.Fprintf(builder, "          VALIDATION_TIMEOUT_SECONDS: %d\n", memoryValidationTimeoutSeconds(cache.Validation))
		}
		builder.WriteString("        with:\n")
		builder.WriteString("          script: |\n")
		builder.WriteString("            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');\n")
		builder.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
		builder.WriteString("            const { validateMemoryStep } = require('${{ runner.temp }}/gh-aw/actions/validate_memory_step.cjs');\n")
		builder.WriteString("            validateMemoryStep(core, { kind: 'cache', writeMarker: true });\n")
	}
}

// generateCacheMemoryArtifactUpload generates artifact upload steps for cache-memory.
// This should be called after agent execution steps to ensure cache is uploaded after the agent has finished.
// pinAction resolves the upload-artifact action reference; pass c.getActionPin from Compiler methods.
func generateCacheMemoryArtifactUpload(builder *strings.Builder, data *WorkflowData, pinAction func(string) string) {
	if data.CacheMemoryConfig == nil || len(data.CacheMemoryConfig.Caches) == 0 {
		return
	}

	// Only upload artifacts when threat detection is enabled (needed for update_cache_memory job)
	// When threat detection is disabled, cache is saved automatically by actions/cache post-action
	threatDetectionEnabled := IsDetectionJobEnabled(data.SafeOutputs)
	if !threatDetectionEnabled {
		cacheLog.Print("Skipping cache-memory artifact upload (threat detection disabled)")
		return
	}

	cacheLog.Printf("Generating cache-memory artifact upload steps for %d caches", len(data.CacheMemoryConfig.Caches))

	// Use backward-compatible paths only when there's a single cache with ID "default"
	useBackwardCompatiblePaths := len(data.CacheMemoryConfig.Caches) == 1 && data.CacheMemoryConfig.Caches[0].ID == "default"

	// In workflow_call context, apply the per-invocation prefix to avoid artifact name clashes.
	prefix := artifactPrefixExprForDownstreamJob(data)

	for _, cache := range data.CacheMemoryConfig.Caches {
		// Skip restore-only caches
		if cache.RestoreOnly {
			continue
		}

		cacheDir := cacheMemoryDirFor(cache.ID)

		// Add a best-effort git integrity check and reseed step before upload.
		// This prevents upload-artifact from failing on torn/corrupt .git object stores.
		if useBackwardCompatiblePaths {
			builder.WriteString("      - name: Check cache-memory git integrity\n")
		} else {
			fmt.Fprintf(builder, "      - name: Check cache-memory git integrity (%s)\n", cache.ID)
		}
		builder.WriteString("        if: always()\n")
		builder.WriteString("        continue-on-error: true\n")
		builder.WriteString("        env:\n")
		fmt.Fprintf(builder, "          GH_AW_CACHE_DIR: %s\n", cacheDir)
		builder.WriteString("        run: bash \"${RUNNER_TEMP}/gh-aw/actions/check_cache_memory_git_integrity.sh\"\n")

		// Add upload-artifact step for each cache (runs always)
		if useBackwardCompatiblePaths {
			builder.WriteString("      - name: Upload cache-memory data as artifact\n")
		} else {
			fmt.Fprintf(builder, "      - name: Upload cache-memory data as artifact (%s)\n", cache.ID)
		}
		fmt.Fprintf(builder, "        uses: %s\n", pinAction("actions/upload-artifact"))
		if cacheHasValidationStep(cache) {
			fmt.Fprintf(builder, "        if: always() && steps.redact_secrets.outcome == 'success' && steps.%s.outcome == 'success'\n", cacheMemoryValidationStepID(cache.ID))
		} else {
			builder.WriteString("        if: always() && steps.redact_secrets.outcome == 'success'\n")
		}
		builder.WriteString("        with:\n")
		// Always use the new artifact name and path format, with prefix in workflow_call context
		if useBackwardCompatiblePaths {
			fmt.Fprintf(builder, "          name: %scache-memory\n", prefix)
		} else {
			fmt.Fprintf(builder, "          name: %scache-memory-%s\n", prefix, cache.ID)
		}
		builder.WriteString("          include-hidden-files: true\n")
		fmt.Fprintf(builder, "          path: %s\n", cacheDir)
		// Add retention-days if configured
		if cache.RetentionDays != nil {
			fmt.Fprintf(builder, "          retention-days: %d\n", *cache.RetentionDays)
		}
	}
}

// buildCacheMemoryPromptSection builds a PromptSection for cache memory instructions
// Returns a PromptSection that references a template file with substitutions, or nil if no cache is configured
func buildCacheMemoryPromptSection(config *CacheMemoryConfig) *PromptSection {
	if config == nil || len(config.Caches) == 0 {
		return nil
	}

	// Check if there's only one cache with ID "default" to use singular template
	if len(config.Caches) == 1 && config.Caches[0].ID == "default" {
		cache := config.Caches[0]
		// Trailing slash makes the path look like a directory in prompt context.
		cacheDir := cacheMemoryDirFor(cache.ID) + "/"

		// Build description text
		descriptionText := ""
		if cache.Description != "" {
			descriptionText = cache.Description
		}

		// Build allowed extensions text.
		// When non-empty, add a compact plain-text restriction line.
		// When empty (all extensions allowed), the placeholder is replaced with nothing.
		var allowedExtsText string
		if len(cache.AllowedExtensions) > 0 {
			allowedExtsText = "\nAllowed file extensions: " + strings.Join(cache.AllowedExtensions, ", ") + "."
		}

		cacheLog.Printf("Building cache memory prompt section with env vars: cache_dir=%s, description=%s, allowed_extensions=%v", cacheDir, descriptionText, cache.AllowedExtensions)

		// Return prompt section with template file and environment variables for substitution
		return &PromptSection{
			Content: cacheMemoryPromptFile,
			IsFile:  true,
			EnvVars: map[string]string{
				"GH_AW_CACHE_DIR":          cacheDir,
				"GH_AW_CACHE_DESCRIPTION":  descriptionText,
				"GH_AW_ALLOWED_EXTENSIONS": allowedExtsText,
			},
		}
	}

	// Multiple caches or non-default single cache - use template file with substitutions
	cacheLog.Print("Building cache memory prompt section for multiple caches using template")

	// Build cache list
	var cacheList strings.Builder
	for _, cache := range config.Caches {
		// Trailing slash makes the path look like a directory in prompt context.
		cacheDir := cacheMemoryDirFor(cache.ID) + "/"
		if cache.Description != "" {
			fmt.Fprintf(&cacheList, "- **%s**: `%s` - %s\n", cache.ID, cacheDir, cache.Description)
		} else {
			fmt.Fprintf(&cacheList, "- **%s**: `%s`\n", cache.ID, cacheDir)
		}
	}

	// Build allowed extensions text.
	// Compute the union of all allowed extensions across all caches.
	// When non-empty, add a compact plain-text restriction line.
	// When empty (all extensions allowed for all caches), the placeholder is replaced with nothing.
	allSame := true
	for i := 1; i < len(config.Caches); i++ {
		if len(config.Caches[i].AllowedExtensions) != len(config.Caches[0].AllowedExtensions) {
			allSame = false
			break
		}
		for j, ext := range config.Caches[i].AllowedExtensions {
			if ext != config.Caches[0].AllowedExtensions[j] {
				allSame = false
				break
			}
		}
		if !allSame {
			break
		}
	}

	var extsUnion []string
	if allSame {
		extsUnion = config.Caches[0].AllowedExtensions
	} else {
		extensionSet := make(map[string]struct {
		})
		for _, cache := range config.Caches {
			for _, ext := range cache.AllowedExtensions {
				extensionSet[ext] = struct {
				}{}
			}
		}
		for ext := range extensionSet {
			extsUnion = append(extsUnion, ext)
		}
		sort.Strings(extsUnion)
	}

	var allowedExtsText string
	if len(extsUnion) > 0 {
		allowedExtsText = "\nAllowed file extensions: " + strings.Join(extsUnion, ", ") + "."
	}

	// Build cache examples
	var cacheExamples strings.Builder
	for _, cache := range config.Caches {
		cacheDir := cacheMemoryDirFor(cache.ID)
		fmt.Fprintf(&cacheExamples, "- `%s/notes.txt` - general notes and observations\n", cacheDir)
		fmt.Fprintf(&cacheExamples, "- `%s/notes.md` - markdown formatted notes\n", cacheDir)
		fmt.Fprintf(&cacheExamples, "- `%s/preferences.json` - user preferences and settings\n", cacheDir)
		fmt.Fprintf(&cacheExamples, "- `%s/history.jsonl` - activity history in JSON Lines format\n", cacheDir)
		fmt.Fprintf(&cacheExamples, "- `%s/data.csv` - tabular data\n", cacheDir)
		fmt.Fprintf(&cacheExamples, "- `%s/state/` - organized state files in subdirectories (with allowed file types)\n", cacheDir)
	}

	return &PromptSection{
		Content: cacheMemoryPromptMultiFile,
		IsFile:  true,
		EnvVars: map[string]string{
			"GH_AW_CACHE_LIST":         cacheList.String(),
			"GH_AW_ALLOWED_EXTENSIONS": allowedExtsText,
			"GH_AW_CACHE_EXAMPLES":     cacheExamples.String(),
		},
	}
}

// buildUpdateCacheMemoryJob builds a job that updates cache-memory after detection passes
// This job downloads cache-memory artifacts and saves them to GitHub Actions cache
func (c *Compiler) buildUpdateCacheMemoryJob(data *WorkflowData, threatDetectionEnabled bool) (*Job, error) {
	if data.CacheMemoryConfig == nil || len(data.CacheMemoryConfig.Caches) == 0 {
		return nil, nil
	}

	// Only create this job if threat detection is enabled
	// Otherwise, cache is updated automatically by actions/cache post-action
	if !threatDetectionEnabled {
		return nil, nil
	}

	cacheLog.Printf("Building update_cache_memory job for %d caches (threatDetectionEnabled=%v)", len(data.CacheMemoryConfig.Caches), threatDetectionEnabled)

	var steps []string
	hasCacheSaveStep := false

	// Build steps for each cache
	// In workflow_call context, use the per-invocation prefix from the agent job.
	cacheArtifactPrefix := artifactPrefixExprForAgentDownstreamJob(data)

	for _, cache := range data.CacheMemoryConfig.Caches {
		// Skip restore-only caches
		if cache.RestoreOnly {
			continue
		}

		// Determine artifact name and cache directory.
		// Apply the workflow_call prefix to ensure we download the correct invocation's artifact.
		cacheDir := cacheMemoryDirFor(cache.ID)
		var artifactName string
		if cache.ID == "default" {
			artifactName = cacheArtifactPrefix + "cache-memory"
		} else {
			artifactName = cacheArtifactPrefix + "cache-memory-" + cache.ID
		}

		// Download artifact step
		var downloadStep strings.Builder
		// Generate a safe step ID from cache ID (replace hyphens with underscores)
		downloadStepID := strings.ReplaceAll("download_cache_"+cache.ID, "-", "_")
		fmt.Fprintf(&downloadStep, "      - name: Download cache-memory artifact (%s)\n", cache.ID)
		fmt.Fprintf(&downloadStep, "        id: %s\n", downloadStepID)
		fmt.Fprintf(&downloadStep, "        uses: %s\n", c.getActionPin("actions/download-artifact"))
		downloadStep.WriteString("        continue-on-error: true\n")
		downloadStep.WriteString("        with:\n")
		fmt.Fprintf(&downloadStep, "          name: %s\n", artifactName)
		fmt.Fprintf(&downloadStep, "          path: %s\n", cacheDir)
		steps = append(steps, downloadStep.String())

		// Check if cache folder exists and is not empty
		var checkStep strings.Builder
		checkStepID := strings.ReplaceAll("check_cache_"+cache.ID, "-", "_")
		fmt.Fprintf(&checkStep, "      - name: Check if cache-memory folder has content (%s)\n", cache.ID)
		fmt.Fprintf(&checkStep, "        id: %s\n", checkStepID)
		checkStep.WriteString("        shell: bash\n")
		checkStep.WriteString("        run: |\n")
		fmt.Fprintf(&checkStep, "          if [ -d \"%s\" ] && [ \"$(ls -A %s 2>/dev/null)\" ]; then\n", cacheDir, cacheDir)
		checkStep.WriteString("            echo \"has_content=true\" >> \"$GITHUB_OUTPUT\"\n")
		checkStep.WriteString("          else\n")
		checkStep.WriteString("            echo \"has_content=false\" >> \"$GITHUB_OUTPUT\"\n")
		checkStep.WriteString("          fi\n")
		steps = append(steps, checkStep.String())

		if !cacheHasValidationStep(cache) {
			cacheLog.Printf("Skipping validation step for cache %s in update job (empty allowed-extensions and no custom validation)", cache.ID)
		} else {
			allowedExtsJSON, _ := json.Marshal(cache.AllowedExtensions) //nolint:jsonmarshalignoredeerror // marshaling a string slice cannot fail
			var validationStep strings.Builder
			fmt.Fprintf(&validationStep, "      - name: Validate cache-memory before save (%s)\n", cache.ID)
			fmt.Fprintf(&validationStep, "        id: %s\n", cacheMemoryValidationStepID(cache.ID))
			fmt.Fprintf(&validationStep, "        if: steps.%s.outputs.has_content == 'true'\n", checkStepID)
			fmt.Fprintf(&validationStep, "        uses: %s\n", getCachedActionPin("actions/github-script", data))
			validationStep.WriteString("        env:\n")
			fmt.Fprintf(&validationStep, "          MEMORY_DIR: %s\n", cacheDir)
			fmt.Fprintf(&validationStep, "          MEMORY_ID: %s\n", cache.ID)
			fmt.Fprintf(&validationStep, "          ALLOWED_EXTENSIONS: '%s'\n", allowedExtsJSON)
			if cache.Validation != nil {
				fmt.Fprintf(&validationStep, "          VALIDATION_SCRIPT_B64: %s\n", memoryValidationScriptBase64(cache.Validation))
				fmt.Fprintf(&validationStep, "          VALIDATION_TIMEOUT_SECONDS: %d\n", memoryValidationTimeoutSeconds(cache.Validation))
			}
			validationStep.WriteString("        with:\n")
			validationStep.WriteString("          script: |\n")
			validationStep.WriteString("            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');\n")
			validationStep.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
			validationStep.WriteString("            const { validateMemoryStep } = require('${{ runner.temp }}/gh-aw/actions/validate_memory_step.cjs');\n")
			validationStep.WriteString("            validateMemoryStep(core, { kind: 'cache' });\n")
			steps = append(steps, validationStep.String())
		}

		// Generate cache key using integrity-aware format (matches generateCacheMemorySteps)
		var githubConfig *GitHubToolConfig
		if data.ParsedTools != nil {
			githubConfig = data.ParsedTools.GitHub
		}
		cacheKey := computeIntegrityCacheKey(cache, githubConfig)

		// Ensure run_id suffix is present
		runIdSuffix := "-${{ github.run_id }}"
		if !strings.HasSuffix(cacheKey, runIdSuffix) {
			cacheKey = cacheKey + runIdSuffix
		}

		// Save to cache step - only run if cache has content
		var saveStep strings.Builder
		fmt.Fprintf(&saveStep, "      - name: Save cache-memory to cache (%s)\n", cache.ID)
		if cacheHasValidationStep(cache) {
			fmt.Fprintf(&saveStep, "        if: steps.%s.outputs.has_content == 'true' && steps.%s.outcome == 'success'\n", checkStepID, cacheMemoryValidationStepID(cache.ID))
		} else {
			fmt.Fprintf(&saveStep, "        if: steps.%s.outputs.has_content == 'true'\n", checkStepID)
		}
		fmt.Fprintf(&saveStep, "        uses: %s\n", getActionPin("actions/cache/save"))
		saveStep.WriteString("        with:\n")
		fmt.Fprintf(&saveStep, "          key: %s\n", cacheKey)
		fmt.Fprintf(&saveStep, "          path: %s\n", cacheDir)
		steps = append(steps, saveStep.String())
		hasCacheSaveStep = true
	}

	// If no writable caches, return nil
	if len(steps) == 0 {
		return nil, nil
	}

	// Add setup step to copy scripts at the beginning
	var setupSteps []string
	setupActionRef := c.resolveActionReference("./actions/setup", data)
	if setupActionRef != "" || c.actionMode.IsScript() {
		// For dev mode (local action path), checkout the actions folder first
		setupSteps = append(setupSteps, c.generateCheckoutActionsFolder(data)...)

		// Cache restore job doesn't need project support
		// Cache job depends on agent job; reuse the agent's trace ID so all jobs share one OTLP trace
		cacheTraceID := fmt.Sprintf("${{ needs.%s.outputs.setup-trace-id }}", constants.ActivationJobName)
		cacheParentSpanID := setupParentSpanNeedsExpr(constants.ActivationJobName)
		setupSteps = append(setupSteps, c.generateSetupStep(data, setupActionRef, SetupActionDestination, false, cacheTraceID, cacheParentSpanID)...)
	}

	// Prepend setup steps to all cache steps
	steps = append(setupSteps, steps...)

	// Job condition: run only if detection job succeeded (no threats found),
	// AND the agent job succeeded (do not persist cache when agent failed or was skipped).
	// Using always() so this condition is evaluated even if an upstream job is skipped/failed.
	// Detection always runs when the agent ran (even for noop), so detection.result == 'success'
	// is sufficient — detection short-circuits with success=true when there is nothing to analyze.
	agentSucceeded := BuildEquals(
		BuildPropertyAccess(fmt.Sprintf("needs.%s.result", constants.AgentJobName)),
		BuildStringLiteral("success"),
	)
	jobCondition := RenderCondition(BuildAnd(BuildAnd(BuildFunctionCall("always"), buildDetectionSuccessCondition()), agentSucceeded))

	// Set up permissions for the cache update job.
	// actions: write is required only when this job actually emits cache-save steps.
	// Without it, cache saves fail with "cache write denied: token has no writable scopes".
	perms := NewPermissionsEmpty()
	if hasCacheSaveStep {
		perms.Set(PermissionActions, PermissionWrite)
	}
	if setupActionRef != "" && len(c.generateCheckoutActionsFolder(data)) > 0 {
		// In dev mode (local action path), also need contents: read to checkout the actions folder
		perms.Set(PermissionContents, PermissionRead)
	}
	permissions := perms.RenderToYAML()

	// Set GH_AW_WORKFLOW_ID_SANITIZED so cache keys match those used in the agent job
	var jobEnv map[string]string
	if data.WorkflowID != "" {
		jobEnv = map[string]string{
			"GH_AW_WORKFLOW_ID_SANITIZED": SanitizeWorkflowIDForCacheKey(data.WorkflowID),
		}
	}

	job := &Job{
		Name:        updateCacheMemoryJobName,
		DisplayName: "", // No display name - job ID is sufficient
		RunsOn:      c.formatFrameworkJobRunsOn(data),
		If:          jobCondition,
		Permissions: permissions,
		Needs:       []string{string(constants.AgentJobName), string(constants.DetectionJobName), string(constants.ActivationJobName)},
		Env:         jobEnv,
		Steps:       steps,
	}

	return job, nil
}
