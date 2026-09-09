package workflow

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"strconv"

	"github.com/github/gh-aw/pkg/importinpututil"
	"github.com/github/gh-aw/pkg/logger"
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
	ContinueOnError  *TemplatableBool  `yaml:"continue-on-error,omitempty"` // Can be bool or string expression
	TimeoutMinutes   int               `yaml:"timeout-minutes,omitempty"`
}

// IsUsesStep returns true if this step uses an action (has a "uses" field)
func (s *WorkflowStep) IsUsesStep() bool {
	return s.Uses != ""
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
		switch s.ContinueOnError.String() {
		case "true":
			result["continue-on-error"] = true
		case "false":
			result["continue-on-error"] = false
		default:
			result["continue-on-error"] = s.ContinueOnError.String()
		}
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
		return nil, errors.New("step map is nil")
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
		step.Env = parseStepEnv(env)
	}
	if continueOnError, ok := stepMap["continue-on-error"]; ok {
		step.ContinueOnError = parseStepContinueOnError(continueOnError)
	}
	if timeoutMinutesVal, ok := stepMap["timeout-minutes"]; ok {
		step.TimeoutMinutes = parseStepTimeoutMinutes(timeoutMinutesVal)
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

func parseStepEnv(env map[string]any) map[string]string {
	result := make(map[string]string)
	for k, v := range env {
		if strVal, ok := v.(string); ok {
			result[k] = strVal
		} else if v != nil {
			result[k] = marshalEnvValue(v)
		}
	}
	return result
}

func parseStepContinueOnError(val any) *TemplatableBool {
	switch value := val.(type) {
	case bool:
		templatableValue := TemplatableBool(strconv.FormatBool(value))
		return &templatableValue
	case string:
		if value == "true" || value == "false" || isExpression(value) {
			templatableValue := TemplatableBool(value)
			return &templatableValue
		}
	}
	return nil
}

// parseStepTimeoutMinutes converts a YAML `timeout-minutes` value into a positive
// number of minutes. Values that are not positive integers within the platform int
// range are ignored (returning 0, which omits the field from the rendered step).
func parseStepTimeoutMinutes(val any) int {
	switch v := val.(type) {
	case int:
		if v > 0 {
			return v
		}
	case int64:
		if v > 0 && v <= int64(math.MaxInt) {
			return int(v)
		}
	case uint64:
		if v > 0 && v <= uint64(math.MaxInt) {
			return int(v)
		}
	case float64:
		// float64 loses integer precision near MaxInt on 64-bit platforms, so treat
		// values at or above the rounded float boundary as out of range. Only
		// integral values are accepted so fractional timeouts are not truncated.
		if math.IsNaN(v) || math.IsInf(v, 0) || v != math.Trunc(v) {
			return 0
		}
		if v >= 1 && v < float64(math.MaxInt) {
			return int(v)
		}
	case string:
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 0
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
		TimeoutMinutes:   s.TimeoutMinutes,
	}

	if s.ContinueOnError != nil {
		continueOnError := *s.ContinueOnError
		clone.ContinueOnError = &continueOnError
	}

	if s.With != nil {
		clone.With = make(map[string]any, len(s.With))
		maps.Copy(clone.With, s.With)
	}

	if s.Env != nil {
		clone.Env = make(map[string]string, len(s.Env))
		maps.Copy(clone.Env, s.Env)
	}

	return clone
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

// marshalEnvValue serializes a non-string env var value to a string suitable
// for use in a GitHub Actions step env block.
// Arrays and maps are serialized as JSON (e.g. ["a","b"]) via
// importinpututil.FormatResolvedValue so import substitutions and env
// serialization stay aligned. Scalar values (int, bool, float64, etc.)
// fall back to fmt.Sprint.
func marshalEnvValue(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := importinpututil.FormatResolvedValue(v); ok {
		return s
	}
	return fmt.Sprint(v)
}
