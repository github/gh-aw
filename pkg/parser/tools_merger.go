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
// Only supports merging arrays and maps for neutral tools (bash, web-fetch, web-search, edit, mcp-*).
// Removes all legacy Claude tool merging logic.
func MergeTools(base, additional map[string]any) (map[string]any, error) {
	parserLog.Printf("Merging tools: base_keys=%d, additional_keys=%d", len(base), len(additional))
	result := make(map[string]any)

	maps.Copy(result, base)

	for key, newValue := range additional {
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

	if key == "ledger" {
		var err error
		existingMap, newMap, err = normalizeLedgerMaps(existingMap, newMap)
		if err != nil {
			return nil, false, err
		}
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

// normalizeLedgerMaps preserves the default ledger when merging the concise
// single-ledger form with named ledgers from another workflow.
func normalizeLedgerMaps(existing, additional map[string]any) (map[string]any, map[string]any, error) {
	existingSingle := isSingleLedgerMap(existing)
	additionalSingle := isSingleLedgerMap(additional)
	if existingSingle == additionalSingle {
		return existing, additional, nil
	}
	if existingSingle {
		if _, hasDefault := additional["default"]; hasDefault {
			return nil, nil, errors.New("ledger name 'default' conflicts with the synthesized default ledger from a concise single-ledger config")
		}
		return map[string]any{"default": existing}, additional, nil
	}
	if _, hasDefault := existing["default"]; hasDefault {
		return nil, nil, errors.New("ledger name 'default' conflicts with the synthesized default ledger from a concise single-ledger config")
	}
	return existing, map[string]any{"default": additional}, nil
}

// Keep these discriminator keys synchronized with parseLedgerToolConfig in pkg/workflow/ledger.go.
func isSingleLedgerMap(config map[string]any) bool {
	if len(config) == 0 {
		return true
	}
	for key, value := range config {
		switch key {
		case "schema", "max-record-kb", "max-segment-kb", "max-patch-kb":
			return true
		case "compaction":
			switch compaction := value.(type) {
			case bool:
				return true
			case map[string]any:
				for compactionKey := range compaction {
					switch compactionKey {
					case "schedule", "min-segments", "max-segments", "script":
						return true
					}
				}
			}
		case "replay":
			if replay, ok := value.(map[string]any); ok {
				if _, hasScript := replay["script"]; hasScript {
					return true
				}
				if _, hasConfig := replay["config"]; hasConfig {
					return true
				}
			}
		}
	}
	return false
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
	if !ok {
		return false
	}
	return IsMCPType(mcpType)
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
