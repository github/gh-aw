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
	"sync"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var importedStepsValidationLog = logger.New("workflow:imported_steps_validation")

// buildAgenticEngineSecretsMap dynamically builds a map of agentic engine secret names
// by querying each registered engine for its required secrets
func buildAgenticEngineSecretsMap() map[string]string {
	secretsMap := make(map[string]string)

	// Create a minimal WorkflowData for querying engine secrets
	// (we don't need MCP servers or other config for base secret names)
	workflowData := &WorkflowData{}

	// Get the global engine registry
	registry := GetGlobalEngineRegistry()

	// Iterate through all registered engines
	for _, engine := range registry.GetAllEngines() {
		engineID := engine.GetID()
		engineName := engine.GetDisplayName()

		// Skip custom engine as it doesn't have predefined secrets
		if engineID == "custom" {
			continue
		}

		// Get required secrets from this engine
		requiredSecrets := engine.GetRequiredSecretNames(workflowData)

		for _, secret := range requiredSecrets {
			// Filter out non-agentic secrets (infrastructure/gateway secrets)
			// Only include secrets that are specific to the AI engine itself
			if isAgenticEngineSecret(secret) {
				secretsMap[secret] = engineName
				importedStepsValidationLog.Printf("Registered agentic secret: %s (engine: %s)", secret, engineName)
			}
		}
	}

	return secretsMap
}

// isAgenticEngineSecret returns true if the secret is an agentic engine-specific secret
// (not an infrastructure secret like MCP_GATEWAY_API_KEY or GITHUB_MCP_SERVER_TOKEN)
//
// Infrastructure secrets are used for internal plumbing (MCP gateway, GitHub API access)
// and are not agentic engine authentication credentials. These secrets are safe to use
// in custom engine steps as they don't bypass the firewall or expose AI engine credentials.
func isAgenticEngineSecret(secretName string) bool {
	// Infrastructure/gateway secrets that are NOT agentic engine secrets
	// These secrets are ignored in the validation because they are safe to use
	// in custom engine steps (they don't expose AI engine credentials)
	nonAgenticSecrets := map[string]bool{
		"MCP_GATEWAY_API_KEY":           true, // MCP gateway infrastructure
		"GITHUB_MCP_SERVER_TOKEN":       true, // GitHub MCP server access
		"GH_AW_GITHUB_MCP_SERVER_TOKEN": true, // GitHub MCP server access (alternative)
		"GH_AW_GITHUB_TOKEN":            true, // GitHub API access
		"GITHUB_TOKEN":                  true, // GitHub Actions default token
	}

	return !nonAgenticSecrets[secretName]
}

// getAgenticEngineSecrets returns the map of agentic engine secrets
// Lazily builds the map on first call
var (
	agenticEngineSecretsMap     map[string]string
	agenticEngineSecretsMapOnce sync.Once
)

func getAgenticEngineSecrets() map[string]string {
	agenticEngineSecretsMapOnce.Do(func() {
		agenticEngineSecretsMap = buildAgenticEngineSecretsMap()
		importedStepsValidationLog.Printf("Built agentic engine secrets map with %d entries", len(agenticEngineSecretsMap))
	})
	return agenticEngineSecretsMap
}

// isCustomAgenticEngine checks if the custom engine is actually another agentic framework
// (like OpenCode) that legitimately needs agentic engine secrets
func isCustomAgenticEngine(engineConfig *EngineConfig) bool {
	if engineConfig == nil || len(engineConfig.Steps) == 0 {
		return false
	}

	// List of known agentic framework packages/commands that should be exempt
	agenticFrameworks := []string{
		"opencode-ai",
		"opencode",
		// Add other agentic frameworks here as needed
	}

	// Check if any step installs or runs a known agentic framework
	for _, step := range engineConfig.Steps {
		stepYAML, err := convertStepToYAML(step)
		if err != nil {
			continue
		}

		stepYAMLLower := strings.ToLower(stepYAML)
		for _, framework := range agenticFrameworks {
			if strings.Contains(stepYAMLLower, framework) {
				importedStepsValidationLog.Printf("Detected custom agentic framework: %s", framework)
				return true
			}
		}
	}

	return false
}

// validateImportedStepsNoAgenticSecrets validates that custom engine steps don't use agentic engine secrets
// In strict mode, this returns an error. In non-strict mode, this prints a warning to stderr.
// This validation is skipped for custom engines that are actually agentic frameworks (like OpenCode).
func (c *Compiler) validateImportedStepsNoAgenticSecrets(engineConfig *EngineConfig, engineID string) error {
	if engineConfig == nil || engineID != "custom" {
		importedStepsValidationLog.Print("Skipping validation: not a custom engine")
		return nil
	}

	if len(engineConfig.Steps) == 0 {
		importedStepsValidationLog.Print("No custom steps to validate")
		return nil
	}

	// Skip validation for custom agentic engines like OpenCode
	if isCustomAgenticEngine(engineConfig) {
		importedStepsValidationLog.Print("Skipping validation: custom engine is an agentic framework")
		return nil
	}

	importedStepsValidationLog.Printf("Validating %d custom engine steps for agentic secrets", len(engineConfig.Steps))

	// Get the map of agentic engine secrets (dynamically built from engine instances)
	agenticSecrets := getAgenticEngineSecrets()

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
			if engineName, isAgenticSecret := agenticSecrets[secretName]; isAgenticSecret {
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
			fmt.Fprintf(&builder, "%s%s: %s\n", indent, key, v)
		case map[string]any:
			fmt.Fprintf(&builder, "%s%s:\n", indent, key)
			for k, val := range v {
				writeValue(k, val, indent+"  ")
			}
		case []any:
			fmt.Fprintf(&builder, "%s%s:\n", indent, key)
			for _, item := range v {
				if str, ok := item.(string); ok {
					fmt.Fprintf(&builder, "%s  - %s\n", indent, str)
				}
			}
		default:
			fmt.Fprintf(&builder, "%s%s: %v\n", indent, key, v)
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
