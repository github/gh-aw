package types

import (
	"fmt"
	"strconv"

	"github.com/github/gh-aw/pkg/logger"
)

var inputDefinitionLog = logger.New("types:input_definition")

// InputDefinition defines an input parameter for workflows, safe-jobs, and imported workflows.
// The structure follows the workflow_dispatch input schema from GitHub Actions:
// https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#onworkflow_dispatchinputs
type InputDefinition struct {
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Required    bool     `yaml:"required,omitempty" json:"required,omitempty"`
	Default     any      `yaml:"default,omitempty" json:"default,omitempty"` // Can be string, number, or boolean
	Type        string   `yaml:"type,omitempty" json:"type,omitempty"`       // "string", "choice", "boolean", "number", "environment"
	Options     []string `yaml:"options,omitempty" json:"options,omitempty"` // Options for choice type
	MaxLength   *int     `yaml:"max-length,omitempty" json:"max-length,omitempty"`
}

// GetDefaultAsString returns the default value as a string.
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
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		// Handle both integer and float values
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return fmt.Sprintf("%g", v)
	default:
		inputDefinitionLog.Printf("Coercing default value of unexpected type %T to string via fallback formatting", v)
		return fmt.Sprintf("%v", v)
	}
}
