package workflow

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var cacheLog = logger.New("workflow:cache")

// defaultCacheMemoryDir is the canonical runtime path for the default cache-memory.
// Backward-compatible: workflows that were compiled before multi-cache support was added
// continue to use this exact path.
const defaultCacheMemoryDir = "/tmp/gh-aw/cache-memory"

// cacheMemoryDirPrefix is the path prefix for non-default cache-memory directories.
// The full path is formed by appending the cache ID: cacheMemoryDirPrefix + cacheID.
const cacheMemoryDirPrefix = "/tmp/gh-aw/cache-memory-"

// cacheMemoryDirFor returns the canonical runtime directory for the given cache ID.
// Default cache → /tmp/gh-aw/cache-memory
// Named cache   → /tmp/gh-aw/cache-memory-{id}
//
// The returned path has no trailing slash. Callers that display the path as a directory
// (e.g. in LLM prompt context) should append "/" explicitly.
//
// An empty cacheID is treated the same as "default" as a safety net, though callers
// should always provide a non-empty ID.
//
// Non-default IDs must have already been validated by isValidCacheID before reaching
// this function. This function panics on invalid IDs as a defence-in-depth measure
// (the parser should have rejected them first).
func cacheMemoryDirFor(cacheID string) string {
	if cacheID == "default" || cacheID == "" {
		return defaultCacheMemoryDir
	}

	if !isValidCacheID(cacheID) {
		// This should never happen: parseCacheMemoryEntry validates IDs at parse time.
		// Panic here to surface a clear programming error rather than silently producing
		// a dangerous path.
		panic(fmt.Sprintf("cacheMemoryDirFor called with invalid cache ID %q; IDs must match [A-Za-z0-9_-]{1,64}", cacheID))
	}
	return cacheMemoryDirPrefix + cacheID
}

func cacheMemoryValidationStepID(cacheID string) string {
	return memoryValidationStepID("validate_cache_memory", cacheID)
}

func cacheHasValidationStep(cache CacheMemoryEntry) bool {
	return len(cache.AllowedExtensions) > 0 || cache.Validation != nil
}

// validCacheMemoryScopes defines the allowed values for cache-memory scope
var validCacheMemoryScopes = []string{"workflow", "repo"}

// isValidCacheID reports whether id is a safe cache identifier.
// Allowed pattern: ^[A-Za-z0-9_-]{1,64}$ (1-64 characters).
// This prevents path-traversal attacks (e.g. "../../etc") when the ID is
// appended to cacheMemoryDirPrefix to form a filesystem path.
func isValidCacheID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, c := range id {
		isLower := c >= 'a' && c <= 'z'
		isUpper := c >= 'A' && c <= 'Z'
		isDigit := c >= '0' && c <= '9'
		isAllowed := c == '_' || c == '-'
		if !isLower && !isUpper && !isDigit && !isAllowed {
			return false
		}
	}
	return true
}

// isValidFileExtension reports whether s is a valid file extension of the form ^\.[A-Za-z0-9]+$
// (e.g. ".json", ".md"). This strict pattern prevents YAML injection when extensions are
// embedded in generated workflow YAML as single-quoted scalars.
func isValidFileExtension(s string) bool {
	if len(s) < 2 || s[0] != '.' {
		return false
	}
	for _, c := range s[1:] {
		isLower := c >= 'a' && c <= 'z'
		isUpper := c >= 'A' && c <= 'Z'
		isDigit := c >= '0' && c <= '9'
		if !isLower && !isUpper && !isDigit {
			return false
		}
	}
	return true
}

// CacheMemoryConfig holds configuration for cache-memory functionality
type CacheMemoryConfig struct {
	Caches []CacheMemoryEntry `yaml:"caches,omitempty"` // cache configurations
}

// CacheMemoryEntry represents a single cache-memory configuration
type CacheMemoryEntry struct {
	ID                string                  `yaml:"id"`                           // cache identifier (required for array notation)
	Key               string                  `yaml:"key,omitempty"`                // custom cache key
	Description       string                  `yaml:"description,omitempty"`        // optional description for this cache
	RetentionDays     *int                    `yaml:"retention-days,omitempty"`     // retention days for upload-artifact action
	RestoreOnly       bool                    `yaml:"restore-only,omitempty"`       // if true, only restore cache without saving
	Scope             string                  `yaml:"scope,omitempty"`              // scope for restore keys: "workflow" (default) or "repo"
	AllowedExtensions []string                `yaml:"allowed-extensions,omitempty"` // allowed file extensions (default: [".json", ".jsonl", ".txt", ".md", ".csv"])
	Validation        *MemoryValidationConfig `yaml:"validation,omitempty"`         // optional custom JavaScript validation hook
}

// generateDefaultCacheKey generates a default cache key for a given cache ID.
// Uses the legacy format (without integrity prefix) for backward compatibility when
// computing keys during initial entry parsing. The final key used in generated steps
// is produced by computeIntegrityCacheKey, which includes integrity level and policy hash.
func generateDefaultCacheKey(cacheID string) string {
	if cacheID == "default" {
		return "memory-${{ env.GH_AW_WORKFLOW_ID_SANITIZED }}-${{ github.run_id }}"
	}
	return fmt.Sprintf("memory-%s-${{ env.GH_AW_WORKFLOW_ID_SANITIZED }}-${{ github.run_id }}", cacheID)
}

// parseCacheMemoryEntry parses a single cache-memory entry from a map
func parseCacheMemoryEntry(cacheMap map[string]any, defaultID string) (CacheMemoryEntry, error) {
	cacheLog.Printf("Parsing cache-memory entry: defaultID=%s", defaultID)
	entry := CacheMemoryEntry{
		ID:  defaultID,
		Key: generateDefaultCacheKey(defaultID),
	}
	if err := parseCacheMemoryIdentity(cacheMap, defaultID, &entry); err != nil {
		return entry, err
	}
	parseCacheMemoryDescription(cacheMap, &entry)
	if err := parseCacheMemoryRetentionDays(cacheMap, &entry); err != nil {
		return entry, err
	}
	parseCacheMemoryRestoreOnly(cacheMap, &entry)
	if err := parseCacheMemoryScope(cacheMap, &entry); err != nil {
		return entry, err
	}
	allowedExtensions, err := parseMemoryAllowedExtensions(cacheMap)
	if err != nil {
		return entry, err
	}
	entry.AllowedExtensions = allowedExtensions
	validation, err := parseMemoryValidationConfig(cacheMap, "tools.cache-memory.validation")
	if err != nil {
		return entry, err
	}
	entry.Validation = validation
	applyDefaultAllowedExtensions(&entry)
	if err := validateMemorySchemaTargets(entry.Validation, entry.AllowedExtensions, nil, "tools.cache-memory.validation"); err != nil {
		return entry, err
	}
	cacheLog.Printf("Parsed cache-memory entry: id=%s, scope=%s, restore-only=%v, retention-days=%v", entry.ID, entry.Scope, entry.RestoreOnly, entry.RetentionDays)
	return entry, nil
}

func parseCacheMemoryIdentity(cacheMap map[string]any, defaultID string, entry *CacheMemoryEntry) error {
	if idStr, ok := cacheMap["id"].(string); ok {
		if idStr != "default" && !isValidCacheID(idStr) {
			return fmt.Errorf("invalid cache-memory id %q: must contain only letters, digits, underscores, or hyphens (1-64 characters)", idStr)
		}
		entry.ID = idStr
	}
	if entry.ID != defaultID {
		entry.Key = generateDefaultCacheKey(entry.ID)
	}
	keyStr, ok := cacheMap["key"].(string)
	if !ok {
		return nil
	}
	if err := validateNoCacheKeyRunID(keyStr); err != nil {
		return err
	}
	entry.Key = ensureCacheRunIDSuffix(keyStr)
	return nil
}

func ensureCacheRunIDSuffix(key string) string {
	runIdSuffix := "-${{ github.run_id }}"
	if strings.HasSuffix(key, runIdSuffix) {
		return key
	}
	return key + runIdSuffix
}

func parseCacheMemoryDescription(cacheMap map[string]any, entry *CacheMemoryEntry) {
	if descStr, ok := cacheMap["description"].(string); ok {
		entry.Description = descStr
	}
}

func parseCacheMemoryRetentionDays(cacheMap map[string]any, entry *CacheMemoryEntry) error {
	retentionDays, exists := cacheMap["retention-days"]
	if !exists {
		return nil
	}
	entry.RetentionDays = parseOptionalInt(retentionDays)
	if entry.RetentionDays == nil {
		return nil
	}
	return validateIntRange(*entry.RetentionDays, 1, 90, "retention-days")
}

// parseOptionalInt safely converts YAML numeric values (int, float64, uint64) to *int.
//
// It returns nil when the input cannot be represented as an integer for the current
// architecture, including:
//   - NaN/Inf float64 values
//   - fractional float64 values
//   - float64 values outside the exact-integer range [-2^53, 2^53]
//   - float64 values outside the current architecture int range
//   - uint64 values larger than math.MaxInt
//   - unsupported types
func parseOptionalInt(value any) *int {
	// YAML unmarshaling can yield int, float64, or uint64 depending on parser/input.
	if intValue, ok := value.(int); ok {
		return &intValue
	}
	if floatValue, ok := value.(float64); ok {
		if math.IsNaN(floatValue) || math.IsInf(floatValue, 0) {
			return nil
		}
		if floatValue != math.Trunc(floatValue) {
			return nil
		}
		if floatValue < float64(math.MinInt) || floatValue > float64(math.MaxInt) {
			return nil
		}
		// float64 can exactly represent integers only in [-2^53, 2^53].
		const maxExactFloatInt = float64(1 << 53)
		if floatValue < -maxExactFloatInt || floatValue > maxExactFloatInt {
			return nil
		}
		intValue := int(floatValue)
		return &intValue
	}
	if uintValue, ok := value.(uint64); ok {
		// Guard int conversion on 32-bit/64-bit architectures.
		if uintValue > uint64(math.MaxInt) {
			return nil
		}
		intValue := int(uintValue)
		return &intValue
	}
	return nil
}

func parseCacheMemoryRestoreOnly(cacheMap map[string]any, entry *CacheMemoryEntry) {
	if restoreOnlyBool, ok := cacheMap["restore-only"].(bool); ok {
		entry.RestoreOnly = restoreOnlyBool
	}
}

func parseCacheMemoryScope(cacheMap map[string]any, entry *CacheMemoryEntry) error {
	if scopeStr, ok := cacheMap["scope"].(string); ok {
		entry.Scope = scopeStr
	}
	if entry.Scope == "" {
		entry.Scope = "workflow"
	}
	if slices.Contains(validCacheMemoryScopes, entry.Scope) {
		return nil
	}
	return fmt.Errorf("invalid cache-memory scope %q: must be one of %v", entry.Scope, validCacheMemoryScopes)
}

func applyDefaultAllowedExtensions(entry *CacheMemoryEntry) {
	if len(entry.AllowedExtensions) == 0 {
		entry.AllowedExtensions = constants.DefaultAllowedMemoryExtensions
	}
}

// extractCacheMemoryConfig extracts cache-memory configuration from tools section
// Updated to use ToolsConfig instead of map[string]any
func (c *Compiler) extractCacheMemoryConfig(toolsConfig *ToolsConfig) (*CacheMemoryConfig, error) {
	if toolsConfig == nil || toolsConfig.CacheMemory == nil {
		return nil, nil
	}
	cacheLog.Print("Extracting cache-memory configuration from ToolsConfig")
	config := &CacheMemoryConfig{}
	cacheMemoryValue := toolsConfig.CacheMemory.Raw
	if cacheMemoryValue == nil {
		config.Caches = defaultCacheMemoryEntries()
		return config, nil
	}
	if boolValue, ok := cacheMemoryValue.(bool); ok {
		if boolValue {
			config.Caches = defaultCacheMemoryEntries()
		}
		return config, nil
	}
	if cacheArray, ok := cacheMemoryValue.([]any); ok {
		entries, err := parseCacheMemoryEntries(cacheArray)
		if err != nil {
			return nil, err
		}
		config.Caches = entries
		return config, nil
	}
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

func defaultCacheMemoryEntries() []CacheMemoryEntry {
	return []CacheMemoryEntry{
		{
			ID:                "default",
			Key:               generateDefaultCacheKey("default"),
			AllowedExtensions: constants.DefaultAllowedMemoryExtensions,
		},
	}
}

func parseCacheMemoryEntries(cacheArray []any) ([]CacheMemoryEntry, error) {
	cacheLog.Printf("Processing cache array with %d entries", len(cacheArray))
	entries := make([]CacheMemoryEntry, 0, len(cacheArray))
	for _, item := range cacheArray {
		cacheMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		entry, err := parseCacheMemoryEntry(cacheMap, "default")
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := validateNoDuplicateCacheIDs(entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// extractCacheMemoryConfigFromMap is a backward compatibility wrapper for extractCacheMemoryConfig
// that accepts map[string]any instead of *ToolsConfig. This allows gradual migration of calling code.
func (c *Compiler) extractCacheMemoryConfigFromMap(tools map[string]any) (*CacheMemoryConfig, error) {
	toolsConfig, err := ParseToolsConfig(tools)
	if err != nil {
		return nil, err
	}
	return c.extractCacheMemoryConfig(toolsConfig)
}
