package parser

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

var schemaCompilerLog = logger.New("parser:schema_compiler")

//go:embed schemas/main_workflow_schema.json
var mainWorkflowSchema string

//go:embed schemas/mcp_config_schema.json
var mcpConfigSchema string

// validateWithSchema validates frontmatter against a JSON schema
// Cached compiled schemas to avoid recompiling on every validation
var (
	mainWorkflowSchemaOnce sync.Once
	mcpConfigSchemaOnce    sync.Once

	compiledMainWorkflowSchema *jsonschema.Schema
	compiledMcpConfigSchema    *jsonschema.Schema

	mainWorkflowSchemaError error
	mcpConfigSchemaError    error
)

// getCompiledMainWorkflowSchema returns the compiled main workflow schema, compiling it once and caching
func getCompiledMainWorkflowSchema() (*jsonschema.Schema, error) {
	mainWorkflowSchemaOnce.Do(func() {
		compiledMainWorkflowSchema, mainWorkflowSchemaError = compileSchema(mainWorkflowSchema, "http://contoso.com/main-workflow-schema.json")
	})
	return compiledMainWorkflowSchema, mainWorkflowSchemaError
}

// getCompiledMcpConfigSchema returns the compiled MCP config schema, compiling it once and caching
func getCompiledMcpConfigSchema() (*jsonschema.Schema, error) {
	mcpConfigSchemaOnce.Do(func() {
		compiledMcpConfigSchema, mcpConfigSchemaError = compileSchema(mcpConfigSchema, "http://contoso.com/mcp-config-schema.json")
	})
	return compiledMcpConfigSchema, mcpConfigSchemaError
}

// compileSchema compiles a JSON schema from a JSON string
func compileSchema(schemaJSON, schemaURL string) (*jsonschema.Schema, error) {
	schemaCompilerLog.Printf("Compiling JSON schema: %s", schemaURL)

	// Create a new compiler
	compiler := jsonschema.NewCompiler()

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

// safeOutputMetaFields are the meta-configuration fields in safe-outputs that are NOT actual safe output types.
// These are used for configuration, not for defining safe output operations.
var safeOutputMetaFields = map[string]bool{
	"allowed-domains": true,
	"staged":          true,
	"env":             true,
	"github-token":    true,
	"app":             true,
	"max-patch-size":  true,
	"jobs":            true,
	"runs-on":         true,
	"messages":        true,
}

// GetSafeOutputTypeKeys returns the list of safe output type keys from the embedded main workflow schema.
// These are the keys under safe-outputs that define actual safe output operations (like create-issue, add-comment, etc.)
// Meta-configuration fields (like allowed-domains, staged, env, etc.) are excluded.
func GetSafeOutputTypeKeys() ([]string, error) {
	schemaCompilerLog.Print("Extracting safe output type keys from main workflow schema")

	// Parse the embedded schema JSON
	var schemaDoc map[string]any
	if err := json.Unmarshal([]byte(mainWorkflowSchema), &schemaDoc); err != nil {
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

func validateWithSchema(frontmatter map[string]any, schemaJSON, context string) error {
	// Determine which cached schema to use based on the schemaJSON
	var schema *jsonschema.Schema
	var err error

	switch schemaJSON {
	case mainWorkflowSchema:
		schema, err = getCompiledMainWorkflowSchema()
	case mcpConfigSchema:
		schema, err = getCompiledMcpConfigSchema()
	default:
		// Fallback for unknown schemas (shouldn't happen in normal operation)
		// Compile the schema on-the-fly
		schema, err = compileSchema(schemaJSON, "http://contoso.com/schema.json")
	}

	if err != nil {
		return fmt.Errorf("schema validation error for %s: %w", context, err)
	}

	// Convert frontmatter to JSON and back to normalize types for validation
	// Handle nil frontmatter as empty object to satisfy schema validation
	var frontmatterToValidate map[string]any
	if frontmatter == nil {
		frontmatterToValidate = make(map[string]any)
	} else {
		frontmatterToValidate = frontmatter
	}

	frontmatterJSON, err := json.Marshal(frontmatterToValidate)
	if err != nil {
		return fmt.Errorf("schema validation error for %s: failed to marshal frontmatter: %w", context, err)
	}

	var normalizedFrontmatter any
	if err := json.Unmarshal(frontmatterJSON, &normalizedFrontmatter); err != nil {
		return fmt.Errorf("schema validation error for %s: failed to unmarshal frontmatter: %w", context, err)
	}

	// Validate the normalized frontmatter
	if err := schema.Validate(normalizedFrontmatter); err != nil {
		return err
	}

	return nil
}

// validateWithSchemaAndLocation validates frontmatter against a JSON schema with location information
func validateWithSchemaAndLocation(frontmatter map[string]any, schemaJSON, context, filePath string) error {
	// First try the basic validation
	err := validateWithSchema(frontmatter, schemaJSON, context)
	if err == nil {
		return nil
	}

	// If there's an error, try to format it with precise location information
	errorMsg := err.Error()

	// Check if this is a jsonschema validation error before cleaning
	isJSONSchemaError := strings.Contains(errorMsg, "jsonschema validation failed")

	// Clean up the jsonschema error message to remove unhelpful prefixes
	if isJSONSchemaError {
		errorMsg = cleanJSONSchemaErrorMessage(errorMsg)
	}

	// Try to read the actual file content for better context
	var contextLines []string
	var frontmatterContent string
	var frontmatterStart = 2 // Default: frontmatter starts at line 2

	// Sanitize the path to prevent path traversal attacks
	cleanPath := filepath.Clean(filePath)

	if filePath != "" {
		if content, readErr := os.ReadFile(cleanPath); readErr == nil {
			lines := strings.Split(string(content), "\n")

			// Look for frontmatter section with improved detection
			frontmatterStartIdx, frontmatterEndIdx, actualFrontmatterContent := findFrontmatterBounds(lines)

			if frontmatterStartIdx >= 0 && frontmatterEndIdx > frontmatterStartIdx {
				frontmatterContent = actualFrontmatterContent
				frontmatterStart = frontmatterStartIdx + 2 // +2 because we skip the opening "---" and use 1-based indexing

				// Use the frontmatter section plus a bit of context as context lines
				contextStart := max(0, frontmatterStartIdx)
				contextEnd := min(len(lines), frontmatterEndIdx+1)

				for i := contextStart; i < contextEnd; i++ {
					contextLines = append(contextLines, lines[i])
				}
			}
		}
	}

	// Fallback context if we couldn't read the file
	if len(contextLines) == 0 {
		contextLines = []string{"---", "# (frontmatter validation failed)", "---"}
	}

	// Try to extract precise location information from the error
	if isJSONSchemaError {
		// Extract JSON path information from the validation error
		jsonPaths := ExtractJSONPathFromValidationError(err)

		// If we have paths and frontmatter content, try to get precise locations
		if len(jsonPaths) > 0 && frontmatterContent != "" {
			// Use the first error path for the primary error location
			primaryPath := jsonPaths[0]
			location := LocateJSONPathInYAMLWithAdditionalProperties(frontmatterContent, primaryPath.Path, primaryPath.Message)

			if location.Found {
				// Adjust line number to account for frontmatter position in file
				adjustedLine := location.Line + frontmatterStart - 1

				// Create context lines around the adjusted line number in the full file
				var adjustedContextLines []string
				if filePath != "" {
					// Use the same sanitized path
					if content, readErr := os.ReadFile(cleanPath); readErr == nil {
						allLines := strings.Split(string(content), "\n")
						// Create context around the adjusted line (±3 lines)
						// The console formatter expects context to be centered around the error line
						contextSize := 7                                     // ±3 lines around the error
						contextStart := max(0, adjustedLine-contextSize/2-1) // -1 for 0-based indexing
						contextEnd := min(len(allLines), contextStart+contextSize)

						for i := contextStart; i < contextEnd; i++ {
							adjustedContextLines = append(adjustedContextLines, allLines[i])
						}
					}
				}

				// If we couldn't create adjusted context, fall back to frontmatter context
				if len(adjustedContextLines) == 0 {
					adjustedContextLines = contextLines
				}

				// Rewrite "additional properties not allowed" errors to be more friendly
				message := rewriteAdditionalPropertiesError(primaryPath.Message)

				// Add schema-based suggestions
				suggestions := generateSchemaBasedSuggestions(schemaJSON, primaryPath.Message, primaryPath.Path)
				if suggestions != "" {
					message = message + ". " + suggestions
				}

				// Create a compiler error with precise location information
				compilerErr := console.CompilerError{
					Position: console.ErrorPosition{
						File:   filePath,
						Line:   adjustedLine,
						Column: location.Column, // Use original column, we'll extend to word in console rendering
					},
					Type:    "error",
					Message: message,
					Context: adjustedContextLines,
					// Hints removed as per requirements
				}

				// Format and return the error
				formattedErr := console.FormatError(compilerErr)
				return errors.New(formattedErr)
			}
		}

		// Rewrite "additional properties not allowed" errors to be more friendly
		message := rewriteAdditionalPropertiesError(errorMsg)

		// Add schema-based suggestions for fallback case
		suggestions := generateSchemaBasedSuggestions(schemaJSON, errorMsg, "")
		if suggestions != "" {
			message = message + ". " + suggestions
		}

		// Fallback: Create a compiler error with basic location information
		compilerErr := console.CompilerError{
			Position: console.ErrorPosition{
				File:   filePath,
				Line:   frontmatterStart,
				Column: 1, // Use column 1 for fallback, we'll extend to word in console rendering
			},
			Type:    "error",
			Message: message,
			Context: contextLines,
			// Hints removed as per requirements
		}

		// Format and return the error
		formattedErr := console.FormatError(compilerErr)
		return errors.New(formattedErr)
	}

	// Fallback to the original error if we can't format it nicely
	return err
}

// GetMainWorkflowSchema returns the embedded main workflow schema JSON
func GetMainWorkflowSchema() string {
	return mainWorkflowSchema
}
