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
//  1. In strict mode: emit a warning about elevated permissions and secret access,
//     unless on.pull_request_target.acknowledge-risk is explicitly true.
//
//  2. When checkout is neither explicitly disabled nor restricted to trusted
//     base-repository refs or literal on.pull_request_target.allowed-checkouts:
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

var pullRequestTargetLiteralRepositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var pullRequestTargetLiteralRefPattern = regexp.MustCompile(`^[^\s${}*?\[\\:~^]+$`)

// validatePullRequestTargetTrigger validates security requirements for pull_request_target triggers.
//
// The pull_request_target trigger runs with full write permissions and repository secret access
// on the base branch. When checkout is not explicitly disabled (checkout: false), the workflow
// may execute untrusted PR code with elevated privileges — a critical security vulnerability
// commonly known as a "pwn request" attack.
//
// In strict mode, a warning is emitted unless the trigger-specific risk is acknowledged,
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
	var acknowledged bool
	var allowedCheckouts []any
	if on, ok := workflowData.RawFrontmatter["on"].(map[string]any); ok {
		if trigger, ok := on["pull_request_target"].(map[string]any); ok {
			acknowledged = trigger["acknowledge-risk"] == true
			if entries, ok := trigger["allowed-checkouts"].([]any); ok {
				allowedCheckouts = entries
			}
		}
	}

	// In strict mode, emit a warning unless the trigger-specific risk is acknowledged,
	// regardless of whether checkout is disabled. The workflow still runs with full write
	// permissions and has access to all repository secrets.
	if effectiveStrictMode && !acknowledged {
		pullRequestTargetLog.Print("Emitting strict mode warning: pull_request_target is a very dangerous trigger")
		warningMsg := "pull_request_target is a very dangerous trigger.\n" +
			"This event runs with full write permissions and access to all repository secrets.\n" +
			"Unlike pull_request, it runs in the context of the target (base) branch, giving\n" +
			"the workflow elevated access even for PRs from untrusted fork contributors.\n" +
			"Even with checkout: false, consider whether pull_request_target is truly necessary.\n" +
			"If you only need to react to PR events without write access, use pull_request instead.\n" +
			"To acknowledge this risk only, set on.pull_request_target.acknowledge-risk: true.\n" +
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

	// Every checkout must target a supported base ref or a declared literal repository/ref pair.
	if hasOnlyTrustedPullRequestTargetCheckouts(workflowData.CheckoutConfigs, allowedCheckouts) {
		pullRequestTargetLog.Print("checkout config is restricted to trusted repository/ref pairs, skipping insecure-checkout error")
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
		"For a fixed external checkout, declare its literal repository/ref pair in\n" +
		"on.pull_request_target.allowed-checkouts and configure checkout to match exactly.\n" +
		"See: https://securitylab.github.com/resources/github-actions-preventing-pwn-requests/"

	if effectiveStrictMode {
		return formatCompilerError(markdownPath, "error", message, nil)
	}

	// Non-strict mode: emit a warning so existing workflows continue to compile.
	fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning", message))
	c.IncrementWarningCount()

	return nil
}

func hasOnlyTrustedPullRequestTargetCheckouts(configs []*CheckoutConfig, allowedCheckouts []any) bool {
	if len(configs) == 0 {
		return false
	}
	for _, cfg := range configs {
		if !isTrustedPullRequestTargetCheckout(cfg) && !isAllowedPullRequestTargetCheckout(cfg, allowedCheckouts) {
			return false
		}
	}
	return true
}

func isAllowedPullRequestTargetCheckout(cfg *CheckoutConfig, allowedCheckouts []any) bool {
	if cfg == nil || cfg.Wiki || len(cfg.Fetch) > 0 {
		return false
	}
	// Fail closed even when called without schema validation. Never allow expressions
	// or PR refs merely because the same string appears in the policy.
	if !pullRequestTargetLiteralRepositoryPattern.MatchString(cfg.Repository) ||
		!pullRequestTargetLiteralRefPattern.MatchString(cfg.Ref) ||
		strings.HasPrefix(cfg.Ref, "refs/pull/") {
		return false
	}
	for _, entry := range allowedCheckouts {
		pair, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		repository, repositoryOK := pair["repository"].(string)
		ref, refOK := pair["ref"].(string)
		if repositoryOK && refOK && repository == cfg.Repository && ref == cfg.Ref {
			return true
		}
	}
	return false
}

func isTrustedPullRequestTargetCheckout(cfg *CheckoutConfig) bool {
	if cfg == nil || len(cfg.Fetch) > 0 {
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
	const expressionCaptureIndex = 1
	trimmed := strings.TrimSpace(value)
	matches := pullRequestTargetGitHubExpressionPattern.FindStringSubmatch(trimmed)
	if len(matches) > expressionCaptureIndex {
		return strings.TrimSpace(matches[expressionCaptureIndex]) == expectedExpression
	}
	return false
}
