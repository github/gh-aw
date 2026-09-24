package workflow

import (
	"fmt"
	"strings"
)

// generateDynamicCheckoutSteps emits one runtime checkout step per expression-valued
// checkout declaration. GitHub Actions cannot expand an expression into a variable
// number of steps, so the bundled script performs the additional checkouts with git.
func (c *Compiler) generateDynamicCheckoutSteps(expressions []string, overrideToken string, persistCredentials bool) []string {
	var steps []string
	for index, expression := range expressions {
		var step strings.Builder
		fmt.Fprintf(&step, "      - name: Checkout dynamic repositories (%d)\n", index+1)
		fmt.Fprintf(&step, "        uses: %s\n", c.getActionPin("actions/github-script"))
		step.WriteString("        env:\n")
		fmt.Fprintf(&step, "          GH_AW_DYNAMIC_CHECKOUTS: %s\n", wrapExpressionWithToJSON(expression))
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

func buildDynamicCheckoutsPromptContent(expressions []string) string {
	if len(expressions) == 0 {
		return ""
	}
	return "- **dynamic checkouts**: Additional repositories were selected and checked out at runtime. " +
		"Inspect the workspace directories and `$RUNNER_TEMP/gh-aw/safeoutputs/checkout-manifest.json` " +
		"to identify their repository names, paths, and current target. These checkouts are shallow and credential-free.\n"
}
