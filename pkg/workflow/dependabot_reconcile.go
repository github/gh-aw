//go:build !js && !wasm

package workflow

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/goccy/go-yaml"
)

// loadOrInitDependabotConfig reads and parses an existing dependabot.yml at path, or returns a
// freshly initialized DependabotConfig if the file does not exist or cannot be parsed.
func loadOrInitDependabotConfig(path string) (DependabotConfig, error) {
	var config DependabotConfig

	// Check if dependabot.yml already exists
	if _, err := os.Stat(path); err == nil {
		// File exists - read and merge configuration
		dependabotLog.Print("Existing dependabot.yml found, merging configuration")
		existingData, err := os.ReadFile(path)
		if err != nil {
			return config, fmt.Errorf("failed to read existing dependabot.yml: %w", err)
		}

		if err := yaml.Unmarshal(existingData, &config); err != nil {
			// If we can't parse it, start fresh
			dependabotLog.Print("Could not parse existing dependabot.yml, creating new one")
			config = DependabotConfig{Version: 2}
		}
	} else {
		// New dependabot.yml
		dependabotLog.Print("Creating new dependabot.yml")
		config = DependabotConfig{Version: 2}
	}

	return config, nil
}

// addMissingDependabotEcosystems appends update entries for ecosystems that don't already
// have a .github/workflows entry in the given config.
func addMissingDependabotEcosystems(config *DependabotConfig, ecosystems map[string]struct{}) {
	for ecosystem := range ecosystems {
		exists := false
		for _, update := range config.Updates {
			if update.PackageEcosystem == ecosystem && update.Directory == "/.github/workflows" {
				exists = true
				break
			}
		}

		if !exists {
			entry := DependabotUpdateEntry{
				PackageEcosystem: ecosystem,
				Directory:        "/.github/workflows",
			}
			entry.Schedule.Interval = "weekly"
			config.Updates = append(config.Updates, entry)
		}
	}
}

// generateDependabotConfig creates or updates .github/dependabot.yml
func (c *Compiler) generateDependabotConfig(path string, ecosystems map[string]struct {
}, forceOverwrite bool) error {
	dependabotLog.Printf("Generating dependabot.yml at %s", path)

	config, err := loadOrInitDependabotConfig(path)
	if err != nil {
		return err
	}
	addMissingDependabotEcosystems(&config, ecosystems)

	// Write dependabot.yml
	yamlData, err := yaml.Marshal(&config)
	if err != nil {
		return fmt.Errorf("failed to marshal dependabot.yml: %w", err)
	}

	if err := os.WriteFile(path, yamlData, constants.FilePermPublic); err != nil {
		return fmt.Errorf("failed to write dependabot.yml: %w", err)
	}

	dependabotLog.Print("Successfully wrote dependabot.yml")
	if c.verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Updated .github/dependabot.yml"))
	}

	// Track the created file
	if c.fileTracker != nil {
		c.fileTracker.TrackCreated(path)
	}

	return nil
}

// ReconcileManagedDependabotIgnores updates existing github-actions entries in .github/dependabot.yml
// with compiler-managed ignore rules for compiler-emitted action refs.
// This function is a no-op when dependabot.yml does not exist or has no github-actions update entries.
func (c *Compiler) ReconcileManagedDependabotIgnores(path string) error {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}

	original, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read dependabot.yml: %w", err)
	}

	var root map[string]any
	if err := yaml.Unmarshal(original, &root); err != nil {
		return fmt.Errorf("failed to parse dependabot.yml: %w", err)
	}

	updatesAny, ok := root["updates"]
	if !ok {
		return nil
	}
	updates, ok := dependabotToAnySlice(updatesAny)
	if !ok {
		return nil
	}

	managedPatterns := []string{c.effectiveActionsRepo() + "/*"}
	changed := false
	originalStr := string(original)
	managedPatternsWithComment := managedPatternsWithInlineComment(originalStr, managedPatterns)

	for i, updateAny := range updates {
		result := reconcileGithubActionsIgnoreEntry(updateAny, managedPatterns, managedPatternsWithComment)
		if result.changed {
			changed = true
		}
		if result.writeBack {
			updates[i] = result.updateMap
		}
	}

	if !changed {
		return nil
	}

	root["updates"] = updates
	updated, err := yaml.Marshal(root)
	if err != nil {
		return fmt.Errorf("failed to encode dependabot.yml: %w", err)
	}
	updated = normalizeDependabotIgnoreEntries(updated, managedPatterns)

	if bytes.Equal(original, updated) {
		return nil
	}
	if err := os.WriteFile(path, updated, constants.FilePermPublic); err != nil {
		return fmt.Errorf("failed to write dependabot.yml: %w", err)
	}
	return nil
}

type reconcileGithubActionsIgnoreResult struct {
	updateMap map[string]any
	changed   bool
	writeBack bool
}

// reconcileGithubActionsIgnoreEntry adds compiler-managed ignore rules to a single
// dependabot.yml update entry if it targets the github-actions ecosystem.
func reconcileGithubActionsIgnoreEntry(updateAny any, managedPatterns []string, managedPatternsWithComment map[string]struct{}) reconcileGithubActionsIgnoreResult {
	updateMap, ok := dependabotToStringAnyMap(updateAny)
	if !ok {
		return reconcileGithubActionsIgnoreResult{}
	}

	ecosystem, _ := updateMap["package-ecosystem"].(string)
	if ecosystem != "github-actions" {
		return reconcileGithubActionsIgnoreResult{}
	}

	changed := false

	ignoreAny, hasIgnore := updateMap["ignore"]
	if !hasIgnore || isYAMLNullOrEmptyScalar(ignoreAny) {
		updateMap["ignore"] = []any{}
		ignoreAny = updateMap["ignore"]
		changed = true
	}

	ignoreEntries, ok := dependabotToAnySlice(ignoreAny)
	if !ok {
		// Only reached for a pre-existing non-empty, non-list ignore value; preserve
		// the original skip-without-write-back behavior.
		return reconcileGithubActionsIgnoreResult{changed: changed}
	}

	managedPresent := make(map[string]struct{}, len(managedPatterns))
	reconciledIgnoreEntries := make([]any, 0, len(ignoreEntries))
	for _, ignoreEntryAny := range ignoreEntries {
		ignoreEntryMap, ok := dependabotToStringAnyMap(ignoreEntryAny)
		dependencyName, _ := ignoreEntryMap["dependency-name"].(string)
		if !ok || dependencyName == "" {
			reconciledIgnoreEntries = append(reconciledIgnoreEntries, ignoreEntryAny)
			continue
		}
		for _, pattern := range managedPatterns {
			if dependencyName == pattern {
				managedPresent[pattern] = struct{}{}
				if !setutil.Contains(managedPatternsWithComment, pattern) {
					changed = true
				}
			}
		}
		reconciledIgnoreEntries = append(reconciledIgnoreEntries, ignoreEntryAny)
	}
	for _, pattern := range managedPatterns {
		if setutil.Contains(managedPresent, pattern) {
			continue
		}
		reconciledIgnoreEntries = append(reconciledIgnoreEntries, map[string]any{"dependency-name": pattern})
		changed = true
	}
	updateMap["ignore"] = reconciledIgnoreEntries
	return reconcileGithubActionsIgnoreResult{updateMap: updateMap, changed: changed, writeBack: true}
}

// DependabotConfigPath resolves the repository-local Dependabot config path.
func DependabotConfigPath(gitRoot string) string {
	return filepath.Join(gitRoot, dependabotConfigRelativePath)
}

// ReconcileManagedDependabotIgnoresInRepo reconciles managed ignores in the
// Dependabot config located under a repository root.
func (c *Compiler) ReconcileManagedDependabotIgnoresInRepo(gitRoot string) error {
	return c.ReconcileManagedDependabotIgnores(DependabotConfigPath(gitRoot))
}

func dependabotToAnySlice(value any) ([]any, bool) {
	if value == nil {
		return nil, false
	}
	if direct, ok := value.([]any); ok {
		return direct, true
	}

	// goccy/go-yaml can decode typed slices depending on source shape.
	// Use reflection fallback to safely normalize those typed slices to []any.
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Slice {
		return nil, false
	}
	dependabotLog.Printf(
		"Normalizing typed slice %T to []any via reflection (goccy/go-yaml may return typed slices depending on YAML structure)",
		value,
	)

	length := rv.Len()
	out := make([]any, length)
	for i := range length {
		out[i] = rv.Index(i).Interface()
	}
	return out, true
}

func dependabotToStringAnyMap(value any) (map[string]any, bool) {
	if value == nil {
		return nil, false
	}
	if direct, ok := value.(map[string]any); ok {
		return direct, true
	}

	// goccy/go-yaml can decode typed maps in dynamic sections.
	// Use reflection fallback to safely normalize those maps to map[string]any.
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Map {
		return nil, false
	}
	dependabotLog.Printf(
		"Normalizing typed map %T to map[string]any via reflection (goccy/go-yaml may return typed maps in dynamic sections)",
		value,
	)

	out := make(map[string]any, rv.Len())
	iter := rv.MapRange()
	for iter.Next() {
		key, ok := iter.Key().Interface().(string)
		if !ok {
			return nil, false
		}
		out[key] = iter.Value().Interface()
	}
	return out, true
}

func isYAMLNullOrEmptyScalar(value any) bool {
	if value == nil {
		return true
	}
	rawValue, ok := value.(string)
	if !ok {
		return false
	}
	trimmed := strings.TrimSpace(rawValue)
	return trimmed == "" || strings.EqualFold(trimmed, "null") || trimmed == "~"
}

func managedPatternsWithInlineComment(content string, managedPatterns []string) map[string]struct {
} {
	result := make(map[string]struct {
	}, len(managedPatterns))
	for line := range strings.SplitSeq(content, "\n") {
		if !strings.Contains(line, "dependency-name:") || !strings.Contains(line, managedDependabotIgnoreComment) {
			continue
		}
		beforeComment, _, _ := strings.Cut(line, "#")
		_, rawDependencyName, found := strings.Cut(beforeComment, "dependency-name:")
		if !found {
			continue
		}
		dependencyName := strings.Trim(strings.TrimSpace(rawDependencyName), `"'`)
		for _, pattern := range managedPatterns {
			if dependencyName == pattern {
				result[pattern] = struct {
				}{}
			}
		}
	}
	return result
}

func normalizeDependabotIgnoreEntries(content []byte, managedPatterns []string) []byte {
	lines := strings.Split(string(content), "\n")
	for i, line := range lines {
		if !strings.Contains(line, "dependency-name:") {
			continue
		}

		beforeComment, comment, hasComment := strings.Cut(line, "#")
		parts := strings.SplitN(beforeComment, "dependency-name:", 2)
		if len(parts) != 2 {
			continue
		}

		prefix := parts[0] + "dependency-name: "
		rawDependencyName := strings.TrimSpace(parts[1])
		quote := `"`
		// Assume quote characters are balanced when present. If the scalar starts
		// with a quote but does not end with the same quote, skip normalization.
		if strings.HasPrefix(rawDependencyName, "'") {
			if !strings.HasSuffix(rawDependencyName, "'") {
				continue
			}
			quote = `'`
		} else if strings.HasPrefix(rawDependencyName, `"`) && !strings.HasSuffix(rawDependencyName, `"`) {
			continue
		}
		dependencyName := strings.Trim(rawDependencyName, `"'`)
		if dependencyName == "" {
			continue
		}

		line = prefix + quote + dependencyName + quote

		managed := slices.Contains(managedPatterns, dependencyName)

		if managed {
			line += " # " + managedDependabotIgnoreComment
		} else if hasComment {
			line += " #" + strings.TrimSpace(comment)
		}

		lines[i] = line
	}
	return []byte(strings.Join(lines, "\n"))
}
