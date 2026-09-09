// This file contains shell script heuristic validation for custom steps.
//
// It applies a set of crude, pattern-based checks to the run: scripts in
// step sections (pre-steps, steps, pre-agent-steps, post-steps) to catch
// common agent mistakes that would otherwise only be detected at runtime.
//
// Current rules:
//   - gh-cli-missing-token: if a step's run: script uses the gh CLI,
//     its env: section must define GH_TOKEN.
//   - runner-tool-cache-in-run: runner.tool_cache must be passed through an
//     env: entry instead of interpolated directly into a run: script.

package workflow

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var stepShellValidatorLog = logger.New("workflow:step_shell_validator")

// ghCLIPattern detects invocations of the gh CLI in shell run scripts.
// It matches "gh" as a command token at the start of a line (optionally
// preceded by whitespace) or immediately after common shell operators
// (&&, ||, ;, |), followed by whitespace or end-of-line.
//
// This heuristic avoids matching "gh" inside echo strings or variable names
// while still catching the common forms:
//
//	gh issue list
//	echo foo && gh pr create
//	set -e; gh release create
//
// False negatives: command substitutions like $(gh repo list) are not matched
// by this pattern, which is acceptable for this crude validator.
var ghCLIPattern = regexp.MustCompile(`(?m)(?:^|&&|\|\||;|\|)\s*gh(?:\s|$)`)

var runnerToolCacheInRunPattern = regexp.MustCompile(`\$\{\{\s*runner\.tool_cache\s*\}\}`)

// validateStepShellScripts checks the "pre-steps", "steps", "pre-agent-steps",
// and "post-steps" frontmatter sections for common shell script mistakes.
//
// Current rules:
//   - gh-cli-missing-token: any step whose run: script invokes the gh CLI
//     must define GH_TOKEN in its env: section to avoid authentication
//     failures at runtime.
//   - runner-tool-cache-in-run: runner.tool_cache must be passed through an
//     env: entry instead of interpolated directly into a run: script.
//
// Detection uses a line-oriented heuristic: "gh" is matched as a command
// token at the start of a line or after shell operators (&&, ||, ;, |).
// Known limitation: command substitutions of the form $(gh ...) are not
// detected.
//
// In strict mode violations are errors; in non-strict mode they are warnings.
func (c *Compiler) validateStepShellScripts(frontmatter map[string]any) error {
	stepShellValidatorLog.Printf("Validating step shell scripts: strictMode=%t", c.strictMode)
	for _, sectionName := range []string{"pre-steps", "steps", "pre-agent-steps", "post-steps"} {
		if err := c.validateStepShellScriptsSection(frontmatter, sectionName); err != nil {
			return err
		}
	}
	return nil
}

// validateStepShellScriptsSection inspects a single steps section for shell
// script heuristic violations and returns an error (strict mode) or emits a
// warning (non-strict mode) for each violation found.
func (c *Compiler) validateStepShellScriptsSection(frontmatter map[string]any, sectionName string) error {
	rawValue, exists := frontmatter[sectionName]
	if !exists {
		stepShellValidatorLog.Printf("No %s section found, skipping shell validation", sectionName)
		return nil
	}

	steps, ok := rawValue.([]any)
	if !ok {
		stepShellValidatorLog.Printf("%s section is not a list, skipping shell validation", sectionName)
		return nil
	}

	workflowHasGHToken := workflowEnvHasGHToken(frontmatter)

	var violations []string
	for _, step := range steps {
		if v := checkStepGHToken(step, workflowHasGHToken); v != "" {
			violations = append(violations, v)
		}
		if v := checkStepRunnerToolCache(step); v != "" {
			violations = append(violations, v)
		}
	}

	if len(violations) == 0 {
		stepShellValidatorLog.Printf("No shell script violations found in %s section", sectionName)
		return nil
	}

	stepShellValidatorLog.Printf("Found %d shell script violation(s) in %s section", len(violations), sectionName)

	message := fmt.Sprintf("shell script validation failed in '%s'. Affected step(s): %s", sectionName, strings.Join(violations, ", "))

	if c.strictMode {
		return fmt.Errorf("strict mode: %s", message)
	}

	fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Warning: "+message))
	c.IncrementWarningCount()
	return nil
}

// checkStepRunnerToolCache returns a non-empty description string if a step's
// run: script directly interpolates runner.tool_cache. The expression must be
// passed through env: so it is treated as data rather than shell code.
func checkStepRunnerToolCache(step any) string {
	stepMap, ok := step.(map[string]any)
	if !ok {
		return ""
	}

	runVal, hasRun := stepMap["run"]
	if !hasRun {
		return ""
	}
	runStr, ok := runVal.(string)
	if !ok || !runnerToolCacheInRunPattern.MatchString(runStr) {
		return ""
	}

	stepName := "(unnamed step)"
	if nameVal, ok := stepMap["name"]; ok {
		if nameStr, ok := nameVal.(string); ok && nameStr != "" {
			stepName = fmt.Sprintf("%q", nameStr)
		}
	}
	return fmt.Sprintf(
		"%s directly interpolates runner.tool_cache in its run: script. "+
			"Pass it through the step's env: (for example, `GH_AW_RUNNER_TOOL_CACHE: ${{ runner.tool_cache }}`) "+
			"and reference `$GH_AW_RUNNER_TOOL_CACHE` in the shell script",
		stepName,
	)
}

// checkStepGHToken returns a non-empty description string if the step uses the
// gh CLI in its run: script without defining GH_TOKEN in its env: section and
// without inheriting GH_TOKEN from the workflow-level env: section.
// Returns an empty string when the step is compliant or when the rule does not
// apply (no run: field, not a map, etc.).
func checkStepGHToken(step any, workflowHasGHToken bool) string {
	stepMap, ok := step.(map[string]any)
	if !ok {
		return ""
	}

	runVal, hasRun := stepMap["run"]
	if !hasRun {
		return ""
	}
	runStr, ok := runVal.(string)
	if !ok {
		return ""
	}

	if !ghCLIPattern.MatchString(runStr) {
		return ""
	}

	// gh CLI is used — verify GH_TOKEN is present in env.
	if workflowHasGHToken || stepEnvHasGHToken(stepMap) {
		return ""
	}

	// Build a human-readable identifier for the step for the error message.
	if nameVal, ok := stepMap["name"]; ok {
		if nameStr, ok := nameVal.(string); ok && nameStr != "" {
			return fmt.Sprintf("%q", nameStr)
		}
	}
	return "(unnamed step)"
}

// workflowEnvHasGHToken returns true when the workflow-level env: section is a
// map that contains a GH_TOKEN key.
func workflowEnvHasGHToken(frontmatter map[string]any) bool {
	envVal, exists := frontmatter["env"]
	if !exists {
		return false
	}
	envMap, ok := envVal.(map[string]any)
	if !ok {
		return false
	}
	_, hasToken := envMap["GH_TOKEN"]
	return hasToken
}

// stepEnvHasGHToken returns true when the step's env: section is a map that
// contains a GH_TOKEN key.
func stepEnvHasGHToken(stepMap map[string]any) bool {
	envVal, exists := stepMap["env"]
	if !exists {
		return false
	}
	envMap, ok := envVal.(map[string]any)
	if !ok {
		return false
	}
	_, hasToken := envMap["GH_TOKEN"]
	return hasToken
}
