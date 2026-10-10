// This file contains strict mode validation for secrets in custom steps.
//
// It validates that secrets expressions are not used in custom steps (pre-steps,
// steps, pre-agent-steps, and post-steps injected in the agent job). In strict mode, secrets in step-level
// env: bindings and with: inputs for uses: action steps are allowed (controlled
// binding, masked by GitHub Actions), while secrets in other fields (run, etc.)
// are treated as errors. In non-strict mode a warning is emitted instead.
//
// The goal is to minimise the number of secrets present in the agent job: the
// only secrets that should appear there are those required to configure the
// agentic engine itself.

package workflow

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/sliceutil"
)

// validateStepsSecrets checks the "pre-steps", "steps", "pre-agent-steps", and "post-steps" frontmatter sections
// for secrets expressions (e.g. ${{ secrets.MY_SECRET }}).
//
// In strict mode, secrets in step-level env: bindings and with: inputs for
// uses: action steps are allowed (controlled, masked binding), while secrets
// in other fields (run, etc.) are errors.
// In non-strict mode a warning is emitted for all secrets.
func (c *Compiler) validateStepsSecrets(frontmatter map[string]any) error {
	strictModeValidationLog.Printf("Validating secrets across steps sections: strictMode=%t", c.strictMode)
	for _, sectionName := range []string{"pre-steps", "steps", "pre-agent-steps", "post-steps"} {
		if err := c.validateStepsSectionSecrets(frontmatter, sectionName); err != nil {
			return err
		}
	}
	return nil
}

// validateStepsSectionSecrets inspects a single steps section (named by sectionName)
// inside frontmatter for any secrets.* expressions.
//
// In strict mode, secrets in step-level env: bindings and with: inputs for
// uses: action steps are allowed because they are controlled bindings that are
// automatically masked by GitHub Actions. Secrets in other step fields (run,
// etc.) are still treated as errors.
func (c *Compiler) validateStepsSectionSecrets(frontmatter map[string]any, sectionName string) error {
	rawValue, exists := frontmatter[sectionName]
	if !exists {
		strictModeValidationLog.Printf("No %s section found, skipping secrets validation", sectionName)
		return nil
	}

	steps, ok := rawValue.([]any)
	if !ok {
		strictModeValidationLog.Printf("%s section is not a list, skipping secrets validation", sectionName)
		return nil
	}

	// Separate secrets found in safe bindings (env: maps, with: maps in uses:
	// action steps) from secrets found in other fields (unsafe, potential leak).
	strictModeValidationLog.Printf("Classifying secrets in %s section: %d step(s)", sectionName, len(steps))
	var unsafeSecretRefs []string
	var safeSecretRefs []string
	for _, step := range steps {
		unsafe, safe := classifyStepSecrets(step)
		unsafeSecretRefs = append(unsafeSecretRefs, unsafe...)
		safeSecretRefs = append(safeSecretRefs, safe...)
	}

	// Filter out the built-in GITHUB_TOKEN: it is already present in every runner
	// environment and is not a user-defined secret that could be accidentally leaked.
	unsafeSecretRefs = filterBuiltinTokens(unsafeSecretRefs)
	safeSecretRefs = filterBuiltinTokens(safeSecretRefs)

	allSecretRefs := append(unsafeSecretRefs, safeSecretRefs...)

	if len(allSecretRefs) == 0 {
		strictModeValidationLog.Printf("No secrets found in %s section", sectionName)
		return nil
	}

	strictModeValidationLog.Printf("Found %d secret expression(s) in %s section: %d unsafe, %d in safe bindings",
		len(allSecretRefs), sectionName, len(unsafeSecretRefs), len(safeSecretRefs))

	if c.strictMode {
		// In strict mode, secrets in step-level env: bindings and with: inputs
		// for uses: action steps are allowed (controlled binding, masked by
		// GitHub Actions). Only block secrets found in other fields (run, etc.).
		if len(unsafeSecretRefs) == 0 {
			strictModeValidationLog.Printf("All secrets in %s section are in safe bindings (allowed in strict mode)", sectionName)
			return nil
		}

		unsafeSecretRefs = sliceutil.Deduplicate(unsafeSecretRefs)
		sort.Strings(unsafeSecretRefs)
		return fmt.Errorf(
			"strict mode: secrets expressions detected in '%s' section may be leaked to the agent job. Found: %s. "+
				"Operations requiring secrets must be moved to a separate job outside the agent job, "+
				"or use step-level env: bindings (for run: steps) or with: inputs (for uses: action steps) instead",
			sectionName, strings.Join(unsafeSecretRefs, ", "),
		)
	}

	// Non-strict mode: emit a warning for all secrets.
	allSecretRefs = sliceutil.Deduplicate(allSecretRefs)
	sort.Strings(allSecretRefs)
	strictModeValidationLog.Printf("Emitting non-strict warning for %d unique secret reference(s) in %s section", len(allSecretRefs), sectionName)
	warningMsg := fmt.Sprintf(
		"Warning: secrets expressions detected in '%s' section may be leaked to the agent job. Found: %s. "+
			"Consider moving operations requiring secrets to a separate job outside the agent job.",
		sectionName, strings.Join(allSecretRefs, ", "),
	)
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(warningMsg))
	c.IncrementWarningCount()

	return nil
}

// githubEnvWritePattern matches common patterns that write to $GITHUB_ENV,
// which would leak step-level env-bound secrets to subsequent steps.
// Covers: >> "$GITHUB_ENV", >> $GITHUB_ENV, >> ${GITHUB_ENV}
var githubEnvWritePattern = regexp.MustCompile(`(?i)GITHUB_ENV`)

// classifyStepSecrets separates secrets found in a step into two categories:
//   - unsafeRefs: secrets found in fields other than "env" or "with" (for uses:
//     action steps), or secrets in env:/with: bindings when the step also writes
//     to $GITHUB_ENV
//   - safeRefs: secrets found in step-level env: bindings (controlled, masked),
//     or in with: inputs for uses: action steps (passed to external actions,
//     masked by the runner)
//
// Only secrets in well-formed mappings (map[string]any) are classified as safe.
// Malformed values (string, slice, etc.) are treated as unsafe to prevent
// strict-mode bypass via invalid YAML like `env: "${{ secrets.TOKEN }}"`.
//
// Steps that reference $GITHUB_ENV in their run: command while also using
// safe-bound secrets are treated as entirely unsafe, because writing to
// $GITHUB_ENV would leak the secret to subsequent steps (including the agent).
func classifyStepSecrets(step any) (unsafeRefs, safeRefs []string) {
	stepMap, ok := step.(map[string]any)
	if !ok {
		// Non-map steps: all secrets are considered unsafe.
		return extractSecretsFromStepValue(step), nil
	}

	// Check if this is a uses: action step. For action steps, with: inputs are
	// passed to the external action (not interpolated into shell scripts), and
	// the GitHub Actions runner masks with: values derived from secrets.
	// Only treat with: as safe when uses is a valid non-empty string reference.
	usesVal, hasUses := stepMap["uses"]
	if hasUses {
		usesStr, isString := usesVal.(string)
		hasUses = isString && strings.TrimSpace(usesStr) != ""
	}

	var localUnsafe, localSafe []string
	for key, val := range stepMap {
		refs := extractSecretsFromStepValue(val)
		if key == "env" {
			if _, isMap := val.(map[string]any); isMap {
				localSafe = append(localSafe, refs...)
			} else {
				// Malformed env (string, slice, etc.): treat as unsafe.
				localUnsafe = append(localUnsafe, refs...)
			}
		} else if key == "with" && hasUses {
			if _, isMap := val.(map[string]any); isMap {
				localSafe = append(localSafe, refs...)
			} else {
				// Malformed with (string, slice, etc.): treat as unsafe.
				localUnsafe = append(localUnsafe, refs...)
			}
		} else {
			localUnsafe = append(localUnsafe, refs...)
		}
	}

	// If the step has safe-bound secrets AND references $GITHUB_ENV in any
	// non-env/non-with field, reclassify all safe refs as unsafe. Writing to
	// $GITHUB_ENV would persist the secret to subsequent steps.
	if len(localSafe) > 0 && stepReferencesGitHubEnv(stepMap) {
		localUnsafe = append(localUnsafe, localSafe...)
		localSafe = nil
	}

	return localUnsafe, localSafe
}

// extractSecretsFromStepValue recursively walks a step value (which may be a map,
// slice, or primitive) and returns all secrets.* expressions found in string values.
func extractSecretsFromStepValue(value any) []string {
	var refs []string
	switch v := value.(type) {
	case string:
		for _, expr := range ExtractSecretsFromValue(v) {
			refs = append(refs, expr)
		}
	case map[string]any:
		for _, fieldValue := range v {
			refs = append(refs, extractSecretsFromStepValue(fieldValue)...)
		}
	case []any:
		for _, item := range v {
			refs = append(refs, extractSecretsFromStepValue(item)...)
		}
	}
	return refs
}

// filterBuiltinTokens removes secret expressions that reference *only* GitHub's
// built-in GITHUB_TOKEN from the list. GITHUB_TOKEN is automatically provided by
// the runner environment and is not a user-defined secret; it therefore does not
// represent an accidental leak into the agent job.
//
// Expressions that reference GITHUB_TOKEN alongside other secrets (e.g.
// "${{ secrets.GITHUB_TOKEN && secrets.OTHER }}") are NOT filtered, because the
// other secret still represents a potential leak. Expressions referencing secrets
// whose names merely start with GITHUB_TOKEN (e.g. secrets.GITHUB_TOKEN_SUFFIX)
// are also NOT filtered.
func filterBuiltinTokens(refs []string) []string {
	out := refs[:0:0]
	for _, ref := range refs {
		names := secretsNamePattern.FindAllStringSubmatch(ref, -1)
		allBuiltin := len(names) > 0
		for _, m := range names {
			if len(m) >= 2 && m[1] != "GITHUB_TOKEN" {
				allBuiltin = false
				break
			}
		}
		if !allBuiltin {
			out = append(out, ref)
		}
	}
	return out
}

// stepReferencesGitHubEnv returns true if any non-env, non-with field in the
// step map contains a reference to GITHUB_ENV (e.g. in a run: command that
// writes to it). Both env: and with: are safe binding surfaces, so their
// values are excluded from GITHUB_ENV leak detection.
func stepReferencesGitHubEnv(stepMap map[string]any) bool {
	for key, val := range stepMap {
		if key == "env" || key == "with" {
			continue
		}
		if valueReferencesGitHubEnv(val) {
			return true
		}
	}
	return false
}

// valueReferencesGitHubEnv recursively checks whether a value contains a
// reference to GITHUB_ENV.
func valueReferencesGitHubEnv(value any) bool {
	switch v := value.(type) {
	case string:
		return githubEnvWritePattern.MatchString(v)
	case map[string]any:
		for _, fieldValue := range v {
			if valueReferencesGitHubEnv(fieldValue) {
				return true
			}
		}
	case []any:
		return slices.ContainsFunc(v, valueReferencesGitHubEnv)
	}
	return false
}
