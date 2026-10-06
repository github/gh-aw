package workqueue

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

var identitySchemas sync.Map

func contractIdentitySchema(name string) (map[string]any, error) {
	if cached, ok := identitySchemas.Load(name); ok {
		return cached.(map[string]any), nil
	}
	data, err := schemas.ReadFile("schema/" + name + ".json")
	if err != nil {
		return nil, err
	}
	var resource map[string]any
	if err := json.Unmarshal(data, &resource); err != nil {
		return nil, err
	}
	cached, _ := identitySchemas.LoadOrStore(name, resource)
	return cached.(map[string]any), nil
}

// JSON Schema maxLength counts Unicode scalars, not UTF-8 bytes. The generated
// extension bounds only declared identities and keys, never opaque Work text.
func validateContractIdentityBytes(name string, value any) error {
	schema, err := contractIdentitySchema(name)
	if err != nil {
		return err
	}
	return walkContractIdentities(schema, schema["$defs"].(map[string]any), value, name)
}

func walkContractIdentities(schema, definitions map[string]any, value any, path string) error {
	if len(schema) == 0 {
		return nil
	}
	if reference, ok := schema["$ref"].(string); ok {
		name := strings.TrimPrefix(reference, "#/$defs/")
		definition, ok := definitions[name].(map[string]any)
		if !ok {
			return queueError("unsupported_protocol", "unknown identity contract reference %s", name)
		}
		if err := walkContractIdentities(definition, definitions, value, path); err != nil {
			return err
		}
		if len(schema) == 1 {
			return nil
		}
	}
	if text, ok := value.(string); ok {
		if limit, ok := schema["x-utf8-max-bytes"].(float64); ok && len(text) > int(limit) {
			return queueError("identity_invalid", "%s exceeds its UTF-8 byte bound", path)
		}
		if schema["x-forbid-ascii-controls"] == true && asciiControl(text) {
			return queueError("identity_invalid", "%s contains a forbidden control character", path)
		}
	}
	if alternatives, ok := schema["anyOf"].([]any); ok {
		for _, alternative := range alternatives {
			if err := walkContractIdentities(alternative.(map[string]any), definitions, value, path); err != nil {
				return err
			}
		}
	}
	switch value := value.(type) {
	case []any:
		if item, ok := schema["items"].(map[string]any); ok {
			for index, member := range value {
				if err := walkContractIdentities(item, definitions, member, fmt.Sprintf("%s[%d]", path, index)); err != nil {
					return err
				}
			}
		}
	case map[string]any:
		properties, _ := schema["properties"].(map[string]any)
		additional, _ := schema["additionalProperties"].(map[string]any)
		for key, member := range value {
			if minimum, ok := schema["x-key-min-utf8-bytes"].(float64); ok && len(key) < int(minimum) {
				return queueError("identity_invalid", "%s contains an empty identity key", path)
			}
			if limit, ok := schema["x-key-utf8-max-bytes"].(float64); ok && len(key) > int(limit) {
				return queueError("identity_invalid", "%s contains an oversized identity key", path)
			}
			if schema["x-forbid-key-ascii-controls"] == true && asciiControl(key) {
				return queueError("identity_invalid", "%s contains a forbidden identity key control", path)
			}
			child, ok := properties[key].(map[string]any)
			if !ok {
				child = additional
			}
			if child != nil {
				if err := walkContractIdentities(child, definitions, member, path+"."+key); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func asciiControl(value string) bool {
	for _, character := range value {
		if character < 32 || character == 127 {
			return true
		}
	}
	return false
}
