package workflow

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var memoryValidationLog = logger.New("workflow:memory_validation_config")

const (
	defaultMemoryValidationTimeoutMinutes = 1
	maxMemoryValidationTimeoutMinutes     = 5
)

type MemoryValidationConfig struct {
	Script         string                   `yaml:"script,omitempty" json:"script,omitempty"`
	TimeoutMinutes int                      `yaml:"timeout-minutes,omitempty" json:"timeout-minutes,omitempty"`
	JSONSchemas    []MemoryJSONSchemaConfig `yaml:"json-schemas,omitempty" json:"json-schemas,omitempty"`
}

type MemoryJSONSchemaConfig struct {
	File   string         `yaml:"file,omitempty" json:"file,omitempty"`
	Format string         `yaml:"format,omitempty" json:"format,omitempty"`
	Schema map[string]any `yaml:"schema" json:"schema"`
}

func parseMemoryValidationConfig(configMap map[string]any, fieldPath string) (*MemoryValidationConfig, error) {
	raw, ok := configMap["validation"]
	if !ok {
		if script, ok := configMap["validation-script"].(string); ok {
			memoryValidationLog.Printf("Using legacy validation-script field at %s", fieldPath)
			return normalizeMemoryValidationConfig(&MemoryValidationConfig{Script: script}, fieldPath)
		}
		if script, ok := configMap["custom-validation"].(string); ok {
			memoryValidationLog.Printf("Using legacy custom-validation field at %s", fieldPath)
			return normalizeMemoryValidationConfig(&MemoryValidationConfig{Script: script}, fieldPath)
		}
		return nil, nil
	}

	switch value := raw.(type) {
	case string:
		if value == "" {
			return nil, fmt.Errorf("%s.script must not be empty", fieldPath)
		}
		return normalizeMemoryValidationConfig(&MemoryValidationConfig{Script: value}, fieldPath)
	case map[string]any:
		if _, exists := value["timeout"]; exists {
			memoryValidationLog.Printf("Rejecting deprecated timeout field at %s", fieldPath)
			return nil, fmt.Errorf("%s.timeout has been renamed to %s.timeout-minutes. Example:\n%s:\n  timeout-minutes: 1", fieldPath, fieldPath, fieldPath)
		}
		config := &MemoryValidationConfig{}
		for key := range value {
			if key != "script" && key != "timeout-minutes" && key != "json-schemas" {
				return nil, fmt.Errorf("%s has unknown property %q (only script, timeout-minutes, and json-schemas are supported)", fieldPath, key)
			}
		}
		if scriptValue, exists := value["script"]; exists {
			script, ok := scriptValue.(string)
			if !ok || script == "" {
				return nil, fmt.Errorf("%s.script must be a non-empty string", fieldPath)
			}
			config.Script = script
		}
		if timeout, exists := value["timeout-minutes"]; exists {
			parsed, err := parseMemoryValidationTimeoutMinutes(timeout, fieldPath+".timeout-minutes")
			if err != nil {
				return nil, err
			}
			config.TimeoutMinutes = parsed
		}
		if schemas, exists := value["json-schemas"]; exists {
			parsed, err := parseMemoryJSONSchemas(schemas, fieldPath+".json-schemas")
			if err != nil {
				return nil, err
			}
			config.JSONSchemas = parsed
		}
		return normalizeMemoryValidationConfig(config, fieldPath)
	default:
		return nil, fmt.Errorf("%s must be an object with script and optional timeout-minutes, or a script string. Example:\n%s:\n  script: \"throw new Error('invalid state')\"\n  timeout-minutes: 1", fieldPath, fieldPath)
	}
}

func parseMemoryValidationTimeoutMinutes(value any, fieldPath string) (int, error) {
	switch v := value.(type) {
	case int:
		return validateMemoryValidationTimeoutMinutes(v, fieldPath)
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) || v != math.Trunc(v) {
			return 0, fmt.Errorf("%s must be an integer number of minutes. Example: %s: 1", fieldPath, fieldPath)
		}
		if v < 1 || v > maxMemoryValidationTimeoutMinutes {
			return 0, fmt.Errorf("%s must be between 1 and %d minutes. Example: %s: 1", fieldPath, maxMemoryValidationTimeoutMinutes, fieldPath)
		}
		return validateMemoryValidationTimeoutMinutes(int(v), fieldPath)
	case uint64:
		if v > uint64(^uint(0)>>1) {
			return 0, fmt.Errorf("%s must be between 1 and %d minutes. Example: %s: 1", fieldPath, maxMemoryValidationTimeoutMinutes, fieldPath)
		}
		return validateMemoryValidationTimeoutMinutes(int(v), fieldPath)
	case string:
		parsed, err := strconv.Atoi(v)
		if err != nil {
			return 0, fmt.Errorf("%s must be an integer number of minutes. Example: %s: 1", fieldPath, fieldPath)
		}
		return validateMemoryValidationTimeoutMinutes(parsed, fieldPath)
	default:
		return 0, fmt.Errorf("%s must be an integer number of minutes. Example: %s: 1", fieldPath, fieldPath)
	}
}

func validateMemoryValidationTimeoutMinutes(timeout int, fieldPath string) (int, error) {
	if timeout < 1 || timeout > maxMemoryValidationTimeoutMinutes {
		return 0, fmt.Errorf("%s must be between 1 and %d minutes. Example: %s: 1", fieldPath, maxMemoryValidationTimeoutMinutes, fieldPath)
	}
	return timeout, nil
}

func normalizeMemoryValidationConfig(config *MemoryValidationConfig, fieldPath string) (*MemoryValidationConfig, error) {
	if config == nil {
		return nil, nil
	}
	if config.Script == "" && len(config.JSONSchemas) == 0 {
		return nil, fmt.Errorf("%s must configure script or a non-empty json-schemas list", fieldPath)
	}
	if config.TimeoutMinutes == 0 {
		memoryValidationLog.Printf("Applying default timeout of %d minute(s) at %s", defaultMemoryValidationTimeoutMinutes, fieldPath)
		config.TimeoutMinutes = defaultMemoryValidationTimeoutMinutes
	}
	return config, nil
}

func memoryValidationTimeoutSeconds(config *MemoryValidationConfig) int {
	return config.TimeoutMinutes * 60
}

func appendMemoryValidationEnvironment(builder *strings.Builder, config *MemoryValidationConfig) {
	if config == nil {
		return
	}
	if config.Script != "" {
		fmt.Fprintf(builder, "          VALIDATION_SCRIPT_B64: %s\n", memoryValidationScriptBase64(config)) //nolint:fprintferrorunchecked
		builder.WriteString("          VALIDATION_SCRIPT_REQUIRED: 'true'\n")
	}
	if len(config.JSONSchemas) > 0 {
		fmt.Fprintf(builder, "          MEMORY_JSON_SCHEMAS_B64: %s\n", memoryValidationSchemasBase64(config)) //nolint:fprintferrorunchecked
		builder.WriteString("          MEMORY_JSON_SCHEMAS_REQUIRED: 'true'\n")
	}
	fmt.Fprintf(builder, "          VALIDATION_TIMEOUT_SECONDS: %d\n", memoryValidationTimeoutSeconds(config)) //nolint:fprintferrorunchecked
}

func memoryValidationScriptBase64(config *MemoryValidationConfig) string {
	if config == nil || config.Script == "" {
		return ""
	}
	return base64.StdEncoding.EncodeToString([]byte(config.Script))
}

func memoryValidationSchemasBase64(config *MemoryValidationConfig) string {
	if config == nil || len(config.JSONSchemas) == 0 {
		return ""
	}
	encoded, err := json.Marshal(config.JSONSchemas)
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(encoded)
}

func parseMemoryJSONSchemas(raw any, fieldPath string) ([]MemoryJSONSchemaConfig, error) {
	entries, ok := raw.([]any)
	if !ok || len(entries) == 0 {
		return nil, fmt.Errorf("%s must be a non-empty list of file and schema declarations with optional format", fieldPath)
	}
	schemas := make([]MemoryJSONSchemaConfig, 0, len(entries))
	seenFiles := make(map[string]struct{}, len(entries))
	for index, rawEntry := range entries {
		entryPath := fmt.Sprintf("%s[%d]", fieldPath, index)
		entry, ok := rawEntry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s must be an object", entryPath)
		}
		for key := range entry {
			if key != "file" && key != "format" && key != "schema" {
				return nil, fmt.Errorf("%s has unknown property %q (only file, format, and schema are supported)", entryPath, key)
			}
		}
		var file string
		if rawFile, exists := entry["file"]; exists {
			file, ok = rawFile.(string)
			if !ok || !validMemorySchemaFilePath(file) {
				return nil, fmt.Errorf("%s.file must be a non-empty relative file path or glob without traversal", entryPath)
			}
		} else if _, exists := entry["format"]; exists {
			return nil, fmt.Errorf("%s.format cannot be set when file is omitted; the format is inferred for each JSON or JSONL file", entryPath)
		}
		if _, exists := seenFiles[file]; exists {
			return nil, fmt.Errorf("%s contains duplicate file target %q", fieldPath, file)
		}
		seenFiles[file] = struct{}{}
		var format string
		if rawFormat, exists := entry["format"]; exists {
			format, ok = rawFormat.(string)
			if !ok || (format != "json" && format != "jsonl") {
				return nil, fmt.Errorf("%s.format must be either json or jsonl", entryPath)
			}
		}
		schema, ok := entry["schema"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s.schema must be an object", entryPath)
		}
		if err := validateMemoryJSONSchema(schema, entryPath+".schema", 0); err != nil {
			return nil, err
		}
		schemas = append(schemas, MemoryJSONSchemaConfig{File: file, Format: format, Schema: schema})
	}
	return schemas, nil
}

func validMemorySchemaFilePath(file string) bool {
	if file == "" || strings.ContainsAny(file, "\\?[]") || path.IsAbs(file) ||
		memorySchemaDrivePattern.MatchString(file) || path.Clean(file) != file {
		return false
	}
	for segment := range strings.SplitSeq(file, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

var memorySchemaDrivePattern = regexp.MustCompile(`^[A-Za-z]:`)

var supportedMemorySchemaTypes = map[string]struct{}{
	"object": {}, "array": {}, "string": {}, "number": {}, "integer": {}, "boolean": {}, "null": {},
}

var supportedMemorySchemaKeywords = map[string]struct{}{
	"type": {}, "enum": {}, "required": {}, "properties": {}, "additionalProperties": {}, "items": {}, "oneOf": {}, "anyOf": {},
}

func validateMemoryJSONSchema(schema map[string]any, fieldPath string, depth int) error {
	if depth > 32 {
		return fmt.Errorf("%s exceeds the maximum schema nesting depth of 32", fieldPath)
	}
	if _, hasOneOf := schema["oneOf"]; hasOneOf && len(schema) != 1 {
		return fmt.Errorf("%s cannot combine oneOf or anyOf with sibling constraints", fieldPath)
	}
	if _, hasAnyOf := schema["anyOf"]; hasAnyOf && len(schema) != 1 {
		return fmt.Errorf("%s cannot combine oneOf or anyOf with sibling constraints", fieldPath)
	}
	for key, value := range schema {
		if _, ok := supportedMemorySchemaKeywords[key]; !ok {
			return fmt.Errorf("%s uses unsupported schema keyword %q", fieldPath, key)
		}
		if err := validateMemoryJSONSchemaKeyword(key, value, fieldPath, depth); err != nil {
			return err
		}
	}
	return nil
}

func validateMemoryJSONSchemaKeyword(key string, value any, fieldPath string, depth int) error {
	switch key {
	case "type":
		return validateMemorySchemaTypes(value, fieldPath)
	case "enum":
		return validateMemorySchemaEnum(value, fieldPath)
	case "required":
		return validateMemorySchemaRequired(value, fieldPath)
	case "additionalProperties":
		if value != false {
			return fmt.Errorf("%s supports only additionalProperties: false", fieldPath)
		}
	case "properties":
		return validateMemorySchemaProperties(value, fieldPath, depth)
	case "items":
		child, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s.items must be an object schema", fieldPath)
		}
		return validateMemoryJSONSchema(child, fieldPath+".items", depth+1)
	case "oneOf", "anyOf":
		return validateMemorySchemaAlternatives(value, key, fieldPath, depth)
	}
	return nil
}

func validateMemorySchemaTypes(value any, fieldPath string) error {
	types, ok := value.([]any)
	if !ok {
		types = []any{value}
	}
	if len(types) == 0 {
		return fmt.Errorf("%s.type must be a supported type or a non-empty list of types", fieldPath)
	}
	for _, rawType := range types {
		typeName, ok := rawType.(string)
		if !ok {
			return fmt.Errorf("%s.type must contain only supported type names", fieldPath)
		}
		if _, ok := supportedMemorySchemaTypes[typeName]; !ok {
			return fmt.Errorf("%s.type contains unsupported type %q", fieldPath, typeName)
		}
	}
	return nil
}

func validateMemorySchemaEnum(value any, fieldPath string) error {
	values, ok := value.([]any)
	if !ok || len(values) == 0 {
		return fmt.Errorf("%s.enum must be a non-empty list of primitive JSON values", fieldPath)
	}
	for _, item := range values {
		switch typed := item.(type) {
		case nil, string, bool:
		case int:
			if int64(typed) > 1<<53-1 || int64(typed) < -(1<<53-1) {
				return fmt.Errorf("%s.enum integer values must be exactly representable", fieldPath)
			}
		case int64:
			if typed > 1<<53-1 || typed < -(1<<53-1) {
				return fmt.Errorf("%s.enum integer values must be exactly representable", fieldPath)
			}
		case uint:
			if uint64(typed) > 1<<53-1 {
				return fmt.Errorf("%s.enum integer values must be exactly representable", fieldPath)
			}
		case uint64:
			if typed > 1<<53-1 {
				return fmt.Errorf("%s.enum integer values must be exactly representable", fieldPath)
			}
		case float64:
			if math.IsNaN(typed) || math.IsInf(typed, 0) {
				return fmt.Errorf("%s.enum values must be finite JSON primitives", fieldPath)
			}
			if math.Trunc(typed) == typed && math.Abs(typed) > 1<<53-1 {
				return fmt.Errorf("%s.enum integer values must be exactly representable", fieldPath)
			}
		default:
			return fmt.Errorf("%s.enum must contain only primitive JSON values", fieldPath)
		}
	}
	return nil
}

func validateMemorySchemaRequired(value any, fieldPath string) error {
	fields, ok := value.([]any)
	if !ok {
		return fmt.Errorf("%s.required must be a list of strings", fieldPath)
	}
	for _, field := range fields {
		if _, ok := field.(string); !ok {
			return fmt.Errorf("%s.required must contain only strings", fieldPath)
		}
	}
	return nil
}

func validateMemorySchemaProperties(value any, fieldPath string, depth int) error {
	properties, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("%s.properties must be an object", fieldPath)
	}
	for name, child := range properties {
		childSchema, ok := child.(map[string]any)
		if !ok {
			return fmt.Errorf("%s.properties.%s must be an object schema", fieldPath, name)
		}
		if err := validateMemoryJSONSchema(childSchema, fieldPath+".properties."+name, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func validateMemorySchemaAlternatives(value any, key, fieldPath string, depth int) error {
	options, ok := value.([]any)
	if !ok || len(options) == 0 {
		return fmt.Errorf("%s.%s must be a non-empty list of schemas", fieldPath, key)
	}
	for index, child := range options {
		childSchema, ok := child.(map[string]any)
		if !ok {
			return fmt.Errorf("%s.%s[%d] must be an object schema", fieldPath, key, index)
		}
		if err := validateMemoryJSONSchema(childSchema, fmt.Sprintf("%s.%s[%d]", fieldPath, key, index), depth+1); err != nil {
			return err
		}
	}
	return nil
}

func validateMemorySchemaTargets(config *MemoryValidationConfig, allowedExtensions, fileGlobs []string, fieldPath string) error {
	if config == nil {
		return nil
	}
	for _, declaration := range config.JSONSchemas {
		if declaration.File == "" {
			continue
		}
		if len(allowedExtensions) > 0 {
			allowed := false
			for _, extension := range allowedExtensions {
				allowed = allowed || strings.EqualFold(path.Ext(declaration.File), extension)
			}
			if !allowed {
				return fmt.Errorf("%s.json-schemas target %q is excluded by allowed-extensions", fieldPath, declaration.File)
			}
		}
		if len(fileGlobs) > 0 {
			matched := false
			for _, glob := range fileGlobs {
				for pattern := range strings.FieldsSeq(glob) {
					matched = matched || memorySchemaFileMatchesGlob(declaration.File, pattern)
				}
			}
			if !matched {
				return fmt.Errorf("%s.json-schemas target %q is excluded by file-glob", fieldPath, declaration.File)
			}
		}
	}
	return nil
}

func memorySchemaFileMatchesGlob(file, glob string) bool {
	pattern := regexp.QuoteMeta(glob)
	pattern = strings.ReplaceAll(pattern, `\*\*/`, `(?:.*/)?`)
	pattern = strings.ReplaceAll(pattern, `\*\*`, `.*`)
	pattern = strings.ReplaceAll(pattern, `\*`, `[^/]*`)
	matched, err := regexp.MatchString("^"+pattern+"$", file)
	return err == nil && matched
}

func memoryValidationStepID(prefix, memoryID string) string {
	return fmt.Sprintf("%s_%x", prefix, memoryID)
}
