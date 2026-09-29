package workflow

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

func (c *Compiler) generateLedgerProjectionStep(yaml *strings.Builder, data *WorkflowData) {
	if data.LedgerConfig == nil || !data.LedgerConfig.Enabled() {
		return
	}
	ledgerConfig, _ := json.Marshal(data.LedgerConfig.Ledgers)
	yaml.WriteString("      - name: Create read-only ledger projections\n")
	yaml.WriteString("        id: ledger_projections\n")
	fmt.Fprintf(yaml, "        uses: %s\n", c.getActionPin("actions/github-script"))
	yaml.WriteString("        env:\n")
	yaml.WriteString("          GH_TOKEN: ${{ github.token }}\n")
	fmt.Fprintf(yaml, "          GH_AW_LEDGER_CONFIG_BASE64: %s\n", base64.StdEncoding.EncodeToString(ledgerConfig))
	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")
	yaml.WriteString("            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n")
	yaml.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	yaml.WriteString("            const { main } = require('" + SetupActionDestination + "/create_ledger_projection.cjs');\n")
	yaml.WriteString("            await main({ githubClient: github, owner: context.repo.owner, repo: context.repo.repo });\n")
}
