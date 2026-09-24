// This file provides validation for GitHub Actions expressions used in
// expression-valued `checkout:` declarations that reference secrets directly.
//
// # Dynamic Checkout Secrets Validation
//
// A dynamic checkout expression (e.g. checkout: ${{ fromJSON(inputs.checkouts) }})
// is serialized once and passed to the runtime checkout script as a single JSON
// payload (GH_AW_DYNAMIC_CHECKOUTS). If the expression itself references
// secrets.* (for example to embed a per-repository github-token value), the
// resolved secret value is written into that JSON payload rather than being
// assigned to its own statically declared environment variable. This makes the
// secret usage invisible to static analysis and to anyone auditing the compiled
// workflow for which secrets a step consumes.
//
// Instead, secrets needed by a dynamic checkout expression should be declared in
// the workflow's top-level `env:` section (or another step-scoped env var) and
// referenced via `env.NAME` inside the checkout expression. Workflow-level env
// vars are available in the `env` context for every job and step, so this keeps
// the secret usage statically visible while still allowing the expression to be
// resolved at runtime.
//
// The validation uses the same strict/non-strict pattern as other secret checks:
//   - In strict mode an error is returned.
//   - In non-strict mode a warning is printed and compilation continues.
//
// For the sibling "toJSON(secrets)" check, see expression_secrets_serialization_validation.go.

package workflow

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var dynamicCheckoutSecretsLog = logger.New("workflow:dynamic_checkout_secrets_validation")

// dynamicCheckoutSecretsDotPattern matches direct secrets.NAME references
// anywhere within an expression.
var dynamicCheckoutSecretsDotPattern = regexp.MustCompile(`\bsecrets\.[A-Za-z_][A-Za-z0-9_]*\b`)

// dynamicCheckoutSecretsBracketPattern matches direct bracket-indexed secrets
// references (e.g. secrets['MY_TOKEN'] or secrets["my-token"]) once the quoted
// key has been blanked out by maskQuotedExpressionLiterals, leaving only
// whitespace between the brackets.
var dynamicCheckoutSecretsBracketPattern = regexp.MustCompile(`\bsecrets\s*\[\s+\]`)

// findDynamicCheckoutSecretsExpressions returns the subset of the given checkout
// expressions that reference secrets.* directly, either via dot notation
// (secrets.NAME) or bracket notation (secrets['NAME']), excluding matches
// inside quoted string literals unrelated to secrets access.
func findDynamicCheckoutSecretsExpressions(expressions []string) []string {
	var found []string
	for _, expr := range expressions {
		masked := maskQuotedExpressionLiterals(expr)
		if dynamicCheckoutSecretsDotPattern.MatchString(masked) || dynamicCheckoutSecretsBracketPattern.MatchString(masked) {
			found = append(found, expr)
		}
	}
	return found
}

// validateDynamicCheckoutSecretsUsage scans expression-valued checkout
// declarations for direct secrets.* references. Referencing secrets directly in
// a dynamic checkout expression embeds their resolved values into the runtime
// GH_AW_DYNAMIC_CHECKOUTS JSON payload instead of a statically declared
// environment variable.
//
// In strict mode this returns an error; in non-strict mode it emits a warning to
// stderr and increments the compiler warning count.
func (c *Compiler) validateDynamicCheckoutSecretsUsage(workflowData *WorkflowData) error {
	found := findDynamicCheckoutSecretsExpressions(workflowData.CheckoutExpressions)
	if len(found) == 0 {
		dynamicCheckoutSecretsLog.Printf("No dynamic checkout secrets usage found")
		return nil
	}

	found = sliceutil.Deduplicate(found)
	sort.Strings(found)

	dynamicCheckoutSecretsLog.Printf("Detected %d dynamic checkout expression(s) referencing secrets directly: %v", len(found), found)

	msg := fmt.Sprintf(
		"dynamic checkout expression(s) reference secrets directly, embedding secret values into the runtime checkout JSON payload instead of a statically declared environment variable. "+
			"Found: %s. "+
			"Declare the secret in the workflow's top-level env: section and reference it via env.NAME inside the checkout expression instead.",
		strings.Join(found, ", "),
	)

	effectiveStrict := c.effectiveStrictMode(workflowData.RawFrontmatter)
	if effectiveStrict {
		return fmt.Errorf("strict mode: %s", msg)
	}

	fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Warning: "+msg))
	c.IncrementWarningCount()
	return nil
}
