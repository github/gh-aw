package parser

import (
	"errors"
	"fmt"
	"maps"
	"net/url"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
)

var schemaValidationLog = logger.New("parser:schema_validation")

// sharedWorkflowForbiddenFields is a map for O(1) lookup of forbidden fields in shared workflows
var sharedWorkflowForbiddenFields = buildForbiddenFieldsMap()

var sharedWorkflowAllowedOnFieldList = []string{
	"skip-if-match",
	"skip-if-no-match",
	"skip-roles",
	"skip-bots",
	"github-token",
	"github-app",
}

var sharedWorkflowAllowedOnFields = map[string]struct{}{
	"skip-if-match":    {},
	"skip-if-no-match": {},
	"skip-roles":       {},
	"skip-bots":        {},
	"github-token":     {},
	"github-app":       {},
}

// buildForbiddenFieldsMap converts the SharedWorkflowForbiddenFields slice to a map for efficient lookup
func buildForbiddenFieldsMap() map[string]struct {
} {
	forbiddenMap := make(map[string]struct {
	})
	for _, field := range constants.SharedWorkflowForbiddenFields {
		forbiddenMap[field] = struct {
		}{}
	}
	return forbiddenMap
}

// validateSharedWorkflowFields checks that a shared workflow doesn't contain forbidden fields
func validateSharedWorkflowFields(frontmatter map[string]any) error {
	schemaValidationLog.Printf("Checking shared workflow for forbidden fields: %d fields present", len(frontmatter))
	var forbiddenFound []string

	for key := range frontmatter {
		if key == "on" {
			if err := validateSharedWorkflowOnField(frontmatter["on"]); err != nil {
				return err
			}
			continue
		}
		if key == "concurrency" {
			if err := validateSharedWorkflowConcurrencyField(frontmatter["concurrency"]); err != nil {
				return err
			}
			continue
		}
		if key == "features" {
			if err := validateSharedWorkflowFeaturesField(frontmatter["features"]); err != nil {
				return err
			}
			continue
		}
		if setutil.Contains(sharedWorkflowForbiddenFields, key) {
			forbiddenFound = append(forbiddenFound, key)
		}
	}

	if len(forbiddenFound) > 0 {
		schemaValidationLog.Printf("Found %d forbidden field(s) in shared workflow: %v", len(forbiddenFound), forbiddenFound)
		if len(forbiddenFound) == 1 {
			return fmt.Errorf("field '%s' cannot be used in shared workflows (only allowed in main workflows with 'on' trigger)", forbiddenFound[0])
		}
		return fmt.Errorf("fields %v cannot be used in shared workflows (only allowed in main workflows with 'on' trigger)", forbiddenFound)
	}

	return nil
}

func validateSharedWorkflowConcurrencyField(concurrencyValue any) error {
	switch value := concurrencyValue.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			return errors.New("field 'concurrency' cannot be used in shared workflows (only concurrency.group and concurrency.job-discriminator are import-safe)")
		}
		return nil
	case map[string]any:
		for key := range value {
			if key != "group" && key != "job-discriminator" {
				return fmt.Errorf("field 'concurrency' in shared workflows can only include import-safe fields group and job-discriminator; found unsupported key: %s", key)
			}
		}
		return nil
	default:
		return errors.New("field 'concurrency' cannot be used in shared workflows (only concurrency.group and concurrency.job-discriminator are import-safe)")
	}
}

// sharedWorkflowAllowedFeaturesFields lists the features.* sub-fields that are safe to
// import from shared workflows. Other feature keys are configuration/experimental
// settings intended only for main workflows and are rejected.
var sharedWorkflowAllowedFeaturesFields = map[string]struct{}{
	"samples":             {},
	"intentional-failure": {},
}

// validateSharedWorkflowFeaturesField validates features: usage in shared workflows.
// Shared workflows may use features: only for import-safe feature flags.
func validateSharedWorkflowFeaturesField(featuresValue any) error {
	featuresMap, ok := featuresValue.(map[string]any)
	if !ok {
		return errors.New("field 'features' cannot be used in shared workflows (only features.samples and features.intentional-failure are import-safe)")
	}

	for key := range featuresMap {
		if _, ok := sharedWorkflowAllowedFeaturesFields[key]; !ok {
			return fmt.Errorf("field 'features' in shared workflows can only include import-safe fields samples and intentional-failure; found unsupported key: %s", key)
		}
	}

	return nil
}

// validateSharedWorkflowOnField validates on: usage in shared workflows.
// Shared workflows may use on: only for import-safe activation fields.
func validateSharedWorkflowOnField(onValue any) error {
	onMap, ok := onValue.(map[string]any)
	if !ok {
		return errors.New("field 'on' cannot be used in shared workflows (only import-safe on fields are allowed)")
	}

	var disallowed []string
	for key := range onMap {
		if _, ok := sharedWorkflowAllowedOnFields[key]; !ok {
			disallowed = append(disallowed, key)
		}
	}

	schemaValidationLog.Printf("Validating shared workflow 'on' field: %d key(s), %d disallowed", len(onMap), len(disallowed))

	if len(disallowed) > 0 {
		return fmt.Errorf(
			"field 'on' in shared workflows can only include import-safe fields (%s); found unsupported keys: %s",
			strings.Join(sharedWorkflowAllowedOnFieldList, ", "),
			strings.Join(disallowed, ", "),
		)
	}

	return nil
}

// IsImportSafeSharedWorkflowOn reports whether an on: block contains only fields
// that are safe for shared workflow imports and no trigger events.
func IsImportSafeSharedWorkflowOn(onValue any) bool {
	return validateSharedWorkflowOnField(onValue) == nil
}

// ValidateMainWorkflowFrontmatterWithSchemaAndLocation validates main workflow frontmatter with file location info.
//
// This function validates all frontmatter fields including pass-through fields that are
// extracted and passed directly to GitHub Actions (concurrency, container, environment, env,
// runs-on, services). The JSON schema validation catches structural errors at compile time:
//   - Invalid data types (e.g., array when object expected)
//   - Missing required properties (e.g., container missing 'image')
//   - Invalid additional properties (e.g., unknown fields)
//
// See pkg/parser/schema_passthrough_validation_test.go for comprehensive test coverage.
func ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter map[string]any, filePath string) error {
	schemaValidationLog.Printf("Validating main workflow frontmatter: file=%s, fields=%d", filePath, len(frontmatter))
	// Filter out ignored fields before validation
	filtered := filterIgnoredFields(frontmatter)

	// First run custom validation for command trigger conflicts (provides better error messages)
	if err := validateCommandTriggerConflicts(filtered); err != nil {
		return err
	}
	if err := validateUnsupportedJobInputs(filtered); err != nil {
		return err
	}
	if err := validateMetadataDocs(filtered); err != nil {
		return err
	}

	// Then run the standard schema validation with location
	if err := validateWithSchemaAndLocation(filtered, mainWorkflowSchema, "main workflow file", filePath); err != nil {
		return err
	}

	// Finally run other custom validation rules
	return validateEngineSpecificRules(filtered)
}

func validateMetadataDocs(frontmatter map[string]any) error {
	metadata, ok := frontmatter["metadata"].(map[string]any)
	if !ok {
		return nil
	}
	docs, ok := metadata["docs"].(string)
	if !ok {
		return nil
	}
	parsed, err := url.ParseRequestURI(docs)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return errors.New("metadata.docs must be a valid absolute HTTPS URL")
	}
	if port := parsed.Port(); port != "" {
		number, err := strconv.ParseUint(port, 10, 16)
		if err != nil || number == 0 {
			return errors.New("metadata.docs must be a valid absolute HTTPS URL")
		}
	}
	return nil
}

// ValidateIncludedFileFrontmatterWithSchemaAndLocation validates included file frontmatter with file location info
func ValidateIncludedFileFrontmatterWithSchemaAndLocation(frontmatter map[string]any, filePath string) error {
	schemaValidationLog.Printf("Validating included file frontmatter: file=%s, fields=%d", filePath, len(frontmatter))

	// Custom agent files (.github/agents/*.md) follow the Copilot agent format,
	// which differs from the gh-aw workflow schema. Skip schema validation for them.
	if isCustomAgentFile(filePath) {
		schemaValidationLog.Printf("Skipping schema validation for custom agent file: %s", filePath)
		return nil
	}

	// Filter out ignored fields before validation
	filtered := filterIgnoredFields(frontmatter)

	// First check for forbidden fields in shared workflows
	if err := validateSharedWorkflowFields(filtered); err != nil {
		return err
	}
	if err := validateMetadataDocs(filtered); err != nil {
		return err
	}

	// To validate shared workflows against the main schema, we temporarily add an 'on' field
	tempFrontmatter := make(map[string]any)
	maps.Copy(tempFrontmatter, filtered)
	// Add a temporary 'on' field to satisfy the schema's required field
	tempFrontmatter["on"] = "push"

	// Validate with the main schema (which will catch unknown fields)
	if err := validateWithSchemaAndLocation(tempFrontmatter, mainWorkflowSchema, "included file", filePath); err != nil {
		return err
	}

	// Run custom validation for engine-specific rules
	return validateEngineSpecificRules(filtered)
}

// ValidateMCPConfigWithSchema validates MCP configuration using JSON schema.
// The caller is responsible for passing only the fields defined in the MCP
// config schema; additional tool-specific fields (e.g. auth, proxy-args)
// must be stripped before calling this function because the schema uses
// additionalProperties: false.
func ValidateMCPConfigWithSchema(mcpConfig map[string]any) error {
	schemaValidationLog.Printf("Validating MCP configuration against JSON schema: %d fields", len(mcpConfig))
	return validateWithSchema(mcpConfig, mcpConfigSchema, "MCP configuration")
}

// ValidateRepositoryPackageManifestWithSchemaAndLocation validates an aw.yml repository package manifest.
func ValidateRepositoryPackageManifestWithSchemaAndLocation(manifest map[string]any, filePath string) error {
	schemaValidationLog.Printf("Validating repository package manifest: file=%s, fields=%d", filePath, len(manifest))
	return validateWithSchemaAndLocation(manifest, awManifestSchema, "Agentic Workflow manifest", filePath)
}
