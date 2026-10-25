package workflow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var mcpSetupScriptsLog = logger.New("workflow:mcp_setup_scripts")

func generateMCPScriptsSetup(yaml *strings.Builder, workflowData *WorkflowData) error {
	if !IsMCPScriptsEnabled(workflowData.MCPScripts) {
		mcpSetupScriptsLog.Print("MCP scripts not enabled, skipping setup generation")
		return nil
	}

	mcpSetupScriptsLog.Printf("Generating MCP scripts setup: tools=%d", len(workflowData.MCPScripts.Tools))
	yaml.WriteString("      - name: Write MCP Scripts Config\n")
	yaml.WriteString("        # runner-guard:ignore RGS-018 -- writes first-party mcp-scripts tools.json manifest and mcp-server.cjs shim generated verbatim from the gh-aw compiler template, not an attacker-controlled dropper.\n")
	yaml.WriteString("        run: |\n")
	yaml.WriteString("          mkdir -p \"${RUNNER_TEMP}/gh-aw/mcp-scripts/logs\"\n")

	toolsJSON := GenerateMCPScriptsToolsConfig(workflowData.MCPScripts)
	toolsDelimiter := GenerateHeredocDelimiterFromContent("MCP_SCRIPTS_TOOLS", toolsJSON)
	if err := ValidateHeredocContent(toolsJSON, toolsDelimiter); err != nil {
		return fmt.Errorf("mcp-scripts tools.json: %w", err)
	}
	yaml.WriteString("          cat > \"${RUNNER_TEMP}/gh-aw/mcp-scripts/tools.json\" << '" + toolsDelimiter + "'\n") //nolint:generatedyamlheredoc // Legacy MCP script config rendering remains to be migrated.
	for line := range strings.SplitSeq(toolsJSON, "\n") {
		yaml.WriteString("          " + line + "\n")
	}
	yaml.WriteString("          " + toolsDelimiter + "\n")

	mcpScriptsMCPServer := GenerateMCPScriptsMCPServerScript(workflowData.MCPScripts)
	serverDelimiter := GenerateHeredocDelimiterFromContent("MCP_SCRIPTS_SERVER", mcpScriptsMCPServer)
	if err := ValidateHeredocContent(mcpScriptsMCPServer, serverDelimiter); err != nil {
		return fmt.Errorf("mcp-scripts mcp-server.cjs: %w", err)
	}
	yaml.WriteString("          cat > \"${RUNNER_TEMP}/gh-aw/mcp-scripts/mcp-server.cjs\" << '" + serverDelimiter + "'\n") //nolint:generatedyamlheredoc // Legacy MCP server rendering remains to be migrated.
	for _, line := range FormatJavaScriptForYAML(mcpScriptsMCPServer) {
		yaml.WriteString(line)
	}
	yaml.WriteString("          " + serverDelimiter + "\n")
	yaml.WriteString("          chmod +x \"${RUNNER_TEMP}/gh-aw/mcp-scripts/mcp-server.cjs\"\n")
	yaml.WriteString("          \n")

	if err := appendMCPScriptToolFiles(yaml, workflowData); err != nil {
		return err
	}
	appendMCPScriptsServerConfig(yaml)
	appendMCPScriptsServerStart(yaml, workflowData)
	return nil
}

func appendMCPScriptToolFiles(yaml *strings.Builder, workflowData *WorkflowData) error {
	yaml.WriteString("      - name: Write MCP Scripts Tool Files\n")
	yaml.WriteString("        run: |\n")
	mcpScriptToolNames := sliceutil.MapKeys(workflowData.MCPScripts.Tools)
	sort.Strings(mcpScriptToolNames)
	mcpSetupScriptsLog.Printf("Writing %d MCP script tool file(s)", len(mcpScriptToolNames))
	for _, toolName := range mcpScriptToolNames {
		toolConfig := workflowData.MCPScripts.Tools[toolName]
		if err := appendMCPScriptToolFile(yaml, workflowData, toolName, toolConfig); err != nil {
			return err
		}
	}
	yaml.WriteString("          \n")
	return nil
}

func appendMCPScriptsServerConfig(yaml *strings.Builder) {
	yaml.WriteString("      - name: Generate MCP Scripts Server Config\n")
	yaml.WriteString("        id: mcp-scripts-config\n")
	yaml.WriteString("        run: |\n")
	yaml.WriteString("          # Generate a secure random API key (360 bits of entropy, 40+ chars)\n")
	yaml.WriteString("          # Mask immediately to prevent timing vulnerabilities\n")
	yaml.WriteString("          API_KEY=$(openssl rand -base64 45 | tr -d '/+=')\n")
	yaml.WriteString("          echo \"::add-mask::${API_KEY}\"\n")
	yaml.WriteString("          \n")
	fmt.Fprintf(yaml, "          PORT=%d\n", constants.DefaultMCPServerPort)
	yaml.WriteString("          \n")
	yaml.WriteString("          # Set outputs for next steps\n")
	yaml.WriteString("          {\n")
	yaml.WriteString("            echo \"mcp_scripts_api_key=${API_KEY}\"\n")
	yaml.WriteString("            echo \"mcp_scripts_port=${PORT}\"\n")
	yaml.WriteString("          } >> \"$GITHUB_OUTPUT\"\n")
	yaml.WriteString("          \n")
	yaml.WriteString("          echo \"MCP Scripts server will run on port ${PORT}\"\n")
	yaml.WriteString("          \n")
}

func appendMCPScriptsServerStart(yaml *strings.Builder, workflowData *WorkflowData) {
	yaml.WriteString("      - name: Start MCP Scripts HTTP Server\n")
	yaml.WriteString("        id: mcp-scripts-start\n")
	yaml.WriteString("        env:\n")
	yaml.WriteString("          DEBUG: " + mcpDebugEnv + "\n")
	yaml.WriteString("          GH_AW_MCP_SCRIPTS_PORT: ${{ steps.mcp-scripts-config.outputs.mcp_scripts_port }}\n")
	yaml.WriteString("          GH_AW_MCP_SCRIPTS_API_KEY: ${{ steps.mcp-scripts-config.outputs.mcp_scripts_api_key }}\n")
	mcpScriptsSecrets := collectMCPScriptsSecrets(workflowData.MCPScripts)
	if len(mcpScriptsSecrets) > 0 {
		envVarNames := sliceutil.MapKeys(mcpScriptsSecrets)
		sort.Strings(envVarNames)
		for _, envVarName := range envVarNames {
			secretExpr := mcpScriptsSecrets[envVarName]
			fmt.Fprintf(yaml, "          %s: %s\n", envVarName, secretExpr)
		}
	}
	yaml.WriteString("        run: |\n")
	yaml.WriteString("          # Environment variables are set above to prevent template injection\n")
	yaml.WriteString("          export DEBUG\n")
	yaml.WriteString("          export GH_AW_MCP_SCRIPTS_PORT\n")
	yaml.WriteString("          export GH_AW_MCP_SCRIPTS_API_KEY\n")
	yaml.WriteString("          \n")
	yaml.WriteString("          bash \"${RUNNER_TEMP}/gh-aw/actions/start_mcp_scripts_server.sh\"\n")
	yaml.WriteString("          \n")
}

func appendMCPScriptToolFile(yaml *strings.Builder, workflowData *WorkflowData, toolName string, toolConfig *MCPScriptToolConfig) error {
	if toolConfig.Script != "" {
		mcpSetupScriptsLog.Printf("Appending MCP script tool %q (type=js)", toolName)
		toolScript := GenerateMCPScriptJavaScriptToolScript(toolConfig)
		jsDelimiter := GenerateHeredocDelimiterFromContent("MCP_SCRIPTS_JS_"+strings.ToUpper(toolName), toolScript)
		if err := ValidateHeredocContent(toolScript, jsDelimiter); err != nil {
			return fmt.Errorf("mcp-scripts tool %q (js): %w", toolName, err)
		}
		fmt.Fprintf(yaml, "          cat > \"${RUNNER_TEMP}/gh-aw/mcp-scripts/%s.cjs\" << '%s'\n", toolName, jsDelimiter) //nolint:generatedyamlheredoc // Legacy MCP tool rendering remains to be migrated.
		for _, line := range FormatJavaScriptForYAML(toolScript) {
			yaml.WriteString(line)
		}
		fmt.Fprintf(yaml, "          %s\n", jsDelimiter)
		return nil
	}
	if toolConfig.Run != "" {
		mcpSetupScriptsLog.Printf("Appending MCP script tool %q (type=sh)", toolName)
		toolScript := GenerateMCPScriptShellToolScript(toolConfig)
		shDelimiter := GenerateHeredocDelimiterFromContent("MCP_SCRIPTS_SH_"+strings.ToUpper(toolName), toolScript)
		if err := ValidateHeredocContent(toolScript, shDelimiter); err != nil {
			return fmt.Errorf("mcp-scripts tool %q (sh): %w", toolName, err)
		}
		fmt.Fprintf(yaml, "          cat > \"${RUNNER_TEMP}/gh-aw/mcp-scripts/%s.sh\" << '%s'\n", toolName, shDelimiter) //nolint:generatedyamlheredoc // Legacy MCP tool rendering remains to be migrated.
		for line := range strings.SplitSeq(toolScript, "\n") {
			yaml.WriteString("          " + line + "\n")
		}
		fmt.Fprintf(yaml, "          %s\n", shDelimiter)
		fmt.Fprintf(yaml, "          chmod +x \"${RUNNER_TEMP}/gh-aw/mcp-scripts/%s.sh\"\n", toolName)
		return nil
	}
	if toolConfig.Py != "" {
		mcpSetupScriptsLog.Printf("Appending MCP script tool %q (type=py)", toolName)
		toolScript := GenerateMCPScriptPythonToolScript(toolConfig)
		pyDelimiter := GenerateHeredocDelimiterFromContent("MCP_SCRIPTS_PY_"+strings.ToUpper(toolName), toolScript)
		if err := ValidateHeredocContent(toolScript, pyDelimiter); err != nil {
			return fmt.Errorf("mcp-scripts tool %q (py): %w", toolName, err)
		}
		fmt.Fprintf(yaml, "          cat > \"${RUNNER_TEMP}/gh-aw/mcp-scripts/%s.py\" << '%s'\n", toolName, pyDelimiter) //nolint:generatedyamlheredoc // Legacy MCP tool rendering remains to be migrated.
		for line := range strings.SplitSeq(toolScript, "\n") {
			yaml.WriteString("          " + line + "\n")
		}
		fmt.Fprintf(yaml, "          %s\n", pyDelimiter)
		fmt.Fprintf(yaml, "          chmod +x \"${RUNNER_TEMP}/gh-aw/mcp-scripts/%s.py\"\n", toolName)
		return nil
	}
	if toolConfig.Go != "" {
		mcpSetupScriptsLog.Printf("Appending MCP script tool %q (type=go)", toolName)
		toolScript := GenerateMCPScriptGoToolScript(toolConfig)
		goDelimiter := GenerateHeredocDelimiterFromContent("MCP_SCRIPTS_GO_"+strings.ToUpper(toolName), toolScript)
		if err := ValidateHeredocContent(toolScript, goDelimiter); err != nil {
			return fmt.Errorf("mcp-scripts tool %q (go): %w", toolName, err)
		}
		fmt.Fprintf(yaml, "          cat > \"${RUNNER_TEMP}/gh-aw/mcp-scripts/%s.go\" << '%s'\n", toolName, goDelimiter) //nolint:generatedyamlheredoc // Legacy MCP tool rendering remains to be migrated.
		for line := range strings.SplitSeq(toolScript, "\n") {
			yaml.WriteString("          " + line + "\n")
		}
		fmt.Fprintf(yaml, "          %s\n", goDelimiter)
	}
	return nil
}
