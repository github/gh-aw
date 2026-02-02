package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/goccy/go-yaml"
)

var stepTypesLog = logger.New("workflow:step_types")

// WorkflowStep represents a single step in a GitHub Actions workflow job
// This struct provides type safety and compile-time validation for step configurations
type WorkflowStep struct {
	Name             string            `yaml:"name,omitempty"`
	ID               string            `yaml:"id,omitempty"`
	If               string            `yaml:"if,omitempty"`
	Uses             string            `yaml:"uses,omitempty"`
	Run              string            `yaml:"run,omitempty"`
	WorkingDirectory string            `yaml:"working-directory,omitempty"`
	Shell            string            `yaml:"shell,omitempty"`
	With             map[string]any    `yaml:"with,omitempty"`
	Env              map[string]string `yaml:"env,omitempty"`
	ContinueOnError  any               `yaml:"continue-on-error,omitempty"` // Can be bool or string expression
	TimeoutMinutes   int               `yaml:"timeout-minutes,omitempty"`
}

// IsUsesStep returns true if this step uses an action (has a "uses" field)
func (s *WorkflowStep) IsUsesStep() bool {
	return s.Uses != ""
}

// IsRunStep returns true if this step runs a command (has a "run" field)
func (s *WorkflowStep) IsRunStep() bool {
	return s.Run != ""
}

// ToMap converts a WorkflowStep to a map[string]any for YAML generation
// This is used when generating the final workflow YAML output
func (s *WorkflowStep) ToMap() map[string]any {
	result := make(map[string]any)

	if s.Name != "" {
		result["name"] = s.Name
	}
	if s.ID != "" {
		result["id"] = s.ID
	}
	if s.If != "" {
		result["if"] = s.If
	}
	if s.Uses != "" {
		result["uses"] = s.Uses
	}
	if s.Run != "" {
		result["run"] = s.Run
	}
	if s.WorkingDirectory != "" {
		result["working-directory"] = s.WorkingDirectory
	}
	if s.Shell != "" {
		result["shell"] = s.Shell
	}
	if len(s.With) > 0 {
		result["with"] = s.With
	}
	if len(s.Env) > 0 {
		result["env"] = s.Env
	}
	if s.ContinueOnError != nil {
		result["continue-on-error"] = s.ContinueOnError
	}
	if s.TimeoutMinutes > 0 {
		result["timeout-minutes"] = s.TimeoutMinutes
	}

	return result
}

// MapToStep converts a map[string]any to a WorkflowStep
// This is the inverse of ToMap and is used when parsing step configurations
func MapToStep(stepMap map[string]any) (*WorkflowStep, error) {
	stepTypesLog.Printf("Converting map to workflow step: map_keys=%d", len(stepMap))
	if stepMap == nil {
		return nil, fmt.Errorf("step map is nil")
	}

	step := &WorkflowStep{}

	if name, ok := stepMap["name"].(string); ok {
		step.Name = name
	}
	if id, ok := stepMap["id"].(string); ok {
		step.ID = id
	}
	if ifCond, ok := stepMap["if"].(string); ok {
		step.If = ifCond
	}
	if uses, ok := stepMap["uses"].(string); ok {
		step.Uses = uses
	}
	if run, ok := stepMap["run"].(string); ok {
		step.Run = run
	}
	if workingDir, ok := stepMap["working-directory"].(string); ok {
		step.WorkingDirectory = workingDir
	}
	if shell, ok := stepMap["shell"].(string); ok {
		step.Shell = shell
	}
	if with, ok := stepMap["with"].(map[string]any); ok {
		step.With = with
	}
	if env, ok := stepMap["env"].(map[string]any); ok {
		// Convert map[string]any to map[string]string
		step.Env = make(map[string]string)
		for k, v := range env {
			if strVal, ok := v.(string); ok {
				step.Env[k] = strVal
			}
		}
	}
	if continueOnError, ok := stepMap["continue-on-error"]; ok {
		// Preserve the original type (bool or string)
		step.ContinueOnError = continueOnError
	}
	if timeoutMinutes, ok := stepMap["timeout-minutes"].(int); ok {
		step.TimeoutMinutes = timeoutMinutes
	}

	stepType := "unknown"
	if step.Uses != "" {
		stepType = "uses"
	} else if step.Run != "" {
		stepType = "run"
	}
	stepTypesLog.Printf("Successfully converted step: type=%s, name=%s", stepType, step.Name)
	return step, nil
}

// Clone creates a deep copy of the WorkflowStep
func (s *WorkflowStep) Clone() *WorkflowStep {
	clone := &WorkflowStep{
		Name:             s.Name,
		ID:               s.ID,
		If:               s.If,
		Uses:             s.Uses,
		Run:              s.Run,
		WorkingDirectory: s.WorkingDirectory,
		Shell:            s.Shell,
		ContinueOnError:  s.ContinueOnError,
		TimeoutMinutes:   s.TimeoutMinutes,
	}

	if s.With != nil {
		clone.With = make(map[string]any, len(s.With))
		for k, v := range s.With {
			clone.With[k] = v
		}
	}

	if s.Env != nil {
		clone.Env = make(map[string]string, len(s.Env))
		for k, v := range s.Env {
			clone.Env[k] = v
		}
	}

	return clone
}

// ToYAML converts the WorkflowStep to YAML string
func (s *WorkflowStep) ToYAML() (string, error) {
	stepTypesLog.Printf("Converting step to YAML: name=%s", s.Name)
	stepMap := s.ToMap()
	yamlBytes, err := yaml.Marshal(stepMap)
	if err != nil {
		stepTypesLog.Printf("Failed to marshal step to YAML: %v", err)
		return "", fmt.Errorf("failed to marshal step to YAML: %w", err)
	}
	stepTypesLog.Printf("Successfully converted step to YAML: size=%d bytes", len(yamlBytes))
	return string(yamlBytes), nil
}

// SliceToSteps converts a slice of any (typically []map[string]any from YAML parsing)
// to a typed slice of WorkflowStep pointers for type-safe manipulation
func SliceToSteps(steps []any) ([]*WorkflowStep, error) {
	stepTypesLog.Printf("Converting slice to typed steps: count=%d", len(steps))
	if steps == nil {
		return nil, nil
	}

	result := make([]*WorkflowStep, 0, len(steps))
	for i, stepAny := range steps {
		stepMap, ok := stepAny.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("step %d is not a map[string]any, got %T", i, stepAny)
		}

		step, err := MapToStep(stepMap)
		if err != nil {
			return nil, fmt.Errorf("failed to convert step %d: %w", i, err)
		}

		result = append(result, step)
	}

	stepTypesLog.Printf("Successfully converted %d steps to typed steps", len(result))
	return result, nil
}

// StepsToSlice converts a typed slice of WorkflowStep pointers back to []any
// for backward compatibility with existing YAML generation code
func StepsToSlice(steps []*WorkflowStep) []any {
	stepTypesLog.Printf("Converting typed steps to slice: count=%d", len(steps))
	if steps == nil {
		return nil
	}

	result := make([]any, 0, len(steps))
	for _, step := range steps {
		if step != nil {
			result = append(result, step.ToMap())
		}
	}

	stepTypesLog.Printf("Successfully converted %d typed steps to slice", len(result))
	return result
}
