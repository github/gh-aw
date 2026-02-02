package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var inputsLog = logger.New("workflow:inputs")

// InputDefinition defines an input parameter for workflows, safe-jobs, and imported workflows.
// This is a unified type that consolidates the common input schema used across:
// - workflow_dispatch inputs (GitHub Actions native)
// - safe-jobs inputs (safe-outputs.jobs.[name].inputs)
// - imported workflow inputs (imports with inputs parameter)
//
// The structure follows the workflow_dispatch input schema from GitHub Actions:
// https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#onworkflow_dispatchinputs
type InputDefinition struct {
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Required    bool     `yaml:"required,omitempty" json:"required,omitempty"`
	Default     any      `yaml:"default,omitempty" json:"default,omitempty"` // Can be string, number, or boolean
	Type        string   `yaml:"type,omitempty" json:"type,omitempty"`       // "string", "choice", "boolean", "number", "environment"
	Options     []string `yaml:"options,omitempty" json:"options,omitempty"` // Options for choice type
}

// ParseInputDefinition parses an input definition from a map.
// This is a shared helper function that handles the common parsing logic
// for input definitions regardless of their source (safe-jobs, imports, etc.).
func ParseInputDefinition(inputConfig map[string]any) *InputDefinition {
	input := &InputDefinition{}

	// Parse description
	if desc, exists := inputConfig["description"]; exists {
		if descStr, ok := desc.(string); ok {
			input.Description = descStr
		}
	}

	// Parse required
	if req, exists := inputConfig["required"]; exists {
		if reqBool, ok := req.(bool); ok {
			input.Required = reqBool
		}
	}

	// Parse default - supports string, number, or boolean
	if def, exists := inputConfig["default"]; exists {
		input.Default = def
	}

	// Parse type
	if typ, exists := inputConfig["type"]; exists {
		if typStr, ok := typ.(string); ok {
			input.Type = typStr
		}
	}

	// Parse options (for choice type)
	if opts, exists := inputConfig["options"]; exists {
		if optsList, ok := opts.([]any); ok {
			for _, opt := range optsList {
				if optStr, ok := opt.(string); ok {
					input.Options = append(input.Options, optStr)
				}
			}
		} else if optsStr, ok := opts.([]string); ok {
			input.Options = optsStr
		}
	}

	return input
}

// ParseInputDefinitions parses a map of input definitions from a frontmatter map.
// Returns a map of input name to InputDefinition.
func ParseInputDefinitions(inputsMap map[string]any) map[string]*InputDefinition {
	if inputsMap == nil {
		return nil
	}

	result := make(map[string]*InputDefinition)

	for inputName, inputValue := range inputsMap {
		if inputConfig, ok := inputValue.(map[string]any); ok {
			result[inputName] = ParseInputDefinition(inputConfig)
		}
	}

	inputsLog.Printf("Parsed %d input definitions", len(result))
	return result
}

// GetDefaultAsString returns the default value as a string.
// Useful for backwards compatibility with code that expects string defaults.
func (i *InputDefinition) GetDefaultAsString() string {
	if i.Default == nil {
		return ""
	}

	switch v := i.Default.(type) {
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case int:
		return fmt.Sprintf("%d", v)
	case int64:
		return fmt.Sprintf("%d", v)
	case float64:
		// Handle both integer and float values
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return fmt.Sprintf("%g", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}
