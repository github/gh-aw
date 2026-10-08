package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var secretMaskingLog = logger.New("workflow:secret_masking")

// secretsPrefix is the literal string used to locate secret references.
// Pattern (informational): `secrets\.([A-Z][A-Z0-9_]*)`
const secretsPrefix = "secrets."

// actionReferencePattern is replaced by a fast string scan in CollectActionReferences.
// Pattern (informational): `(?m)^\s+(?:-\s+)?uses:\s+(\S+)(?:\s+#\s*(.+?))?$`

// escapeSingleQuoteBackslash escapes single quotes and backslashes in a string to prevent
// injection when embedding data in single-quoted YAML strings using backslash escaping.
func escapeSingleQuoteBackslash(s string) string {
	// First escape backslashes, then escape single quotes
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return s
}

// CollectSecretReferences extracts all secret references from the workflow YAML
// This scans for patterns like ${{ secrets.SECRET_NAME }} or secrets.SECRET_NAME
func CollectSecretReferences(yamlContent string) []string {
	secretMaskingLog.Printf("Scanning workflow YAML (%d bytes) for secret references", len(yamlContent))
	secretsMap := make(map[string]struct {
	})

	// Walk through the content looking for every occurrence of "secrets."
	// followed by an uppercase identifier [A-Z][A-Z0-9_]*.
	rest := yamlContent
	for {
		idx := strings.Index(rest, secretsPrefix)
		if idx == -1 {
			break
		}
		// Advance past "secrets."
		rest = rest[idx+len(secretsPrefix):]

		// First character of the name must be an uppercase letter
		if rest == "" || rest[0] < 'A' || rest[0] > 'Z' {
			continue
		}

		// Consume [A-Z0-9_]* for the rest of the name
		end := 1
		for end < len(rest) {
			c := rest[end]
			if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
				end++
			} else {
				break
			}
		}
		secretsMap[rest[:end]] = struct {
		}{}
		rest = rest[end:]
	}

	// Convert map to sorted slice for consistent ordering
	secrets := sliceutil.SortedKeys(secretsMap)

	secretMaskingLog.Printf("Found %d unique secret reference(s) in workflow", len(secrets))

	return secrets
}

// CollectActionReferences extracts all external GitHub Action references from the workflow YAML.
// It returns a sorted, deduplicated list of "uses:" values, excluding local references
// that start with "./" (e.g., "./actions/setup" or "./.github/workflows/...").
// Each entry includes the inline tag comment when present (e.g., "actions/checkout@sha # v4").
func CollectActionReferences(yamlContent string) []string {
	secretMaskingLog.Printf("Scanning workflow YAML (%d bytes) for action references", len(yamlContent))
	actionsMap := make(map[string]struct {
	})

	for line := range strings.SplitSeq(yamlContent, "\n") {
		// Quick check: line must contain "uses:" to avoid scanning every character
		prefix, afterUses, found := strings.Cut(line, "uses:")
		if !found {
			continue
		}

		// Must start with whitespace — rejects bare top-level keys ("uses: action")
		// or top-level list items like "- uses: action".
		if line == "" || (line[0] != ' ' && line[0] != '\t') {
			continue
		}

		// The prefix before "uses:" must be either:
		//   - Only whitespace           (plain key-value: "    uses: action")
		//   - "-" with leading spaces   (list item:       "    - uses: action")
		trimmedPrefix := strings.TrimSpace(prefix)
		if trimmedPrefix != "" && trimmedPrefix != "-" {
			continue
		}

		// Extract the action reference: everything after "uses:" trimmed
		rest := strings.TrimSpace(afterUses)
		if rest == "" {
			continue
		}

		// The action ref is the first whitespace-delimited token; anything after
		// optional whitespace + "#" is treated as the inline tag comment.
		spaceIdx := strings.IndexByte(rest, ' ')
		var ref, comment string
		if spaceIdx == -1 {
			ref = rest
		} else {
			ref = rest[:spaceIdx]
			afterRef := strings.TrimSpace(rest[spaceIdx:])
			if strings.HasPrefix(afterRef, "#") {
				comment = strings.TrimSpace(afterRef[1:])
			}
		}

		// Skip local actions (e.g. "./actions/setup", "./.github/workflows/...")
		if strings.HasPrefix(ref, "./") {
			continue
		}

		entry := ref
		if comment != "" {
			entry = ref + " # " + comment
		}
		actionsMap[entry] = struct {
		}{}
	}

	actions := sliceutil.SortedKeys(actionsMap)

	secretMaskingLog.Printf("Found %d unique external action reference(s) in workflow", len(actions))
	return actions
}

func (c *Compiler) generateSecretRedactionStep(yaml *strings.Builder, yamlContent string, data *WorkflowData) {
	// Extract secret references from the generated YAML
	secretReferences := CollectSecretReferences(yamlContent)

	// Always record that we're adding a secret redaction step, even if no secrets found
	// This is important for validation to ensure the step ordering is correct
	c.stepOrderTracker.RecordSecretRedaction("Redact secrets in logs")

	secretMaskingLog.Printf("Generating redaction step for %d secret(s)", len(secretReferences))
	yaml.WriteString("      - name: Redact secrets in logs\n")
	yaml.WriteString("        if: always()\n")
	fmt.Fprintf(yaml, "        uses: %s\n", getCachedActionPin("actions/github-script", data))
	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")

	// Load redact_secrets script from external file
	// Use setupGlobals helper to attach GitHub Actions builtin objects to global scope
	yaml.WriteString("            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n")
	yaml.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	yaml.WriteString("            const { main } = require('${{ runner.temp }}/gh-aw/actions/redact_secrets.cjs');\n")
	if data.SecretMasking != nil && len(data.SecretMasking.Steps) > 0 {
		yaml.WriteString("            await main({ prepareForCustomMasking: true });\n")
	} else {
		yaml.WriteString("            await main();\n")
	}

	// Add environment variables (only when secrets are present, since a step's
	// `env` must be a mapping and an empty `env:` parses as null)
	if len(secretReferences) > 0 {
		yaml.WriteString("        env:\n")

		// Pass the list of secret names as a comma-separated string
		// Escape each secret reference to prevent injection when embedding in YAML
		escapedRefs := make([]string, len(secretReferences))
		for i, ref := range secretReferences {
			escapedRefs[i] = escapeSingleQuoteBackslash(ref)
		}
		fmt.Fprintf(yaml, "          GH_AW_SECRET_NAMES: '%s'\n", strings.Join(escapedRefs, ","))

		// Pass the actual secret values as environment variables so they can be redacted
		// Each secret will be available as an environment variable
		for _, secretName := range secretReferences {
			// Escape secret name to prevent injection in YAML
			escapedSecretName := escapeSingleQuoteBackslash(secretName)
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
