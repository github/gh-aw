package parser

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
)

var toolsMergerLog = logger.New("parser:tools_merger")

// mergeToolsFromJSON merges multiple JSON tool objects from content
func mergeToolsFromJSON(content string) (string, error) {
	parserLog.Printf("Merging tools from JSON: content_size=%d bytes", len(content))
	content = strings.TrimSpace(content)
	if content == "" {
		return "{}", nil
	}

	if singleObjectJSON, err, ok := marshalSingleToolObject(content); ok {
		if err != nil {
			return "{}", err
		}
		return singleObjectJSON, nil
	}

	jsonObjects := parseToolObjectsByLine(content)
	if len(jsonObjects) == 0 {
		parserLog.Print("No valid JSON objects found in content, returning empty object")
		return "{}", nil
	}

	parserLog.Printf("Found %d JSON objects to merge", len(jsonObjects))
	merged, err := mergeToolObjectList(jsonObjects)
	if err != nil {
		return "{}", err
	}

	result, err := json.Marshal(merged)
	if err != nil {
		return "{}", err
	}
	return string(result), nil
}

// MergeTools merges two neutral tool configurations.
// Supports merging arrays and maps for neutral tools plus repository-profile shorthand.
// Removes all legacy Claude tool merging logic.
func MergeTools(base, additional map[string]any) (map[string]any, error) {
	parserLog.Printf("Merging tools: base_keys=%d, additional_keys=%d", len(base), len(additional))
	result := make(map[string]any)

	if profile, exists := base["profile"]; exists {
		if _, isMCPServer := profile.(map[string]any); !isMCPServer {
			if _, err := normalizeToolProfiles(profile); err != nil {
				return nil, err
			}
		}
	}
	if repository, exists := base["repository"]; exists {
		if _, err := normalizeToolProfiles(repository); err != nil {
			return nil, err
		}
	}
	maps.Copy(result, base)

	for key, newValue := range additional {
		if key == "profile" {
			if _, isMCPServer := newValue.(map[string]any); !isMCPServer {
				if _, err := normalizeToolProfiles(newValue); err != nil {
					return nil, err
				}
			}
		}
		if key == "repository" {
			if _, err := normalizeToolProfiles(newValue); err != nil {
				return nil, err
			}
		}
		if existingValue, exists := result[key]; exists {
			mergedValue, merged, err := mergeExistingToolValue(key, existingValue, newValue)
			if err != nil {
				return nil, err
			}
			if merged {
				result[key] = mergedValue
				continue
			}
			continue
		}
		result[key] = newValue
	}

	return result, nil
}

func marshalSingleToolObject(content string) (string, error, bool) {
	var singleObj map[string]any
	if err := json.Unmarshal([]byte(content), &singleObj); err != nil {
		return "", nil, false
	}
	if len(singleObj) == 0 {
		return "", nil, false
	}
	if profile, exists := singleObj["profile"]; exists {
		if _, err := normalizeToolProfiles(profile); err != nil {
			return "", err, true
		}
	}
	result, err := json.Marshal(singleObj)
	if err != nil {
		return "", err, true
	}
	return string(result), nil, true
}

func parseToolObjectsByLine(content string) []map[string]any {
	var jsonObjects []map[string]any
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "{}" {
			continue
		}
		var toolsObj map[string]any
		if err := json.Unmarshal([]byte(line), &toolsObj); err == nil && len(toolsObj) > 0 {
			jsonObjects = append(jsonObjects, toolsObj)
		}
	}
	return jsonObjects
}

func mergeToolObjectList(jsonObjects []map[string]any) (map[string]any, error) {
	merged := make(map[string]any)
	for _, obj := range jsonObjects {
		var err error
		merged, err = MergeTools(merged, obj)
		if err != nil {
			return nil, err
		}
	}
	return merged, nil
}

func mergeExistingToolValue(key string, existingValue, newValue any) (any, bool, error) {
	if key == "profile" || key == "repository" {
		merged, err := mergeToolProfiles(existingValue, newValue)
		return merged, true, err
	}
	if existingArray, ok := existingValue.([]any); ok {
		if newArray, ok := newValue.([]any); ok {
			return mergeAllowedArrays(existingArray, newArray), true, nil
		}
		return nil, false, nil
	}

	existingMap, existingIsMap := existingValue.(map[string]any)
	newMap, newIsMap := newValue.(map[string]any)
	if !existingIsMap || !newIsMap {
		return nil, false, nil
	}

	if mergedMap, merged, err := mergeMCPIfApplicable(key, existingMap, newMap); merged || err != nil {
		return mergedMap, merged, err
	}
	if mergedMap, merged := mergeAllowedSubfieldIfPresent(existingMap, newMap); merged {
		return mergedMap, true, nil
	}
	recursiveMerged, err := MergeTools(existingMap, newMap)
	if err != nil {
		return nil, false, err
	}
	return recursiveMerged, true, nil
}

func mergeToolProfiles(existingValue, newValue any) ([]any, error) {
	if _, isMCPServer := existingValue.(map[string]any); isMCPServer {
		return nil, errors.New("tools.profile cannot be combined with mcp-servers.profile")
	}
	existingProfiles, err := normalizeToolProfiles(existingValue)
	if err != nil {
		return nil, err
	}
	newProfiles, err := normalizeToolProfiles(newValue)
	if err != nil {
		return nil, err
	}
	merged := make([]any, 0, len(existingProfiles)+len(newProfiles))
	seen := make(map[string]struct{}, len(existingProfiles)+len(newProfiles))
	for _, profile := range append(existingProfiles, newProfiles...) {
		if !setutil.Contains(seen, profile) {
			merged = append(merged, profile)
			seen[profile] = struct{}{}
		}
	}
	return merged, nil
}

func normalizeToolProfiles(value any) ([]string, error) {
	var values []any
	switch profiles := value.(type) {
	case string:
		values = []any{profiles}
	case []any:
		if len(profiles) == 0 {
			return nil, errors.New("tools.profile array must not be empty")
		}
		values = profiles
	default:
		return nil, errors.New("tools.profile must be a string or an array")
	}

	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		profile, ok := value.(string)
		if !ok || strings.TrimSpace(profile) == "" {
			return nil, errors.New("tools.profile entries must be nonempty strings")
		}
		if setutil.Contains(seen, profile) {
			return nil, fmt.Errorf("tools.profile contains duplicate value %q", profile)
		}
		result = append(result, profile)
		seen[profile] = struct{}{}
	}
	return result, nil
}

func mergeMCPIfApplicable(key string, existingMap, newMap map[string]any) (map[string]any, bool, error) {
	if !hasMCPType(existingMap) || !hasMCPType(newMap) {
		return nil, false, nil
	}
	mergedMap, err := mergeMCPTools(existingMap, newMap)
	if err != nil {
		return nil, false, fmt.Errorf("MCP tool conflict for '%s': %w", key, err)
	}
	return mergedMap, true, nil
}

func hasMCPType(tool map[string]any) bool {
	mcpValue, hasMCP := tool["mcp"]
	if !hasMCP {
		return false
	}
	mcpMap, ok := mcpValue.(map[string]any)
	if !ok {
		return false
	}
	mcpType, ok := mcpMap["type"].(string)
	return ok && IsMCPType(mcpType)
}

func mergeAllowedSubfieldIfPresent(existingMap, newMap map[string]any) (map[string]any, bool) {
	existingAllowed, hasExistingAllowed := existingMap["allowed"]
	newAllowed, hasNewAllowed := newMap["allowed"]
	if !hasExistingAllowed || !hasNewAllowed {
		return nil, false
	}

	merged := mergeAllowedArrays(existingAllowed, newAllowed)
	mergedMap := make(map[string]any)
	maps.Copy(mergedMap, newMap)
	maps.Copy(mergedMap, existingMap)
	mergedMap["allowed"] = merged
	return mergedMap, true
}

// mergeAllowedArrays merges two allowed arrays and removes duplicates
func mergeAllowedArrays(existing, new any) []any {
	toolsMergerLog.Print("Merging allowed arrays")
	var result []any
	seen := make(map[string]struct {
	})

	// Add existing items
	if existingSlice, ok := existing.([]any); ok {
		for _, item := range existingSlice {
			if str, ok := item.(string); ok {
				if !setutil.Contains(seen, str) {
					result = append(result, str)
					seen[str] = struct {
					}{}
				}
			}
		}
	}

	// Add new items
	if newSlice, ok := new.([]any); ok {
		for _, item := range newSlice {
			if str, ok := item.(string); ok {
				if !setutil.Contains(seen, str) {
					result = append(result, str)
					seen[str] = struct {
					}{}
				}
			}
		}
	}

	return result
}

// mergeMCPTools merges two MCP tool configurations, detecting conflicts except for 'allowed' arrays
func mergeMCPTools(existing, new map[string]any) (map[string]any, error) {
	toolsMergerLog.Printf("Merging MCP tool configs: existing_keys=%d, new_keys=%d", len(existing), len(new))
	result := make(map[string]any)

	// Copy existing properties
	maps.Copy(result, existing)

	// Merge new properties, checking for conflicts
	for key, newValue := range new {
		if existingValue, exists := result[key]; exists {
			if key == "allowed" {
				// Special handling for allowed arrays - merge them
				if existingArray, ok := existingValue.([]any); ok {
					if newArray, ok := newValue.([]any); ok {
						result[key] = mergeAllowedArrays(existingArray, newArray)
						continue
					}
				}
				// If not arrays, fall through to conflict check
			} else if key == "mcp" {
				// Special handling for mcp sub-objects - merge them recursively
				if existingMcp, ok := existingValue.(map[string]any); ok {
					if newMcp, ok := newValue.(map[string]any); ok {
						mergedMcp, err := mergeMCPTools(existingMcp, newMcp)
						if err != nil {
							return nil, fmt.Errorf("MCP config conflict: %w", err)
						}
						result[key] = mergedMcp
						continue
					}
				}
				// If not both maps, fall through to conflict check
			}

			// Check for conflicts (values must be equal)
			if !areEqual(existingValue, newValue) {
				return nil, fmt.Errorf("conflicting values for '%s': existing=%v, new=%v", key, existingValue, newValue)
			}
			// Values are equal, keep existing
		} else {
			// New property, add it
			result[key] = newValue
		}
	}

	return result, nil
}

// areEqual compares two values for equality, handling different types appropriately
func areEqual(a, b any) bool {
	// Convert to JSON for comparison to handle different types consistently
	aJSON, aErr := json.Marshal(a)
	bJSON, bErr := json.Marshal(b)

	if aErr != nil || bErr != nil {
		return false
	}

	return bytes.Equal(aJSON, bJSON)
}
