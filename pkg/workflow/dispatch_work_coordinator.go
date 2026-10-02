package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const maxDispatchWorkSchemaBytes = 16 * 1024

var dispatchWorkCoordinatorIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

var dispatchWorkSchemaKeywords = map[string]struct{}{
	"type": {}, "enum": {}, "required": {}, "properties": {},
	"additionalProperties": {}, "items": {}, "oneOf": {}, "anyOf": {},
	"title": {}, "description": {},
}

// DispatchWorkCoordinatorConfig enables the built-in durable Work queue.
type DispatchWorkCoordinatorConfig struct {
	ID         string         `json:"id,omitempty" yaml:"id,omitempty"`
	Role       string         `json:"role,omitempty" yaml:"role,omitempty"`
	Schema     map[string]any `json:"schema" yaml:"schema"`
	SchemaJSON string         `json:"-" yaml:"-"`
}

func parseDispatchWorkCoordinatorConfig(raw any) (*DispatchWorkCoordinatorConfig, error) {
	config, ok := raw.(map[string]any)
	if !ok {
		return nil, errors.New("tools.dispatch-work-coordinator must be an object")
	}
	schema, ok := config["schema"].(map[string]any)
	if !ok {
		return nil, errors.New("tools.dispatch-work-coordinator.schema must be a JSON Schema object")
	}
	for key := range config {
		if key != "id" && key != "role" && key != "schema" {
			return nil, fmt.Errorf("tools.dispatch-work-coordinator has unsupported property %q", key)
		}
	}
	id := ""
	if rawID, exists := config["id"]; exists {
		var valid bool
		id, valid = rawID.(string)
		if !valid || !dispatchWorkCoordinatorIDPattern.MatchString(id) {
			return nil, errors.New("tools.dispatch-work-coordinator.id must be 1-64 alphanumeric, dot, underscore, or hyphen characters and start with an alphanumeric character")
		}
	}
	role := ""
	if rawRole, exists := config["role"]; exists {
		var valid bool
		role, valid = rawRole.(string)
		if !valid || (role != "dispatcher" && role != "worker") {
			return nil, errors.New("tools.dispatch-work-coordinator.role must be dispatcher or worker")
		}
	}
	encoded, err := json.Marshal(schema)
	if err != nil || len(encoded) > maxDispatchWorkSchemaBytes {
		return nil, fmt.Errorf("tools.dispatch-work-coordinator.schema must be valid JSON no larger than %d bytes", maxDispatchWorkSchemaBytes)
	}
	if schema["type"] != "object" {
		return nil, errors.New("tools.dispatch-work-coordinator.schema.type must be object")
	}
	if err := validateDispatchWorkSchemaExpressions(schema, 0); err != nil {
		return nil, fmt.Errorf("tools.dispatch-work-coordinator.schema: %w", err)
	}
	if err := validateDispatchWorkSchemaTree(schema, 0); err != nil {
		return nil, fmt.Errorf("tools.dispatch-work-coordinator.schema: %w", err)
	}
	if _, err := compileSchema(string(encoded), "https://github.com/github/gh-aw/dispatch-work.schema.json"); err != nil {
		return nil, fmt.Errorf("tools.dispatch-work-coordinator.schema is invalid: %w", err)
	}
	return &DispatchWorkCoordinatorConfig{ID: id, Role: role, Schema: schema, SchemaJSON: string(encoded)}, nil
}

func (config *DispatchWorkCoordinatorConfig) claimsWork() bool {
	return config != nil && config.Role != "dispatcher"
}

func (config *DispatchWorkCoordinatorConfig) requiresWorkAssignment() bool {
	return config != nil && config.Role == "worker"
}

func validateDispatchWorkSchemaExpressions(value any, depth int) error {
	if depth > 32 {
		return errors.New("schema is too deeply nested")
	}
	switch node := value.(type) {
	case string:
		if strings.Contains(node, "${{") {
			return errors.New("schema cannot contain GitHub expressions")
		}
	case []any:
		for _, child := range node {
			if err := validateDispatchWorkSchemaExpressions(child, depth+1); err != nil {
				return err
			}
		}
	case map[string]any:
		for key, child := range node {
			if strings.Contains(key, "${{") {
				return errors.New("schema cannot contain GitHub expressions")
			}
			if err := validateDispatchWorkSchemaExpressions(child, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateDispatchWorkSchemaTree(value any, depth int) error {
	if depth > 32 {
		return errors.New("schema is too deeply nested")
	}
	node, ok := value.(map[string]any)
	if !ok {
		return errors.New("each JSON Schema definition must be an object")
	}
	for key, child := range node {
		if _, ok := dispatchWorkSchemaKeywords[key]; !ok {
			return fmt.Errorf("unsupported JSON Schema keyword %q", key)
		}
		if err := validateDispatchWorkSchemaKeyword(key, child, node, depth); err != nil {
			return err
		}
	}
	return nil
}

func validateDispatchWorkSchemaKeyword(key string, value any, node map[string]any, depth int) error {
	switch key {
	case "type":
		return validateDispatchWorkSchemaType(value)
	case "enum":
		return validateDispatchWorkSchemaEnum(value)
	case "required":
		return validateDispatchWorkSchemaRequired(value)
	case "additionalProperties":
		if value != false {
			return errors.New("only additionalProperties: false is supported")
		}
	case "properties":
		properties, ok := value.(map[string]any)
		if !ok {
			return errors.New("schema properties must be an object")
		}
		for _, propertySchema := range properties {
			if err := validateDispatchWorkSchemaTree(propertySchema, depth+1); err != nil {
				return err
			}
		}
	case "items":
		return validateDispatchWorkSchemaTree(value, depth+1)
	case "oneOf", "anyOf":
		options, ok := value.([]any)
		if !ok || len(options) == 0 {
			return fmt.Errorf("schema %s must be a nonempty array", key)
		}
		if len(node) != 1 {
			return errors.New("schema alternatives cannot be combined with other constraints")
		}
		for _, option := range options {
			if err := validateDispatchWorkSchemaTree(option, depth+1); err != nil {
				return err
			}
		}
	case "title", "description":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("schema %s must be a string", key)
		}
	}
	return nil
}

func validateDispatchWorkSchemaType(value any) error {
	types := []any{value}
	if list, ok := value.([]any); ok {
		types = list
	}
	if len(types) == 0 {
		return errors.New("schema type must not be empty")
	}
	for _, item := range types {
		typeName, ok := item.(string)
		if !ok || !strings.Contains("|object|array|string|number|integer|boolean|null|", "|"+typeName+"|") {
			return errors.New("schema contains an unsupported type")
		}
	}
	return nil
}

func validateDispatchWorkSchemaEnum(value any) error {
	values, ok := value.([]any)
	if !ok || len(values) == 0 {
		return errors.New("schema enum must be a nonempty array")
	}
	for _, item := range values {
		if item != nil {
			switch item.(type) {
			case string, bool, float64, int, int64:
			default:
				return errors.New("schema enum supports only primitive JSON values")
			}
		}
	}
	return nil
}

func validateDispatchWorkSchemaRequired(value any) error {
	fields, ok := value.([]any)
	if !ok {
		return errors.New("schema required must be an array of strings")
	}
	for _, field := range fields {
		if _, ok := field.(string); !ok {
			return errors.New("schema required must be an array of strings")
		}
	}
	return nil
}

func validateDispatchWorkCoordinatorPermissions(data *WorkflowData) error {
	permissions := NewPermissionsParser(data.Permissions).ToPermissions()
	if level, ok := permissions.Get(PermissionContents); !ok || level != PermissionWrite {
		return errors.New("tools.dispatch-work-coordinator requires contents: write permission")
	}
	if githubTool, ok := data.Tools["github"]; ok && githubTool != false && !isGitHubCLIModeEnabled(data) {
		githubConfig := parseGitHubTool(githubTool)
		if githubConfig == nil || !githubConfig.ReadOnly {
			return errors.New("tools.dispatch-work-coordinator requires tools.github.read-only: true to prevent direct writes outside coordinator reconciliation")
		}
	}
	return nil
}
