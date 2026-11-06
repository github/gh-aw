// This file provides automatic sanitization of GitHub Actions expressions in run: steps.
//
// # Shell Injection Prevention
//
// When a workflow step's run: field contains GitHub Actions expressions like
// ${{ github.event.issue.title }} or ${{ steps.foo.outputs.bar }}, an attacker
// who controls those values can inject arbitrary shell commands — a technique known
// as template injection or script injection.
//
// The safe pattern is to bind the expression to an environment variable in the
// step's env: block and then reference the variable as $VAR_NAME in the shell
// script.  The environment variable assignment is handled by the GitHub Actions
// runner and the value is treated as data, not code.
//
// # What this file does
//
// sanitizeRunStepExpressions inspects a step map and, for every ${{ ... }}
// expression that appears directly in the run: field (outside quoted heredoc bodies),
// it:
//   1. Generates a deterministic GH_AW_ environment variable name for the expression.
//   2. Adds the expression as a value under env: for that step.
//   3. Replaces the inline ${{ ... }} occurrence in the run: script with the
//      corresponding $GH_AW_... shell variable reference.
//
// The function returns the sanitized step map, a slice of human-readable
// descriptions of every substitution made (one entry per unique expression), and
// a boolean indicating whether any changes were made.
//
// sanitizeCustomStepsYAML applies the same logic to a raw "steps:" YAML string as
// produced by the frontmatter parser, re-serialising each modified step back to YAML.
//
// # Warning behaviour
//
// Callers are expected to emit one compiler warning per unique expression that was
// extracted, describing what was changed and why, so that workflow authors are aware
// that their shell script was modified by the compiler.
//
// # Edge cases
//
// Shell comments and expanding heredoc bodies are sanitized because GitHub
// evaluates expressions before Bash sees them. Expressions in quoted heredocs
// cannot be rewritten without changing literal output, so validation rejects
// unsafe expressions that remain there.
// Scripts with unproven heredoc boundaries are left intact for validation to
// reject, rather than risking destructive replacements inside quoted data.

package workflow

import (
	"bytes"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/typeutil"
	"github.com/github/gh-aw/pkg/workflow/gradershell"
	"github.com/goccy/go-yaml"
	"mvdan.cc/sh/v3/syntax"
)

var runStepSanitizerLog = logger.New("workflow:run_step_sanitizer")

// sanitizedExpression holds the details of one expression extracted from a run: step.
type sanitizedExpression struct {
	// Original is the full ${{ ... }} expression as it appeared in the run: script.
	Original string
	// EnvVar is the generated GH_AW_… environment variable name.
	EnvVar string
	// Content is the trimmed expression body (without the ${{ }} wrapper).
	Content string
}

// sanitizeRunStepExpressions extracts unsafe ${{ ... }} expressions from the
// run: field of a step map and moves them into the step's env: block.
//
// Expressions in comments and expanding heredocs are extracted. Expressions in
// quoted heredocs are left in place because replacing them with shell variables
// would change their literal output; validation rejects unsafe expressions that remain.
//
// The returned step map is a shallow copy of the input with updated run: and env:
// fields.  The input map is not modified.
//
// Returns:
//   - the sanitized step map
//   - a slice of human-readable substitution descriptions (one per unique expression)
//   - true when at least one expression was extracted, false otherwise
func sanitizeRunStepExpressions(step map[string]any) (map[string]any, []string, bool) {
	runVal, ok := step["run"].(string)
	if !ok || !hasExpressionMarker(runVal) {
		return step, nil, false
	}
	regions, parsed := shellHeredocRegions(runVal, false)
	if !parsed {
		// Without reliable boundaries, rewriting could corrupt quoted data.
		// Leave the script intact for the fail-closed expression validators.
		return step, nil, false
	}

	// GitHub evaluates expressions before the shell sees comments or heredoc
	// bodies. Scan comments and expanding heredocs as well as executable code,
	// but leave quoted heredocs for validation to reject without rewriting them.
	scanContent := transformShellRegions(runVal, regions, func(content string) string { return content }, true)
	if !hasExpressionMarker(scanContent) {
		return step, nil, false
	}

	ordered := runStepExpressionsToExtract(scanContent)
	if len(ordered) == 0 {
		return step, nil, false
	}

	// Environment-variable expansion is not safe when its result is subsequently
	// parsed as shell code (for example, by eval or bash -c). Leave these expressions
	// intact so the compiler's raw run-expression validator rejects the step.
	if runStepUsesShellCodeEvaluation(runVal) {
		return step, nil, false
	}

	newEnv := mergeSanitizedRunStepEnv(step, ordered)

	// Replace every occurrence of each expression in the run: script.
	// Replacements are limited to non-quoted-heredoc regions: quoted heredocs
	// (e.g. << 'EOF') suppress shell variable expansion, so replacing
	// ${{ expr }} with $GH_AW_VAR inside them would write the literal variable
	// name to the output file instead of the expression value. Expressions
	// that appear exclusively inside quoted heredocs are never added to `ordered` (see
	// the scanContent check above), so they are left intact regardless.
	newRun := runVal
	for _, s := range ordered {
		newRun = replaceSanitizedRunStepExpression(newRun, s, step["shell"])
	}

	// Build the sanitized step as a shallow copy.
	sanitized := make(map[string]any, len(step))
	maps.Copy(sanitized, step)
	sanitized["run"] = newRun
	sanitized["env"] = newEnv

	return sanitized, sanitizedRunStepDescriptions(step, ordered), true
}

func runStepUsesShellCodeEvaluation(run string) bool {
	masked := maskShellActionsExpressions(run)
	tree, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(bytes.NewReader(masked), "")
	if err != nil {
		return true
	}

	expressions := ExpressionPattern.FindAllStringSubmatchIndex(run, -1)
	return gradershell.HasShellCodeEvaluation(tree, func(word *syntax.Word) bool {
		if _, literal := gradershell.LiteralWord(word); !literal {
			return true
		}
		start, end := int(word.Pos().Offset()), int(word.End().Offset())
		for _, expression := range expressions {
			if len(expression) >= 2 && start < expression[1] && expression[0] < end {
				return true
			}
		}
		return false
	})
}

func runStepExpressionsToExtract(scanContent string) []sanitizedExpression {
	// Find all distinct ${{ ... }} expressions in the expandable portions and comments.
	matches := ExpressionPattern.FindAllStringSubmatch(scanContent, -1)

	// Build a deduplicated, ordered list of expressions to extract.
	extractor := NewExpressionExtractor()
	seen := make(map[string]struct{})
	var ordered []sanitizedExpression

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		original := match[0] //nolint:uncheckedsliceindex // The submatch length is checked above.
		if setutil.Contains(seen, original) {
			continue
		}
		seen[original] = struct{}{}
		content := strings.TrimSpace(match[1]) //nolint:uncheckedsliceindex // The submatch length is checked above.
		envVar := extractor.generateEnvVarName(content)
		ordered = append(ordered, sanitizedExpression{
			Original: original,
			EnvVar:   envVar,
			Content:  content,
		})
	}

	// Sort longest expressions first to avoid partial replacements when one
	// expression is a substring of another.
	slices.SortFunc(ordered, func(a, b sanitizedExpression) int {
		switch {
		case len(a.Original) > len(b.Original):
			return -1
		case len(a.Original) < len(b.Original):
			return 1
		default:
			return 0
		}
	})
	return ordered
}

func mergeSanitizedRunStepEnv(step map[string]any, ordered []sanitizedExpression) map[string]any {
	// Merge extracted env vars into a copy of the existing env: map.
	// Collision handling:
	//   - If the generated key already exists with the same value → reuse as-is.
	//   - If it exists with a different value → pick an alternate name by appending
	//     a numeric suffix (_2, _3, …) so the original user-defined value is preserved.
	existingEnv, ok := step["env"].(map[string]any)
	if !ok {
		existingEnv = nil
	}
	newEnv := make(map[string]any, typeutil.SafeAllocationCapacity(len(existingEnv), len(ordered)))
	maps.Copy(newEnv, existingEnv)

	for i := range ordered {
		s := &ordered[i]
		if existingVal, exists := newEnv[s.EnvVar]; exists {
			if existingVal == s.Original {
				// Same expression already bound to this name — nothing to do.
				continue
			}
			// Collision with a different value: find the next available name.
			// Bound to 100 iterations to avoid any pathological infinite loop;
			// in practice a workflow will never have more than a handful of
			// GH_AW_ variables.
			const maxSuffixes = 100
			base := s.EnvVar
			resolved := false
			for suffix := 2; suffix <= maxSuffixes; suffix++ {
				candidate := fmt.Sprintf("%s_%d", base, suffix)
				if _, taken := newEnv[candidate]; !taken {
					s.EnvVar = candidate
					resolved = true
					break
				}
			}
			if !resolved {
				// Extremely unlikely: all 100 numeric suffixes are taken.
				// Log and skip this expression to avoid corrupting the env block.
				runStepSanitizerLog.Printf(
					"skipping extraction of %q: too many name collisions for %s",
					s.Original, base,
				)
				continue
			}
		}
		newEnv[s.EnvVar] = s.Original
	}
	return newEnv
}

func sanitizedRunStepDescriptions(step map[string]any, ordered []sanitizedExpression) []string {
	// Build human-readable descriptions for caller warnings.
	stepName, ok := step["name"].(string)
	if !ok {
		stepName = ""
	}
	var descriptions []string
	for _, s := range ordered {
		var msg string
		if stepName != "" {
			msg = fmt.Sprintf(
				"extracted ${{ %s }} from run: script in step %q into env var %s to prevent shell injection",
				s.Content, stepName, s.EnvVar,
			)
		} else {
			msg = fmt.Sprintf(
				"extracted ${{ %s }} from run: script into env var %s to prevent shell injection",
				s.Content, s.EnvVar,
			)
		}
		descriptions = append(descriptions, msg)
	}
	return descriptions
}

func replaceSanitizedRunStepExpression(run string, expression sanitizedExpression, shellValue any) string {
	shell, shellIsString := shellValue.(string)
	powershell := shellIsString && (strings.EqualFold(shell, "pwsh") || strings.EqualFold(shell, "powershell"))
	regions, parsed := shellHeredocRegions(run, false)
	if !parsed {
		return run
	}
	return transformShellRegions(run, regions, func(segment string) string {
		var result strings.Builder
		for {
			before, after, found := strings.Cut(segment, expression.Original)
			if !found {
				result.WriteString(segment)
				break
			}
			result.WriteString(before)
			reference := "$" + expression.EnvVar
			first, _ := utf8.DecodeRuneInString(after)
			if !powershell && strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_", first) {
				// Bash otherwise consumes the adjacent suffix as part of the name.
				reference = "${" + expression.EnvVar + "}"
			}
			result.WriteString(reference)
			segment = after
		}
		return result.String()
	}, false)
}

// sanitizeCustomStepsYAML parses a raw "steps: ..." YAML string (as produced by
// the frontmatter parser), applies sanitizeRunStepExpressions to each step whose
// run: field contains GitHub Actions expressions, and re-serialises the result.
//
// The returned string is a replacement for the input customSteps that is safe to
// write to the generated workflow YAML.  The returned warnings slice contains one
// entry per unique expression extracted within each step (i.e. warnings are
// collected per step and appended; no global deduplication is performed across
// steps).
//
// When no expressions are found the original customSteps string is returned
// unchanged and the warnings slice will be empty.
// When the input cannot be parsed as YAML it is returned unchanged with a nil
// warnings slice and a nil error (the compiler will surface YAML errors later).
// When re-serialisation of the modified steps fails, the original string is
// returned unchanged and a non-nil error is returned.
func sanitizeCustomStepsYAML(customSteps string) (string, []string, error) {
	if !hasExpressionMarker(customSteps) {
		return customSteps, nil, nil
	}

	// Parse the "steps:" YAML block.
	var stepsDoc map[string]any
	if err := yaml.Unmarshal([]byte(customSteps), &stepsDoc); err != nil {
		// If we can't parse the YAML return it as-is; the compiler will surface any
		// YAML errors later during schema validation.
		runStepSanitizerLog.Printf("skipping run-step sanitization: failed to parse custom steps YAML: %v", err)
		return customSteps, nil, nil
	}

	rawSteps, ok := stepsDoc["steps"]
	if !ok {
		return customSteps, nil, nil
	}

	stepsSlice, ok := rawSteps.([]any)
	if !ok || len(stepsSlice) == 0 {
		return customSteps, nil, nil
	}

	var allWarnings []string
	anyChanged := false

	for i, item := range stepsSlice {
		stepMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		sanitized, warnings, changed := sanitizeRunStepExpressions(stepMap)
		if changed {
			stepsSlice[i] = sanitized
			allWarnings = append(allWarnings, warnings...)
			anyChanged = true
		}
	}

	if !anyChanged {
		return customSteps, nil, nil
	}

	// Re-serialise the modified steps.
	stepsDoc["steps"] = stepsSlice
	out, err := yaml.MarshalWithOptions(stepsDoc, DefaultMarshalOptions...)
	if err != nil {
		// Serialisation failure is unexpected; return the original string to avoid
		// silently dropping user-authored steps.
		return customSteps, nil, fmt.Errorf("failed to re-serialise sanitised steps: %w", err)
	}

	return string(out), allWarnings, nil
}
