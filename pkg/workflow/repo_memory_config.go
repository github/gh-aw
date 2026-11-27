// This file provides repository memory configuration.
//
// This file handles:
//   - Repo-memory configuration structures and defaults
//   - Repo-memory tool configuration extraction and parsing
//
// See repo_memory_validation.go for validation functions.

package workflow

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var repoMemoryLog = logger.New("workflow:repo_memory")

const (
	// defaultRepoMemoryMaxFileSize is the default maximum file size in bytes (100KB).
	defaultRepoMemoryMaxFileSize = 102400
	// defaultRepoMemoryMaxPatchSize is the default maximum total patch size in bytes (10KB).
	defaultRepoMemoryMaxPatchSize = 10240
	// maxRepoMemoryPatchSize is the maximum allowed value for max-patch-size (1MB).
	maxRepoMemoryPatchSize = 1048576
	// pushRepoMemoryTimeoutMinutes bounds the retry loop (about two minutes of
	// capped jittered backoff) plus git/network overhead.
	pushRepoMemoryTimeoutMinutes = 10
)

// Pre-compiled regexes for performance (avoid recompilation in hot paths)
var (
	// branchPrefixValidPattern matches valid branch prefix characters (alphanumeric, hyphens, underscores)
	branchPrefixValidPattern    = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	repoMemoryTargetPattern     = regexp.MustCompile(`^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$`)
	repoMemoryTargetPartPattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)
)

// RepoMemoryConfig holds configuration for repo-memory functionality
type RepoMemoryConfig struct {
	BranchPrefix string            `yaml:"branch-prefix,omitempty"` // branch prefix (default: "memory")
	Memories     []RepoMemoryEntry `yaml:"memories,omitempty"`      // repo-memory configurations
}

// RepoMemoryEntry represents a single repo-memory configuration
type RepoMemoryEntry struct {
	ID                string                  `yaml:"id"`                           // memory identifier (required for array notation)
	TargetRepo        string                  `yaml:"target-repo,omitempty"`        // target repository (default: current repo)
	GitHubApp         *GitHubAppConfig        `yaml:"github-app,omitempty"`         // optional app credentials for memory storage
	GitHubAppRead     *GitHubAppConfig        `yaml:"github-app-read,omitempty"`    // optional app override for memory reads
	GitHubAppWrite    *GitHubAppConfig        `yaml:"github-app-write,omitempty"`   // optional app override for memory writes
	BranchName        string                  `yaml:"branch-name,omitempty"`        // branch name (default: memory/{memory-id})
	FileGlob          []string                `yaml:"file-glob,omitempty"`          // file glob patterns for allowed files
	MaxFileSize       int                     `yaml:"max-file-size,omitempty"`      // maximum size per file in bytes (default: 100KB)
	MaxFileCount      int                     `yaml:"max-file-count,omitempty"`     // maximum file count per commit (default: 100)
	MaxPatchSize      int                     `yaml:"max-patch-size,omitempty"`     // maximum total patch size in bytes (default: 10KB, max: 1MB)
	Description       string                  `yaml:"description,omitempty"`        // optional description for this memory
	CreateOrphan      bool                    `yaml:"create-orphan,omitempty"`      // create orphaned branch if missing (default: true)
	AllowedExtensions []string                `yaml:"allowed-extensions,omitempty"` // allowed file extensions (default: [".json", ".jsonl", ".txt", ".md", ".csv"])
	Wiki              bool                    `yaml:"wiki,omitempty"`               // use the GitHub Wiki git repository instead of the regular repo
	FormatJSON        bool                    `yaml:"format-json,omitempty"`        // pretty-print all .json files before committing (default: false)
	Validation        *MemoryValidationConfig `yaml:"validation,omitempty"`         // optional custom JavaScript validation hook
	Ledger            *RepoMemoryLedgerConfig `yaml:"ledger,omitempty"`             // optional ledger projection
}

// RepoMemoryLedgerConfig enables the ledger projection for this memory.
type RepoMemoryLedgerConfig struct {
	Schema       string                            `yaml:"schema,omitempty"`
	MaxShards    int                               `yaml:"max-shards,omitempty"`
	MaxSegmentKB int                               `yaml:"max-segment-kb,omitempty"`
	MaxRecordKB  int                               `yaml:"max-record-kb,omitempty"`
	MaxPatchKB   int                               `yaml:"max-patch-kb,omitempty"`
	Compaction   *RepoMemoryLedgerCompactionConfig `yaml:"compaction,omitempty"`
}

// RepoMemoryLedgerCompactionConfig bounds deterministic ledger compaction.
type RepoMemoryLedgerCompactionConfig struct {
	MinSegments int `yaml:"min-segments,omitempty" json:"minSegments"`
	MaxSegments int `yaml:"max-segments,omitempty" json:"maxSegments"`
}

// RepoMemoryToolConfig represents the configuration for repo-memory in tools
type RepoMemoryToolConfig struct {
	// Can be boolean, object, or array - handled by this file
	Raw any `yaml:"-"`
}

// generateDefaultBranchName generates a default branch name for a given memory ID and prefix
func generateDefaultBranchName(memoryID string, branchPrefix string) string {
	if branchPrefix == "" {
		branchPrefix = "memory"
	}
	return fmt.Sprintf("%s/%s", branchPrefix, memoryID)
}

// extractRepoMemoryConfig extracts repo-memory configuration from tools section.
// workflowID is used to qualify the default branch name (e.g. "memory/{workflowID}").
func (c *Compiler) extractRepoMemoryConfig(toolsConfig *ToolsConfig, workflowID string) (*RepoMemoryConfig, error) {
	if toolsConfig == nil || toolsConfig.RepoMemory == nil {
		return nil, nil
	}
	repoMemoryLog.Print("Extracting repo-memory configuration from ToolsConfig")
	config := &RepoMemoryConfig{
		BranchPrefix: "memory",
	}
	repoMemoryValue := toolsConfig.RepoMemory.Raw
	if repoMemoryValue == nil {
		repoMemoryLog.Print("Using default repo-memory configuration (nil value)")
		config.Memories = []RepoMemoryEntry{newDefaultRepoMemoryEntry(workflowID, config.BranchPrefix)}
		return config, nil
	}
	if boolValue, ok := repoMemoryValue.(bool); ok {
		if boolValue {
			repoMemoryLog.Print("Using default repo-memory configuration (boolean true)")
			config.Memories = []RepoMemoryEntry{newDefaultRepoMemoryEntry(workflowID, config.BranchPrefix)}
		} else {
			repoMemoryLog.Print("Repo-memory disabled (boolean false)")
		}
		return config, nil
	}
	if memoryArray, ok := repoMemoryValue.([]any); ok {
		memories, err := parseRepoMemoryArray(memoryArray, workflowID, config)
		if err != nil {
			return nil, err
		}
		ledgerEnabled := false
		for _, memory := range memories {
			ledgerEnabled = ledgerEnabled || memory.Ledger != nil
		}
		if ledgerEnabled && len(memories) != 1 {
			return nil, errors.New("tools.repo-memory.ledger requires exactly one repo-memory entry")
		}
		config.Memories = memories
		return config, nil
	}

	if configMap, ok := repoMemoryValue.(map[string]any); ok {
		if err := applyRepoMemoryBranchPrefix(configMap, config); err != nil {
			return nil, err
		}
		entry, err := parseRepoMemoryEntry(configMap, workflowID, config.BranchPrefix, false)
		if err != nil {
			return nil, err
		}
		config.Memories = []RepoMemoryEntry{entry}
		return config, nil
	}
	return nil, nil
}

func (config *RepoMemoryConfig) ledgerEntry() *RepoMemoryEntry {
	if config == nil {
		return nil
	}
	for _, memory := range config.Memories {
		if memory.Ledger != nil {
			entry := memory
			return &entry
		}
	}
	return nil
}

func newDefaultRepoMemoryEntry(workflowID, branchPrefix string) RepoMemoryEntry {
	return RepoMemoryEntry{
		ID:                "default",
		BranchName:        generateDefaultBranchName(defaultRepoMemoryBranchID(workflowID), branchPrefix),
		MaxFileSize:       defaultRepoMemoryMaxFileSize,
		MaxFileCount:      100,
		MaxPatchSize:      defaultRepoMemoryMaxPatchSize,
		CreateOrphan:      true,
		AllowedExtensions: constants.DefaultAllowedMemoryExtensions,
	}
}

func defaultRepoMemoryBranchID(workflowID string) string {
	if workflowID != "" {
		return workflowID
	}
	return "default"
}

func parseRepoMemoryArray(memoryArray []any, workflowID string, config *RepoMemoryConfig) ([]RepoMemoryEntry, error) {
	repoMemoryLog.Printf("Processing memory array with %d entries", len(memoryArray))
	if len(memoryArray) > 0 {
		if firstItem, ok := memoryArray[0].(map[string]any); ok {
			if err := applyRepoMemoryBranchPrefix(firstItem, config); err != nil {
				return nil, err
			}
		}
	}
	memories := make([]RepoMemoryEntry, 0, len(memoryArray))
	for _, item := range memoryArray {
		memoryMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		entry, err := parseRepoMemoryEntry(memoryMap, workflowID, config.BranchPrefix, true)
		if err != nil {
			return nil, err
		}
		memories = append(memories, entry)
	}
	if err := validateNoDuplicateMemoryIDs(memories); err != nil {
		return nil, err
	}
	return memories, nil
}

func applyRepoMemoryBranchPrefix(configMap map[string]any, config *RepoMemoryConfig) error {
	branchPrefix, exists := configMap["branch-prefix"]
	if !exists {
		return nil
	}
	prefixStr, ok := branchPrefix.(string)
	if !ok {
		return nil
	}
	if err := validateBranchPrefix(prefixStr); err != nil {
		return err
	}
	config.BranchPrefix = prefixStr
	repoMemoryLog.Printf("Using custom branch-prefix: %s", prefixStr)
	return nil
}

func parseRepoMemoryEntry(memoryMap map[string]any, workflowID, branchPrefix string, requireID bool) (RepoMemoryEntry, error) {
	entry := newDefaultRepoMemoryEntry(workflowID, branchPrefix)
	explicitID, explicitBranchName := applyRepoMemoryIdentityFields(&entry, memoryMap)
	if err := validateRepoMemoryAppConfig(entry); err != nil {
		return entry, err
	}
	if err := validateRepoMemoryAppTarget(entry); err != nil {
		return entry, err
	}
	if requireID && entry.ID == "" {
		entry.ID = "default"
	}
	if entry.ID == "" {
		entry.ID = "default"
	}
	if !explicitBranchName {
		entry.BranchName = "" // let applyRepoMemoryDefaultBranch derive from ID
	}
	applyRepoMemoryDefaultBranch(&entry, workflowID, branchPrefix, explicitID)
	if err := applyRepoMemoryFileFields(&entry, memoryMap); err != nil {
		return RepoMemoryEntry{}, err
	}
	if err := applyRepoMemoryLimits(&entry, memoryMap); err != nil {
		return RepoMemoryEntry{}, err
	}
	if err := applyRepoMemoryOptionalFields(&entry, memoryMap); err != nil {
		return RepoMemoryEntry{}, err
	}
	if ledger, exists := memoryMap["ledger"]; exists {
		config, err := parseRepoMemoryLedgerConfig(ledger)
		if err != nil {
			return RepoMemoryEntry{}, err
		}
		if !repoMemoryLedgerIDPattern.MatchString(entry.ID) {
			return RepoMemoryEntry{}, errors.New("tools.repo-memory.ledger requires a memory id with only letters, numbers, hyphens or underscores")
		}
		hasJSONLExtension := false
		for _, ext := range entry.AllowedExtensions {
			hasJSONLExtension = hasJSONLExtension || strings.EqualFold(ext, ".jsonl")
		}
		if len(entry.AllowedExtensions) > 0 && !hasJSONLExtension {
			return RepoMemoryEntry{}, errors.New("tools.repo-memory.ledger requires .jsonl in allowed-extensions to persist ledger records")
		}
		if len(entry.FileGlob) > 0 {
			if !slices.Contains(entry.FileGlob, "ledger/shards/*.jsonl") {
				entry.FileGlob = append(entry.FileGlob, "ledger/shards/*.jsonl")
			}
		}
		entry.Ledger = config
	}
	if err := validateMemorySchemaTargets(entry.Validation, entry.AllowedExtensions, entry.FileGlob, "tools.repo-memory.validation"); err != nil {
		return RepoMemoryEntry{}, err
	}
	finalizeRepoMemoryEntry(&entry, explicitBranchName)
	return entry, nil
}

func applyRepoMemoryIdentityFields(entry *RepoMemoryEntry, memoryMap map[string]any) (bool, bool) {
	explicitID := false
	if id, ok := memoryMap["id"].(string); ok {
		entry.ID = id
		explicitID = true
	}
	if targetRepo, ok := memoryMap["target-repo"].(string); ok {
		entry.TargetRepo = targetRepo
	}
	if app, ok := memoryMap["github-app"].(map[string]any); ok {
		entry.GitHubApp = parseAppConfig(app)
	}
	if app, ok := memoryMap["github-app-read"].(map[string]any); ok {
		entry.GitHubAppRead = parseAppConfig(app)
	}
	if app, ok := memoryMap["github-app-write"].(map[string]any); ok {
		entry.GitHubAppWrite = parseAppConfig(app)
	}
	explicitBranchName := false
	if branchName, ok := memoryMap["branch-name"].(string); ok {
		entry.BranchName = branchName
		explicitBranchName = true
	}
	return explicitID, explicitBranchName
}

func applyRepoMemoryDefaultBranch(entry *RepoMemoryEntry, workflowID, branchPrefix string, explicitID bool) {
	if entry.BranchName != "" {
		return
	}
	branchID := entry.ID
	if !explicitID {
		branchID = defaultRepoMemoryBranchID(workflowID)
	}
	entry.BranchName = generateDefaultBranchName(branchID, branchPrefix)
}

func applyRepoMemoryFileFields(entry *RepoMemoryEntry, memoryMap map[string]any) error {
	entry.FileGlob = parseRepoMemoryStringList(memoryMap["file-glob"])
	if len(entry.FileGlob) > 0 {
		if err := validateFileGlobPatterns(entry.FileGlob); err != nil {
			return err
		}
	}
	allowedExtensions := parseRepoMemoryStringList(memoryMap["allowed-extensions"])
	if len(allowedExtensions) > 0 {
		entry.AllowedExtensions = allowedExtensions
	}
	if len(entry.AllowedExtensions) == 0 {
		entry.AllowedExtensions = constants.DefaultAllowedMemoryExtensions
	}
	return nil
}

func applyRepoMemoryLimits(entry *RepoMemoryEntry, memoryMap map[string]any) error {
	limits := []struct {
		key   string
		min   int
		max   int
		field *int
	}{
		{key: "max-file-size", min: 1, max: 104857600, field: &entry.MaxFileSize},
		{key: "max-file-count", min: 1, max: 1000, field: &entry.MaxFileCount},
		{key: "max-patch-size", min: 1, max: maxRepoMemoryPatchSize, field: &entry.MaxPatchSize},
	}
	for _, limit := range limits {
		if value, ok := parseRepoMemoryInt(memoryMap[limit.key]); ok {
			*limit.field = value
			if err := validateIntRange(value, limit.min, limit.max, limit.key); err != nil {
				return err
			}
		}
	}
	return nil
}

func applyRepoMemoryOptionalFields(entry *RepoMemoryEntry, memoryMap map[string]any) error {
	if description, ok := memoryMap["description"].(string); ok {
		entry.Description = description
	}
	if createOrphan, ok := memoryMap["create-orphan"].(bool); ok {
		entry.CreateOrphan = createOrphan
	}
	if wiki, ok := memoryMap["wiki"].(bool); ok {
		entry.Wiki = wiki
	}
	if formatJSON, ok := memoryMap["format-json"].(bool); ok {
		entry.FormatJSON = formatJSON
	}
	validation, err := parseMemoryValidationConfig(memoryMap, "tools.repo-memory.validation")
	if err != nil {
		return err
	}
	entry.Validation = validation
	return nil
}

func finalizeRepoMemoryEntry(entry *RepoMemoryEntry, explicitBranchName bool) {
	if !entry.Wiki {
		return
	}
	if !explicitBranchName {
		entry.BranchName = "master"
	}
	entry.CreateOrphan = false
}

func parseRepoMemoryStringList(value any) []string {
	switch v := value.(type) {
	case []any:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result
	case string:
		return []string{v}
	default:
		return nil
	}
}

func parseRepoMemoryInt(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case float64:
		return int(v), true
	case uint64:
		return int(v), true
	default:
		return 0, false
	}
}
