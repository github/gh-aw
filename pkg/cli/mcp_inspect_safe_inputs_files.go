package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/workflow"
)

// writeSafeInputsFiles writes all safe-inputs MCP server files to the specified directory
func writeSafeInputsFiles(dir string, safeInputsConfig *workflow.SafeInputsConfig, verbose bool) error {
	mcpInspectLog.Printf("Writing safe-inputs files to: %s", dir)

	// Create logs directory
	logsDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
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
		{"safe_inputs_config_loader.cjs", workflow.GetSafeInputsConfigLoaderScript()},
		{"mcp_server_core.cjs", workflow.GetMCPServerCoreScript()},
		{"safe_inputs_validation.cjs", workflow.GetSafeInputsValidationScript()},
		{"mcp_logger.cjs", workflow.GetMCPLoggerScript()},
		{"mcp_handler_shell.cjs", workflow.GetMCPHandlerShellScript()},
		{"mcp_handler_python.cjs", workflow.GetMCPHandlerPythonScript()},
		{"safe_inputs_mcp_server_http.cjs", workflow.GetSafeInputsMCPServerHTTPScript()},
	}

	for _, jsFile := range jsFiles {
		filePath := filepath.Join(dir, jsFile.name)
		if err := os.WriteFile(filePath, []byte(jsFile.content), 0644); err != nil {
			errMsg := fmt.Sprintf("failed to write %s: %v", jsFile.name, err)
			fmt.Fprintln(os.Stderr, console.FormatErrorMessage(errMsg))
			return fmt.Errorf("failed to write %s: %w", jsFile.name, err)
		}
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Wrote %s", jsFile.name)))
		}
	}

	// Generate and write tools.json
	toolsJSON := workflow.GenerateSafeInputsToolsConfigForInspector(safeInputsConfig)
	toolsPath := filepath.Join(dir, "tools.json")
	if err := os.WriteFile(toolsPath, []byte(toolsJSON), 0644); err != nil {
		errMsg := fmt.Sprintf("failed to write tools.json: %v", err)
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(errMsg))
		return fmt.Errorf("failed to write tools.json: %w", err)
	}
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Wrote tools.json"))
	}

	// Generate and write mcp-server.cjs entry point
	mcpServerScript := workflow.GenerateSafeInputsMCPServerScriptForInspector(safeInputsConfig)
	mcpServerPath := filepath.Join(dir, "mcp-server.cjs")
	if err := os.WriteFile(mcpServerPath, []byte(mcpServerScript), 0755); err != nil {
		errMsg := fmt.Sprintf("failed to write mcp-server.cjs: %v", err)
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(errMsg))
		return fmt.Errorf("failed to write mcp-server.cjs: %w", err)
	}
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Wrote mcp-server.cjs"))
	}

	// Generate and write tool handler files
	for toolName, toolConfig := range safeInputsConfig.Tools {
		var content string
		var extension string

		if toolConfig.Script != "" {
			content = workflow.GenerateSafeInputJavaScriptToolScriptForInspector(toolConfig)
			extension = ".cjs"
		} else if toolConfig.Run != "" {
			content = workflow.GenerateSafeInputShellToolScriptForInspector(toolConfig)
			extension = ".sh"
		} else if toolConfig.Py != "" {
			content = workflow.GenerateSafeInputPythonToolScriptForInspector(toolConfig)
			extension = ".py"
		} else {
			continue
		}

		toolPath := filepath.Join(dir, toolName+extension)
		mode := os.FileMode(0644)
		if extension == ".sh" || extension == ".py" {
			mode = 0755
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

	mcpInspectLog.Printf("Successfully wrote all safe-inputs files")
	return nil
}
