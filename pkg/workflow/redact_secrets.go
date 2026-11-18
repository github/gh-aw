package workflow

import (
	"fmt"
	"maps"
	"strings"
	"unicode/utf8"

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
		first, _ := utf8.DecodeRuneInString(rest)
		if first < 'A' || first > 'Z' {
			continue
		}

		// Consume [A-Z0-9_]* for the rest of the name
		end := 1
		for i := range rest {
			if i == 0 {
				continue
			}
			c := rest[i]
			if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
				end = i + 1
				continue
			}
			end = i
			break
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
// It returns a sorted, deduplicated list of "uses:" values, excluding local and
// self-repository references that start with "./" or "$/".
// Each entry includes the inline tag comment when present (e.g., "actions/checkout@sha # v4").
func CollectActionReferences(yamlContent string) []string {
	secretMaskingLog.Printf("Scanning workflow YAML (%d bytes) for action references", len(yamlContent))
	actionsMap := make(map[string]struct {
	})

	for line := range strings.SplitSeq(yamlContent, "\n") {
		if entry, ok := actionReferenceFromLine(line); ok {
			actionsMap[entry] = struct{}{}
		}
	}

	actions := sliceutil.SortedKeys(actionsMap)

	secretMaskingLog.Printf("Found %d unique external action reference(s) in workflow", len(actions))
	return actions
}

func actionReferenceFromLine(line string) (string, bool) {
	prefix, afterUses, found := strings.Cut(line, "uses:")
	if !found {
		return "", false
	}
	if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
		return "", false
	}

	trimmedPrefix := strings.TrimSpace(prefix)
	if trimmedPrefix != "" && trimmedPrefix != "-" {
		return "", false
	}

	rest := strings.TrimSpace(afterUses)
	if rest == "" {
		return "", false
	}
	spaceIdx := strings.IndexByte(rest, ' ')
	ref, comment := rest, ""
	if spaceIdx != -1 {
		ref = rest[:spaceIdx]
		afterRef := strings.TrimSpace(rest[spaceIdx:])
		if strings.HasPrefix(afterRef, "#") {
			comment = strings.TrimSpace(afterRef[1:])
		}
	}
	if strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "$/") {
		return "", false
	}
	if comment != "" {
		ref += " # " + comment
	}
	return ref, true
}

func (c *Compiler) generateSecretRedactionStep(yaml *strings.Builder, yamlContent string, data *WorkflowData, stagedDurableFiles bool) {
	// Extract secret references from the generated YAML
	secretReferences := CollectSecretReferences(yamlContent)

	// Always record that we're adding a secret redaction step, even if no secrets found
	// This is important for validation to ensure the step ordering is correct
	c.stepOrderTracker.RecordSecretRedaction("Redact secrets in logs")

	secretMaskingLog.Printf("Generating redaction step for %d secret(s)", len(secretReferences))
	yaml.WriteString("      - name: Redact secrets in logs\n")
	yaml.WriteString("        if: always()\n")
	yaml.WriteString("        uses: " + getCachedActionPin("actions/github-script", data) + "\n")
	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")
	durableCopilot := stagedDurableFiles && data.durableExecutionPlan != nil && data.EngineConfig != nil && data.EngineConfig.ID == "copilot"
	writeSecretRedactionScript(yaml, data, durableCopilot)

	if durableCopilot || len(secretReferences) > 0 {
		yaml.WriteString("        env:\n")
	}
	if durableCopilot {
		generateCopilotCheckpointRedactionEnv(yaml, data)
	}
	if len(secretReferences) > 0 {
		writeSecretRedactionEnvironment(yaml, secretReferences)
	}

	// Inject custom secret masking steps if configured
	if data.SecretMasking != nil && len(data.SecretMasking.Steps) > 0 {
		secretMaskingLog.Printf("Injecting %d custom secret masking steps", len(data.SecretMasking.Steps))
		for i, id := range customSecretMaskingStepIDs(data) {
			if i >= 0 && i < len(data.SecretMasking.Steps) {
				trackedStep := maps.Clone(data.SecretMasking.Steps[i])
				trackedStep["id"] = id
				c.generateCustomSecretMaskingStep(yaml, trackedStep, data)
			}
		}
	}
}

func writeSecretRedactionScript(yaml *strings.Builder, data *WorkflowData, durableCopilot bool) {
	yaml.WriteString("            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n")
	yaml.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	yaml.WriteString("            const { main } = require('${{ runner.temp }}/gh-aw/actions/redact_secrets.cjs');\n")

	var options []string
	if data.SecretMasking != nil && len(data.SecretMasking.Steps) > 0 {
		options = append(options, "prepareForCustomMasking: true")
	}
	if durableCopilot {
		options = append(options, "redactDurableCopilot: process.env.GH_AW_DURABLE_STAGED === 'true'")
	}
	if len(options) > 0 {
		yaml.WriteString("            await main({ " + strings.Join(options, ", ") + " });\n")
		return
	}
	yaml.WriteString("            await main();\n")
}

func writeSecretRedactionEnvironment(yaml *strings.Builder, secretReferences []string) {
	escapedRefs := make([]string, 0, len(secretReferences))
	for _, ref := range secretReferences {
		escapedRefs = append(escapedRefs, escapeSingleQuoteBackslash(ref))
	}
	yaml.WriteString("          GH_AW_SECRET_NAMES: '" + strings.Join(escapedRefs, ",") + "'\n")

	for _, secretName := range secretReferences {
		escapedSecretName := escapeSingleQuoteBackslash(secretName)
		yaml.WriteString("          SECRET_" + escapedSecretName + ": ${{ secrets." + secretName + " }}\n")
	}
}

// customSecretMaskingStepIDs preserves explicit IDs and assigns stable, non-conflicting
// IDs to unnamed hooks so their actual outcomes can gate artifact uploads.
func customSecretMaskingStepIDs(data *WorkflowData) []string {
	if data.SecretMasking == nil {
		return nil
	}
	ids := make([]string, 0, len(data.SecretMasking.Steps))
	used := map[string]struct{}{"redact_secrets": {}}
	for _, step := range data.SecretMasking.Steps {
		id := ""
		if configuredID, ok := step["id"].(string); ok {
			id = configuredID
		}
		ids = append(ids, id)
		if id != "" {
			used[id] = struct{}{}
		}
	}
	for i, id := range ids {
		if id != "" {
			continue
		}
		base := fmt.Sprintf("redact_secrets_custom_%d", i)
		candidate := base
		for suffix := 1; ; suffix++ {
			if _, exists := used[candidate]; !exists {
				break
			}
			candidate = base + strings.Repeat("_", suffix)
		}
		ids[i] = candidate
		used[candidate] = struct{}{}
	}
	return ids
}

func secretRedactionSuccessCondition(data *WorkflowData) string {
	return secretMaskingSuccessCondition(data) + " && steps.validate_recovery_envelopes.outcome == 'success'"
}

func secretMaskingSuccessCondition(data *WorkflowData) string {
	var condition strings.Builder
	condition.WriteString("steps.redact_secrets.outcome == 'success'")
	for _, id := range customSecretMaskingStepIDs(data) {
		condition.WriteString(" && steps.")
		condition.WriteString(id)
		condition.WriteString(".outcome == 'success'")
	}
	return condition.String()
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
