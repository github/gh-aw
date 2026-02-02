package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/types"
	"github.com/github/gh-aw/pkg/workflow"
)

// spawnSafeInputsInspector generates safe-inputs MCP server files, starts the HTTP server,
// and launches the inspector to inspect it
func spawnSafeInputsInspector(workflowFile string, verbose bool) error {
	mcpInspectLog.Printf("Spawning safe-inputs inspector for workflow: %s", workflowFile)

	// Check if node is available
	if _, err := exec.LookPath("node"); err != nil {
		return fmt.Errorf("node not found. Please install Node.js to run the safe-inputs MCP server: %w", err)
	}

	// Resolve the workflow file path
	workflowPath, err := ResolveWorkflowPath(workflowFile)
	if err != nil {
		return err
	}

	// Convert to absolute path if needed
	if !filepath.IsAbs(workflowPath) {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
		workflowPath = filepath.Join(cwd, workflowPath)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Inspecting safe-inputs from: %s", workflowPath)))
	}

	// Use the workflow compiler to parse the file and resolve imports
	// This ensures that imported safe-inputs are properly merged
	compiler := workflow.NewCompiler(
		workflow.WithVerbose(verbose),
	)
	workflowData, err := compiler.ParseWorkflowFile(workflowPath)
	if err != nil {
		return fmt.Errorf("failed to parse workflow file: %w", err)
	}

	// Get safe-inputs configuration from the parsed WorkflowData
	// This includes both direct and imported safe-inputs configurations
	safeInputsConfig := workflowData.SafeInputs
	if safeInputsConfig == nil || len(safeInputsConfig.Tools) == 0 {
		return fmt.Errorf("no safe-inputs configuration found in workflow")
	}

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %d safe-input tool(s) to configure", len(safeInputsConfig.Tools))))

	// Create temporary directory for safe-inputs files
	tmpDir, err := os.MkdirTemp("", "gh-aw-safe-inputs-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary directory: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil && verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to cleanup temporary directory: %v", err)))
		}
	}()

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Created temporary directory: %s", tmpDir)))
	}

	// Write safe-inputs files to temporary directory
	if err := writeSafeInputsFiles(tmpDir, safeInputsConfig, verbose); err != nil {
		return fmt.Errorf("failed to write safe-inputs files: %w", err)
	}

	// Find an available port for the HTTP server
	port := findAvailablePort(safeInputsStartPort, verbose)
	if port == 0 {
		return fmt.Errorf("failed to find an available port for the HTTP server")
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Using port %d for safe-inputs HTTP server", port)))
	}

	// Start the HTTP server
	serverCmd, err := startSafeInputsHTTPServer(tmpDir, port, verbose)
	if err != nil {
		return fmt.Errorf("failed to start safe-inputs HTTP server: %w", err)
	}
	defer func() {
		if serverCmd.Process != nil {
			// Try graceful shutdown first
			if err := serverCmd.Process.Signal(os.Interrupt); err != nil && verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to send interrupt signal: %v", err)))
			}
			// Wait a moment for graceful shutdown
			time.Sleep(500 * time.Millisecond)
			// Attempt force kill (may fail if process already exited gracefully, which is fine)
			_ = serverCmd.Process.Kill()
		}
	}()

	// Wait for the server to start up
	if !waitForServerReady(port, 5*time.Second, verbose) {
		return fmt.Errorf("safe-inputs HTTP server failed to start within timeout")
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Safe-inputs HTTP server started successfully"))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Server running on: http://localhost:%d", port)))
	fmt.Fprintln(os.Stderr)

	// Create MCP server config for the safe-inputs server
	safeInputsMCPConfig := parser.MCPServerConfig{
		BaseMCPServerConfig: types.BaseMCPServerConfig{
			Type: "http",
			URL:  fmt.Sprintf("http://localhost:%d", port),
			Env:  make(map[string]string),
		},
		Name: "safeinputs",
	}

	// Inspect the safe-inputs MCP server using the Go SDK (like other MCP servers)
	return inspectMCPServer(safeInputsMCPConfig, "", verbose, false)
}
