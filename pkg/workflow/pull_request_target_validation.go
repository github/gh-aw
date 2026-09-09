// This file provides validation for pull_request_target trigger security.
//
// # pull_request_target Trigger Validation
//
// The pull_request_target trigger runs workflows in the context of the base
// (target) branch with full write permissions and access to repository secrets.
// Unlike pull_request, it can access secrets from fork PRs, making it extremely
// dangerous when combined with a checkout of PR code.
//
// # Validation Rules
//
//  1. In strict mode: always emit a warning that pull_request_target is a very
//     dangerous trigger, even when checkout: false is set, because the workflow
//     still runs with full write permissions and secret access.
//     Workflows can opt out by setting strict: false in frontmatter.
//
//  2. When checkout is NOT explicitly disabled (checkout: false not set):
//     - In strict mode: return a hard error (extremely insecure).
//     - In non-strict mode: emit a warning.
//
// # References
//
// See: https://securitylab.github.com/resources/github-actions-preventing-pwn-requests/
//
// # When to Add Validation Here
//
// Add validation to this file when:
//   - It validates pull_request_target-specific security requirements.
//   - It enforces checkout restrictions for this trigger type.
//
// For general validation, see validation.go.
// For detailed documentation, see scratchpad/validation-architecture.md

package workflow

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/github/gh-aw/pkg/logger"
)

var pullRequestTargetLog = logger.New("workflow:pull_request_target_validation")

// [^{}]+? deliberately excludes brace characters so nested expression constructs
// are never treated as a trusted literal allowlist match.
var pullRequestTargetGitHubExpressionPattern = regexp.MustCompile(`^\$\{\{\s*([^{}]+?)\s*\}\}$`)

// validatePullRequestTargetTrigger validates security requirements for pull_request_target triggers.
//
// The pull_request_target trigger runs with full write permissions and repository secret access
// on the base branch. When checkout is not explicitly disabled (checkout: false), the workflow
// may execute untrusted PR code with elevated privileges — a critical security vulnerability
// commonly known as a "pwn request" attack.
//
// In strict mode, a warning is always emitted that pull_request_target is inherently dangerous
// even with checkout disabled, since the workflow still runs with elevated permissions.
// When the workflow frontmatter sets strict: false, effectiveStrictMode is lowered so the
// dangerous-trigger strict-only warning is skipped; the insecure-checkout check still runs
// and emits a non-strict warning when checkout is not explicitly disabled.
func (c *Compiler) validatePullRequestTargetTrigger(workflowData *WorkflowData, markdownPath string) error { //nolint:largefunc // Trigger checks share parsed state and diagnostics.
	// Fast path: skip expensive YAML parsing when the On field cannot possibly contain
	// a pull_request_target trigger. This avoids yaml.Unmarshal on every
	// validateWorkflowData call for the common case of non-pull_request_target workflows.
	// The YAML parsing below is the authoritative check — the fast path only provides
	// early exit when the literal string is absent. If the string appears as part of a
	// longer YAML key (e.g. pull_request_target_staging), the YAML parse will correctly
	// find no "pull_request_target" key and return nil, so there are no false positives.
	if !strings.Contains(workflowData.On, "pull_request_target") {
		return nil
	}

	pullRequestTargetLog.Print("Validating pull_request_target trigger security")

	// Parse the On field as YAML to confirm pull_request_target is actually a trigger key.
	var parsedData map[string]any
	if err := yaml.Unmarshal([]byte(workflowData.On), &parsedData); err != nil {
		pullRequestTargetLog.Printf("Could not parse On field as YAML: %v", err)
		return nil
	}

	onData, hasOn := parsedData["on"]
	if !hasOn {
		return nil
	}

	onMap, isMap := onData.(map[string]any)
	if !isMap {
		return nil
	}

	_, hasPRT := onMap["pull_request_target"]
	if !hasPRT {
		return nil
	}

	effectiveStrictMode := c.effectiveStrictMode(workflowData.RawFrontmatter)

	// In strict mode, always emit a warning that pull_request_target is a very dangerous trigger,
	// regardless of whether checkout is disabled. The workflow still runs with full write
	// permissions and has access to all repository secrets.
	if effectiveStrictMode {
		pullRequestTargetLog.Print("Emitting strict mode warning: pull_request_target is a very dangerous trigger")
		warningMsg := "pull_request_target is a very dangerous trigger.\n" +
			"This event runs with full write permissions and access to all repository secrets.\n" +
			"Unlike pull_request, it runs in the context of the target (base) branch, giving\n" +
			"the workflow elevated access even for PRs from untrusted fork contributors.\n" +
			"Even with checkout: false, consider whether pull_request_target is truly necessary.\n" +
			"If you only need to react to PR events without write access, use pull_request instead.\n" +
			"See: https://securitylab.github.com/resources/github-actions-preventing-pwn-requests/"
		fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning", warningMsg))
		c.IncrementWarningCount()
	}

	// If checkout was explicitly disabled by the user (checkout: false in frontmatter),
	// the workflow will not execute PR code — no further action needed.
	// Auto-disabled checkout (when no checkout key is present) does not count as explicit
	// acknowledgement of the security risk, so the warning/error is still emitted in that case.
	if workflowData.CheckoutExplicitlyDisabled {
		pullRequestTargetLog.Print("checkout: false is explicitly set by user, skipping insecure-checkout error")
		return nil
	}

	// Explicit checkout configurations that are pinned to the base repository/ref are considered
	// safe for pull_request_target because they do not execute untrusted PR head code.
	if hasOnlyTrustedPullRequestTargetCheckouts(workflowData.CheckoutConfigs) {
		pullRequestTargetLog.Print("checkout config is pinned to trusted base repository/ref, skipping insecure-checkout error")
		return nil
	}

	// Checkout is not disabled — the workflow may execute untrusted PR code with elevated privileges.
	pullRequestTargetLog.Print("checkout is NOT disabled, emitting pull_request_target insecure-checkout diagnostic")

	message := "pull_request_target trigger with checkout enabled is extremely insecure.\n\n" +
		"This event runs with full write permissions and access to repository secrets,\n" +
		"but the workflow will check out code from a potentially untrusted PR contributor.\n" +
		"This is a well-known attack vector: a fork PR can inject malicious code that\n" +
		"executes with access to your repository's secrets (\"pwn request\" attack).\n\n" +
		"Suggested fix: Use one of these safe patterns:\n" +
		"1) Disable checkout entirely:\n" +
		"checkout: false\n\n" +
		"2) Check out only the trusted base repo/ref:\n" +
		"checkout:\n" +
		"  repository: ${{ github.repository }}\n" +
		"  ref: ${{ github.event.pull_request.base.sha }}\n\n" +
		"3) Check out only the trusted base repository and omit ref:\n" +
		"checkout:\n" +
		"  repository: ${{ github.repository }}\n\n" +
		"You can also use 'ref: ${{ github.event.pull_request.base.ref }}'.\n" +
		"See: https://securitylab.github.com/resources/github-actions-preventing-pwn-requests/"

	if effectiveStrictMode {
		return formatCompilerError(markdownPath, "error", message, nil)
	}

	// Non-strict mode: emit a warning so existing workflows continue to compile.
	fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning", message))
	c.IncrementWarningCount()

	return nil
}

func hasOnlyTrustedPullRequestTargetCheckouts(configs []*CheckoutConfig) bool {
	if len(configs) == 0 {
		return false
	}
	for _, cfg := range configs {
		if !isTrustedPullRequestTargetCheckout(cfg) {
			return false
		}
	}
	return true
}

func isTrustedPullRequestTargetCheckout(cfg *CheckoutConfig) bool {
	if cfg == nil {
		return false
	}

	repository := strings.TrimSpace(cfg.Repository)
	if repository != "" && !matchesGitHubExpression(repository, "github.repository") {
		return false
	}

	ref := strings.TrimSpace(cfg.Ref)
	return ref == "" || matchesAnyGitHubExpression(ref,
		"github.event.pull_request.base.sha", // immutable commit SHA
		"github.event.pull_request.base.ref", // mutable branch tip, still trusted base code
	)
}

func matchesAnyGitHubExpression(value string, expectedExpressions ...string) bool {
	for _, expected := range expectedExpressions {
		if matchesGitHubExpression(value, expected) {
			return true
		}
	}
	return false
}

func matchesGitHubExpression(value string, expectedExpression string) bool {
	trimmed := strings.TrimSpace(value)
	matches := pullRequestTargetGitHubExpressionPattern.FindStringSubmatch(trimmed)
	if len(matches) != 2 {
		return false
	}
	return strings.TrimSpace(matches[1]) == expectedExpression
}
