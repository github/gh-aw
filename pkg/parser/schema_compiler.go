package parser

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/syncutil"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

var schemaCompilerLog = logger.New("parser:schema_compiler")

//go:embed schemas/main_workflow_schema.json
var mainWorkflowSchema string

//go:embed schemas/mcp_config_schema.json
var mcpConfigSchema string

//go:embed schemas/repo_config_schema.json
var RepoConfigSchema string

//go:embed schemas/aw_manifest_schema.json
var awManifestSchema string

// validateWithSchema validates frontmatter against a JSON schema
// Cached compiled schemas to avoid recompiling on every validation
var (
	mainWorkflowSchemaLoader syncutil.OnceLoader[*jsonschema.Schema]
	mcpConfigSchemaLoader    syncutil.OnceLoader[*jsonschema.Schema]
	repoConfigSchemaLoader   syncutil.OnceLoader[*jsonschema.Schema]
	awManifestSchemaLoader   syncutil.OnceLoader[*jsonschema.Schema]

	// Cached parsed schema documents (as any) for suggestion generation.
	// Parsing the large JSON schema on every error call is expensive; these caches
	// ensure the schema is parsed at most once per process lifetime.
	parsedMainWorkflowSchemaDocLoader syncutil.OnceLoader[map[string]any]
	parsedMcpConfigSchemaDocLoader    syncutil.OnceLoader[map[string]any]
)

// getCompiledMainWorkflowSchema returns the compiled main workflow schema, compiling it once and caching
func getCompiledMainWorkflowSchema() (*jsonschema.Schema, error) {
	return mainWorkflowSchemaLoader.Get(func() (*jsonschema.Schema, error) {
		return compileSchema(mainWorkflowSchema, "http://contoso.com/main-workflow-schema.json")
	})
}

// getCompiledMcpConfigSchema returns the compiled MCP config schema, compiling it once and caching
func getCompiledMcpConfigSchema() (*jsonschema.Schema, error) {
	return mcpConfigSchemaLoader.Get(func() (*jsonschema.Schema, error) {
		return compileSchema(mcpConfigSchema, "http://contoso.com/mcp-config-schema.json")
	})
}

// GetCompiledRepoConfigSchema returns the compiled repo config schema, compiling it once and caching
func GetCompiledRepoConfigSchema() (*jsonschema.Schema, error) {
	return repoConfigSchemaLoader.Get(func() (*jsonschema.Schema, error) {
		return compileSchema(RepoConfigSchema, "http://contoso.com/repo-config-schema.json")
	})
}

// getCompiledAwManifestSchema returns the compiled aw manifest schema, compiling it once and caching.
func getCompiledAwManifestSchema() (*jsonschema.Schema, error) {
	return awManifestSchemaLoader.Get(func() (*jsonschema.Schema, error) {
		return compileSchema(awManifestSchema, "http://contoso.com/aw-manifest-schema.json")
	})
}

// getParsedSchemaDoc returns the parsed object representation of a known schema JSON string.
// For the two well-known schemas (mainWorkflowSchema, mcpConfigSchema) the result is cached
// so the expensive json.Unmarshal is only ever performed once per process lifetime.
// Unknown schema strings fall back to an uncached parse.
func getParsedSchemaDoc(schemaJSON string) (map[string]any, error) {
	switch schemaJSON {
	case mainWorkflowSchema:
		return parsedMainWorkflowSchemaDocLoader.Get(func() (map[string]any, error) {
			return unmarshalSchemaDoc(mainWorkflowSchema)
		})
	case mcpConfigSchema:
		return parsedMcpConfigSchemaDocLoader.Get(func() (map[string]any, error) {
			return unmarshalSchemaDoc(mcpConfigSchema)
		})
	default:
		return unmarshalSchemaDoc(schemaJSON)
	}
}

// unmarshalSchemaDoc parses a JSON schema string into an object document.
func unmarshalSchemaDoc(schemaJSON string) (map[string]any, error) {
	var doc map[string]any
	if err := json.Unmarshal([]byte(schemaJSON), &doc); err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, errors.New("schema root is not an object")
	}
	return doc, nil
}

// CompileSchema compiles a JSON schema from a JSON string.
func CompileSchema(schemaJSON, schemaURL string) (*jsonschema.Schema, error) {
	schemaCompilerLog.Printf("Compiling JSON schema: %s", schemaURL)

	// Create a new compiler
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()

	// Parse the schema JSON first
	var schemaDoc any
	if err := json.Unmarshal([]byte(schemaJSON), &schemaDoc); err != nil {
		return nil, fmt.Errorf("failed to parse schema JSON: %w", err)
	}

	// Add the schema as a resource
	if err := compiler.AddResource(schemaURL, schemaDoc); err != nil {
		return nil, fmt.Errorf("failed to add schema resource: %w", err)
	}

	// Compile the schema
	schema, err := compiler.Compile(schemaURL)
	if err != nil {
		return nil, fmt.Errorf("failed to compile schema: %w", err)
	}

	return schema, nil
}

// compileSchema preserves existing package-local call sites.
func compileSchema(schemaJSON, schemaURL string) (*jsonschema.Schema, error) {
	return CompileSchema(schemaJSON, schemaURL)
}

// safeOutputMetaFields are the meta-configuration fields in safe-outputs that are NOT actual safe output types.
// These are used for configuration, not for defining safe output operations.
var safeOutputMetaFields = map[string]bool{
	"allowed-domains": true,
	"body-footer":     true,
	"data":            true,
	"footer":          true,
	"staged":          true,
	"env":             true,
	"github-token":    true,
	"github-app":      true,
	"max-patch-size":  true,
	"jobs":            true,
	"runs-on":         true,
	"messages":        true,
	"needs":           true,
	"timeout-minutes": true,
}

// GetSafeOutputTypeKeys returns the list of safe output type keys from the embedded main workflow schema.
// These are the keys under safe-outputs that define actual safe output operations (like create-issue, add-comment, etc.)
// Meta-configuration fields (like allowed-domains, staged, env, etc.) are excluded.
func GetSafeOutputTypeKeys() ([]string, error) {
	schemaCompilerLog.Print("Extracting safe output type keys from main workflow schema")

	// Use the cached parsed schema document to avoid re-parsing on every call.
	schemaDoc, err := getParsedSchemaDoc(mainWorkflowSchema)
	if err != nil {
		return nil, fmt.Errorf("failed to parse main workflow schema: %w", err)
	}

	// Navigate to properties.safe-outputs.properties
	properties, ok := schemaDoc["properties"].(map[string]any)
	if !ok {
		return nil, errors.New("schema missing 'properties' field")
	}

	safeOutputs, ok := properties["safe-outputs"].(map[string]any)
	if !ok {
		return nil, errors.New("schema missing 'properties.safe-outputs' field")
	}

	safeOutputsProperties, ok := safeOutputs["properties"].(map[string]any)
	if !ok {
		return nil, errors.New("schema missing 'properties.safe-outputs.properties' field")
	}

	// Extract keys that are actual safe output types (not meta-configuration)
	var keys []string
	for key := range safeOutputsProperties {
		if !safeOutputMetaFields[key] {
			keys = append(keys, key)
		}
	}

	// Sort keys for consistent ordering
	sort.Strings(keys)

	return keys, nil
}

// normalizeForJSONSchema recursively returns a normalized copy of v with YAML-native
// Go types converted to JSON-compatible types for JSON schema validation. It does
// not mutate the caller's maps or slices. goccy/go-yaml produces uint64 for
// positive integers and int64 for negative integers, but JSON schema validators
// expect float64 for all numbers (matching encoding/json's unmarshaling behavior).
// goccy/go-yaml may also produce typed slices (e.g. []string) instead of []any;
// the reflection fallback converts these so the schema validator sees []any.
// This avoids the overhead of a json.Marshal + json.Unmarshal roundtrip.
func normalizeForJSONSchema(v any) any {
	switch val := v.(type) {
	case map[string]any:
		normalized := make(map[string]any, len(val))
		for k, elem := range val {
			normalized[k] = normalizeForJSONSchema(elem)
		}
		return normalized
	case []any:
		normalized := make([]any, len(val))
		for i, elem := range val {
			normalized[i] = normalizeForJSONSchema(elem)
		}
		return normalized
	case int:
		return float64(val)
	case int8:
		return float64(val)
	case int16:
		return float64(val)
	case int32:
		return float64(val)
	case int64:
		return float64(val)
	case uint:
		return float64(val)
	case uint8:
		return float64(val)
	case uint16:
		return float64(val)
	case uint32:
		return float64(val)
	case uint64:
		return float64(val)
	case float32:
		return float64(val)
	default:
		// Use reflection to handle typed slices (e.g. []string) and typed maps
		// that goccy/go-yaml may produce instead of []any / map[string]any.
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.Slice:
			normalized := make([]any, rv.Len())
			for i := range rv.Len() {
				normalized[i] = normalizeForJSONSchema(rv.Index(i).Interface())
			}
			return normalized
		case reflect.Map:
			normalized := make(map[string]any, rv.Len())
			for _, key := range rv.MapKeys() {
				normalized[key.String()] = normalizeForJSONSchema(rv.MapIndex(key).Interface())
			}
			return normalized
		}
		// string, bool, float64, nil pass through unchanged
		return v
	}
}

func validateWithSchema(frontmatter map[string]any, schemaJSON, context string) error {
	schemaCompilerLog.Printf("Validating frontmatter against schema for context: %s (%d fields)", context, len(frontmatter))

	// Determine which cached schema to use based on the schemaJSON
	var schema *jsonschema.Schema
	var err error

	switch schemaJSON {
	case mainWorkflowSchema:
		schemaCompilerLog.Print("Using cached main workflow schema")
		schema, err = getCompiledMainWorkflowSchema()
	case mcpConfigSchema:
		schemaCompilerLog.Print("Using cached MCP config schema")
		schema, err = getCompiledMcpConfigSchema()
	case RepoConfigSchema:
		schemaCompilerLog.Print("Using cached repo config schema")
		schema, err = GetCompiledRepoConfigSchema()
	case awManifestSchema:
		schemaCompilerLog.Print("Using cached aw manifest schema")
		schema, err = getCompiledAwManifestSchema()
	default:
		// Fallback for unknown schemas (shouldn't happen in normal operation)
		// Compile the schema on-the-fly
		schemaCompilerLog.Print("Compiling unknown schema on-the-fly")
		schema, err = compileSchema(schemaJSON, "http://contoso.com/schema.json")
	}

	if err != nil {
		return fmt.Errorf("schema validation error for %s: %w", context, err)
	}

	// Normalize YAML-native Go types to JSON-compatible types for schema validation.
	// goccy/go-yaml produces uint64/int64 for integers, but JSON schema validators
	// expect float64 for all numbers (matching encoding/json's behavior).
	// This avoids the overhead of a json.Marshal/Unmarshal roundtrip.
	var normalized any
	if frontmatter == nil {
		normalized = make(map[string]any)
	} else {
		normalized = normalizeForJSONSchema(frontmatter)
	}

	// Validate the normalized frontmatter
	if err := schema.Validate(normalized); err != nil {
		schemaCompilerLog.Printf("Schema validation failed for %s: %v", context, err)
		return err
	}

	schemaCompilerLog.Printf("Schema validation passed for context: %s", context)
	return nil
}

// pathPositionPrefixPattern matches the "'path' (line N, col M): " prefix that
// formatSchemaFailureDetail prepends to each detail line for multi-failure output.
var pathPositionPrefixPattern = regexp.MustCompile(`^'[^']*' \(line \d+, col \d+\): `)

// stripDetailLinePrefix removes the "'path' (line N, col M): " prefix from a
// formatSchemaFailureDetail result.  This prefix is redundant for single-failure
// errors because the IDE-compatible "file:line:col: error:" header already encodes
// the position; stripping it avoids the information appearing twice to the user.
func stripDetailLinePrefix(detail string) string {
	return pathPositionPrefixPattern.ReplaceAllString(detail, "")
}

// validateWithSchemaAndLocation validates frontmatter against a JSON schema with location information
func validateWithSchemaAndLocation(frontmatter map[string]any, schemaJSON, context, filePath string) error {
	schemaCompilerLog.Printf("Validating with location info: context=%s, file=%s", context, filePath)
	err := validateWithSchema(frontmatter, schemaJSON, context)
	if err == nil {
		return nil
	}

	errorMsg := err.Error()
	isJSONSchemaError := strings.Contains(errorMsg, "jsonschema validation failed")
	if isJSONSchemaError {
		errorMsg = cleanJSONSchemaErrorMessage(errorMsg)
	}

	frontmatterCtx := readFrontmatterContext(filePath)
	if !isJSONSchemaError {
		return err
	}

	jsonPaths := ExtractJSONPathFromValidationError(err)
	schemaCompilerLog.Printf("Extracted %d JSON path(s) from validation error for %s", len(jsonPaths), context)
	if locationErr := formatJSONSchemaValidationWithLocation(jsonPaths, schemaJSON, filePath, frontmatterCtx); locationErr != nil {
		return locationErr
	}
	return buildFallbackSchemaValidationError(errorMsg, schemaJSON, filePath, frontmatterCtx)
}

type frontmatterContextData struct {
	cleanPath          string
	frontmatterStart   int
	frontmatterContent string
	contextLines       []string
	allLines           []string
}

func readFrontmatterContext(filePath string) frontmatterContextData {
	ctx := frontmatterContextData{
		cleanPath:        filepath.Clean(filePath),
		frontmatterStart: 2,
		contextLines:     []string{"---", "# (frontmatter validation failed)", "---"},
	}
	if filePath == "" {
		return ctx
	}
	content, err := os.ReadFile(ctx.cleanPath)
	if err != nil {
		return ctx
	}
	lines := strings.Split(string(content), "\n")
	ctx.allLines = lines
	startIdx, endIdx, frontmatterContent := findFrontmatterBounds(lines)
	if startIdx < 0 || endIdx <= startIdx {
		return ctx
	}
	ctx.frontmatterContent = frontmatterContent
	ctx.frontmatterStart = startIdx + 2
	ctx.contextLines = lines[max(0, startIdx):min(len(lines), endIdx+1)]
	return ctx
}

func formatJSONSchemaValidationWithLocation(
	jsonPaths []JSONPathInfo,
	schemaJSON, filePath string,
	ctx frontmatterContextData,
) error {
	if len(jsonPaths) == 0 || ctx.frontmatterContent == "" {
		return nil
	}

	detailLines := make([]string, 0, len(jsonPaths))
	for _, pathInfo := range jsonPaths {
		detailLines = append(detailLines, formatSchemaFailureDetail(pathInfo, schemaJSON, ctx.frontmatterContent, ctx.frontmatterStart))
	}

	location := LocateJSONPathForPathInfo(ctx.frontmatterContent, jsonPaths[0])
	if !location.Found {
		return nil
	}
	adjustedLine := location.Line + ctx.frontmatterStart - 1
	contextLines := buildAdjustedContextLines(ctx, adjustedLine)
	message := formatSchemaDetailMessage(detailLines)
	return formatCompilerErrorWithLocation(filePath, adjustedLine, location.Column, message, contextLines)
}

func buildAdjustedContextLines(ctx frontmatterContextData, adjustedLine int) []string {
	if len(ctx.allLines) == 0 {
		return ctx.contextLines
	}
	var adjustedContextLines []string
	contextSize := 7
	expectedFirstLine := adjustedLine - contextSize/2
	fileStart := max(0, expectedFirstLine-1)
	for lineNum := expectedFirstLine; lineNum < 1; lineNum++ {
		adjustedContextLines = append(adjustedContextLines, "")
	}
	fileEnd := min(len(ctx.allLines), fileStart+contextSize-len(adjustedContextLines))
	for i := fileStart; i < fileEnd; i++ {
		adjustedContextLines = append(adjustedContextLines, ctx.allLines[i])
	}
	if len(adjustedContextLines) == 0 {
		return ctx.contextLines
	}
	return adjustedContextLines
}

func formatSchemaDetailMessage(detailLines []string) string {
	message := stripDetailLinePrefix(detailLines[0])
	if len(detailLines) != 1 {
		message = "Multiple schema validation failures:\n- " + strings.Join(detailLines, "\n- ")
	}
	return message
}

func formatCompilerErrorWithLocation(filePath string, line, column int, message string, contextLines []string) error {
	compilerErr := console.CompilerError{
		Position: console.ErrorPosition{
			File:   filePath,
			Line:   line,
			Column: column,
		},
		Type:    "error",
		Message: message,
		Context: contextLines,
	}
	formattedErr := console.FormatError(compilerErr)
	return &FormattedParserError{formatted: formattedErr}
}

func buildFallbackSchemaValidationError(errorMsg, schemaJSON, filePath string, ctx frontmatterContextData) error {
	message := rewriteAdditionalPropertiesError(errorMsg)
	suggestions := generateSchemaBasedSuggestions(schemaJSON, errorMsg, "", ctx.frontmatterContent)
	if suggestions != "" {
		message = message + ". " + suggestions
	}
	return formatCompilerErrorWithLocation(filePath, ctx.frontmatterStart, 1, message, ctx.contextLines)
}

// formatSchemaFailureDetail builds a single line of schema error detail for one JSONPathInfo.
// It is called once per failing schema constraint in validateWithSchemaAndLocation, which
// then joins them into a "Multiple schema validation failures:" message. Because
// CompilerError.Position only captures the *first* failure's location, each detail line
// independently includes its own (line N, col M) so secondary failures remain navigable.
// The old "at '/path' (line N, column M):" prefix is replaced with "'path' (line N, col M):"
// to remove the schema-jargon "at" keyword and the leading slash.
func formatSchemaFailureDetail(pathInfo JSONPathInfo, schemaJSON, frontmatterContent string, frontmatterStart int) string {
	path := pathInfo.Path
	if path == "" {
		path = "/"
	}

	location := LocateJSONPathForPathInfo(frontmatterContent, pathInfo)
	line := frontmatterStart
	column := 1
	if location.Found {
		line = location.Line + frontmatterStart - 1
		column = location.Column
	}

	message := rewriteAdditionalPropertiesError(cleanOneOfMessage(pathInfo.Message))
	// Strip any "at '/path': " prefix from the message to avoid duplication with the
	// "'path' (line N, col M):" prefix we prepend below.
	message = stripAtPathPrefix(message)
	// Translate schema constraint language (e.g. "minimum: got X, want Y") to plain English.
	message = translateSchemaConstraintMessage(message)
	// Append valid-values hint for well-known fields (e.g. permissions scopes).
	// hintAdded is true when appendKnownFieldValidValuesHint actually augmented the message
	// (i.e. the path is a known field and the error is an unknown-property error).
	// When a hint was added we skip generateSchemaBasedSuggestions to avoid repeating the
	// same valid-values or "Did you mean" content.
	message, hintAdded := appendKnownFieldValidValuesHint(message, pathInfo.Path)
	if !hintAdded {
		suggestions := generateSchemaBasedSuggestions(schemaJSON, pathInfo.Message, pathInfo.Path, frontmatterContent)
		if suggestions != "" {
			message = message + ". " + suggestions
		}
	}
	displayPath := strings.TrimPrefix(path, "/")
	if displayPath == "" {
		return message
	}
	return fmt.Sprintf("'%s' (line %d, col %d): %s", displayPath, line, column, message)
}
