// Package workflow provides GitHub Actions setup step generation for MCP servers.
//
// # MCP Setup Generator
//
// This file generates the complete setup sequence for MCP servers in GitHub Actions
// workflows. It orchestrates the initialization of all MCP tools including built-in
// servers (GitHub, Playwright, safe-outputs, safe-inputs) and custom HTTP/stdio
// MCP servers.
//
// Key responsibilities:
//   - Identifying and collecting MCP tools from workflow configuration
//   - Generating Docker image download steps
//   - Installing gh-aw extension for agentic-workflows tool
//   - Setting up safe-outputs MCP server (config, API key, HTTP server)
//   - Setting up safe-inputs MCP server (config, tool files, HTTP server)
//   - Starting Serena MCP server in local mode
//   - Starting the MCP gateway with proper environment variables
//   - Rendering MCP configuration for the selected AI engine
//
// Setup sequence:
//  1. Download required Docker images
//  2. Install gh-aw extension (if agentic-workflows enabled)
//  3. Write safe-outputs config files (config.json, tools.json, validation.json)
//  4. Generate and start safe-outputs HTTP server
//  5. Setup safe-inputs config and tool files (JavaScript, Python, Shell, Go)
//  6. Generate and start safe-inputs HTTP server
//  7. Start Serena local mode server
//  8. Start MCP gateway with all environment variables
//  9. Render engine-specific MCP configuration
//
// MCP tools supported:
//   - github: GitHub API access via MCP (local Docker or remote hosted)
//   - playwright: Browser automation with Playwright
//   - safe-outputs: Controlled output storage for AI agents
//   - safe-inputs: Custom tool execution with secret passthrough
//   - cache-memory: Memory/knowledge base management
//   - agentic-workflows: Workflow execution via gh-aw
//   - serena: Local Serena search functionality
//   - Custom HTTP/stdio MCP servers
//
// Gateway modes:
//   - Enabled (default): MCP servers run through gateway proxy
//   - Disabled (sandbox: false): Direct MCP server communication
//
// Related files:
//   - mcp_gateway_config.go: Gateway configuration management
//   - mcp_environment.go: Environment variable collection
//   - mcp_renderer.go: MCP configuration YAML rendering
//   - safe_outputs.go: Safe outputs server configuration
//   - safe_inputs.go: Safe inputs server configuration
//
// Example workflow setup:
//   - Download Docker images
//   - Write safe-outputs config to /opt/gh-aw/safeoutputs/
//   - Start safe-outputs HTTP server on port 3001
//   - Write safe-inputs config to /opt/gh-aw/safe-inputs/
//   - Start safe-inputs HTTP server on port 3000
//   - Start MCP gateway on port 80
//   - Render MCP config based on engine (copilot/claude/codex/custom)
package workflow

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var mcpSetupGeneratorLog = logger.New("workflow:mcp_setup_generator")

// generateMCPSetup generates the MCP server configuration setup
func (c *Compiler) generateMCPSetup(yaml *strings.Builder, tools map[string]any, engine CodingAgentEngine, workflowData *WorkflowData) {
	mcpSetupGeneratorLog.Print("Generating MCP server configuration setup")
	// Collect tools that need MCP server configuration
	var mcpTools []string

	// Check if workflowData is valid before accessing its fields
	if workflowData == nil {
		return
	}

	workflowTools := workflowData.Tools

	for toolName, toolValue := range workflowTools {
		// Skip if the tool is explicitly disabled (set to false)
		if toolValue == false {
			continue
		}
		// Standard MCP tools
		if toolName == "github" || toolName == "playwright" || toolName == "serena" || toolName == "cache-memory" || toolName == "agentic-workflows" {
			mcpTools = append(mcpTools, toolName)
		} else if mcpConfig, ok := toolValue.(map[string]any); ok {
			// Check if it's explicitly marked as MCP type in the new format
			if hasMcp, _ := hasMCPConfig(mcpConfig); hasMcp {
				mcpTools = append(mcpTools, toolName)
			}
		}
	}

	// Check if safe-outputs is enabled and add to MCP tools
	if HasSafeOutputsEnabled(workflowData.SafeOutputs) {
		mcpTools = append(mcpTools, "safe-outputs")
	}

	// Check if safe-inputs is configured and feature flag is enabled, add to MCP tools
	if IsSafeInputsEnabled(workflowData.SafeInputs, workflowData) {
		mcpTools = append(mcpTools, "safe-inputs")
	}

	// Populate dispatch-workflow file mappings before generating config
	// This ensures workflow_files is available in the config.json
	populateDispatchWorkflowFiles(workflowData, c.markdownPath)

	// Generate safe-outputs configuration once to avoid duplicate computation
	var safeOutputConfig string
	if HasSafeOutputsEnabled(workflowData.SafeOutputs) {
		safeOutputConfig = generateSafeOutputsConfig(workflowData)
	}

	// Sort tools to ensure stable code generation
	sort.Strings(mcpTools)

	if mcpSetupGeneratorLog.Enabled() {
		mcpSetupGeneratorLog.Printf("Collected %d MCP tools: %v", len(mcpTools), mcpTools)
	}

	// Ensure MCP gateway config has defaults set before collecting Docker images
	ensureDefaultMCPGatewayConfig(workflowData)

	// Collect all Docker images that will be used and generate download step
	dockerImages := collectDockerImages(tools, workflowData, c.actionMode)
	generateDownloadDockerImagesStep(yaml, dockerImages)

	// If no MCP tools, no configuration needed
	if len(mcpTools) == 0 {
		mcpSetupGeneratorLog.Print("No MCP tools configured, skipping MCP setup")
		return
	}

	// Install gh-aw extension if agentic-workflows tool is enabled
	hasAgenticWorkflows := false
	for _, toolName := range mcpTools {
		if toolName == "agentic-workflows" {
			hasAgenticWorkflows = true
			break
		}
	}

	// Check if shared/mcp/gh-aw.md is imported (which already installs gh-aw)
	hasGhAwImport := false
	for _, importPath := range workflowData.ImportedFiles {
		if strings.Contains(importPath, "shared/mcp/gh-aw.md") {
			hasGhAwImport = true
			break
		}
	}

	if hasAgenticWorkflows && hasGhAwImport {
		mcpSetupGeneratorLog.Print("Skipping gh-aw extension installation step (provided by shared/mcp/gh-aw.md import)")
	}

	// Only install gh-aw if needed and not already provided by imports
	if hasAgenticWorkflows && !hasGhAwImport {
		// Use effective token with precedence: top-level github-token > default
		effectiveToken := getEffectiveGitHubToken("", workflowData.GitHubToken)

		yaml.WriteString("      - name: Install gh-aw extension\n")
		yaml.WriteString("        env:\n")
		fmt.Fprintf(yaml, "          GH_TOKEN: %s\n", effectiveToken)
		yaml.WriteString("        run: |\n")
		yaml.WriteString("          # Check if gh-aw extension is already installed\n")
		yaml.WriteString("          if gh extension list | grep -q \"github/gh-aw\"; then\n")
		yaml.WriteString("            echo \"gh-aw extension already installed, upgrading...\"\n")
		yaml.WriteString("            gh extension upgrade gh-aw || true\n")
		yaml.WriteString("          else\n")
		yaml.WriteString("            echo \"Installing gh-aw extension...\"\n")
		yaml.WriteString("            gh extension install github/gh-aw\n")
		yaml.WriteString("          fi\n")
		yaml.WriteString("          gh aw --version\n")
		yaml.WriteString("          # Copy the gh-aw binary to /opt/gh-aw for MCP server containerization\n")
		yaml.WriteString("          mkdir -p /opt/gh-aw\n")
		yaml.WriteString("          GH_AW_BIN=$(which gh-aw 2>/dev/null || find ~/.local/share/gh/extensions/gh-aw -name 'gh-aw' -type f 2>/dev/null | head -1)\n")
		yaml.WriteString("          if [ -n \"$GH_AW_BIN\" ] && [ -f \"$GH_AW_BIN\" ]; then\n")
		yaml.WriteString("            cp \"$GH_AW_BIN\" /opt/gh-aw/gh-aw\n")
		yaml.WriteString("            chmod +x /opt/gh-aw/gh-aw\n")
		yaml.WriteString("            echo \"Copied gh-aw binary to /opt/gh-aw/gh-aw\"\n")
		yaml.WriteString("          else\n")
		yaml.WriteString("            echo \"::error::Failed to find gh-aw binary for MCP server\"\n")
		yaml.WriteString("            exit 1\n")
		yaml.WriteString("          fi\n")
	}

	// Write safe-outputs MCP server if enabled
	if HasSafeOutputsEnabled(workflowData.SafeOutputs) {
		// Step 1: Write config files (config.json, tools.json, validation.json)
		yaml.WriteString("      - name: Write Safe Outputs Config\n")
		yaml.WriteString("        run: |\n")
		yaml.WriteString("          mkdir -p /opt/gh-aw/safeoutputs\n")
		yaml.WriteString("          mkdir -p /tmp/gh-aw/safeoutputs\n")
		yaml.WriteString("          mkdir -p /tmp/gh-aw/mcp-logs/safeoutputs\n")

		// Write the safe-outputs configuration to config.json
		if safeOutputConfig != "" {
			yaml.WriteString("          cat > /opt/gh-aw/safeoutputs/config.json << 'EOF'\n")
			yaml.WriteString("          " + safeOutputConfig + "\n")
			yaml.WriteString("          EOF\n")
		}

		// Generate and write the filtered tools.json file
		filteredToolsJSON, err := generateFilteredToolsJSON(workflowData, c.markdownPath)
		if err != nil {
			mcpSetupGeneratorLog.Printf("Error generating filtered tools JSON: %v", err)
			// Fall back to empty array on error
			filteredToolsJSON = "[]"
		}
		yaml.WriteString("          cat > /opt/gh-aw/safeoutputs/tools.json << 'EOF'\n")
		// Write each line of the indented JSON with proper YAML indentation
		for _, line := range strings.Split(filteredToolsJSON, "\n") {
			yaml.WriteString("          " + line + "\n")
		}
		yaml.WriteString("          EOF\n")

		// Generate and write the validation configuration from Go source of truth
		// Only include validation for activated safe output types to keep validation.json small
		var enabledTypes []string
		if safeOutputConfig != "" {
			var configMap map[string]any
			if err := json.Unmarshal([]byte(safeOutputConfig), &configMap); err == nil {
				for typeName := range configMap {
					enabledTypes = append(enabledTypes, typeName)
				}
			}
		}
		validationConfigJSON, err := GetValidationConfigJSON(enabledTypes)
		if err != nil {
			// Log error prominently - validation config is critical for safe output processing
			// The error will be caught at compile time if this ever fails
			mcpSetupGeneratorLog.Printf("CRITICAL: Error generating validation config JSON: %v - validation will not work correctly", err)
			validationConfigJSON = "{}"
		}
		yaml.WriteString("          cat > /opt/gh-aw/safeoutputs/validation.json << 'EOF'\n")
		// Write each line of the indented JSON with proper YAML indentation
		for _, line := range strings.Split(validationConfigJSON, "\n") {
			yaml.WriteString("          " + line + "\n")
		}
		yaml.WriteString("          EOF\n")

		// Note: The MCP server entry point (mcp-server.cjs) is now copied by actions/setup
		// from safe-outputs-mcp-server.cjs - no need to generate it here

		// Step 2: Generate API key and choose port for HTTP server
		yaml.WriteString("      - name: Generate Safe Outputs MCP Server Config\n")
		yaml.WriteString("        id: safe-outputs-config\n")
		yaml.WriteString("        run: |\n")
		yaml.WriteString("          # Generate a secure random API key (360 bits of entropy, 40+ chars)\n")
		yaml.WriteString("          # Mask immediately to prevent timing vulnerabilities\n")
		yaml.WriteString("          API_KEY=$(openssl rand -base64 45 | tr -d '/+=')\n")
		yaml.WriteString("          echo \"::add-mask::${API_KEY}\"\n")
		yaml.WriteString("          \n")
		fmt.Fprintf(yaml, "          PORT=%d\n", constants.DefaultMCPInspectorPort)
		yaml.WriteString("          \n")
		yaml.WriteString("          # Set outputs for next steps\n")
		yaml.WriteString("          {\n")
		yaml.WriteString("            echo \"safe_outputs_api_key=${API_KEY}\"\n")
		yaml.WriteString("            echo \"safe_outputs_port=${PORT}\"\n")
		yaml.WriteString("          } >> \"$GITHUB_OUTPUT\"\n")
		yaml.WriteString("          \n")
		yaml.WriteString("          echo \"Safe Outputs MCP server will run on port ${PORT}\"\n")
		yaml.WriteString("          \n")

		// Step 3: Start the HTTP server in the background
		yaml.WriteString("      - name: Start Safe Outputs MCP HTTP Server\n")
		yaml.WriteString("        id: safe-outputs-start\n")

		// Add env block with step outputs
		yaml.WriteString("        env:\n")
		yaml.WriteString("          DEBUG: '*'\n")
		yaml.WriteString("          GH_AW_SAFE_OUTPUTS_PORT: ${{ steps.safe-outputs-config.outputs.safe_outputs_port }}\n")
		yaml.WriteString("          GH_AW_SAFE_OUTPUTS_API_KEY: ${{ steps.safe-outputs-config.outputs.safe_outputs_api_key }}\n")
		yaml.WriteString("          GH_AW_SAFE_OUTPUTS_TOOLS_PATH: /opt/gh-aw/safeoutputs/tools.json\n")
		yaml.WriteString("          GH_AW_SAFE_OUTPUTS_CONFIG_PATH: /opt/gh-aw/safeoutputs/config.json\n")
		yaml.WriteString("          GH_AW_MCP_LOG_DIR: /tmp/gh-aw/mcp-logs/safeoutputs\n")

		yaml.WriteString("        run: |\n")
		yaml.WriteString("          # Environment variables are set above to prevent template injection\n")
		yaml.WriteString("          export DEBUG\n")
		yaml.WriteString("          export GH_AW_SAFE_OUTPUTS_PORT\n")
		yaml.WriteString("          export GH_AW_SAFE_OUTPUTS_API_KEY\n")
		yaml.WriteString("          export GH_AW_SAFE_OUTPUTS_TOOLS_PATH\n")
		yaml.WriteString("          export GH_AW_SAFE_OUTPUTS_CONFIG_PATH\n")
		yaml.WriteString("          export GH_AW_MCP_LOG_DIR\n")
		yaml.WriteString("          \n")

		// Call the bundled shell script to start the server
		yaml.WriteString("          bash /opt/gh-aw/actions/start_safe_outputs_server.sh\n")
		yaml.WriteString("          \n")
	}

	// Write safe-inputs MCP server if configured and feature flag is enabled
	// For stdio mode, we only write the files but don't start the HTTP server
	if IsSafeInputsEnabled(workflowData.SafeInputs, workflowData) {
		// Step 1: Write config files (JavaScript files are now copied by actions/setup)
		yaml.WriteString("      - name: Setup Safe Inputs Config\n")
		yaml.WriteString("        run: |\n")
		yaml.WriteString("          mkdir -p /opt/gh-aw/safe-inputs/logs\n")

		// Generate the tools.json configuration file
		toolsJSON := generateSafeInputsToolsConfig(workflowData.SafeInputs)
		yaml.WriteString("          cat > /opt/gh-aw/safe-inputs/tools.json << 'EOF_TOOLS_JSON'\n")
		for _, line := range strings.Split(toolsJSON, "\n") {
			yaml.WriteString("          " + line + "\n")
		}
		yaml.WriteString("          EOF_TOOLS_JSON\n")

		// Generate the MCP server entry point
		safeInputsMCPServer := generateSafeInputsMCPServerScript(workflowData.SafeInputs)
		yaml.WriteString("          cat > /opt/gh-aw/safe-inputs/mcp-server.cjs << 'EOFSI'\n")
		for _, line := range FormatJavaScriptForYAML(safeInputsMCPServer) {
			yaml.WriteString(line)
		}
		yaml.WriteString("          EOFSI\n")
		yaml.WriteString("          chmod +x /opt/gh-aw/safe-inputs/mcp-server.cjs\n")
		yaml.WriteString("          \n")

		// Step 2: Generate tool files (js/py/sh)
		yaml.WriteString("      - name: Setup Safe Inputs Tool Files\n")
		yaml.WriteString("        run: |\n")

		// Generate individual tool files (sorted by name for stable code generation)
		safeInputToolNames := sliceutil.MapToSlice(workflowData.SafeInputs.Tools)
		sort.Strings(safeInputToolNames)

		for _, toolName := range safeInputToolNames {
			toolConfig := workflowData.SafeInputs.Tools[toolName]
			if toolConfig.Script != "" {
				// JavaScript tool
				toolScript := generateSafeInputJavaScriptToolScript(toolConfig)
				fmt.Fprintf(yaml, "          cat > /opt/gh-aw/safe-inputs/%s.cjs << 'EOFJS_%s'\n", toolName, toolName)
				for _, line := range FormatJavaScriptForYAML(toolScript) {
					yaml.WriteString(line)
				}
				fmt.Fprintf(yaml, "          EOFJS_%s\n", toolName)
			} else if toolConfig.Run != "" {
				// Shell script tool
				toolScript := generateSafeInputShellToolScript(toolConfig)
				fmt.Fprintf(yaml, "          cat > /opt/gh-aw/safe-inputs/%s.sh << 'EOFSH_%s'\n", toolName, toolName)
				for _, line := range strings.Split(toolScript, "\n") {
					yaml.WriteString("          " + line + "\n")
				}
				fmt.Fprintf(yaml, "          EOFSH_%s\n", toolName)
				fmt.Fprintf(yaml, "          chmod +x /opt/gh-aw/safe-inputs/%s.sh\n", toolName)
			} else if toolConfig.Py != "" {
				// Python script tool
				toolScript := generateSafeInputPythonToolScript(toolConfig)
				fmt.Fprintf(yaml, "          cat > /opt/gh-aw/safe-inputs/%s.py << 'EOFPY_%s'\n", toolName, toolName)
				for _, line := range strings.Split(toolScript, "\n") {
					yaml.WriteString("          " + line + "\n")
				}
				fmt.Fprintf(yaml, "          EOFPY_%s\n", toolName)
				fmt.Fprintf(yaml, "          chmod +x /opt/gh-aw/safe-inputs/%s.py\n", toolName)
			} else if toolConfig.Go != "" {
				// Go script tool
				toolScript := generateSafeInputGoToolScript(toolConfig)
				fmt.Fprintf(yaml, "          cat > /opt/gh-aw/safe-inputs/%s.go << 'EOFGO_%s'\n", toolName, toolName)
				for _, line := range strings.Split(toolScript, "\n") {
					yaml.WriteString("          " + line + "\n")
				}
				fmt.Fprintf(yaml, "          EOFGO_%s\n", toolName)
			}
		}
		yaml.WriteString("          \n")

		// Step 3: Generate API key and choose port for HTTP server
		yaml.WriteString("      - name: Generate Safe Inputs MCP Server Config\n")
		yaml.WriteString("        id: safe-inputs-config\n")
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
		yaml.WriteString("            echo \"safe_inputs_api_key=${API_KEY}\"\n")
		yaml.WriteString("            echo \"safe_inputs_port=${PORT}\"\n")
		yaml.WriteString("          } >> \"$GITHUB_OUTPUT\"\n")
		yaml.WriteString("          \n")
		yaml.WriteString("          echo \"Safe Inputs MCP server will run on port ${PORT}\"\n")
		yaml.WriteString("          \n")

		// Step 4: Start the HTTP server in the background
		yaml.WriteString("      - name: Start Safe Inputs MCP HTTP Server\n")
		yaml.WriteString("        id: safe-inputs-start\n")

		// Add env block with step outputs and tool-specific secrets
		// Security: Pass step outputs through environment variables to prevent template injection
		yaml.WriteString("        env:\n")
		yaml.WriteString("          DEBUG: '*'\n")
		yaml.WriteString("          GH_AW_SAFE_INPUTS_PORT: ${{ steps.safe-inputs-config.outputs.safe_inputs_port }}\n")
		yaml.WriteString("          GH_AW_SAFE_INPUTS_API_KEY: ${{ steps.safe-inputs-config.outputs.safe_inputs_api_key }}\n")

		safeInputsSecrets := collectSafeInputsSecrets(workflowData.SafeInputs)
		if len(safeInputsSecrets) > 0 {
			// Sort env var names for consistent output - using functional helper
			envVarNames := sliceutil.MapToSlice(safeInputsSecrets)
			sort.Strings(envVarNames)

			for _, envVarName := range envVarNames {
				secretExpr := safeInputsSecrets[envVarName]
				fmt.Fprintf(yaml, "          %s: %s\n", envVarName, secretExpr)
			}
		}

		yaml.WriteString("        run: |\n")
		yaml.WriteString("          # Environment variables are set above to prevent template injection\n")
		yaml.WriteString("          export DEBUG\n")
		yaml.WriteString("          export GH_AW_SAFE_INPUTS_PORT\n")
		yaml.WriteString("          export GH_AW_SAFE_INPUTS_API_KEY\n")
		yaml.WriteString("          \n")

		// Call the bundled shell script to start the server
		yaml.WriteString("          bash /opt/gh-aw/actions/start_safe_inputs_server.sh\n")
		yaml.WriteString("          \n")
	}

	// Generate Serena MCP server startup steps if in local mode
	if isSerenaInLocalMode(workflowData.ParsedTools) {
		generateSerenaLocalModeSteps(yaml)
	}

	// Skip gateway setup if sandbox is disabled
	// When sandbox: false, MCP servers are configured without the gateway
	if !isSandboxDisabled(workflowData) {
		// Use the engine's RenderMCPConfig method
		yaml.WriteString("      - name: Start MCP gateway\n")
		yaml.WriteString("        id: start-mcp-gateway\n")

		// Collect all MCP-related environment variables using centralized helper
		mcpEnvVars := collectMCPEnvironmentVariables(tools, mcpTools, workflowData, hasAgenticWorkflows)

		// Add env block if any environment variables are needed
		if len(mcpEnvVars) > 0 {
			yaml.WriteString("        env:\n")

			// Sort environment variable names for consistent output
			// Using functional helper to extract map keys
			envVarNames := sliceutil.MapToSlice(mcpEnvVars)
			sort.Strings(envVarNames)

			// Write environment variables in sorted order
			for _, envVarName := range envVarNames {
				envVarValue := mcpEnvVars[envVarName]
				fmt.Fprintf(yaml, "          %s: %s\n", envVarName, envVarValue)
			}
		}

		yaml.WriteString("        run: |\n")
		yaml.WriteString("          set -eo pipefail\n")
		yaml.WriteString("          mkdir -p /tmp/gh-aw/mcp-config\n")

		// Export gateway environment variables and build docker command BEFORE rendering MCP config
		// This allows the config to be piped directly to the gateway script
		// Per MCP Gateway Specification v1.0.0 section 4.2, variable expressions use "${VARIABLE_NAME}" syntax
		ensureDefaultMCPGatewayConfig(workflowData)
		gatewayConfig := workflowData.SandboxConfig.MCP

		port := gatewayConfig.Port
		if port == 0 {
			port = int(DefaultMCPGatewayPort)
		}

		domain := gatewayConfig.Domain
		if domain == "" {
			if workflowData.SandboxConfig.Agent != nil && workflowData.SandboxConfig.Agent.Disabled {
				domain = "localhost"
			} else {
				domain = "host.docker.internal"
			}
		}

		apiKey := gatewayConfig.APIKey

		yaml.WriteString("          \n")
		yaml.WriteString("          # Export gateway environment variables for MCP config and gateway script\n")
		yaml.WriteString("          export MCP_GATEWAY_PORT=\"" + fmt.Sprintf("%d", port) + "\"\n")
		yaml.WriteString("          export MCP_GATEWAY_DOMAIN=\"" + domain + "\"\n")

		// Generate API key with proper error handling (avoid SC2155)
		// Mask immediately after generation to prevent timing vulnerabilities
		if apiKey == "" {
			yaml.WriteString("          MCP_GATEWAY_API_KEY=$(openssl rand -base64 45 | tr -d '/+=')\n")
			yaml.WriteString("          echo \"::add-mask::${MCP_GATEWAY_API_KEY}\"\n")
			yaml.WriteString("          export MCP_GATEWAY_API_KEY\n")
		} else {
			yaml.WriteString("          export MCP_GATEWAY_API_KEY=\"" + apiKey + "\"\n")
			yaml.WriteString("          echo \"::add-mask::${MCP_GATEWAY_API_KEY}\"\n")
		}

		// Export payload directory and ensure it exists
		payloadDir := gatewayConfig.PayloadDir
		if payloadDir == "" {
			payloadDir = constants.DefaultMCPGatewayPayloadDir
		}
		yaml.WriteString("          export MCP_GATEWAY_PAYLOAD_DIR=\"" + payloadDir + "\"\n")
		yaml.WriteString("          mkdir -p \"${MCP_GATEWAY_PAYLOAD_DIR}\"\n")

		yaml.WriteString("          export DEBUG=\"*\"\n")
		yaml.WriteString("          \n")

		// Export engine type
		yaml.WriteString("          export GH_AW_ENGINE=\"" + engine.GetID() + "\"\n")

		// For Copilot engine with GitHub remote MCP, export GITHUB_PERSONAL_ACCESS_TOKEN
		// This is needed because the MCP gateway validates ${VAR} references in headers at config load time
		// and the Copilot MCP config uses ${GITHUB_PERSONAL_ACCESS_TOKEN} in the Authorization header
		githubTool, hasGitHub := tools["github"]
		if hasGitHub && getGitHubType(githubTool) == "remote" && engine.GetID() == "copilot" {
			yaml.WriteString("          export GITHUB_PERSONAL_ACCESS_TOKEN=\"$GITHUB_MCP_SERVER_TOKEN\"\n")
		}

		// Add user-configured environment variables
		if len(gatewayConfig.Env) > 0 {
			// Using functional helper to extract map keys
			envVarNames := sliceutil.MapToSlice(gatewayConfig.Env)
			sort.Strings(envVarNames)

			for _, envVarName := range envVarNames {
				envVarValue := gatewayConfig.Env[envVarName]
				fmt.Fprintf(yaml, "          export %s=%s\n", envVarName, envVarValue)
			}
		}

		// Build container command
		containerImage := gatewayConfig.Container
		if gatewayConfig.Version != "" {
			containerImage += ":" + gatewayConfig.Version
		} else {
			containerImage += ":" + string(constants.DefaultMCPGatewayVersion)
		}

		containerCmd := "docker run -i --rm --network host"
		containerCmd += " -v /var/run/docker.sock:/var/run/docker.sock" // Enable docker-in-docker for MCP gateway
		// Pass required gateway environment variables
		containerCmd += " -e MCP_GATEWAY_PORT"
		containerCmd += " -e MCP_GATEWAY_DOMAIN"
		containerCmd += " -e MCP_GATEWAY_API_KEY"
		containerCmd += " -e MCP_GATEWAY_PAYLOAD_DIR"
		containerCmd += " -e DEBUG"
		// Pass environment variables that MCP servers reference in their config
		// These are needed because awmg v0.0.12+ validates and resolves ${VAR} patterns at config load time
		// Environment variables used by MCP gateway
		containerCmd += " -e MCP_GATEWAY_LOG_DIR"
		// Environment variables used by safeoutputs MCP server
		containerCmd += " -e GH_AW_MCP_LOG_DIR"
		containerCmd += " -e GH_AW_SAFE_OUTPUTS"
		containerCmd += " -e GH_AW_SAFE_OUTPUTS_CONFIG_PATH"
		containerCmd += " -e GH_AW_SAFE_OUTPUTS_TOOLS_PATH"
		containerCmd += " -e GH_AW_ASSETS_BRANCH"
		containerCmd += " -e GH_AW_ASSETS_MAX_SIZE_KB"
		containerCmd += " -e GH_AW_ASSETS_ALLOWED_EXTS"
		containerCmd += " -e DEFAULT_BRANCH"
		// Environment variables used by GitHub MCP server
		containerCmd += " -e GITHUB_MCP_SERVER_TOKEN"
		// For Copilot engine with GitHub remote MCP, also pass GITHUB_PERSONAL_ACCESS_TOKEN
		// This allows the gateway to expand ${GITHUB_PERSONAL_ACCESS_TOKEN} references in headers
		if hasGitHub && getGitHubType(githubTool) == "remote" && engine.GetID() == "copilot" {
			containerCmd += " -e GITHUB_PERSONAL_ACCESS_TOKEN"
		}
		containerCmd += " -e GITHUB_MCP_LOCKDOWN"
		// Standard GitHub Actions environment variables (repository context)
		containerCmd += " -e GITHUB_REPOSITORY"
		containerCmd += " -e GITHUB_SERVER_URL"
		containerCmd += " -e GITHUB_SHA"
		containerCmd += " -e GITHUB_WORKSPACE"
		containerCmd += " -e GITHUB_TOKEN"
		// GitHub Actions run context
		containerCmd += " -e GITHUB_RUN_ID"
		containerCmd += " -e GITHUB_RUN_NUMBER"
		containerCmd += " -e GITHUB_RUN_ATTEMPT"
		containerCmd += " -e GITHUB_JOB"
		containerCmd += " -e GITHUB_ACTION"
		// GitHub Actions event context
		containerCmd += " -e GITHUB_EVENT_NAME"
		containerCmd += " -e GITHUB_EVENT_PATH"
		// GitHub Actions actor context
		containerCmd += " -e GITHUB_ACTOR"
		containerCmd += " -e GITHUB_ACTOR_ID"
		containerCmd += " -e GITHUB_TRIGGERING_ACTOR"
		// GitHub Actions workflow context
		containerCmd += " -e GITHUB_WORKFLOW"
		containerCmd += " -e GITHUB_WORKFLOW_REF"
		containerCmd += " -e GITHUB_WORKFLOW_SHA"
		// GitHub Actions ref context
		containerCmd += " -e GITHUB_REF"
		containerCmd += " -e GITHUB_REF_NAME"
		containerCmd += " -e GITHUB_REF_TYPE"
		containerCmd += " -e GITHUB_HEAD_REF"
		containerCmd += " -e GITHUB_BASE_REF"
		// Environment variables used by safeinputs MCP server
		// Only add if safe-inputs is actually enabled (has tools configured)
		if IsSafeInputsEnabled(workflowData.SafeInputs, workflowData) {
			containerCmd += " -e GH_AW_SAFE_INPUTS_PORT"
			containerCmd += " -e GH_AW_SAFE_INPUTS_API_KEY"
		}
		// Environment variables used by safeoutputs MCP server
		// Only add if safe-outputs is actually enabled (has tools configured)
		if HasSafeOutputsEnabled(workflowData.SafeOutputs) {
			containerCmd += " -e GH_AW_SAFE_OUTPUTS_PORT"
			containerCmd += " -e GH_AW_SAFE_OUTPUTS_API_KEY"
		}
		if len(gatewayConfig.Env) > 0 {
			// Using functional helper to extract map keys
			envVarNames := sliceutil.MapToSlice(gatewayConfig.Env)
			sort.Strings(envVarNames)
			for _, envVarName := range envVarNames {
				containerCmd += " -e " + envVarName
			}
		}

		// Add environment variables collected from HTTP MCP servers (e.g., TAVILY_API_KEY)
		// These are needed for the gateway to resolve ${VAR} references in MCP server configs
		if len(mcpEnvVars) > 0 {
			// Get list of environment variable names already added to avoid duplicates
			addedEnvVars := make(map[string]bool)

			// Mark standard environment variables as already added
			standardEnvVars := []string{
				"MCP_GATEWAY_PORT", "MCP_GATEWAY_DOMAIN", "MCP_GATEWAY_API_KEY", "MCP_GATEWAY_PAYLOAD_DIR", "DEBUG",
				"MCP_GATEWAY_LOG_DIR", "GH_AW_MCP_LOG_DIR", "GH_AW_SAFE_OUTPUTS",
				"GH_AW_SAFE_OUTPUTS_CONFIG_PATH", "GH_AW_SAFE_OUTPUTS_TOOLS_PATH",
				"GH_AW_ASSETS_BRANCH", "GH_AW_ASSETS_MAX_SIZE_KB", "GH_AW_ASSETS_ALLOWED_EXTS",
				"DEFAULT_BRANCH", "GITHUB_MCP_SERVER_TOKEN", "GITHUB_MCP_LOCKDOWN",
				"GITHUB_REPOSITORY", "GITHUB_SERVER_URL", "GITHUB_SHA", "GITHUB_WORKSPACE",
				"GITHUB_TOKEN", "GITHUB_RUN_ID", "GITHUB_RUN_NUMBER", "GITHUB_RUN_ATTEMPT",
				"GITHUB_JOB", "GITHUB_ACTION", "GITHUB_EVENT_NAME", "GITHUB_EVENT_PATH",
				"GITHUB_ACTOR", "GITHUB_ACTOR_ID", "GITHUB_TRIGGERING_ACTOR",
				"GITHUB_WORKFLOW", "GITHUB_WORKFLOW_REF", "GITHUB_WORKFLOW_SHA",
				"GITHUB_REF", "GITHUB_REF_NAME", "GITHUB_REF_TYPE", "GITHUB_HEAD_REF", "GITHUB_BASE_REF",
			}
			for _, envVar := range standardEnvVars {
				addedEnvVars[envVar] = true
			}

			// Mark conditionally added environment variables
			if hasGitHub && getGitHubType(githubTool) == "remote" && engine.GetID() == "copilot" {
				addedEnvVars["GITHUB_PERSONAL_ACCESS_TOKEN"] = true
			}
			if IsSafeInputsEnabled(workflowData.SafeInputs, workflowData) {
				addedEnvVars["GH_AW_SAFE_INPUTS_PORT"] = true
				addedEnvVars["GH_AW_SAFE_INPUTS_API_KEY"] = true
			}
			if HasSafeOutputsEnabled(workflowData.SafeOutputs) {
				addedEnvVars["GH_AW_SAFE_OUTPUTS_PORT"] = true
				addedEnvVars["GH_AW_SAFE_OUTPUTS_API_KEY"] = true
			}

			// Mark gateway config environment variables as added
			if len(gatewayConfig.Env) > 0 {
				for envVarName := range gatewayConfig.Env {
					addedEnvVars[envVarName] = true
				}
			}

			// Add remaining environment variables from mcpEnvVars
			var envVarNames []string
			for envVarName := range mcpEnvVars {
				if !addedEnvVars[envVarName] {
					envVarNames = append(envVarNames, envVarName)
				}
			}
			sort.Strings(envVarNames)

			for _, envVarName := range envVarNames {
				containerCmd += " -e " + envVarName
			}

			if mcpSetupGeneratorLog.Enabled() && len(envVarNames) > 0 {
				mcpSetupGeneratorLog.Printf("Added %d HTTP MCP environment variables to gateway container: %v", len(envVarNames), envVarNames)
			}
		}

		// Add volume mounts
		// First, add the payload directory mount (rw for both agent and gateway)
		if payloadDir != "" {
			containerCmd += " -v " + payloadDir + ":" + payloadDir + ":rw"
		}

		// Then add user-configured mounts
		if len(gatewayConfig.Mounts) > 0 {
			for _, mount := range gatewayConfig.Mounts {
				containerCmd += " -v " + mount
			}
		}

		// Add entrypoint override if specified
		if gatewayConfig.Entrypoint != "" {
			containerCmd += " --entrypoint " + shellEscapeArg(gatewayConfig.Entrypoint)
		}

		containerCmd += " " + containerImage

		if len(gatewayConfig.EntrypointArgs) > 0 {
			for _, arg := range gatewayConfig.EntrypointArgs {
				containerCmd += " " + shellEscapeArg(arg)
			}
		}

		if len(gatewayConfig.Args) > 0 {
			for _, arg := range gatewayConfig.Args {
				containerCmd += " " + shellEscapeArg(arg)
			}
		}

		// Build the export command with proper quoting that allows variable expansion
		// We need to break out of quotes for ${GITHUB_WORKSPACE} variables
		cmdWithExpandableVars := buildDockerCommandWithExpandableVars(containerCmd)
		yaml.WriteString("          export MCP_GATEWAY_DOCKER_COMMAND=" + cmdWithExpandableVars + "\n")
		yaml.WriteString("          \n")

		// Render MCP config - this will pipe directly to the gateway script
		engine.RenderMCPConfig(yaml, tools, mcpTools, workflowData)
	}
	// Note: When sandbox is disabled, gateway config will be nil and MCP config will be generated
	// without the gateway section. The engine's RenderMCPConfig handles both cases.
}
