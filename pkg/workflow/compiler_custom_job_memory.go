package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var customJobMemoryLog = logger.New("workflow:compiler_custom_job_memory")

// restoreMemoryConfig holds the parsed restore-memory configuration for a custom job.
// Each field is set to true only when the corresponding memory store is configured in tools:.
// No write-back or commit steps are ever emitted for restore-memory.
type restoreMemoryConfig struct {
	CacheMemory   bool
	DriveMemory   bool
	RepoMemory    bool
	CommentMemory bool
}

// extractRestoreMemoryConfig parses the restore-memory field from a custom job config map.
// The field accepts a boolean: true enables all memory stores that are configured in tools:,
// false (or absent) disables restore-memory entirely.
// Returns nil if the field is absent or false, an error if the value is not a boolean,
// and an error if true is set but no memory stores are configured in tools:.
func extractRestoreMemoryConfig(configMap map[string]any, jobName string, data *WorkflowData) (*restoreMemoryConfig, error) {
	rawVal, hasField := configMap["restore-memory"]
	if !hasField {
		return nil, nil
	}

	enabled, ok := rawVal.(bool)
	if !ok {
		return nil, fmt.Errorf("jobs.%s.restore-memory must be a boolean (true or false)", jobName)
	}
	if !enabled {
		return nil, nil
	}

	cfg := &restoreMemoryConfig{
		CacheMemory:   data.CacheMemoryConfig != nil && len(data.CacheMemoryConfig.Caches) > 0,
		DriveMemory:   data.DriveMemoryConfig != nil && len(data.DriveMemoryConfig.Drives) > 0,
		RepoMemory:    data.RepoMemoryConfig != nil && len(data.RepoMemoryConfig.Memories) > 0,
		CommentMemory: data.CommentMemoryConfig != nil,
	}

	if !cfg.CacheMemory && !cfg.DriveMemory && !cfg.RepoMemory && !cfg.CommentMemory {
		return nil, fmt.Errorf("jobs.%s.restore-memory: no memory stores are configured in tools", jobName)
	}
	customJobMemoryLog.Printf("restore-memory enabled for job %s: cache=%t drive=%t repo=%t comment=%t", jobName, cfg.CacheMemory, cfg.DriveMemory, cfg.RepoMemory, cfg.CommentMemory)
	return cfg, nil
}

// buildRestoreMemorySteps returns two slices:
//   - setupLines: gh-aw checkout + setup steps (only when repo-memory or comment-memory
//     is requested, since those need scripts from the setup action)
//   - memoryLines: the actual memory restore/clone/prepare steps
//
// No write-back steps are ever emitted; all injected steps are read-only.
func (c *Compiler) buildRestoreMemorySteps(cfg *restoreMemoryConfig, jobName string, data *WorkflowData) (setupLines []string, memoryLines []string, err error) {
	if cfg == nil {
		return nil, nil, nil
	}

	customJobMemoryLog.Printf("Building restore-memory steps for job %s", jobName)

	// repo-memory and comment-memory rely on scripts installed by the gh-aw setup action.
	// Inject the setup step before any memory steps so those scripts are available.
	if cfg.DriveMemory || cfg.RepoMemory || cfg.CommentMemory {
		setupActionRef := c.resolveActionReference("./actions/setup", data)
		if setupActionRef == "" && !c.actionMode.IsScript() {
			return nil, nil, fmt.Errorf("jobs.%s.restore-memory: drive-memory/repo-memory/comment-memory require the setup action but no action ref was found", jobName)
		}
		setupLines = append(setupLines, c.generateCheckoutActionsFolder(data)...)
		// Pass empty trace IDs — custom jobs do not inherit the activation span.
		setupLines = append(setupLines, c.generateSetupStep(data, setupActionRef, SetupActionDestination, false, "", "")...)
	}

	if cfg.CacheMemory {
		memoryLines = append(memoryLines, generateCacheMemoryRestoreLines(data)...)
	}
	if cfg.DriveMemory {
		memoryLines = append(memoryLines, c.generateDriveMemoryRestoreLines(data)...)
	}
	if cfg.RepoMemory {
		memoryLines = append(memoryLines, generateRepoMemoryRestoreLines(data)...)
	}

	if cfg.CommentMemory {
		if configLines, ok := c.generateCommentMemoryEarlyConfigLines(data); ok {
			memoryLines = append(memoryLines, configLines...)
			memoryLines = append(memoryLines, generateCommentMemoryRestoreLines(data)...)
		}
	}

	return setupLines, memoryLines, nil
}

func (c *Compiler) generateDriveMemoryRestoreLines(data *WorkflowData) []string {
	if data.DriveMemoryConfig == nil {
		return nil
	}
	var builder strings.Builder
	generateDriveMemorySteps(&builder, driveMemoryRestoreOnlyData(data), c.getActionPin)
	return splitGeneratedStepLines(builder.String())
}

func driveMemoryRestoreOnlyData(data *WorkflowData) *WorkflowData {
	restoreData := &WorkflowData{
		DriveMemoryConfig: &DriveMemoryConfig{Drives: make([]DriveMemoryEntry, len(data.DriveMemoryConfig.Drives))},
		ParsedTools:       data.ParsedTools,
	}
	for i, drive := range data.DriveMemoryConfig.Drives {
		drive.RestoreOnly = true
		restoreData.DriveMemoryConfig.Drives[i] = drive
	}
	return restoreData
}

func splitGeneratedStepLines(raw string) []string {
	parts := strings.SplitAfter(raw, "\n")
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			lines = append(lines, part)
		}
	}
	return lines
}

// generateCacheMemoryRestoreLines produces read-only cache-memory restore steps for a
// custom job. Unlike the agent-job path, these steps:
//   - always use actions/cache/restore (never actions/cache, so no auto-save)
//   - create the cache directory inline with mkdir -p (no script needed)
//   - omit the git integrity setup step (only relevant for the write path)
func generateCacheMemoryRestoreLines(data *WorkflowData) []string {
	if data.CacheMemoryConfig == nil || len(data.CacheMemoryConfig.Caches) == 0 {
		return nil
	}

	var lines []string
	lines = append(lines, "      # restore-memory: cache-memory (read-only restore)\n")

	var githubConfig *GitHubToolConfig
	if data.ParsedTools != nil {
		githubConfig = data.ParsedTools.GitHub
	}

	for i, cache := range data.CacheMemoryConfig.Caches {
		cacheDir := cacheMemoryDirFor(cache.ID)
		restoreStepID := fmt.Sprintf("restore_cache_memory_%d", i)

		// Create the cache directory inline; no script required.
		lines = append(lines, fmt.Sprintf("      - name: Create cache-memory directory (%s)\n", cache.ID))
		lines = append(lines, "        run: |\n")
		lines = append(lines, fmt.Sprintf("          mkdir -p %s\n", cacheDir))

		// Compute the same integrity-aware cache key as the agent job uses.
		cacheKey := computeIntegrityCacheKey(cache, githubConfig)

		// Restore step — always read-only in custom jobs.
		lines = append(lines, fmt.Sprintf("      - name: Restore cache-memory (%s)\n", cache.ID))
		lines = append(lines, fmt.Sprintf("        id: %s\n", restoreStepID))
		lines = append(lines, fmt.Sprintf("        uses: %s\n", getActionPin("actions/cache/restore")))
		lines = append(lines, "        with:\n")
		lines = append(lines, fmt.Sprintf("          key: %s\n", cacheKey))
		lines = append(lines, fmt.Sprintf("          path: %s\n", cacheDir))

		// Build restore keys using the shared helper (same semantics as the agent job).
		// Only emit the restore-keys block when there is at least one key; an empty
		// literal block scalar (restore-keys: | with no lines) is invalid YAML.
		if restoreKeys := buildCacheRestoreKeys(cacheKey, cache.Scope); len(restoreKeys) > 0 {
			lines = append(lines, "          restore-keys: |\n")
			for _, key := range restoreKeys {
				lines = append(lines, fmt.Sprintf("            %s\n", key))
			}
		}
	}

	return lines
}

// generateRepoMemoryRestoreLines produces read-only repo-memory clone steps for a
// custom job by reusing the existing generateRepoMemorySteps builder and converting
// its output to the []string line format expected by job.Steps.
func generateRepoMemoryRestoreLines(data *WorkflowData) []string {
	if data.RepoMemoryConfig == nil || len(data.RepoMemoryConfig.Memories) == 0 {
		return nil
	}

	var b strings.Builder
	generateRepoMemorySteps(&b, data)
	raw := b.String()
	if raw == "" {
		return nil
	}

	// SplitAfter keeps each line's trailing newline, so no phantom extra newline is
	// appended (unlike strings.Split + "\n" which adds a spurious blank line for the
	// empty trailing element produced by a newline-terminated string).
	return splitGeneratedStepLines(raw)
}

// generateCommentMemoryRestoreLines produces a read-only comment-memory prepare step
// for a custom job. The step fetches the comment-memory content from GitHub and
// materialises it as local files — the same operation performed in the agent job.
func generateCommentMemoryRestoreLines(data *WorkflowData) []string {
	if data.CommentMemoryConfig == nil {
		return nil
	}

	var lines []string
	lines = append(lines, "      # restore-memory: comment-memory (read-only restore)\n")
	lines = append(lines, "      - name: Prepare comment memory files\n")
	lines = append(lines, fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)))
	lines = append(lines, "        with:\n")
	lines = append(lines, fmt.Sprintf("          github-token: %s\n", resolveSafeOutputGitHubToken(data.CommentMemoryConfig.GitHubToken)))
	lines = append(lines, "          script: |\n")
	lines = append(lines, "            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');\n")
	lines = append(lines, "            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	lines = append(lines, "            const { main } = require('${{ runner.temp }}/gh-aw/actions/setup_comment_memory_files.cjs');\n")
	lines = append(lines, "            await main();\n")
	return lines
}
