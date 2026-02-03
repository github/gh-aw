package parser

import (
	"fmt"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var schemaValidationLog = logger.New("parser:schema_validation")

// sharedWorkflowForbiddenFields is a map for O(1) lookup of forbidden fields in shared workflows
var sharedWorkflowForbiddenFields = buildForbiddenFieldsMap()

// buildForbiddenFieldsMap converts the SharedWorkflowForbiddenFields slice to a map for efficient lookup
func buildForbiddenFieldsMap() map[string]bool {
	forbiddenMap := make(map[string]bool)
	for _, field := range constants.SharedWorkflowForbiddenFields {
		forbiddenMap[field] = true
	}
	return forbiddenMap
}

// validateSharedWorkflowFields checks that a shared workflow doesn't contain forbidden fields
func validateSharedWorkflowFields(frontmatter map[string]any) error {
	var forbiddenFound []string

	for key := range frontmatter {
		if sharedWorkflowForbiddenFields[key] {
			forbiddenFound = append(forbiddenFound, key)
		}
	}

	if len(forbiddenFound) > 0 {
		if len(forbiddenFound) == 1 {
			return fmt.Errorf("field '%s' cannot be used in shared workflows (only allowed in main workflows with 'on' trigger)", forbiddenFound[0])
		}
		return fmt.Errorf("fields %v cannot be used in shared workflows (only allowed in main workflows with 'on' trigger)", forbiddenFound)
	}

	return nil
}

// ValidateMainWorkflowFrontmatterWithSchema validates main workflow frontmatter using JSON schema
//
// This function validates all frontmatter fields including pass-through fields that are
// extracted and passed directly to GitHub Actions (concurrency, container, environment, env,
// runs-on, services). The JSON schema validation catches structural errors at compile time:
//   - Invalid data types (e.g., array when object expected)
//   - Missing required properties (e.g., container missing 'image')
//   - Invalid additional properties (e.g., unknown fields)
//
// See pkg/parser/schema_passthrough_validation_test.go for comprehensive test coverage.
func ValidateMainWorkflowFrontmatterWithSchema(frontmatter map[string]any) error {
	schemaValidationLog.Print("Validating main workflow frontmatter with schema")

	// Filter out ignored fields before validation
	filtered := filterIgnoredFields(frontmatter)

	// First run custom validation for command trigger conflicts (provides better error messages)
	if err := validateCommandTriggerConflicts(filtered); err != nil {
		schemaValidationLog.Printf("Command trigger validation failed: %v", err)
		return err
	}

	// Then run the standard schema validation
	// This validates all fields including pass-through fields (concurrency, container, etc.)
	if err := validateWithSchema(filtered, mainWorkflowSchema, "main workflow file"); err != nil {
		schemaValidationLog.Printf("Schema validation failed for main workflow: %v", err)
		return err
	}

	// Finally run other custom validation rules
	return validateEngineSpecificRules(filtered)
}

// ValidateMainWorkflowFrontmatterWithSchemaAndLocation validates main workflow frontmatter with file location info
func ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter map[string]any, filePath string) error {
	// Filter out ignored fields before validation
	filtered := filterIgnoredFields(frontmatter)

	// First run custom validation for command trigger conflicts (provides better error messages)
	if err := validateCommandTriggerConflicts(filtered); err != nil {
		return err
	}

	// Then run the standard schema validation with location
	if err := validateWithSchemaAndLocation(filtered, mainWorkflowSchema, "main workflow file", filePath); err != nil {
		return err
	}

	// Finally run other custom validation rules
	return validateEngineSpecificRules(filtered)
}

// ValidateIncludedFileFrontmatterWithSchema validates included file frontmatter using JSON schema
func ValidateIncludedFileFrontmatterWithSchema(frontmatter map[string]any) error {
	schemaValidationLog.Print("Validating included file frontmatter with schema")

	// Filter out ignored fields before validation
	filtered := filterIgnoredFields(frontmatter)

	// First check for forbidden fields in shared workflows
	if err := validateSharedWorkflowFields(filtered); err != nil {
		schemaValidationLog.Printf("Shared workflow field validation failed: %v", err)
		return err
	}

	// To validate shared workflows against the main schema, we temporarily add an 'on' field
	// This allows us to use the full schema validation while still enforcing the forbidden field check above
	tempFrontmatter := make(map[string]any)
	for k, v := range filtered {
		tempFrontmatter[k] = v
	}
	// Add a temporary 'on' field to satisfy the schema's required field
	tempFrontmatter["on"] = "push"

	// Validate with the main schema (which will catch unknown fields)
	if err := validateWithSchema(tempFrontmatter, mainWorkflowSchema, "included file"); err != nil {
		schemaValidationLog.Printf("Schema validation failed for included file: %v", err)
		return err
	}

	// Run custom validation for engine-specific rules
	return validateEngineSpecificRules(filtered)
}

// ValidateIncludedFileFrontmatterWithSchemaAndLocation validates included file frontmatter with file location info
func ValidateIncludedFileFrontmatterWithSchemaAndLocation(frontmatter map[string]any, filePath string) error {
	// Filter out ignored fields before validation
	filtered := filterIgnoredFields(frontmatter)

	// First check for forbidden fields in shared workflows
	if err := validateSharedWorkflowFields(filtered); err != nil {
		return err
	}

	// To validate shared workflows against the main schema, we temporarily add an 'on' field
	tempFrontmatter := make(map[string]any)
	for k, v := range filtered {
		tempFrontmatter[k] = v
	}
	// Add a temporary 'on' field to satisfy the schema's required field
	tempFrontmatter["on"] = "push"

	// Validate with the main schema (which will catch unknown fields)
	if err := validateWithSchemaAndLocation(tempFrontmatter, mainWorkflowSchema, "included file", filePath); err != nil {
		return err
	}

	// Run custom validation for engine-specific rules
	return validateEngineSpecificRules(filtered)
}

// ValidateMCPConfigWithSchema validates MCP configuration using JSON schema
func ValidateMCPConfigWithSchema(mcpConfig map[string]any, toolName string) error {
	schemaValidationLog.Printf("Validating MCP configuration for tool: %s", toolName)
	return validateWithSchema(mcpConfig, mcpConfigSchema, fmt.Sprintf("MCP configuration for tool '%s'", toolName))
}
