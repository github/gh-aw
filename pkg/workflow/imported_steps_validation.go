// This file provides validation for imported steps in custom engine configurations.
//
// # Imported Steps Validation
//
// This file validates that imported custom engine steps do not use agentic engine
// secrets. These secrets (COPILOT_GITHUB_TOKEN, ANTHROPIC_API_KEY, CODEX_API_KEY, etc.)
// are meant to be used only within the secure firewall environment. Using them in
// imported custom steps is unsafe because:
//  - Custom steps run outside the firewall
//  - They bypass security isolation
//  - They expose sensitive tokens to user-defined actions
//
// # Validation Functions
//
// The imported steps validator performs progressive validation:
//  1. validateImportedStepsNoAgenticSecrets() - Checks for agentic engine secrets
//  2. In strict mode: Returns error if secrets found
//  3. In non-strict mode: Returns warning if secrets found
//
// # When to Add Validation Here
//
// Add validation to this file when:
//   - It validates imported/custom engine steps
//   - It checks for secret usage in custom steps
//   - It enforces security boundaries for custom actions
//
// For general validation, see validation.go.
// For strict mode validation, see strict_mode_validation.go.
// For detailed documentation, see scratchpad/validation-architecture.md

package workflow

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var importedStepsValidationLog = logger.New("workflow:imported_steps_validation")

// agenticEngineSecrets maps secret names to their display names for validation
var agenticEngineSecrets = map[string]string{
	"COPILOT_GITHUB_TOKEN":    "Copilot engine",
	"ANTHROPIC_API_KEY":       "Claude engine",
	"CLAUDE_CODE_OAUTH_TOKEN": "Claude engine",
	"CODEX_API_KEY":           "Codex engine",
	"OPENAI_API_KEY":          "Codex engine",
}

// validateImportedStepsNoAgenticSecrets validates that custom engine steps don't use agentic engine secrets
// In strict mode, this returns an error. In non-strict mode, this prints a warning to stderr.
func (c *Compiler) validateImportedStepsNoAgenticSecrets(engineConfig *EngineConfig, engineID string) error {
	if engineConfig == nil || engineID != "custom" {
		importedStepsValidationLog.Print("Skipping validation: not a custom engine")
		return nil
	}

	if len(engineConfig.Steps) == 0 {
		importedStepsValidationLog.Print("No custom steps to validate")
		return nil
	}

	importedStepsValidationLog.Printf("Validating %d custom engine steps for agentic secrets", len(engineConfig.Steps))

	// Build regex pattern to detect secrets references
	// Matches: ${{ secrets.SECRET_NAME }} or ${{secrets.SECRET_NAME}}
	secretsPattern := regexp.MustCompile(`\$\{\{\s*secrets\.([A-Z_][A-Z0-9_]*)\s*(?:\|\||&&)?[^}]*\}\}`)

	var foundSecrets []string
	var secretEngines []string

	// Check each custom step for secret usage
	for stepIdx, step := range engineConfig.Steps {
		importedStepsValidationLog.Printf("Checking step %d", stepIdx)
		
		// Convert step to YAML string for pattern matching
		stepYAML, err := convertStepToYAML(step)
		if err != nil {
			importedStepsValidationLog.Printf("Failed to convert step to YAML, skipping: %v", err)
			continue
		}

		// Find all secret references in the step
		matches := secretsPattern.FindAllStringSubmatch(stepYAML, -1)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			
			secretName := match[1]
			if engineName, isAgenticSecret := agenticEngineSecrets[secretName]; isAgenticSecret {
				importedStepsValidationLog.Printf("Found agentic secret in step %d: %s (engine: %s)", stepIdx, secretName, engineName)
				if !containsSecretName(foundSecrets, secretName) {
					foundSecrets = append(foundSecrets, secretName)
					secretEngines = append(secretEngines, engineName)
				}
			}
		}
	}

	// If no agentic secrets found, validation passes
	if len(foundSecrets) == 0 {
		importedStepsValidationLog.Print("No agentic secrets found in custom steps")
		return nil
	}

	// Build error message
	secretsList := strings.Join(foundSecrets, ", ")
	enginesList := uniqueStrings(secretEngines)
	enginesDisplay := strings.Join(enginesList, " and ")

	errorMsg := fmt.Sprintf(
		"custom engine steps use agentic engine secrets (%s) which are not allowed. "+
			"These secrets are for %s and should only be used within the secure firewall environment. "+
			"Custom engine steps run outside the firewall and bypass security isolation. "+
			"Remove references to %s from your custom engine steps. "+
			"See: https://github.github.com/gh-aw/reference/engines/",
		secretsList, enginesDisplay, secretsList,
	)

	if c.strictMode {
		importedStepsValidationLog.Printf("Strict mode: returning error for agentic secrets in custom steps")
		return fmt.Errorf("strict mode: %s", errorMsg)
	}

	// Non-strict mode: warning only
	importedStepsValidationLog.Printf("Non-strict mode: emitting warning for agentic secrets in custom steps")
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(errorMsg))
	c.IncrementWarningCount()
	return nil
}

// convertStepToYAML converts a step map to YAML string for pattern matching
func convertStepToYAML(step map[string]any) (string, error) {
	var builder strings.Builder
	
	// Helper function to write key-value pairs
	var writeValue func(key string, value any, indent string)
	writeValue = func(key string, value any, indent string) {
		switch v := value.(type) {
		case string:
			builder.WriteString(fmt.Sprintf("%s%s: %s\n", indent, key, v))
		case map[string]any:
			builder.WriteString(fmt.Sprintf("%s%s:\n", indent, key))
			for k, val := range v {
				writeValue(k, val, indent+"  ")
			}
		case []any:
			builder.WriteString(fmt.Sprintf("%s%s:\n", indent, key))
			for _, item := range v {
				if str, ok := item.(string); ok {
					builder.WriteString(fmt.Sprintf("%s  - %s\n", indent, str))
				}
			}
		default:
			builder.WriteString(fmt.Sprintf("%s%s: %v\n", indent, key, v))
		}
	}

	for key, value := range step {
		writeValue(key, value, "")
	}

	return builder.String(), nil
}

// containsSecretName checks if a string slice contains a string (helper for secret detection)
func containsSecretName(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// uniqueStrings returns unique strings from a slice
func uniqueStrings(slice []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, s := range slice {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}
