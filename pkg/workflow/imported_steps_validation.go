// This file provides validation for imported steps in engine configurations.
//
// # Imported Steps Validation
//
// This file validates steps used in the agent job, including:
//   - Main frontmatter steps (under the 'steps' key)
//   - Imported steps merged from shared workflows (MergedSteps)
//
// # Checkout Credential Validation
//
// When actions/checkout is used without 'persist-credentials: false', the GitHub
// token is stored in .git/config and accessible to the agent, which is a security
// concern. This file detects this pattern and:
//   - In strict mode: raises a compilation error
//   - In non-strict mode: emits a warning
//
// Only the agent job is checked (frontmatter 'steps' and imported MergedSteps).
// Custom jobs defined under 'jobs:' are not affected by this validation.
//
// For general validation, see validation.go.
// For strict mode validation, see strict_mode_validation.go.
// For detailed documentation, see scratchpad/validation-architecture.md

package workflow

import (
	"fmt"
	"os"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/github/gh-aw/pkg/actionpins"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var importedStepsValidationLog = logger.New("workflow:imported_steps_validation")

// validateImportedStepsNoAgenticSecrets validates that engine steps don't use agentic engine secrets
// This validation is now a no-op since custom engine support has been removed.
// The function is kept for backwards compatibility but always returns nil.
func (c *Compiler) validateImportedStepsNoAgenticSecrets(engineConfig *EngineConfig, engineID string) error {
	// Custom engine has been removed, so this validation no longer applies
	importedStepsValidationLog.Print("Skipping validation: custom engine support removed")
	return nil
}

// validateCheckoutPersistCredentials checks that actions/checkout steps in the agent job
// include 'persist-credentials: false'. Without this setting, the git token is stored in
// .git/config and accessible to the agent.
//
// This validates steps from:
//   - The main frontmatter 'steps' section (agent job steps)
//   - Imported steps merged from shared workflows (MergedSteps)
//
// In strict mode this returns an error; in non-strict mode it emits a warning.
// Only applies to the agent job. Custom jobs under 'jobs:' are not checked.
func (c *Compiler) validateCheckoutPersistCredentials(frontmatter map[string]any, mergedSteps string) error {
	importedStepsValidationLog.Printf("Validating checkout persist-credentials in agent job steps (strict=%v)", c.strictMode)

	var offendingStepNames []string

	// Check main frontmatter steps (agent job steps defined in the main workflow)
	if stepsValue, exists := frontmatter["steps"]; exists {
		if steps, ok := stepsValue.([]any); ok {
			for _, step := range steps {
				if stepMap, ok := step.(map[string]any); ok {
					if checkoutMissingPersistCredentialsFalse(stepMap) {
						offendingStepNames = append(offendingStepNames, stepDisplayName(stepMap))
					}
				}
			}
		}
	}

	// Check imported steps merged from shared workflow files
	if mergedSteps != "" {
		var importedSteps []any
		if err := yaml.Unmarshal([]byte(mergedSteps), &importedSteps); err == nil {
			for _, step := range importedSteps {
				if stepMap, ok := step.(map[string]any); ok {
					if checkoutMissingPersistCredentialsFalse(stepMap) {
						offendingStepNames = append(offendingStepNames, stepDisplayName(stepMap))
					}
				}
			}
		}
	}

	if len(offendingStepNames) == 0 {
		importedStepsValidationLog.Print("Checkout persist-credentials validation passed")
		return nil
	}

	checkoutVersion := "v4"
	if pin, ok := actionpins.GetLatestActionPinByRepo("actions/checkout"); ok && pin.Version != "" {
		checkoutVersion = pin.Version
	}

	persistCredentialsExample := fmt.Sprintf(`      # example (keep your existing "uses:" ref):
      - uses: actions/checkout@%s
        with:
          persist-credentials: false`, checkoutVersion)

	msg := fmt.Sprintf(
		"actions/checkout step(s) without 'persist-credentials: false' detected in the agent job: %s. "+
			"Without this setting the git token is stored in .git/config and leaked to the agent. "+
			"Add 'persist-credentials: false' to the 'with:' block of each checkout step, for example:\n"+
			persistCredentialsExample+"\n"+
			"See: https://github.github.com/gh-aw/reference/steps/",
		strings.Join(offendingStepNames, ", "),
	)

	importedStepsValidationLog.Printf("Checkout validation failed: %s", msg)

	if c.strictMode {
		return fmt.Errorf("strict mode: %s", msg)
	}

	// Non-strict mode: emit a warning
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(msg))
	c.IncrementWarningCount()
	return nil
}

// checkoutMissingPersistCredentialsFalse returns true when the step uses actions/checkout
// but does not have 'persist-credentials: false' in its 'with' block.
func checkoutMissingPersistCredentialsFalse(step map[string]any) bool {
	usesValue, exists := step["uses"]
	if !exists {
		return false
	}

	usesStr, ok := usesValue.(string)
	if !ok {
		return false
	}

	// Strip action pin comments (e.g., "actions/checkout@abc123 # v4") before matching
	usesStr = strings.TrimSpace(strings.SplitN(usesStr, " #", 2)[0])

	// Match "actions/checkout" or "actions/checkout@<ref>"
	if usesStr != "actions/checkout" && !strings.HasPrefix(usesStr, "actions/checkout@") {
		return false
	}

	// Check whether persist-credentials is explicitly set to false
	withValue, hasWithBlock := step["with"]
	if !hasWithBlock {
		// No with block: persist-credentials defaults to true → insecure
		return true
	}

	withMap, ok := withValue.(map[string]any)
	if !ok {
		// Unexpected with format: treat conservatively as insecure
		return true
	}

	persistCredentials, hasPersistCredentials := withMap["persist-credentials"]
	if !hasPersistCredentials {
		// Not set: defaults to true → insecure
		return true
	}

	// Bool false is the safe value
	if boolVal, ok := persistCredentials.(bool); ok {
		return boolVal // true = insecure, false = safe
	}

	// String "false" is also accepted by GitHub Actions
	if strVal, ok := persistCredentials.(string); ok {
		return strVal != "false"
	}

	// Unknown type: treat conservatively as insecure
	return true
}

// stepDisplayName returns a human-readable identifier for a step.
// It uses the step name if available, otherwise the uses value.
func stepDisplayName(step map[string]any) string {
	if name, ok := step["name"].(string); ok && name != "" {
		return fmt.Sprintf("'%s'", name)
	}
	if uses, ok := step["uses"].(string); ok && uses != "" {
		return fmt.Sprintf("'%s'", uses)
	}
	return "'<unnamed step>'"
}
