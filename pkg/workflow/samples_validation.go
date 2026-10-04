package workflow

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/syncutil"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

var samplesValidationLog = logger.New("workflow:samples_validation")

var sampleRuntimeExpressionPattern = regexp.MustCompile(`(?s)\$\{\{.*?\}\}`)

// sampleRuntimeExpressionPlaceholder is the sentinel substituted for any
// sample value that contains a `${{ ... }}` GitHub Actions expression, used
// for compile-time schema validation only. It is chosen to satisfy every
// pattern currently declared in pkg/workflow/js/safe_outputs_tools.json that
// accepts an `aw_`-prefixed temporary id (3-12 chars after the prefix).
const sampleRuntimeExpressionPlaceholder = "aw_sample"

// sampleSidecarFields lists fields recognized inside a `samples` entry
// that are NOT passed to the MCP tool's `tools/call` arguments. They are stripped
// from the sample before schema validation and consumed by the replay driver
// (e.g. to pre-stage a branch + patch on disk).
var sampleSidecarFields = map[string]map[string]struct{}{
	"create_pull_request": {
		"patch": {},
	},
	"push_to_pull_request_branch": {
		"patch": {},
	},
}

// sampleValidationDeferredTools are dynamic safe-output tool families whose
// concrete schemas are assembled at runtime from workflow configuration.
// Keep this list in sync with the dynamic handler entries in
// safe_output_handlers.go and the tool names exposed via safeOutputFieldMapping.
// Compile-time sample validation defers these to apply_samples.cjs +
// safe_outputs_mcp_server.cjs.
var sampleValidationDeferredTools = map[string]bool{
	"dispatch_workflow":   true,
	"call_workflow":       true,
	"dispatch_repository": true,
}

// toolSchemaEntry pairs a compiled jsonschema.Schema with the raw parsed
// document used to drive schema-aware runtime-expression substitution.
type toolSchemaEntry struct {
	raw      map[string]any
	compiled *jsonschema.Schema
}

// compiledToolSchemas caches the per-tool jsonschema.Schema parsed from the
// embedded safe_outputs_tools.json. Compiled lazily on first use.
var (
	compiledToolSchemasLoader      syncutil.OnceLoader[map[string]toolSchemaEntry]
	sortedSafeOutputFieldNamesOnce sync.Once
	sortedSafeOutputFieldNames     []string
)

func getCompiledToolSchemas() (map[string]toolSchemaEntry, error) {
	return compiledToolSchemasLoader.Get(func() (map[string]toolSchemaEntry, error) {
		var tools []struct {
			Name        string          `json:"name"`
			InputSchema json.RawMessage `json:"inputSchema"`
		}
		if err := json.Unmarshal([]byte(safeOutputsToolsJSONContent), &tools); err != nil {
			return nil, fmt.Errorf("failed to parse safe_outputs_tools.json for samples validation: %w", err)
		}

		sharedDefs := extractSharedInputSchemaDefs(tools)
		out := make(map[string]toolSchemaEntry, len(tools))
		for _, t := range tools {
			if len(t.InputSchema) == 0 {
				continue
			}
			var rawMap map[string]any
			if err := json.Unmarshal(t.InputSchema, &rawMap); err != nil {
				return nil, fmt.Errorf("failed to parse inputSchema for tool %q: %w", t.Name, err)
			}
			normalizeInputSchemaRefs(rawMap, sharedDefs)
			normalizedSchemaBytes, err := json.Marshal(rawMap)
			if err != nil {
				return nil, fmt.Errorf("failed to normalize inputSchema for tool %q: %w", t.Name, err)
			}

			schemaURL := fmt.Sprintf("inmem://safe-outputs-tools/%s.json", t.Name)
			schema, err := compileSchema(string(normalizedSchemaBytes), schemaURL)
			if err != nil {
				return nil, fmt.Errorf("failed to compile inputSchema for tool %q: %w", t.Name, err)
			}
			out[t.Name] = toolSchemaEntry{raw: rawMap, compiled: schema}
		}
		samplesValidationLog.Printf("Compiled %d safe-outputs tool schemas for sample validation", len(out))
		return out, nil
	})
}

func extractSharedInputSchemaDefs(tools []struct {
	Name        string          `json:"name"`
	InputSchema json.RawMessage `json:"inputSchema"`
}) map[string]any {
	if len(tools) == 0 || len(tools[0].InputSchema) == 0 {
		return nil
	}

	var firstSchema map[string]any
	if err := json.Unmarshal(tools[0].InputSchema, &firstSchema); err != nil {
		return nil
	}
	defs, ok := firstSchema["$defs"].(map[string]any)
	if !ok || len(defs) == 0 {
		return nil
	}
	return defs
}

func normalizeInputSchemaRefs(schema map[string]any, sharedDefs map[string]any) {
	if schema == nil {
		return
	}

	rewroteRefs := rewriteSchemaRefPaths(schema)
	if !rewroteRefs || len(sharedDefs) == 0 {
		return
	}

	localDefs, _ := schema["$defs"].(map[string]any)
	if localDefs == nil {
		localDefs = map[string]any{}
		schema["$defs"] = localDefs
	}
	for name, def := range sharedDefs {
		if _, exists := localDefs[name]; !exists {
			localDefs[name] = def
		}
	}
}

func rewriteSchemaRefPaths(node any) bool {
	rewrote := false
	switch typed := node.(type) {
	case map[string]any:
		if ref, ok := typed["$ref"].(string); ok && strings.HasPrefix(ref, "#/0/inputSchema/$defs/") {
			typed["$ref"] = "#/$defs/" + strings.TrimPrefix(ref, "#/0/inputSchema/$defs/")
			rewrote = true
		}
		for _, value := range typed {
			if rewriteSchemaRefPaths(value) {
				rewrote = true
			}
		}
	case []any:
		for _, item := range typed {
			if rewriteSchemaRefPaths(item) {
				rewrote = true
			}
		}
	}
	return rewrote
}

func getSortedSafeOutputFieldNames() []string {
	sortedSafeOutputFieldNamesOnce.Do(func() {
		sortedSafeOutputFieldNames = sliceutil.SortedKeys(safeOutputFieldMapping) //nolint:packagelevelmutableslicemap // sync.Once guards initialization.
	})
	return sortedSafeOutputFieldNames
}

// validateSafeOutputsSamples validates every `samples` entry on every
// enabled safe-output handler against the corresponding MCP tool's inputSchema.
// Sample sidecar fields (e.g. `patch`) are stripped before validation. Returns
// the first error encountered; iteration order is deterministic (sorted by
// struct field name) so error messages are stable.
func validateSafeOutputsSamples(config *SafeOutputsConfig) error {
	if config == nil {
		return nil
	}

	fieldNames := getSortedSafeOutputFieldNames()
	samplesValidationLog.Printf("Validating safe-outputs samples across %d candidate fields", len(fieldNames))

	for _, fieldName := range fieldNames {
		toolName := safeOutputFieldMapping[fieldName]
		base := extractBaseSafeOutputConfig(config, fieldName)
		if base == nil || len(base.Samples) == 0 {
			continue
		}
		samplesValidationLog.Printf("Validating %d sample(s) for field %q (tool %q)", len(base.Samples), fieldName, toolName)
		if err := validateSamplesForTool(toolName, base.Samples); err != nil {
			return err
		}
	}
	return nil
}

// extractBaseSafeOutputConfig returns the embedded BaseSafeOutputConfig of the
// non-nil safe-output config at SafeOutputsConfig.<fieldName>, or nil if the
// field is unset or the struct does not embed BaseSafeOutputConfig.
func extractBaseSafeOutputConfig(config *SafeOutputsConfig, fieldName string) *BaseSafeOutputConfig {
	field, ok := safeOutputPointerFieldValue(config, fieldName)
	if !ok || field.IsNil() {
		return nil
	}
	elem := field.Elem()
	if elem.Kind() != reflect.Struct {
		return nil
	}
	baseField := elem.FieldByName("BaseSafeOutputConfig")
	if !baseField.IsValid() || !baseField.CanAddr() {
		return nil
	}
	if base, ok := baseField.Addr().Interface().(*BaseSafeOutputConfig); ok {
		return base
	}
	return nil
}

// validateSamplesForTool validates each sample against the named MCP tool's
// inputSchema after stripping recognized sidecar fields.
func validateSamplesForTool(toolName string, samples []map[string]any) error {
	schemas, err := getCompiledToolSchemas()
	if err != nil {
		return err
	}
	entry, found := schemas[toolName]
	if !found {
		if sampleValidationDeferredTools[toolName] {
			samplesValidationLog.Printf("Deferring sample validation for dynamic tool %q to runtime", toolName)
			return nil
		}
		return fmt.Errorf("samples: no MCP tool schema found for %q (yaml key %q). Available tools come from pkg/workflow/js/safe_outputs_tools.json", toolName, toolDisplayKey(toolName))
	}
	displayKey := toolDisplayKey(toolName)
	sidecars := sampleSidecarFields[toolName]
	for i, sample := range samples {
		stripped := stripSidecarFields(sample, sidecars)
		substituted, ok := substituteRuntimeExpressionsForValidation(stripped, entry.raw).(map[string]any)
		if !ok {
			substituted = stripped
		}
		if err := entry.compiled.Validate(substituted); err != nil {
			return fmt.Errorf("safe-outputs.%s.samples[%d]: %w", displayKey, i, err)
		}
	}
	return nil
}

// substituteRuntimeExpressionsForValidation returns a deep copy of v in which
// every string value containing a `${{ ... }}` GitHub Actions expression has
// been replaced by a placeholder chosen to satisfy the corresponding schema
// node (first enum value, a numeric/boolean default, a date stub, or the
// generic sampleRuntimeExpressionPlaceholder). The schema argument may be nil
// when no schema is known for the current position, in which case the generic
// placeholder is used. The original sample is left unchanged and is what gets
// emitted into the lock file, so the real expression is preserved for GitHub
// Actions to substitute at runtime.
func substituteRuntimeExpressionsForValidation(v any, schema map[string]any) any {
	switch val := v.(type) {
	case string:
		if sampleRuntimeExpressionPattern.MatchString(val) {
			return placeholderForSchema(schema)
		}
		return val
	case map[string]any:
		var props map[string]any
		if schema != nil {
			props, _ = schema["properties"].(map[string]any)
		}
		out := make(map[string]any, len(val))
		for k, vv := range val {
			var propSchema map[string]any
			if props != nil {
				propSchema, _ = props[k].(map[string]any)
			}
			out[k] = substituteRuntimeExpressionsForValidation(vv, propSchema)
		}
		return out
	case []any:
		var itemSchema map[string]any
		if schema != nil {
			itemSchema, _ = schema["items"].(map[string]any)
		}
		out := make([]any, len(val))
		for i, vv := range val {
			out[i] = substituteRuntimeExpressionsForValidation(vv, itemSchema)
		}
		return out
	default:
		return v
	}
}

// placeholderForSchema returns a value that satisfies common JSON-schema
// constraints on the given schema node. It is best-effort: enum and the
// listed types/formats are honoured; anything else falls back to the generic
// string placeholder, which matches the only repository-wide string patterns
// currently in safe_outputs_tools.json (the `aw_*` temporary-id patterns).
func placeholderForSchema(schema map[string]any) any {
	if schema == nil {
		return sampleRuntimeExpressionPlaceholder
	}
	if enumVals, ok := schema["enum"].([]any); ok && len(enumVals) > 0 {
		return enumVals[0]
	}
	switch t := schema["type"].(type) {
	case string:
		return placeholderForType(t, schema)
	case []any:
		// Type union; prefer string for the most flexible substitution.
		for _, tv := range t {
			if ts, ok := tv.(string); ok && ts == "string" {
				return placeholderForType("string", schema)
			}
		}
		for _, tv := range t {
			if ts, ok := tv.(string); ok {
				if p := placeholderForType(ts, schema); p != nil {
					return p
				}
			}
		}
	}
	return sampleRuntimeExpressionPlaceholder
}

func placeholderForType(t string, schema map[string]any) any {
	switch t {
	case "number", "integer":
		return float64(1)
	case "boolean":
		return true
	case "string":
		if pattern, ok := schema["pattern"].(string); ok {
			switch pattern {
			case `^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$`:
				return "octo-org/octo-repo"
			case `^\\d{4}-\\d{2}-\\d{2}$`:
				return "2024-01-01"
			case `^https://github\\.com/(orgs|users)/[^/]+/projects/\\d+$`:
				return "https://github.com/orgs/example/projects/1"
			}
			if strings.Contains(pattern, "aw_") {
				return sampleRuntimeExpressionPlaceholder
			}
		}
		if format, ok := schema["format"].(string); ok {
			switch format {
			case "date":
				return "2024-01-01"
			case "date-time":
				return "2024-01-01T00:00:00Z"
			case "uri", "url":
				return "https://example.com"
			}
		}
		return stringPlaceholderForSchema(schema)
	case "array":
		return []any{}
	case "object":
		props, _ := schema["properties"].(map[string]any)
		required, _ := schema["required"].([]any)
		out := make(map[string]any, len(required))
		for _, rv := range required {
			k, ok := rv.(string)
			if !ok || k == "" {
				continue
			}
			var propSchema map[string]any
			if props != nil {
				propSchema, _ = props[k].(map[string]any)
			}
			out[k] = placeholderForSchema(propSchema)
		}
		return out
	case "null":
		return nil
	}
	return nil
}

func stringPlaceholderForSchema(schema map[string]any) string {
	placeholder := sampleRuntimeExpressionPlaceholder
	minLength, hasMinLength := schemaNumberAsInt(schema, "minLength")
	if !hasMinLength || minLength <= len(placeholder) {
		return placeholder
	}
	return placeholder + strings.Repeat("x", minLength-len(placeholder))
}

func schemaNumberAsInt(schema map[string]any, key string) (int, bool) {
	if schema == nil {
		return 0, false
	}
	raw, ok := schema[key].(float64)
	if !ok || raw < 0 {
		return 0, false
	}
	return int(raw), true
}

// stripSidecarFields returns a shallow copy of sample with sidecar keys removed.
// The original map is never modified, even when no sidecars are configured —
// callers may mutate the returned map without affecting the caller's input.
func stripSidecarFields(sample map[string]any, sidecars map[string]struct{}) map[string]any {
	out := make(map[string]any, len(sample))
	for k, v := range sample {
		if _, isSidecar := sidecars[k]; isSidecar {
			continue
		}
		out[k] = v
	}
	return out
}

// toolDisplayKey converts a snake_case MCP tool name into the hyphenated YAML
// frontmatter key (e.g. "create_pull_request" -> "create-pull-request").
func toolDisplayKey(toolName string) string {
	return strings.ReplaceAll(toolName, "_", "-")
}
