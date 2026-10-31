package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/types"
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
type InputDefinition = types.InputDefinition

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

	if maxLength, exists := inputConfig["max-length"]; exists {
		input.MaxLength = parseInputMaxLength(maxLength)
	}

	inputsLog.Printf("Parsed input definition: type=%s, required=%t, options=%d", input.Type, input.Required, len(input.Options))
	return input
}

func parseInputMaxLength(value any) *int {
	var maxLength int
	switch value := value.(type) {
	case int:
		maxLength = value
	case int64:
		maxLength = int(value)
	case uint64:
		maxLength = int(value)
	case float64:
		if value == float64(int(value)) {
			maxLength = int(value)
		}
	}
	if maxLength > 0 {
		return &maxLength
	}
	return nil
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
