package workflow

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var secretMaskingLog = logger.New("workflow:secret_masking")

// escapeSingleQuote escapes single quotes and backslashes in a string to prevent injection
// when embedding data in single-quoted YAML strings
func escapeSingleQuote(s string) string {
	// First escape backslashes, then escape single quotes
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return s
}

// CollectSecretReferences extracts all secret references from the workflow YAML
// This scans for patterns like ${{ secrets.SECRET_NAME }} or secrets.SECRET_NAME
func CollectSecretReferences(yamlContent string) []string {
	secretsMap := make(map[string]bool)

	// Pattern to match ${{ secrets.SECRET_NAME }} or secrets.SECRET_NAME
	// This matches both with and without the ${{ }} wrapper
	secretPattern := regexp.MustCompile(`secrets\.([A-Z][A-Z0-9_]*)`)

	matches := secretPattern.FindAllStringSubmatch(yamlContent, -1)
	for _, match := range matches {
		if len(match) > 1 {
			secretsMap[match[1]] = true
		}
	}

	// Convert map to sorted slice for consistent ordering
	secrets := make([]string, 0, len(secretsMap))
	for secret := range secretsMap {
		secrets = append(secrets, secret)
	}

	// Sort for consistent output
	SortStrings(secrets)

	return secrets
}

// generateSecretRedactionStep generates a workflow step that redacts secrets from files in /tmp
func (c *Compiler) generateSecretRedactionStep(yaml *strings.Builder, yamlContent string, data *WorkflowData) {
	// Extract secret references from the generated YAML
	secretReferences := CollectSecretReferences(yamlContent)

	// Always record that we're adding a secret redaction step, even if no secrets found
	// This is important for validation to ensure the step ordering is correct
	c.stepOrderTracker.RecordSecretRedaction("Redact secrets in logs")

	// If no secrets found, we still generate the step but it will be a no-op at runtime
	// This ensures consistent step ordering and validation
	if len(secretReferences) == 0 {
		// Generate a minimal no-op redaction step for validation purposes
		yaml.WriteString("      - name: Redact secrets in logs\n")
		yaml.WriteString("        if: always()\n")
		yaml.WriteString("        run: echo 'No secrets to redact'\n")
	} else {
		yaml.WriteString("      - name: Redact secrets in logs\n")
		yaml.WriteString("        if: always()\n")
		fmt.Fprintf(yaml, "        uses: %s\n", GetActionPin("actions/github-script"))
		yaml.WriteString("        with:\n")
		yaml.WriteString("          script: |\n")

		// Load redact_secrets script from external file
		// Use setupGlobals helper to attach GitHub Actions builtin objects to global scope
		yaml.WriteString("            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n")
		yaml.WriteString("            setupGlobals(core, github, context, exec, io);\n")
		yaml.WriteString("            const { main } = require('/opt/gh-aw/actions/redact_secrets.cjs');\n")
		yaml.WriteString("            await main();\n")

		// Add environment variables
		yaml.WriteString("        env:\n")

		// Pass the list of secret names as a comma-separated string
		// Escape each secret reference to prevent injection when embedding in YAML
		escapedRefs := make([]string, len(secretReferences))
		for i, ref := range secretReferences {
			escapedRefs[i] = escapeSingleQuote(ref)
		}
		fmt.Fprintf(yaml, "          GH_AW_SECRET_NAMES: '%s'\n", strings.Join(escapedRefs, ","))

		// Pass the actual secret values as environment variables so they can be redacted
		// Each secret will be available as an environment variable
		for _, secretName := range secretReferences {
			// Escape secret name to prevent injection in YAML
			escapedSecretName := escapeSingleQuote(secretName)
			// Use original secretName in GitHub Actions expression since it's already validated
			// to only contain safe characters (uppercase letters, numbers, underscores)
			fmt.Fprintf(yaml, "          SECRET_%s: ${{ secrets.%s }}\n", escapedSecretName, secretName)
		}
	}

	// Inject custom secret masking steps if configured
	if data.SecretMasking != nil && len(data.SecretMasking.Steps) > 0 {
		secretMaskingLog.Printf("Injecting %d custom secret masking steps", len(data.SecretMasking.Steps))
		for _, step := range data.SecretMasking.Steps {
			c.generateCustomSecretMaskingStep(yaml, step, data)
		}
	}
}

// generateCustomSecretMaskingStep generates a custom secret masking step from configuration
func (c *Compiler) generateCustomSecretMaskingStep(yaml *strings.Builder, step map[string]any, data *WorkflowData) {
	// Record the custom secret masking step for validation
	stepName := "Custom secret masking"
	if name, ok := step["name"].(string); ok {
		stepName = name
	}
	c.stepOrderTracker.RecordSecretRedaction(stepName)

	// Generate the step YAML
	c.renderStepFromMap(yaml, step, data, "      ")
}

// renderStepFromMap renders a GitHub Actions step from a map to YAML
func (c *Compiler) renderStepFromMap(yaml *strings.Builder, step map[string]any, data *WorkflowData, indent string) {
	// Start the step with a dash
	yaml.WriteString(indent + "- ")

	// Track if we've written the first line
	firstField := true

	// Order of fields to write (matches GitHub Actions convention)
	fieldOrder := []string{"name", "id", "if", "uses", "with", "run", "env", "working-directory", "continue-on-error", "timeout-minutes", "shell"}

	for _, field := range fieldOrder {
		if value, exists := step[field]; exists {
			// Add proper indentation for non-first fields
			if !firstField {
				yaml.WriteString(indent + "  ")
			}
			firstField = false

			// Render the field based on its type
			switch v := value.(type) {
			case string:
				// Handle multi-line strings (especially for 'run' field)
				if field == "run" && strings.Contains(v, "\n") {
					fmt.Fprintf(yaml, "%s: |\n", field)
					lines := strings.Split(v, "\n")
					for _, line := range lines {
						fmt.Fprintf(yaml, "%s    %s\n", indent, line)
					}
				} else {
					fmt.Fprintf(yaml, "%s: %s\n", field, v)
				}
			case map[string]any:
				// For complex fields like "with" or "env"
				fmt.Fprintf(yaml, "%s:\n", field)
				for key, val := range v {
					fmt.Fprintf(yaml, "%s    %s: %v\n", indent, key, val)
				}
			default:
				fmt.Fprintf(yaml, "%s: %v\n", field, v)
			}
		}
	}

	// Add any remaining fields not in the predefined order
	for field, value := range step {
		// Skip fields we've already processed
		skip := false
		for _, f := range fieldOrder {
			if f == field {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		if !firstField {
			yaml.WriteString(indent + "  ")
		}
		firstField = false

		switch v := value.(type) {
		case string:
			// Handle multi-line strings
			if strings.Contains(v, "\n") {
				fmt.Fprintf(yaml, "%s: |\n", field)
				lines := strings.Split(v, "\n")
				for _, line := range lines {
					fmt.Fprintf(yaml, "%s    %s\n", indent, line)
				}
			} else {
				fmt.Fprintf(yaml, "%s: %s\n", field, v)
			}
		case map[string]any:
			fmt.Fprintf(yaml, "%s:\n", field)
			for key, val := range v {
				fmt.Fprintf(yaml, "%s    %s: %v\n", indent, key, val)
			}
		default:
			fmt.Fprintf(yaml, "%s: %v\n", field, v)
		}
	}
}
