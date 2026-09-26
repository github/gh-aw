package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Dynamic checkout compilation bridges expression-valued checkout declarations
// to runtime git operations and records metadata for agent guidance and safe
// output jobs through the checkout manifest.

// generateDynamicCheckoutSteps emits one runtime checkout step per expression-valued
// checkout declaration. GitHub Actions cannot expand an expression into a variable
// number of steps, so the bundled script performs the additional checkouts with git.
func (c *Compiler) generateDynamicCheckoutSteps(checkouts []DynamicCheckoutConfig, overrideToken string, persistCredentials bool) []string {
	var steps []string
	for index, checkout := range checkouts {
		var step strings.Builder
		fmt.Fprintf(&step, "      - name: Checkout dynamic repositories (%d)\n", index+1)
		fmt.Fprintf(&step, "        uses: %s\n", c.getActionPin("actions/github-script"))
		step.WriteString("        env:\n")
		fmt.Fprintf(&step, "          GH_AW_DYNAMIC_CHECKOUTS: %s\n", wrapExpressionWithToJSON(checkout.Expression))
		allowedReposExpression := ""
		if len(checkout.AllowedRepos) == 1 {
			for _, allowedRepoExpression := range checkout.AllowedRepos {
				if isExpression(allowedRepoExpression) {
					allowedReposExpression = allowedRepoExpression
				}
			}
		}
		if allowedReposExpression != "" {
			fmt.Fprintf(&step, "          GH_AW_DYNAMIC_CHECKOUT_ALLOWED_REPOS: %s\n", wrapExpressionWithToJSON(allowedReposExpression))
		} else if allowedReposJSON, err := json.Marshal(checkout.AllowedRepos); err == nil {
			writeYAMLEnv(&step, "          ", "GH_AW_DYNAMIC_CHECKOUT_ALLOWED_REPOS", string(allowedReposJSON))
		}
		step.WriteString("          GH_TOKEN: ${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}\n")
		if overrideToken != "" {
			fmt.Fprintf(&step, "          GH_AW_DYNAMIC_CHECKOUT_TOKEN: %s\n", overrideToken)
		}
		fmt.Fprintf(&step, "          GH_AW_DYNAMIC_CHECKOUT_PERSIST_CREDENTIALS: %t\n", persistCredentials)
		step.WriteString("        with:\n")
		step.WriteString("          script: |\n")
		step.WriteString("            const { setupGlobals } = require('${{ runner.temp }}/gh-aw/actions/setup_globals.cjs');\n")
		step.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
		step.WriteString("            const { main } = require('${{ runner.temp }}/gh-aw/actions/dynamic_checkouts.cjs');\n")
		step.WriteString("            await main();\n")
		steps = append(steps, step.String())
	}
	return steps
}

func buildDynamicCheckoutsPromptContent(checkouts []DynamicCheckoutConfig) string {
	if len(checkouts) == 0 {
		return ""
	}
	return "- **dynamic checkouts**: Additional repositories were selected and checked out at runtime. " +
		"Inspect the workspace directories and `$RUNNER_TEMP/gh-aw/safeoutputs/checkout-manifest.json` " +
		"to identify their repository names and paths. These checkouts are shallow and credential-free.\n"
}
