// This file provides GitHub Actions schema validation for compiled workflows.
//
// # GitHub Actions Schema Validation
//
// This file validates that compiled workflow YAML conforms to the official
// GitHub Actions workflow schema. It uses JSON Schema validation with caching
// to avoid recompiling the schema on every validation.
//
// # Validation Functions
//
//   - validateGitHubActionsSchema() - Validates YAML against GitHub Actions schema
//   - getCompiledSchema() - Returns cached compiled schema (compiled once)
//
// # Validation Pattern: Schema Validation with Caching
//
// Schema validation uses a singleton pattern for efficiency:
//   - sync.Once ensures schema is compiled only once
//   - Schema is embedded in the binary as githubWorkflowSchema
//   - Cached compiled schema is reused across all validations
//   - YAML is parsed directly and validated without JSON conversion
//
// # Schema Source
//
// The GitHub Actions workflow schema is embedded from:
//
//	https://json.schemastore.org/github-workflow.json
//
// # When to Add Validation Here
//
// Add validation to this file when:
//   - It validates against JSON schemas
//   - It checks GitHub Actions YAML structure
//   - It verifies workflow syntax correctness
//   - It requires schema compilation and caching
//
// For general validation, see validation.go.
// For detailed documentation, see scratchpad/validation-architecture.md

package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/goccy/go-yaml"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

var schemaValidationLog = logger.New("workflow:schema_validation")

// Cached compiled schema to avoid recompiling on every validation
var (
	compiledSchemaOnce sync.Once
	compiledSchema     *jsonschema.Schema
	schemaCompileError error
)

// getCompiledSchema returns the compiled GitHub Actions schema, compiling it once and caching
func getCompiledSchema() (*jsonschema.Schema, error) {
	compiledSchemaOnce.Do(func() {
		schemaValidationLog.Print("Compiling GitHub Actions schema (first time)")
		// Parse the embedded schema
		var schemaDoc any
		if err := json.Unmarshal([]byte(githubWorkflowSchema), &schemaDoc); err != nil {
			schemaCompileError = fmt.Errorf("failed to parse embedded GitHub Actions schema: %w", err)
			return
		}

		// Create compiler and add the schema as a resource
		loader := jsonschema.NewCompiler()
		schemaURL := "https://json.schemastore.org/github-workflow.json"
		if err := loader.AddResource(schemaURL, schemaDoc); err != nil {
			schemaCompileError = fmt.Errorf("failed to add schema resource: %w", err)
			return
		}

		// Compile the schema once
		schema, err := loader.Compile(schemaURL)
		if err != nil {
			schemaCompileError = fmt.Errorf("failed to compile GitHub Actions schema: %w", err)
			return
		}

		compiledSchema = schema
		schemaValidationLog.Print("GitHub Actions schema compiled successfully")
	})

	return compiledSchema, schemaCompileError
}

// validateGitHubActionsSchema validates the generated YAML content against the GitHub Actions workflow schema
func (c *Compiler) validateGitHubActionsSchema(yamlContent string) error {
	schemaValidationLog.Print("Validating workflow YAML against GitHub Actions schema")

	// Parse YAML directly into any type for schema validation
	// The jsonschema library accepts any type directly, no JSON conversion needed
	var workflowData any
	if err := yaml.Unmarshal([]byte(yamlContent), &workflowData); err != nil {
		return fmt.Errorf("failed to parse YAML for schema validation: %w", err)
	}

	// Get the cached compiled schema
	schema, err := getCompiledSchema()
	if err != nil {
		return err
	}

	// Validate the parsed YAML data directly against the schema
	// No JSON roundtrip required - the schema.Validate() accepts any type
	if err := schema.Validate(workflowData); err != nil {
		// Enhance error message with field-specific examples
		enhancedErr := enhanceSchemaValidationError(err)
		schemaValidationLog.Printf("Schema validation failed: %v", enhancedErr)
		return fmt.Errorf("GitHub Actions schema validation failed: %w", enhancedErr)
	}

	schemaValidationLog.Print("Schema validation passed successfully")
	return nil
}

// enhanceSchemaValidationError adds inline examples to schema validation errors
func enhanceSchemaValidationError(err error) error {
	ve, ok := err.(*jsonschema.ValidationError)
	if !ok {
		return err
	}

	// Extract field path from InstanceLocation
	fieldPath := extractFieldPath(ve.InstanceLocation)
	if fieldPath == "" {
		return err // Cannot enhance, return original error
	}

	// Get field-specific example
	example := getFieldExample(fieldPath, ve)
	if example == "" {
		return err // No example available, return original error
	}

	// Return enhanced error with example
	return fmt.Errorf("%v. %s", err, example)
}

// extractFieldPath converts InstanceLocation to a readable field path
func extractFieldPath(location []string) string {
	if len(location) == 0 {
		return ""
	}

	// Join the location parts to form a path like "timeout-minutes" or "jobs/build/runs-on"
	return location[len(location)-1] // Return the last element as the field name
}

// getFieldExample returns an example for the given field based on the validation error
func getFieldExample(fieldPath string, err error) string {
	// Map of common fields to their examples
	fieldExamples := map[string]string{
		"timeout-minutes": "Example: timeout-minutes: 10",
		"engine":          "Valid engines are: copilot, claude, codex, custom. Example: engine: copilot",
		"permissions":     "Example: permissions:\\n  contents: read\\n  issues: write",
		"on":              "Example: on: push or on:\\n  issues:\\n    types: [opened]",
		"runs-on":         "Example: runs-on: ubuntu-latest",
		"concurrency":     "Example: concurrency: production or concurrency:\\n  group: ${{ github.workflow }}\\n  cancel-in-progress: true",
		"env":             "Example: env:\\n  NODE_ENV: production",
		"tools":           "Example: tools:\\n  github:\\n    allowed: [list_issues]",
		"steps":           "Example: steps:\\n  - name: Checkout\\n    uses: actions/checkout@v4",
		"jobs":            "Example: jobs:\\n  build:\\n    runs-on: ubuntu-latest\\n    steps:\\n      - run: echo 'hello'",
		"strategy":        "Example: strategy:\\n  matrix:\\n    os: [ubuntu-latest, windows-latest]",
		"container":       "Example: container: node:20 or container:\\n  image: node:20\\n  options: --user root",
		"services":        "Example: services:\\n  postgres:\\n    image: postgres:15\\n    env:\\n      POSTGRES_PASSWORD: postgres",
		"defaults":        "Example: defaults:\\n  run:\\n    shell: bash",
		"name":            "Example: name: \"Build and Test\"",
		"if":              "Example: if: github.event_name == 'push'",
		"environment":     "Example: environment: production or environment:\\n  name: production\\n  url: https://example.com",
		"outputs":         "Example: outputs:\\n  build-id: ${{ steps.build.outputs.id }}",
		"needs":           "Example: needs: build or needs: [build, test]",
		"uses":            "Example: uses: ./.github/workflows/reusable.yml",
		"with":            "Example: with:\\n  node-version: '20'",
		"secrets":         "Example: secrets:\\n  token: ${{ secrets.GITHUB_TOKEN }}",
	}

	// Check if we have a specific example for this field
	if example, ok := fieldExamples[fieldPath]; ok {
		return example
	}

	// Generic examples based on error type
	errorMsg := err.Error()
	if strings.Contains(errorMsg, "string") {
		return fmt.Sprintf("Example: %s: \"value\"", fieldPath)
	}
	if strings.Contains(errorMsg, "boolean") {
		return fmt.Sprintf("Example: %s: true", fieldPath)
	}
	if strings.Contains(errorMsg, "object") {
		return fmt.Sprintf("Example: %s:\\n  key: value", fieldPath)
	}
	if strings.Contains(errorMsg, "array") {
		return fmt.Sprintf("Example: %s: [item1, item2]", fieldPath)
	}
	if strings.Contains(errorMsg, "integer") || strings.Contains(errorMsg, "type") {
		return fmt.Sprintf("Example: %s: 10", fieldPath)
	}

	return "" // No example available
}
