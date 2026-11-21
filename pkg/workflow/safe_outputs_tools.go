package workflow

import (
	_ "embed"
	"encoding/json"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var safeOutputsToolsLog = logger.New("workflow:safe_outputs_tools")

//go:embed js/safe_outputs_tools.json
var safeOutputsToolsJSONContent string

// SafeOutputToolOption represents a safe output tool with its name and description.
type SafeOutputToolOption struct {
	// Name is the snake_case name from the JSON (e.g. "create_issue").
	Name string
	// Key is the hyphenated YAML config key (e.g. "create-issue").
	Key string
	// Description is the tool's description from safe_outputs_tools.json.
	Description string
}

// internalSafeOutputs are tool names that are internal / system-level and should
// not be presented to users as selectable safe outputs.
var internalSafeOutputs = map[string]bool{
	"report_missing_tool":  true,
	"noop":                 true,
	"report_missing_data":  true,
	"report_incomplete":    true,
	"request_continuation": true,
}

// GetSafeOutputToolOptions parses safe_outputs_tools.json and returns all user-facing
// safe output tools with their human-readable descriptions.
// Tools that are internal (report_missing_tool, noop, report_missing_data) are excluded.
func GetSafeOutputToolOptions() []SafeOutputToolOption {
	var tools []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal([]byte(safeOutputsToolsJSONContent), &tools); err != nil {
		safeOutputsToolsLog.Printf("Failed to parse safe_outputs_tools.json: %v", err)
		return nil
	}

	safeOutputsToolsLog.Printf("Parsing safe output tool options: totalTools=%d, internalTools=%d", len(tools), len(internalSafeOutputs))

	options := make([]SafeOutputToolOption, 0, len(tools))
	for _, t := range tools {
		if internalSafeOutputs[t.Name] {
			continue
		}
		options = append(options, SafeOutputToolOption{
			Name:        t.Name,
			Key:         strings.ReplaceAll(t.Name, "_", "-"),
			Description: t.Description,
		})
	}

	safeOutputsToolsLog.Printf("Loaded %d user-facing safe output tool options", len(options))
	return options
}
