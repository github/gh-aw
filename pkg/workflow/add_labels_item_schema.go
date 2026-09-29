package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
)

func validateAddLabelsItemSchema(config *SafeOutputsConfig) error {
	if config == nil || config.AddLabels == nil || config.AddLabels.ItemSchema == nil {
		return nil
	}
	_, err := normalizedAddLabelsItemSchema(config.AddLabels.ItemSchema)
	return err
}

func normalizedAddLabelsItemSchema(narrowing map[string]any) (map[string]any, error) {
	base, err := addLabelsBaseItemSchema(narrowing["type"])
	if err != nil {
		return nil, err
	}
	return narrowItemSchema(base, narrowing, "safe-outputs.add-labels.item-schema")
}

func addLabelsBaseItemSchema(itemType any) (map[string]any, error) {
	type toolDefinition struct {
		Name        string `json:"name"`
		InputSchema struct {
			Properties map[string]struct {
				Items map[string]any `json:"items"`
			} `json:"properties"`
		} `json:"inputSchema"`
	}
	var tools []toolDefinition
	if err := json.Unmarshal([]byte(safeOutputsToolsJSONContent), &tools); err != nil {
		return nil, fmt.Errorf("safe-outputs.add-labels.item-schema: unable to load built-in schema: %w", err)
	}
	requestedType, ok := itemType.(string)
	if !ok || (requestedType != "object" && requestedType != "string") {
		return nil, errors.New("safe-outputs.add-labels.item-schema.type must be \"object\" or \"string\"")
	}
	for _, tool := range tools {
		if tool.Name != "add_labels" {
			continue
		}
		alternatives, ok := tool.InputSchema.Properties["labels"].Items["oneOf"].([]any)
		if !ok {
			continue
		}
		for _, alternative := range alternatives {
			schema, ok := alternative.(map[string]any)
			if !ok {
				continue
			}
			if schema["type"] == requestedType {
				return schema, nil
			}
		}
	}
	return nil, fmt.Errorf("safe-outputs.add-labels.item-schema: built-in %s label schema not found", requestedType)
}

func narrowItemSchema(base, narrowing map[string]any, fieldPath string) (map[string]any, error) {
	allowedKeywords := map[string]struct{}{"type": {}, "description": {}}
	switch base["type"] {
	case "object":
		allowedKeywords["properties"] = struct{}{}
		allowedKeywords["required"] = struct{}{}
		allowedKeywords["additionalProperties"] = struct{}{}
	case "string", "boolean":
		allowedKeywords["enum"] = struct{}{}
	}
	for keyword := range narrowing {
		if _, ok := allowedKeywords[keyword]; !ok {
			return nil, fmt.Errorf("%s.%s is not supported; item schemas may only tighten built-in types, required fields, properties, and values", fieldPath, keyword)
		}
	}
	if narrowing["type"] != base["type"] {
		return nil, fmt.Errorf("%s.type must remain %q", fieldPath, base["type"])
	}

	result := cloneSchemaMap(base)
	if description, exists := narrowing["description"]; exists {
		descriptionString, ok := description.(string)
		if !ok {
			return nil, fmt.Errorf("%s.description must be a string", fieldPath)
		}
		result["description"] = descriptionString
	}
	switch base["type"] {
	case "object":
		return narrowObjectItemSchema(base, narrowing, result, fieldPath)
	case "string", "boolean":
		return narrowScalarItemSchema(base, narrowing, result, fieldPath)
	default:
		return nil, fmt.Errorf("%s.type %q cannot be narrowed", fieldPath, base["type"])
	}
}

func narrowObjectItemSchema(base, narrowing, result map[string]any, fieldPath string) (map[string]any, error) {
	if additional, exists := narrowing["additionalProperties"]; exists && additional != false {
		return nil, fmt.Errorf("%s.additionalProperties must be false", fieldPath)
	}
	result["additionalProperties"] = false

	baseProperties, ok := base["properties"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: built-in properties are invalid", fieldPath)
	}
	narrowProperties, ok := narrowing["properties"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s.properties must be an object", fieldPath)
	}
	properties := make(map[string]any, len(narrowProperties))
	for name, rawSchema := range narrowProperties {
		baseProperty, exists := baseProperties[name].(map[string]any)
		if !exists {
			return nil, fmt.Errorf("%s.properties.%s is not a built-in label field", fieldPath, name)
		}
		propertySchema, ok := rawSchema.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s.properties.%s must be an object", fieldPath, name)
		}
		normalized, err := narrowItemSchema(baseProperty, propertySchema, fieldPath+".properties."+name)
		if err != nil {
			return nil, err
		}
		properties[name] = normalized
	}
	result["properties"] = properties

	required, err := stringList(narrowing["required"], fieldPath+".required")
	if err != nil {
		return nil, err
	}
	baseRequired, _ := stringList(base["required"], fieldPath+".required")
	for _, name := range baseRequired {
		if !slices.Contains(required, name) {
			required = append(required, name)
		}
	}
	for _, name := range required {
		if _, exists := properties[name]; !exists {
			return nil, fmt.Errorf("%s requires %q, so that field must be defined in properties", fieldPath, name)
		}
	}
	result["required"] = required
	return result, nil
}

func narrowScalarItemSchema(base, narrowing, result map[string]any, fieldPath string) (map[string]any, error) {
	if enum, exists := narrowing["enum"]; exists {
		values, ok := enum.([]any)
		if !ok || len(values) == 0 {
			return nil, fmt.Errorf("%s.enum must be a non-empty array", fieldPath)
		}
		for _, value := range values {
			if !schemaValueMatchesType(value, base["type"]) {
				return nil, fmt.Errorf("%s.enum contains value %v with the wrong type", fieldPath, value)
			}
		}
		if baseEnum, ok := base["enum"].([]any); ok {
			for _, value := range values {
				if !slices.Contains(baseEnum, value) {
					return nil, fmt.Errorf("%s.enum contains unsupported value %v", fieldPath, value)
				}
			}
		}
		result["enum"] = values
	}
	return result, nil
}

func schemaValueMatchesType(value, schemaType any) bool {
	switch schemaType {
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	default:
		return false
	}
}

func stringList(value any, fieldPath string) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	values, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array of strings", fieldPath)
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		item, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("%s must be an array of strings", fieldPath)
		}
		if !slices.Contains(result, item) {
			result = append(result, item)
		}
	}
	return result, nil
}

func cloneSchemaMap(schema map[string]any) map[string]any {
	clone := make(map[string]any, len(schema))
	maps.Copy(clone, schema)
	return clone
}
