// Package parser provides functions for parsing and processing workflow markdown files.
// import_schema_validation.go validates 'with' inputs against import-schema declarations
// and applies default values from import-schema to missing inputs.
package parser

import (
	"fmt"
	"maps"
)

// validateWithImportSchema validates the provided 'with'/'inputs' values against
// the 'import-schema' declared in the imported workflow's frontmatter.
// It checks that:
//   - all required parameters declared in import-schema are present in 'with'
//   - no unknown parameters are provided (i.e., not declared in import-schema)
//   - provided values match the declared type (string, number, boolean, choice)
//   - choice values are within the allowed options list
//
// If the imported workflow has no 'import-schema', all provided 'with' values are
// accepted without validation (backward compatibility with 'inputs' form).
func validateWithImportSchema(inputs map[string]any, fm map[string]any, importPath string) error {
	importLog.Printf("Validating 'with' inputs against import-schema: import=%s, inputs=%d", importPath, len(inputs))
	rawSchema, hasSchema := fm["import-schema"]
	if !hasSchema {
		return nil
	}
	schemaMap, ok := rawSchema.(map[string]any)
	if !ok {
		return nil
	}
	if len(schemaMap) == 0 {
		return nil
	}
	importLog.Printf("Import-schema declares %d field(s) for %s", len(schemaMap), importPath)

	// Check for unknown keys not declared in import-schema
	for key := range inputs {
		if _, declared := schemaMap[key]; !declared {
			return fmt.Errorf("import '%s': unknown 'with' input %q is not declared in the import-schema", importPath, key)
		}
	}

	// Check each declared schema field
	for paramName, paramDefRaw := range schemaMap {
		paramDef, isObject := paramDefRaw.(map[string]any)
		if !isObject {
			continue
		}

		// Check required parameters
		if req, isBool := paramDef["required"].(bool); isBool && req {
			if _, provided := inputs[paramName]; !provided {
				return fmt.Errorf("import '%s': required 'with' input %q is missing (declared in import-schema)", importPath, paramName)
			}
		}

		value, provided := inputs[paramName]
		if !provided {
			continue
		}

		// Skip type validation when type is not specified
		declaredType, isString := paramDef["type"].(string)
		if !isString || declaredType == "" {
			continue
		}

		// Validate type
		if err := validateImportInputType(paramName, value, declaredType, paramDef, importPath); err != nil {
			return err
		}
	}
	return nil
}

// validateObjectInput validates a 'with' value of type object against the
// one-level deep 'properties' declared in the import-schema.
func validateObjectInput(name string, value any, paramDef map[string]any, importPath string) error {
	objMap, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("import '%s': 'with' input %q must be an object (got %T)", importPath, name, value)
	}
	propsAny, hasProps := paramDef["properties"]
	if !hasProps {
		return nil // no schema for properties - accept any object
	}
	propsMap, ok := propsAny.(map[string]any)
	if !ok {
		return nil
	}
	// Check for unknown sub-keys
	for subKey := range objMap {
		if _, declared := propsMap[subKey]; !declared {
			return fmt.Errorf("import '%s': 'with' input %q has unknown property %q (not in import-schema)", importPath, name, subKey)
		}
	}
	// Validate each declared property
	for propName, propDefRaw := range propsMap {
		propDef, isObject := propDefRaw.(map[string]any)
		if !isObject {
			continue
		}
		// Check required sub-fields
		if req, isBool := propDef["required"].(bool); isBool && req {
			if _, provided := objMap[propName]; !provided {
				return fmt.Errorf("import '%s': required property %q of 'with' input %q is missing", importPath, propName, name)
			}
		}
		subValue, provided := objMap[propName]
		if !provided {
			continue
		}
		propType, isString := propDef["type"].(string)
		if !isString || propType == "" {
			continue
		}
		qualifiedName := name + "." + propName
		if err := validateImportInputType(qualifiedName, subValue, propType, propDef, importPath); err != nil {
			return err
		}
	}
	return nil
}

// validateImportInputType checks that a single 'with' value matches the declared type.
func validateImportInputType(name string, value any, declaredType string, paramDef map[string]any, importPath string) error {
	switch declaredType {
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("import '%s': 'with' input %q must be a string (got %T)", importPath, name, value)
		}
	case "number":
		// Accept all numeric types that YAML parsers may produce
		switch value.(type) {
		case int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64,
			float32, float64:
			// OK
		default:
			return fmt.Errorf("import '%s': 'with' input %q must be a number (got %T)", importPath, name, value)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("import '%s': 'with' input %q must be a boolean (got %T)", importPath, name, value)
		}
	case "choice":
		strVal, ok := value.(string)
		if !ok {
			return fmt.Errorf("import '%s': 'with' input %q must be a string for choice type (got %T)", importPath, name, value)
		}
		if opts, hasOpts := paramDef["options"]; hasOpts {
			if optsList, ok := opts.([]any); ok {
				for _, opt := range optsList {
					if optStr, ok := opt.(string); ok && optStr == strVal {
						return nil
					}
				}
				return fmt.Errorf("import '%s': 'with' input %q value %q is not in the allowed options", importPath, name, strVal)
			}
		}
	case "array":
		arr, ok := value.([]any)
		if !ok {
			return fmt.Errorf("import '%s': 'with' input %q must be an array (got %T)", importPath, name, value)
		}
		// Validate item types if an 'items' schema is declared
		itemsDefRaw, hasItems := paramDef["items"]
		if !hasItems {
			return nil
		}
		itemsDef, isObject := itemsDefRaw.(map[string]any)
		if !isObject {
			return nil
		}
		itemType, isString := itemsDef["type"].(string)
		if !isString || itemType == "" {
			return nil
		}
		for i, item := range arr {
			itemName := fmt.Sprintf("%s[%d]", name, i)
			if err := validateImportInputType(itemName, item, itemType, itemsDef, importPath); err != nil {
				return err
			}
		}
	case "object":
		return validateObjectInput(name, value, paramDef, importPath)
	}
	return nil
}

// applyImportSchemaDefaultsFromFrontmatter applies import-schema defaults from an
// already-parsed frontmatter map, avoiding a redundant YAML parse when the caller
// has already extracted the frontmatter. Returns the original inputs map unchanged
// when no import-schema defaults apply; otherwise returns a copy augmented with
// default values for schema parameters declared with a "default" field but not
// present in the provided inputs map. Parameters already in inputs are left unchanged.
func applyImportSchemaDefaultsFromFrontmatter(frontmatter map[string]any, inputs map[string]any) map[string]any {
	rawSchema, ok := frontmatter["import-schema"]
	if !ok {
		return inputs
	}
	schemaMap, ok := rawSchema.(map[string]any)
	if !ok || len(schemaMap) == 0 {
		return inputs
	}

	// Check if there are any defaults to apply - avoid copying if not needed.
	hasDefaults := false
	for paramName, paramDefRaw := range schemaMap {
		if _, provided := inputs[paramName]; provided {
			continue
		}
		if paramDef, ok := paramDefRaw.(map[string]any); ok {
			if _, hasDefault := paramDef["default"]; hasDefault {
				hasDefaults = true
				break
			}
		}
	}
	if !hasDefaults {
		return inputs
	}

	importLog.Printf("Applying import-schema defaults for unprovided inputs: schemaFields=%d", len(schemaMap))

	// Copy the inputs map and add defaults for unprovided parameters.
	augmented := make(map[string]any, len(inputs))
	maps.Copy(augmented, inputs)
	for paramName, paramDefRaw := range schemaMap {
		if _, provided := augmented[paramName]; provided {
			continue
		}
		paramDef, ok := paramDefRaw.(map[string]any)
		if !ok {
			continue
		}
		if defaultVal, hasDefault := paramDef["default"]; hasDefault {
			augmented[paramName] = defaultVal
		}
	}
	return augmented
}
