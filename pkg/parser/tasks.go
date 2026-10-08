package parser

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// TaskDefinition is a workflow-owned command, never a model-supplied command.
type TaskDefinition struct {
	Description string   `json:"description" yaml:"description"`
	Command     string   `json:"command" yaml:"command"`
	Args        []string `json:"args" yaml:"args"`
	Timeout     int      `json:"timeout" yaml:"timeout"`
}

var taskNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}$`)
var taskCommandPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.+-]{0,127}$`)

// GoTasks returns fresh definitions so callers cannot mutate the built-in set.
func GoTasks() map[string]TaskDefinition {
	return map[string]TaskDefinition{
		"go.test":      {Description: "Run all Go tests", Command: "go", Args: []string{"test", "-count=1", "./..."}, Timeout: 300},
		"go.vet":       {Description: "Run Go static analysis", Command: "go", Args: []string{"vet", "./..."}, Timeout: 300},
		"go.build":     {Description: "Build Go packages", Command: "go", Args: []string{"build", "./..."}, Timeout: 300},
		"go.fmt":       {Description: "Format Go source files in the workspace", Command: "gofmt", Args: []string{"-w", "."}, Timeout: 60},
		"go.readiness": {Description: "Check Go test compilation and initialization", Command: "go", Args: []string{"test", "-run", "^$", "./..."}, Timeout: 300},
	}
}

func parseTaskDefinition(name string, value any) (TaskDefinition, error) {
	var task TaskDefinition
	if name == "constructor" || name == "prototype" || name == "__proto__" {
		return task, fmt.Errorf("tools.tasks task name %q is reserved; choose a different name", name)
	}
	if !taskNamePattern.MatchString(name) {
		return task, fmt.Errorf("tools.tasks task name %q must start with a letter and contain at most 64 letters, digits, dots, underscores or hyphens", name)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return task, fmt.Errorf("tools.tasks.%s: %w", name, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&task); err != nil {
		return task, fmt.Errorf("tools.tasks.%s must be a task definition: %w", name, err)
	}
	if strings.TrimSpace(task.Description) == "" || len(task.Description) > 1024 || !taskCommandPattern.MatchString(task.Command) {
		return task, fmt.Errorf("tools.tasks.%s requires a non-empty description (at most 1024 bytes) and an executable name without paths", name)
	}
	if task.Args == nil {
		if fields, ok := value.(map[string]any); ok {
			if _, present := fields["args"]; present {
				return task, fmt.Errorf("tools.tasks.%s.args must be an array of strings", name)
			}
		}
		task.Args = []string{}
	}
	if task.Timeout == 0 {
		if fields, ok := value.(map[string]any); ok {
			if _, present := fields["timeout"]; present {
				return task, fmt.Errorf("tools.tasks.%s.timeout must be between 1 and 600 seconds", name)
			}
		}
		task.Timeout = 60
	}
	if task.Timeout < 1 || task.Timeout > 600 || len(task.Args) > 128 {
		return task, fmt.Errorf("tools.tasks.%s requires a timeout between 1 and 600 seconds and at most 128 arguments", name)
	}
	for _, literal := range append([]string{task.Description, task.Command}, task.Args...) {
		if strings.Contains(literal, "${{") || strings.ContainsRune(literal, '\x00') || len(literal) > 8192 {
			return task, fmt.Errorf("tools.tasks.%s fields must be literal strings without Actions expressions or NUL bytes, at most 8192 bytes each", name)
		}
	}
	return task, nil
}

// ResolveTasks validates custom tasks and expands enabled built-in sets.
func ResolveTasks(value any) (map[string]TaskDefinition, error) {
	result := make(map[string]TaskDefinition)
	if disabled, ok := value.(bool); ok && !disabled {
		return result, nil
	}
	definitions, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("tools.tasks must be false or a map of named tasks and built-in sets")
	}
	builtins := GoTasks()
	for _, name := range slices.Sorted(maps.Keys(definitions)) {
		value := definitions[name]
		if name == "go" {
			enabled, ok := value.(bool)
			if !ok {
				return nil, errors.New("tools.tasks.go must be true or false")
			}
			if enabled {
				maps.Copy(result, builtins)
			}
			continue
		}
		if _, reserved := builtins[name]; reserved {
			return nil, fmt.Errorf("tools.tasks.%s is reserved by the built-in Go set; use a different custom task name", name)
		}
		task, err := parseTaskDefinition(name, value)
		if err != nil {
			return nil, err
		}
		result[name] = task
	}
	if len(result) > 64 {
		return nil, errors.New("tools.tasks supports at most 64 expanded tasks")
	}
	return result, nil
}

func mergeTaskConfigurations(base, additional any) (any, error) {
	for _, value := range []any{base, additional} {
		if _, err := ResolveTasks(value); err != nil {
			return nil, err
		}
	}
	if disabled, ok := base.(bool); ok && !disabled {
		return false, nil
	}
	if disabled, ok := additional.(bool); ok && !disabled {
		return false, nil
	}
	baseMap := base.(map[string]any)
	additionalMap := additional.(map[string]any)
	result := maps.Clone(baseMap)
	for name, value := range additionalMap {
		existing, exists := result[name]
		if !exists {
			result[name] = value
			continue
		}
		if name == "go" {
			result[name] = existing == true && value == true
			continue
		}
		left, err := parseTaskDefinition(name, existing)
		if err != nil {
			return nil, err
		}
		right, err := parseTaskDefinition(name, value)
		if err != nil {
			return nil, err
		}
		if left.Command != right.Command || left.Description != right.Description || left.Timeout != right.Timeout || !slices.Equal(left.Args, right.Args) {
			return nil, fmt.Errorf("conflicting definitions of tools.tasks.%s across workflow imports; task arguments and executable definitions cannot be merged", name)
		}
	}
	return result, nil
}
