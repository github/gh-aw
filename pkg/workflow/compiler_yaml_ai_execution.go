package workflow

import (
	"fmt"
	"strings"
)

// generateEngineExecutionSteps generates the GitHub Actions steps for executing the AI engine
func (c *Compiler) generateEngineExecutionSteps(yaml *strings.Builder, data *WorkflowData, engine CodingAgentEngine, logFile string) {
	steps := engine.GetExecutionSteps(data, logFile)

	for _, step := range steps {
		for _, line := range step {
			yaml.WriteString(line + "\n")
		}
	}
}

// generateLogParsing generates a step that parses the agent's logs and adds them to the step summary
func (c *Compiler) generateLogParsing(yaml *strings.Builder, engine CodingAgentEngine) {
	parserScriptName := engine.GetLogParserScriptId()
	if parserScriptName == "" {
		// Skip log parsing if engine doesn't provide a parser
		compilerYamlLog.Printf("Skipping log parsing: engine %s has no parser script", engine.GetID())
		return
	}

	compilerYamlLog.Printf("Generating log parsing step for engine: %s (parser=%s)", engine.GetID(), parserScriptName)

	logParserScript := GetLogParserScript(parserScriptName)
	if logParserScript == "" {
		// Skip if parser script not found
		compilerYamlLog.Printf("Warning: parser script %s not found, skipping log parsing", parserScriptName)
		return
	}

	// Get the log file path for parsing (may be different from stdout/stderr log)
	logFileForParsing := engine.GetLogFileForParsing()

	yaml.WriteString("      - name: Parse agent logs for step summary\n")
	yaml.WriteString("        if: always()\n")
	fmt.Fprintf(yaml, "        uses: %s\n", GetActionPin("actions/github-script"))
	yaml.WriteString("        env:\n")
	fmt.Fprintf(yaml, "          GH_AW_AGENT_OUTPUT: %s\n", logFileForParsing)
	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")

	// Use the setup_globals helper to store GitHub Actions objects in global scope
	yaml.WriteString("            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n")
	yaml.WriteString("            setupGlobals(core, github, context, exec, io);\n")
	// Load log parser script from external file using require()
	yaml.WriteString("            const { main } = require('/opt/gh-aw/actions/" + parserScriptName + ".cjs');\n")
	yaml.WriteString("            await main();\n")
}

// generateSafeInputsLogParsing generates a step that parses safe-inputs logs and adds them to the step summary
func (c *Compiler) generateSafeInputsLogParsing(yaml *strings.Builder) {
	compilerYamlLog.Print("Generating safe-inputs log parsing step")

	yaml.WriteString("      - name: Parse safe-inputs logs for step summary\n")
	yaml.WriteString("        if: always()\n")
	fmt.Fprintf(yaml, "        uses: %s\n", GetActionPin("actions/github-script"))
	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")

	// Use the setup_globals helper to store GitHub Actions objects in global scope
	yaml.WriteString("            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n")
	yaml.WriteString("            setupGlobals(core, github, context, exec, io);\n")
	// Load safe-inputs log parser script from external file using require()
	yaml.WriteString("            const { main } = require('/opt/gh-aw/actions/parse_safe_inputs_logs.cjs');\n")
	yaml.WriteString("            await main();\n")
}

// generateMCPGatewayLogParsing generates a step that parses MCP gateway logs and adds them to the step summary
func (c *Compiler) generateMCPGatewayLogParsing(yaml *strings.Builder) {
	compilerYamlLog.Print("Generating MCP gateway log parsing step")

	yaml.WriteString("      - name: Parse MCP gateway logs for step summary\n")
	yaml.WriteString("        if: always()\n")
	fmt.Fprintf(yaml, "        uses: %s\n", GetActionPin("actions/github-script"))
	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")

	// Use the setup_globals helper to store GitHub Actions objects in global scope
	yaml.WriteString("            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n")
	yaml.WriteString("            setupGlobals(core, github, context, exec, io);\n")
	// Load MCP gateway log parser script from external file using require()
	yaml.WriteString("            const { main } = require('/opt/gh-aw/actions/parse_mcp_gateway_log.cjs');\n")
	yaml.WriteString("            await main();\n")
}

// generateStopMCPGateway generates a step that stops the MCP gateway process using its PID from step output
// It passes the gateway port and API key to enable graceful shutdown via /close endpoint
func (c *Compiler) generateStopMCPGateway(yaml *strings.Builder, data *WorkflowData) {
	compilerYamlLog.Print("Generating MCP gateway stop step")

	yaml.WriteString("      - name: Stop MCP gateway\n")
	yaml.WriteString("        if: always()\n")
	yaml.WriteString("        continue-on-error: true\n")

	// Add environment variables for graceful shutdown via /close endpoint
	// These values come from the Start MCP gateway step outputs
	// Security: Pass all step outputs through environment variables to prevent template injection
	yaml.WriteString("        env:\n")
	yaml.WriteString("          MCP_GATEWAY_PORT: ${{ steps.start-mcp-gateway.outputs.gateway-port }}\n")
	yaml.WriteString("          MCP_GATEWAY_API_KEY: ${{ steps.start-mcp-gateway.outputs.gateway-api-key }}\n")
	yaml.WriteString("          GATEWAY_PID: ${{ steps.start-mcp-gateway.outputs.gateway-pid }}\n")

	yaml.WriteString("        run: |\n")
	yaml.WriteString("          bash /opt/gh-aw/actions/stop_mcp_gateway.sh \"$GATEWAY_PID\"\n")
}

// convertGoPatternToJavaScript converts a Go regex pattern to JavaScript-compatible format
// This removes Go's (?i) inline case-insensitive flag since JavaScript doesn't support it
func (c *Compiler) convertGoPatternToJavaScript(goPattern string) string {
	// Convert (?i) inline case-insensitive flag by removing it
	// JavaScript RegExp will be created with "gi" flags to handle case insensitivity
	if strings.HasPrefix(goPattern, "(?i)") {
		return goPattern[4:] // Remove (?i) prefix
	}
	return goPattern
}
