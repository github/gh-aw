package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/goccy/go-yaml"
)

var cacheLog = logger.New("workflow:cache")

// CacheMemoryConfig holds configuration for cache-memory functionality
type CacheMemoryConfig struct {
	Caches []CacheMemoryEntry `yaml:"caches,omitempty"` // cache configurations
}

// CacheMemoryEntry represents a single cache-memory configuration
type CacheMemoryEntry struct {
	ID                string   `yaml:"id"`                           // cache identifier (required for array notation)
	Key               string   `yaml:"key,omitempty"`                // custom cache key
	Description       string   `yaml:"description,omitempty"`        // optional description for this cache
	RetentionDays     *int     `yaml:"retention-days,omitempty"`     // retention days for upload-artifact action
	RestoreOnly       bool     `yaml:"restore-only,omitempty"`       // if true, only restore cache without saving
	Scope             string   `yaml:"scope,omitempty"`              // scope for restore keys: "workflow" (default) or "repo"
	AllowedExtensions []string `yaml:"allowed-extensions,omitempty"` // allowed file extensions (default: [".json", ".jsonl", ".txt", ".md", ".csv"])
}

// generateDefaultCacheKey generates a default cache key for a given cache ID
// Uses GH_AW_WORKFLOW_ID_SANITIZED (workflow ID with hyphens removed) instead of github.workflow
func generateDefaultCacheKey(cacheID string) string {
	if cacheID == "default" {
		return "memory-${{ env.GH_AW_WORKFLOW_ID_SANITIZED }}-${{ github.run_id }}"
	}
	return fmt.Sprintf("memory-%s-${{ env.GH_AW_WORKFLOW_ID_SANITIZED }}-${{ github.run_id }}", cacheID)
}

// parseCacheMemoryEntry parses a single cache-memory entry from a map
func parseCacheMemoryEntry(cacheMap map[string]any, defaultID string) (CacheMemoryEntry, error) {
	entry := CacheMemoryEntry{
		ID:  defaultID,
		Key: generateDefaultCacheKey(defaultID),
	}

	// Parse ID (for array notation)
	if id, exists := cacheMap["id"]; exists {
		if idStr, ok := id.(string); ok {
			entry.ID = idStr
		}
	}
	// Update key if ID changed
	if entry.ID != defaultID {
		entry.Key = generateDefaultCacheKey(entry.ID)
	}

	// Parse custom key
	if key, exists := cacheMap["key"]; exists {
		if keyStr, ok := key.(string); ok {
			entry.Key = keyStr
			// Automatically append -${{ github.run_id }} if the key doesn't already end with it
			runIdSuffix := "-${{ github.run_id }}"
			if !strings.HasSuffix(entry.Key, runIdSuffix) {
				entry.Key = entry.Key + runIdSuffix
			}
		}
	}

	// Parse description
	if description, exists := cacheMap["description"]; exists {
		if descStr, ok := description.(string); ok {
			entry.Description = descStr
		}
	}

	// Parse retention days
	if retentionDays, exists := cacheMap["retention-days"]; exists {
		if retentionDaysInt, ok := retentionDays.(int); ok {
			entry.RetentionDays = &retentionDaysInt
		} else if retentionDaysFloat, ok := retentionDays.(float64); ok {
			retentionDaysIntValue := int(retentionDaysFloat)
			entry.RetentionDays = &retentionDaysIntValue
		} else if retentionDaysUint64, ok := retentionDays.(uint64); ok {
			retentionDaysIntValue := int(retentionDaysUint64)
			entry.RetentionDays = &retentionDaysIntValue
		}
		// Validate retention-days bounds
		if entry.RetentionDays != nil {
			if err := validateIntRange(*entry.RetentionDays, 1, 90, "retention-days"); err != nil {
				return entry, err
			}
		}
	}

	// Parse restore-only flag
	if restoreOnly, exists := cacheMap["restore-only"]; exists {
		if restoreOnlyBool, ok := restoreOnly.(bool); ok {
			entry.RestoreOnly = restoreOnlyBool
		}
	}

	// Parse scope field
	if scope, exists := cacheMap["scope"]; exists {
		if scopeStr, ok := scope.(string); ok {
			entry.Scope = scopeStr
		}
	}
	// Default to "workflow" scope if not specified
	if entry.Scope == "" {
		entry.Scope = "workflow"
	}

	// Parse allowed-extensions field
	if allowedExts, exists := cacheMap["allowed-extensions"]; exists {
		if extArray, ok := allowedExts.([]any); ok {
			entry.AllowedExtensions = make([]string, 0, len(extArray))
			for _, ext := range extArray {
				if extStr, ok := ext.(string); ok {
					entry.AllowedExtensions = append(entry.AllowedExtensions, extStr)
				}
			}
		}
	}
	// Default to standard allowed extensions if not specified
	if len(entry.AllowedExtensions) == 0 {
		entry.AllowedExtensions = constants.DefaultAllowedMemoryExtensions
	}

	return entry, nil
}

// extractCacheMemoryConfig extracts cache-memory configuration from tools section
// Updated to use ToolsConfig instead of map[string]any
func (c *Compiler) extractCacheMemoryConfig(toolsConfig *ToolsConfig) (*CacheMemoryConfig, error) {
	// Check if cache-memory tool is configured
	if toolsConfig == nil || toolsConfig.CacheMemory == nil {
		return nil, nil
	}

	cacheLog.Print("Extracting cache-memory configuration from ToolsConfig")

	config := &CacheMemoryConfig{}
	cacheMemoryValue := toolsConfig.CacheMemory.Raw

	// Handle nil value (simple enable with defaults) - same as true
	// This handles the case where cache-memory: is specified without a value
	if cacheMemoryValue == nil {
		config.Caches = []CacheMemoryEntry{
			{
				ID:                "default",
				Key:               generateDefaultCacheKey("default"),
				AllowedExtensions: constants.DefaultAllowedMemoryExtensions,
			},
		}
		return config, nil
	}

	// Handle boolean value (simple enable/disable)
	if boolValue, ok := cacheMemoryValue.(bool); ok {
		if boolValue {
			// Create a single default cache entry
			config.Caches = []CacheMemoryEntry{
				{
					ID:                "default",
					Key:               generateDefaultCacheKey("default"),
					AllowedExtensions: constants.DefaultAllowedMemoryExtensions,
				},
			}
		}
		// If false, return empty config (empty array means disabled)
		return config, nil
	}

	// Handle array of cache configurations
	if cacheArray, ok := cacheMemoryValue.([]any); ok {
		cacheLog.Printf("Processing cache array with %d entries", len(cacheArray))
		config.Caches = make([]CacheMemoryEntry, 0, len(cacheArray))
		for _, item := range cacheArray {
			if cacheMap, ok := item.(map[string]any); ok {
				entry, err := parseCacheMemoryEntry(cacheMap, "default")
				if err != nil {
					return nil, err
				}
				config.Caches = append(config.Caches, entry)
			}
		}

		// Check for duplicate cache IDs
		if err := validateNoDuplicateCacheIDs(config.Caches); err != nil {
			return nil, err
		}

		return config, nil
	}

	// Handle object configuration (single cache, backward compatible)
	// Convert to array with single entry
	if configMap, ok := cacheMemoryValue.(map[string]any); ok {
		entry, err := parseCacheMemoryEntry(configMap, "default")
		if err != nil {
			return nil, err
		}
		config.Caches = []CacheMemoryEntry{entry}
		return config, nil
	}

	return nil, nil
}

// extractCacheMemoryConfigFromMap is a backward compatibility wrapper for extractCacheMemoryConfig
// extractCacheMemoryConfigFromMap is a backward compatibility wrapper for extractCacheMemoryConfig
// that accepts map[string]any instead of *ToolsConfig. This allows gradual migration of calling code.
func (c *Compiler) extractCacheMemoryConfigFromMap(tools map[string]any) (*CacheMemoryConfig, error) {
	toolsConfig, err := ParseToolsConfig(tools)
	if err != nil {
		return nil, err
	}
	return c.extractCacheMemoryConfig(toolsConfig)
}

// generateCacheSteps generates cache steps for the workflow based on cache configuration
func generateCacheSteps(builder *strings.Builder, data *WorkflowData, verbose bool) {
	if data.Cache == "" {
		return
	}

	// Add comment indicating cache configuration was processed
	builder.WriteString("      # Cache configuration from frontmatter processed below\n")

	// Parse cache configuration to determine if it's a single cache or array
	var caches []map[string]any

	// Try to parse the cache YAML string back to determine structure
	var topLevel map[string]any
	if err := yaml.Unmarshal([]byte(data.Cache), &topLevel); err != nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "Warning: Failed to parse cache configuration: %v\n", err)
		}
		return
	}

	// Extract the cache section from the top-level map
	cacheConfig, exists := topLevel["cache"]
	if !exists {
		if verbose {
			fmt.Fprintf(os.Stderr, "Warning: No cache key found in parsed configuration\n")
		}
		return
	}

	// Handle both single cache object and array of caches
	if cacheArray, isArray := cacheConfig.([]any); isArray {
		// Multiple caches
		for _, cacheItem := range cacheArray {
			if cacheMap, ok := cacheItem.(map[string]any); ok {
				caches = append(caches, cacheMap)
			}
		}
	} else if cacheMap, isMap := cacheConfig.(map[string]any); isMap {
		// Single cache
		caches = append(caches, cacheMap)
	}

	// Generate cache steps
	for i, cache := range caches {
		stepName := "Cache"
		if len(caches) > 1 {
			stepName = fmt.Sprintf("Cache %d", i+1)
		}
		if key, hasKey := cache["key"]; hasKey {
			if keyStr, ok := key.(string); ok && keyStr != "" {
				stepName = fmt.Sprintf("Cache (%s)", keyStr)
			}
		}

		fmt.Fprintf(builder, "      - name: %s\n", stepName)
		fmt.Fprintf(builder, "        uses: %s\n", GetActionPin("actions/cache"))
		builder.WriteString("        with:\n")

		// Add required cache parameters
		if key, hasKey := cache["key"]; hasKey {
			fmt.Fprintf(builder, "          key: %v\n", key)
		}
		if path, hasPath := cache["path"]; hasPath {
			if pathArray, isArray := path.([]any); isArray {
				builder.WriteString("          path: |\n")
				for _, p := range pathArray {
					fmt.Fprintf(builder, "            %v\n", p)
				}
			} else {
				fmt.Fprintf(builder, "          path: %v\n", path)
			}
		}

		// Add optional cache parameters
		if restoreKeys, hasRestoreKeys := cache["restore-keys"]; hasRestoreKeys {
			if restoreArray, isArray := restoreKeys.([]any); isArray {
				builder.WriteString("          restore-keys: |\n")
				for _, key := range restoreArray {
					fmt.Fprintf(builder, "            %v\n", key)
				}
			} else {
				fmt.Fprintf(builder, "          restore-keys: %v\n", restoreKeys)
			}
		}
		if uploadChunkSize, hasSize := cache["upload-chunk-size"]; hasSize {
			fmt.Fprintf(builder, "          upload-chunk-size: %v\n", uploadChunkSize)
		}
		if failOnMiss, hasFail := cache["fail-on-cache-miss"]; hasFail {
			fmt.Fprintf(builder, "          fail-on-cache-miss: %v\n", failOnMiss)
		}
		if lookupOnly, hasLookup := cache["lookup-only"]; hasLookup {
			fmt.Fprintf(builder, "          lookup-only: %v\n", lookupOnly)
		}
	}
}

// generateCacheMemorySteps generates cache setup steps (directory creation and restore) for the cache-memory configuration
// Cache-memory provides a simple file share that LLMs can read/write freely
// Artifact upload is handled separately by generateCacheMemoryArtifactUpload after agent execution
func generateCacheMemorySteps(builder *strings.Builder, data *WorkflowData) {
	if data.CacheMemoryConfig == nil || len(data.CacheMemoryConfig.Caches) == 0 {
		return
	}

	cacheLog.Printf("Generating cache-memory setup steps for %d caches", len(data.CacheMemoryConfig.Caches))

	builder.WriteString("      # Cache memory file share configuration from frontmatter processed below\n")

	// Use backward-compatible paths only when there's a single cache with ID "default"
	// This maintains compatibility with existing workflows
	useBackwardCompatiblePaths := len(data.CacheMemoryConfig.Caches) == 1 && data.CacheMemoryConfig.Caches[0].ID == "default"

	for _, cache := range data.CacheMemoryConfig.Caches {
		// Default cache uses /tmp/gh-aw/cache-memory/ for backward compatibility
		// Other caches use /tmp/gh-aw/cache-memory-{id}/ to prevent overlaps
		var cacheDir string
		if cache.ID == "default" {
			cacheDir = "/tmp/gh-aw/cache-memory"
		} else {
			cacheDir = fmt.Sprintf("/tmp/gh-aw/cache-memory-%s", cache.ID)
		}

		// Add step to create cache-memory directory for this cache
		if useBackwardCompatiblePaths {
			// For single default cache, use the original directory for backward compatibility
			builder.WriteString("      - name: Create cache-memory directory\n")
			builder.WriteString("        run: bash /opt/gh-aw/actions/create_cache_memory_dir.sh\n")
		} else {
			fmt.Fprintf(builder, "      - name: Create cache-memory directory (%s)\n", cache.ID)
			builder.WriteString("        run: |\n")
			fmt.Fprintf(builder, "          mkdir -p %s\n", cacheDir)
		}

		cacheKey := cache.Key
		if cacheKey == "" {
			if useBackwardCompatiblePaths {
				cacheKey = "memory-${{ env.GH_AW_WORKFLOW_ID_SANITIZED }}-${{ github.run_id }}"
			} else {
				cacheKey = fmt.Sprintf("memory-%s-${{ env.GH_AW_WORKFLOW_ID_SANITIZED }}-${{ github.run_id }}", cache.ID)
			}
		}

		// Automatically append -${{ github.run_id }} if the key doesn't already end with it
		runIdSuffix := "-${{ github.run_id }}"
		if !strings.HasSuffix(cacheKey, runIdSuffix) {
			cacheKey = cacheKey + runIdSuffix
		}

		// Generate restore keys based on scope
		// - "workflow" (default): Single restore key with workflow ID (secure)
		// - "repo": Two restore keys - with and without workflow ID (allows cross-workflow sharing)
		var restoreKeys []string

		// Determine scope (default to "workflow" for safety)
		scope := cache.Scope
		if scope == "" {
			scope = "workflow"
		}

		// First restore key: remove the run_id suffix as a single unit (don't split the key)
		// The cacheKey always ends with "-${{ github.run_id }}" (ensured by code above)
		if strings.HasSuffix(cacheKey, runIdSuffix) {
			// Remove the run_id suffix to create the restore key
			restoreKey := strings.TrimSuffix(cacheKey, "${{ github.run_id }}") // Keep the trailing "-"
			restoreKeys = append(restoreKeys, restoreKey)
		} else {
			// Fallback: split on last dash if run_id suffix not found
			// This handles edge cases where the key format might be different
			keyParts := strings.Split(cacheKey, "-")
			if len(keyParts) >= 2 {
				workflowLevelKey := strings.Join(keyParts[:len(keyParts)-1], "-") + "-"
				restoreKeys = append(restoreKeys, workflowLevelKey)
			}
		}

		// For repo scope, add an additional restore key without the workflow ID
		// This allows cache sharing across all workflows in the repository
		if scope == "repo" {
			// Remove both workflow and run_id to create a repo-wide restore key
			// For example: "memory-chroma-${{ env.GH_AW_WORKFLOW_ID_SANITIZED }}-${{ github.run_id }}" -> "memory-chroma-"
			repoKey := strings.TrimSuffix(cacheKey, "${{ env.GH_AW_WORKFLOW_ID_SANITIZED }}-${{ github.run_id }}")
			if repoKey != cacheKey && repoKey != "" {
				restoreKeys = append(restoreKeys, repoKey)
			}
		}

		// Step name and action
		// Use actions/cache/restore for restore-only caches or when threat detection is enabled
		// When threat detection is enabled, we only restore the cache and defer saving to a separate job after detection
		// Use actions/cache for normal caches (which auto-saves via post-action)
		threatDetectionEnabled := data.SafeOutputs != nil && data.SafeOutputs.ThreatDetection != nil
		useRestoreOnly := cache.RestoreOnly || threatDetectionEnabled

		var actionName string
		if useRestoreOnly {
			actionName = "Restore cache-memory file share data"
		} else {
			actionName = "Cache cache-memory file share data"
		}

		if useBackwardCompatiblePaths {
			fmt.Fprintf(builder, "      - name: %s\n", actionName)
		} else {
			fmt.Fprintf(builder, "      - name: %s (%s)\n", actionName, cache.ID)
		}

		// Use actions/cache/restore@v4 when restore-only or threat detection enabled
		// Use actions/cache@v4 for normal caches
		if useRestoreOnly {
			fmt.Fprintf(builder, "        uses: %s\n", GetActionPin("actions/cache/restore"))
		} else {
			fmt.Fprintf(builder, "        uses: %s\n", GetActionPin("actions/cache"))
		}
		builder.WriteString("        with:\n")
		fmt.Fprintf(builder, "          key: %s\n", cacheKey)

		// Path - always use the new cache directory format
		fmt.Fprintf(builder, "          path: %s\n", cacheDir)

		builder.WriteString("          restore-keys: |\n")
		for _, key := range restoreKeys {
			fmt.Fprintf(builder, "            %s\n", key)
		}
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

		// Skip validation step if allowed extensions is empty (means all files are allowed)
		if len(cache.AllowedExtensions) == 0 {
			cacheLog.Printf("Skipping validation step for cache %s (empty allowed-extensions means all files are allowed)", cache.ID)
			continue
		}

		// Default cache uses /tmp/gh-aw/cache-memory/ for backward compatibility
		// Other caches use /tmp/gh-aw/cache-memory-{id}/ to prevent overlaps
		var cacheDir string
		if cache.ID == "default" {
			cacheDir = "/tmp/gh-aw/cache-memory"
		} else {
			cacheDir = fmt.Sprintf("/tmp/gh-aw/cache-memory-%s", cache.ID)
		}

		// Prepare allowed extensions array for JavaScript
		allowedExtsJSON, _ := json.Marshal(cache.AllowedExtensions)

		// Build validation script
		var validationScript strings.Builder
		validationScript.WriteString("            const { setupGlobals } = require('/opt/gh-aw/actions/setup_globals.cjs');\n")
		validationScript.WriteString("            setupGlobals(core, github, context, exec, io);\n")
		validationScript.WriteString("            const { validateMemoryFiles } = require('/opt/gh-aw/actions/validate_memory_files.cjs');\n")
		fmt.Fprintf(&validationScript, "            const allowedExtensions = %s;\n", allowedExtsJSON)
		fmt.Fprintf(&validationScript, "            const result = validateMemoryFiles('%s', 'cache', allowedExtensions);\n", cacheDir)
		validationScript.WriteString("            if (!result.valid) {\n")
		fmt.Fprintf(&validationScript, "              core.setFailed(`File type validation failed: Found $${result.invalidFiles.length} file(s) with invalid extensions. Only %s are allowed.`);\n", strings.Join(cache.AllowedExtensions, ", "))
		validationScript.WriteString("            }\n")

		// Generate validation step using helper
		stepName := "Validate cache-memory file types"
		if !useBackwardCompatiblePaths {
			stepName = fmt.Sprintf("Validate cache-memory file types (%s)", cache.ID)
		}
		builder.WriteString(generateInlineGitHubScriptStep(stepName, validationScript.String(), "always()"))
	}
}

// generateCacheMemoryArtifactUpload generates artifact upload steps for cache-memory
// This should be called after agent execution steps to ensure cache is uploaded after the agent has finished
func generateCacheMemoryArtifactUpload(builder *strings.Builder, data *WorkflowData) {
	if data.CacheMemoryConfig == nil || len(data.CacheMemoryConfig.Caches) == 0 {
		return
	}

	// Only upload artifacts when threat detection is enabled (needed for update_cache_memory job)
	// When threat detection is disabled, cache is saved automatically by actions/cache post-action
	threatDetectionEnabled := data.SafeOutputs != nil && data.SafeOutputs.ThreatDetection != nil
	if !threatDetectionEnabled {
		cacheLog.Print("Skipping cache-memory artifact upload (threat detection disabled)")
		return
	}

	cacheLog.Printf("Generating cache-memory artifact upload steps for %d caches", len(data.CacheMemoryConfig.Caches))

	// Use backward-compatible paths only when there's a single cache with ID "default"
	useBackwardCompatiblePaths := len(data.CacheMemoryConfig.Caches) == 1 && data.CacheMemoryConfig.Caches[0].ID == "default"

	for _, cache := range data.CacheMemoryConfig.Caches {
		// Skip restore-only caches
		if cache.RestoreOnly {
			continue
		}

		// Default cache uses /tmp/gh-aw/cache-memory/ for backward compatibility
		// Other caches use /tmp/gh-aw/cache-memory-{id}/ to prevent overlaps
		var cacheDir string
		if cache.ID == "default" {
			cacheDir = "/tmp/gh-aw/cache-memory"
		} else {
			cacheDir = fmt.Sprintf("/tmp/gh-aw/cache-memory-%s", cache.ID)
		}

		// Add upload-artifact step for each cache (runs always)
		if useBackwardCompatiblePaths {
			builder.WriteString("      - name: Upload cache-memory data as artifact\n")
		} else {
			fmt.Fprintf(builder, "      - name: Upload cache-memory data as artifact (%s)\n", cache.ID)
		}
		fmt.Fprintf(builder, "        uses: %s\n", GetActionPin("actions/upload-artifact"))
		builder.WriteString("        if: always()\n")
		builder.WriteString("        with:\n")
		// Always use the new artifact name and path format
		if useBackwardCompatiblePaths {
			builder.WriteString("          name: cache-memory\n")
		} else {
			fmt.Fprintf(builder, "          name: cache-memory-%s\n", cache.ID)
		}
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
		cacheDir := "/tmp/gh-aw/cache-memory/"

		// Build description text
		descriptionText := ""
		if cache.Description != "" {
			descriptionText = " " + cache.Description
		}

		// Build allowed extensions text
		allowedExtsText := strings.Join(cache.AllowedExtensions, ", ")

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
		var cacheDir string
		if cache.ID == "default" {
			cacheDir = "/tmp/gh-aw/cache-memory/"
		} else {
			cacheDir = fmt.Sprintf("/tmp/gh-aw/cache-memory-%s/", cache.ID)
		}
		if cache.Description != "" {
			fmt.Fprintf(&cacheList, "- **%s**: `%s` - %s\n", cache.ID, cacheDir, cache.Description)
		} else {
			fmt.Fprintf(&cacheList, "- **%s**: `%s`\n", cache.ID, cacheDir)
		}
	}

	// Build allowed extensions text
	// Check if all caches have the same allowed extensions
	allowedExtsText := strings.Join(config.Caches[0].AllowedExtensions, ", ")
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

	// If not all the same, build a union of all extensions
	if !allSame {
		extensionSet := make(map[string]bool)
		for _, cache := range config.Caches {
			for _, ext := range cache.AllowedExtensions {
				extensionSet[ext] = true
			}
		}
		// Convert set to sorted slice for consistent output
		var allExtensions []string
		for ext := range extensionSet {
			allExtensions = append(allExtensions, ext)
		}
		sort.Strings(allExtensions)
		allowedExtsText = strings.Join(allExtensions, ", ")
	}

	// Build cache examples
	var cacheExamples strings.Builder
	for _, cache := range config.Caches {
		var cacheDir string
		if cache.ID == "default" {
			cacheDir = "/tmp/gh-aw/cache-memory"
		} else {
			cacheDir = fmt.Sprintf("/tmp/gh-aw/cache-memory-%s", cache.ID)
		}
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

	// Build steps for each cache
	for _, cache := range data.CacheMemoryConfig.Caches {
		// Skip restore-only caches
		if cache.RestoreOnly {
			continue
		}

		// Determine artifact name and cache directory
		var artifactName, cacheDir string
		if cache.ID == "default" {
			artifactName = "cache-memory"
			cacheDir = "/tmp/gh-aw/cache-memory"
		} else {
			artifactName = fmt.Sprintf("cache-memory-%s", cache.ID)
			cacheDir = fmt.Sprintf("/tmp/gh-aw/cache-memory-%s", cache.ID)
		}

		// Download artifact step
		var downloadStep strings.Builder
		fmt.Fprintf(&downloadStep, "      - name: Download cache-memory artifact (%s)\n", cache.ID)
		fmt.Fprintf(&downloadStep, "        uses: %s\n", GetActionPin("actions/download-artifact"))
		downloadStep.WriteString("        continue-on-error: true\n")
		downloadStep.WriteString("        with:\n")
		fmt.Fprintf(&downloadStep, "          name: %s\n", artifactName)
		fmt.Fprintf(&downloadStep, "          path: %s\n", cacheDir)
		steps = append(steps, downloadStep.String())

		// Skip validation step if allowed extensions is empty (means all files are allowed)
		if len(cache.AllowedExtensions) == 0 {
			cacheLog.Printf("Skipping validation step for cache %s in update job (empty allowed-extensions means all files are allowed)", cache.ID)
		} else {
			// Prepare allowed extensions array for JavaScript
			allowedExtsJSON, _ := json.Marshal(cache.AllowedExtensions)

			// Build validation script
			var validationScript strings.Builder
			validationScript.WriteString("            const { setupGlobals } = require('/opt/gh-aw/actions/setup_globals.cjs');\n")
			validationScript.WriteString("            setupGlobals(core, github, context, exec, io);\n")
			validationScript.WriteString("            const { validateMemoryFiles } = require('/opt/gh-aw/actions/validate_memory_files.cjs');\n")
			fmt.Fprintf(&validationScript, "            const allowedExtensions = %s;\n", allowedExtsJSON)
			fmt.Fprintf(&validationScript, "            const result = validateMemoryFiles('%s', 'cache', allowedExtensions);\n", cacheDir)
			validationScript.WriteString("            if (!result.valid) {\n")
			fmt.Fprintf(&validationScript, "              core.setFailed(`File type validation failed: Found ${result.invalidFiles.length} file(s) with invalid extensions. Only %s are allowed.`);\n", strings.Join(cache.AllowedExtensions, ", "))
			validationScript.WriteString("            }\n")

			// Generate validation step using helper
			stepName := fmt.Sprintf("Validate cache-memory file types (%s)", cache.ID)
			steps = append(steps, generateInlineGitHubScriptStep(stepName, validationScript.String(), ""))
		}

		// Generate cache key (same logic as in generateCacheMemorySteps)
		cacheKey := cache.Key
		if cacheKey == "" {
			if cache.ID == "default" {
				cacheKey = "memory-${{ env.GH_AW_WORKFLOW_ID_SANITIZED }}-${{ github.run_id }}"
			} else {
				cacheKey = fmt.Sprintf("memory-%s-${{ env.GH_AW_WORKFLOW_ID_SANITIZED }}-${{ github.run_id }}", cache.ID)
			}
		}

		// Automatically append -${{ github.run_id }} if the key doesn't already end with it
		runIdSuffix := "-${{ github.run_id }}"
		if !strings.HasSuffix(cacheKey, runIdSuffix) {
			cacheKey = cacheKey + runIdSuffix
		}

		// Save to cache step
		var saveStep strings.Builder
		fmt.Fprintf(&saveStep, "      - name: Save cache-memory to cache (%s)\n", cache.ID)
		fmt.Fprintf(&saveStep, "        uses: %s\n", GetActionPin("actions/cache/save"))
		saveStep.WriteString("        with:\n")
		fmt.Fprintf(&saveStep, "          key: %s\n", cacheKey)
		fmt.Fprintf(&saveStep, "          path: %s\n", cacheDir)
		steps = append(steps, saveStep.String())
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
		setupSteps = append(setupSteps, c.generateSetupStep(setupActionRef, SetupActionDestination, false)...)
	}

	// Prepend setup steps to all cache steps
	steps = append(setupSteps, steps...)

	// Job condition: only run if detection passed
	jobCondition := "always() && needs.detection.outputs.success == 'true'"

	// Set up permissions for the cache update job
	// If using local actions (dev mode without action-tag), we need contents: read to checkout the actions folder
	permissions := NewPermissionsEmpty().RenderToYAML() // Default: no special permissions needed
	if setupActionRef != "" && len(c.generateCheckoutActionsFolder(data)) > 0 {
		// Need contents: read to checkout the actions folder
		perms := NewPermissionsContentsRead()
		permissions = perms.RenderToYAML()
	}

	job := &Job{
		Name:        "update_cache_memory",
		DisplayName: "", // No display name - job ID is sufficient
		RunsOn:      "runs-on: ubuntu-latest",
		If:          jobCondition,
		Permissions: permissions,
		Needs:       []string{"agent", "detection"},
		Steps:       steps,
	}

	return job, nil
}
