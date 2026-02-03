package workflow

import (
	"strings"
)

// isSerenaInLocalMode checks if Serena tool is configured with local mode
func isSerenaInLocalMode(tools *ToolsConfig) bool {
	if tools == nil || tools.Serena == nil {
		return false
	}
	return tools.Serena.Mode == "local"
}

// generateSerenaLocalModeSteps generates steps to start Serena MCP server locally using uvx
func generateSerenaLocalModeSteps(yaml *strings.Builder) {
	// Step 1: Choose port for Serena HTTP server
	yaml.WriteString("      - name: Generate Serena MCP Server Config\n")
	yaml.WriteString("        id: serena-config\n")
	yaml.WriteString("        run: |\n")
	yaml.WriteString("          PORT=4000\n")
	yaml.WriteString("          \n")
	yaml.WriteString("          # Set output for next steps\n")
	yaml.WriteString("          echo \"serena_port=${PORT}\" >> \"$GITHUB_OUTPUT\"\n")
	yaml.WriteString("          \n")
	yaml.WriteString("          echo \"Serena MCP server will run on port ${PORT}\"\n")
	yaml.WriteString("          \n")

	// Step 2: Start the Serena HTTP server in the background using uvx
	yaml.WriteString("      - name: Start Serena MCP HTTP Server\n")
	yaml.WriteString("        id: serena-start\n")
	yaml.WriteString("        env:\n")
	yaml.WriteString("          DEBUG: '*'\n")
	yaml.WriteString("          GH_AW_SERENA_PORT: ${{ steps.serena-config.outputs.serena_port }}\n")
	yaml.WriteString("          GITHUB_WORKSPACE: ${{ github.workspace }}\n")
	yaml.WriteString("        run: bash /opt/gh-aw/actions/start_serena_server.sh\n")
}
