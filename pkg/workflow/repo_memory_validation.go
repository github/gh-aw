// This file provides validation for repo-memory configuration.
//
// # Repo Memory Validation
//
// This file validates that repo-memory entries have unique IDs and that
// branch prefix configuration meets naming requirements.
//
// # Validation Functions
//
//   - validateBranchPrefix() - Validates branch prefix length, format, and reserved names
//   - validateNoDuplicateMemoryIDs() - Ensures each memory entry has a unique ID
//   - validateFileGlobPatterns() - Validates file-glob patterns for unsupported forms
//
// # When to Add Validation Here
//
// Add validation to this file when:
//   - Adding new repo-memory configuration constraints
//   - Adding new branch naming rules

package workflow

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var repoMemoryLedgerSchemaPathPattern = regexp.MustCompile(`^[a-zA-Z0-9._/-]+$`)
var repoMemoryLedgerIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func parseRepoMemoryLedgerConfig(raw any) (*RepoMemoryLedgerConfig, error) {
	if raw == nil {
		return &RepoMemoryLedgerConfig{}, nil
	}
	fields, ok := raw.(map[string]any)
	if !ok {
		return nil, errors.New("tools.repo-memory.ledger must be an object (use ledger: {} to enable defaults)")
	}
	config := &RepoMemoryLedgerConfig{}
	for key, value := range fields {
		if key == "compactor" {
			compactor, ok := value.(map[string]any)
			if !ok || len(compactor) != 1 {
				return nil, errors.New("tools.repo-memory.ledger.compactor must contain only script")
			}
			script, ok := compactor["script"].(string)
			if !ok || strings.TrimSpace(script) == "" || len(script) > 65536 {
				return nil, errors.New("tools.repo-memory.ledger.compactor.script must be a non-empty script no larger than 65536 bytes")
			}
			config.Compactor = &RepoMemoryLedgerCompactorConfig{Script: script}
			continue
		}
		if key == "max-shards" {
			maxShards, ok := value.(int)
			if !ok || maxShards < 1 || maxShards > 1024 {
				return nil, fmt.Errorf("tools.repo-memory.ledger.max-shards must be between 1 and 1024, got %v", value)
			}
			config.MaxShards = maxShards
			continue
		}
		if key != "schema" {
			return nil, fmt.Errorf("tools.repo-memory.ledger has unknown property %q (only schema, max-shards, and compactor are supported)", key)
		}
		schema, ok := value.(string)
		if !ok {
			return nil, errors.New("tools.repo-memory.ledger.schema must be a repository-relative path")
		}
		if err := validateRepoMemoryLedgerSchemaPath(schema); err != nil {
			return nil, err
		}
		config.Schema = schema
	}
	return config, nil
}

func validateRepoMemoryLedgerSchemaPath(schema string) error {
	if schema == "" || !repoMemoryLedgerSchemaPathPattern.MatchString(schema) ||
		strings.HasPrefix(schema, "/") || path.Clean(schema) != schema ||
		schema == "." || schema == ".." || strings.HasPrefix(schema, "../") {
		return fmt.Errorf("tools.repo-memory.ledger.schema must be a repository-relative path without traversal or expressions, got %q", schema)
	}
	return nil
}

var repoMemValidationLog = logger.New("workflow:repo_memory_validation")

// validateBranchPrefix validates that the branch prefix meets requirements
func validateBranchPrefix(prefix string) error {
	if prefix == "" {
		return nil // Empty means use default
	}

	repoMemValidationLog.Printf("Validating branch prefix: %q", prefix)

	// Check length (4-32 characters)
	if len(prefix) < 4 {
		return fmt.Errorf("branch-prefix must be at least 4 characters long, got %d. Example: branch-prefix: my-bot", len(prefix))
	}
	if len(prefix) > 32 {
		return fmt.Errorf("branch-prefix must be at most 32 characters long, got %d. Example: branch-prefix: my-bot", len(prefix))
	}

	// Check for alphanumeric and branch-friendly characters (alphanumeric, hyphens, underscores)
	// Use pre-compiled regex from package level for performance
	if !branchPrefixValidPattern.MatchString(prefix) {
		return fmt.Errorf("branch-prefix must contain only alphanumeric characters, hyphens, and underscores, got '%s'. Example: branch-prefix: my-bot", prefix)
	}

	// Cannot be "copilot"
	if strings.EqualFold(prefix, "copilot") {
		return errors.New("branch-prefix cannot be 'copilot' (reserved). Example: branch-prefix: my-bot")
	}

	repoMemValidationLog.Printf("Branch prefix %q passed validation", prefix)
	return nil
}

// validateNoDuplicateMemoryIDs checks for duplicate memory IDs and returns an error if found.
// Uses the generic validateNoDuplicateIDs helper for consistent duplicate detection.
func validateNoDuplicateMemoryIDs(memories []RepoMemoryEntry) error {
	repoMemValidationLog.Printf("Validating %d memory entries for duplicate IDs", len(memories))
	return validateNoDuplicateIDs(memories, func(m RepoMemoryEntry) string { return m.ID }, func(id string) error {
		return fmt.Errorf("duplicate memory ID found: '%s'. Each memory must have a unique ID. Example: id: my-memory", id)
	})
}

// validateFileGlobPatterns validates file-glob patterns for a repo-memory entry.
//
// Patterns are evaluated against the full relative path from the artifact root.
// Slashless patterns such as "*.json" match only files at the artifact root (depth 0),
// since a single "*" does not cross "/"; use "**/*.json" to also match nested files.
// Patterns containing "/" match against the full relative path from the artifact root.
//
// Rejected patterns:
//   - Patterns starting with "/" — absolute paths are not supported.
func validateFileGlobPatterns(patterns []string) error {
	for _, pat := range patterns {
		repoMemValidationLog.Printf("Validating file-glob pattern: %q", pat)
		if strings.HasPrefix(pat, "/") {
			return fmt.Errorf("file-glob pattern %q is not supported: patterns must not start with '/' (absolute paths are not allowed). Example: file-glob: [\"*.json\", \"*.md\"]", pat)
		}
	}
	return nil
}
