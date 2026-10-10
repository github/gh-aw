package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/constants"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/workflow"
)

// writeMCPScriptsFiles writes all mcp-scripts MCP server files to the specified directory
func writeMCPScriptsFiles(dir string, mcpScriptsConfig *workflow.MCPScriptsConfig, verbose bool) error {
	mcpInspectLog.Printf("Writing mcp-scripts files to: %s", dir)

	// Create logs directory
	logsDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logsDir, constants.DirPermPublic); err != nil {
		errMsg := fmt.Sprintf("failed to create logs directory: %v", err)
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(errMsg))
		return fmt.Errorf("failed to create logs directory: %w", err)
	}

	// Write JavaScript dependencies that are needed
	jsFiles := []struct {
		name    string
		content string
	}{
		{"read_buffer.cjs", workflow.GetReadBufferScript()},
		{"mcp_http_transport.cjs", workflow.GetMCPHTTPTransportScript()},
		{"mcp_scripts_config_loader.cjs", workflow.GetMCPScriptsConfigLoaderScript()},
		{"mcp_server_core.cjs", workflow.GetMCPServerCoreScript()},
		{"mcp_scripts_validation.cjs", workflow.GetMCPScriptsValidationScript()},
		{"mcp_logger.cjs", workflow.GetMCPLoggerScript()},
		{"mcp_handler_shell.cjs", workflow.GetMCPHandlerShellScript()},
		{"mcp_handler_python.cjs", workflow.GetMCPHandlerPythonScript()},
		{"mcp_scripts_mcp_server_http.cjs", workflow.GetMCPScriptsMCPServerHTTPScript()},
	}

	for _, jsFile := range jsFiles {
		filePath := filepath.Join(dir, jsFile.name)
		if err := os.WriteFile(filePath, []byte(jsFile.content), constants.FilePermPublic); err != nil {
			errMsg := fmt.Sprintf("failed to write %s: %v", jsFile.name, err)
			fmt.Fprintln(os.Stderr, console.FormatErrorMessage(errMsg))
			return fmt.Errorf("failed to write %s: %w", jsFile.name, err)
		}
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Wrote "+jsFile.name))
		}
	}

	// Generate and write tools.json
	toolsJSON := workflow.GenerateMCPScriptsToolsConfig(mcpScriptsConfig)
	toolsPath := filepath.Join(dir, "tools.json")
	if err := os.WriteFile(toolsPath, []byte(toolsJSON), constants.FilePermPublic); err != nil {
		errMsg := fmt.Sprintf("failed to write tools.json: %v", err)
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(errMsg))
		return fmt.Errorf("failed to write tools.json: %w", err)
	}
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Wrote tools.json"))
	}

	// Generate and write mcp-server.cjs entry point
	mcpServerScript := workflow.GenerateMCPScriptsMCPServerScript(mcpScriptsConfig)
	mcpServerPath := filepath.Join(dir, "mcp-server.cjs")
	if err := os.WriteFile(mcpServerPath, []byte(mcpServerScript), constants.FilePermExecutable); err != nil {
		errMsg := fmt.Sprintf("failed to write mcp-server.cjs: %v", err)
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(errMsg))
		return fmt.Errorf("failed to write mcp-server.cjs: %w", err)
	}
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Wrote mcp-server.cjs"))
	}

	// Generate and write tool handler files
	for toolName, toolConfig := range mcpScriptsConfig.Tools {
		var content string
		var extension string

		if toolConfig.Script != "" {
			content = workflow.GenerateMCPScriptJavaScriptToolScript(toolConfig)
			extension = ".cjs"
		} else if toolConfig.Run != "" {
			content = workflow.GenerateMCPScriptShellToolScript(toolConfig)
			extension = ".sh"
		} else if toolConfig.Py != "" {
			content = workflow.GenerateMCPScriptPythonToolScript(toolConfig)
			extension = ".py"
		} else {
			continue
		}

		toolPath := filepath.Join(dir, toolName+extension)
		mode := constants.FilePermPublic
		if extension == ".sh" || extension == ".py" {
			mode = constants.FilePermExecutable
		}
		if err := os.WriteFile(toolPath, []byte(content), mode); err != nil {
			errMsg := fmt.Sprintf("failed to write tool %s: %v", toolName, err)
			fmt.Fprintln(os.Stderr, console.FormatErrorMessage(errMsg))
			return fmt.Errorf("failed to write tool %s: %w", toolName, err)
		}
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Wrote tool handler: %s%s", toolName, extension)))
		}
	}

	mcpInspectLog.Printf("Successfully wrote all mcp-scripts files")
	return nil
}
