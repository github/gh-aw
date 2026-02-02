package cli

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/types"
	"github.com/github/gh-aw/pkg/workflow"
)

const (
	// Port range for safe-inputs HTTP server
	safeInputsStartPort = 3000
	safeInputsPortRange = 10
)

// findAvailablePort finds an available port starting from the given port
func findAvailablePort(startPort int, verbose bool) int {
	for port := startPort; port < startPort+safeInputsPortRange; port++ {
		listener, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", port))
		if err == nil {
			// Close listener and check for errors
			if err := listener.Close(); err != nil && verbose {
				mcpInspectLog.Printf("Warning: Failed to close listener on port %d: %v", port, err)
			}
			if verbose {
				mcpInspectLog.Printf("Found available port: %d", port)
			}
			return port
		}
	}
	return 0
}

// waitForServerReady waits for the HTTP server to be ready by polling the endpoint
func waitForServerReady(port int, timeout time.Duration, verbose bool) bool {
	deadline := time.Now().Add(timeout)
	client := &http.Client{
		Timeout: 1 * time.Second,
	}
	url := fmt.Sprintf("http://localhost:%d/", port)

	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			if closeErr := resp.Body.Close(); closeErr != nil {
				mcpInspectLog.Printf("Warning: failed to close response body: %v", closeErr)
			}
			if verbose {
				mcpInspectLog.Printf("Server is ready on port %d", port)
			}
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}

	mcpInspectLog.Printf("Server did not become ready within timeout")
	return false
}

// startSafeInputsHTTPServer starts the safe-inputs HTTP MCP server
func startSafeInputsHTTPServer(dir string, port int, verbose bool) (*exec.Cmd, error) {
	mcpInspectLog.Printf("Starting safe-inputs HTTP server on port %d", port)

	mcpServerPath := filepath.Join(dir, "mcp-server.cjs")

	cmd := exec.Command("node", mcpServerPath)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("GH_AW_SAFE_INPUTS_PORT=%d", port),
	)

	// Capture output for debugging
	if verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	if err := cmd.Start(); err != nil {
		errMsg := fmt.Sprintf("failed to start server: %v", err)
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(errMsg))
		return nil, fmt.Errorf("failed to start server: %w", err)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Started safe-inputs server (PID: %d)", cmd.Process.Pid)))
	}

	return cmd, nil
}

// startSafeInputsServer starts the safe-inputs HTTP server and returns the MCP config
func startSafeInputsServer(safeInputsConfig *workflow.SafeInputsConfig, verbose bool) (*parser.MCPServerConfig, *exec.Cmd, string, error) {
	mcpInspectLog.Printf("Starting safe-inputs server with %d tools", len(safeInputsConfig.Tools))

	// Check if node is available
	if _, err := exec.LookPath("node"); err != nil {
		errMsg := "node not found. Please install Node.js to run the safe-inputs MCP server"
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(errMsg))
		return nil, nil, "", fmt.Errorf("node not found. Please install Node.js to run the safe-inputs MCP server: %w", err)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %d safe-input tool(s) to configure", len(safeInputsConfig.Tools))))
	}

	// Create temporary directory for safe-inputs files
	tmpDir, err := os.MkdirTemp("", "gh-aw-safe-inputs-*")
	if err != nil {
		errMsg := fmt.Sprintf("failed to create temporary directory: %v", err)
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(errMsg))
		return nil, nil, "", fmt.Errorf("failed to create temporary directory: %w", err)
	}

	if verbose {
		if _, err := fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Created temporary directory: %s", tmpDir))); err != nil {
			mcpInspectLog.Printf("Warning: failed to write to stderr: %v", err)
		}
	}

	// Write safe-inputs files to temporary directory
	if err := writeSafeInputsFiles(tmpDir, safeInputsConfig, verbose); err != nil {
		// Clean up temporary directory on error
		if err := os.RemoveAll(tmpDir); err != nil && verbose {
			mcpInspectLog.Printf("Warning: failed to clean up temporary directory %s: %v", tmpDir, err)
		}
		errMsg := fmt.Sprintf("failed to write safe-inputs files: %v", err)
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(errMsg))
		return nil, nil, "", fmt.Errorf("failed to write safe-inputs files: %w", err)
	}

	// Find an available port for the HTTP server
	port := findAvailablePort(safeInputsStartPort, verbose)
	if port == 0 {
		if err := os.RemoveAll(tmpDir); err != nil && verbose {
			mcpInspectLog.Printf("Warning: failed to clean up temporary directory %s: %v", tmpDir, err)
		}
		errMsg := "failed to find an available port for the HTTP server"
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(errMsg))
		return nil, nil, "", fmt.Errorf("failed to find an available port for the HTTP server")
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Using port %d for safe-inputs HTTP server", port)))
	}

	// Start the HTTP server
	serverCmd, err := startSafeInputsHTTPServer(tmpDir, port, verbose)
	if err != nil {
		// Clean up temporary directory on error
		if rmErr := os.RemoveAll(tmpDir); rmErr != nil && verbose {
			mcpInspectLog.Printf("Warning: failed to clean up temporary directory %s: %v", tmpDir, rmErr)
		}
		return nil, nil, "", fmt.Errorf("failed to start safe-inputs HTTP server: %w", err)
	}

	// Wait for the server to start up
	if !waitForServerReady(port, 5*time.Second, verbose) {
		if serverCmd.Process != nil {
			// Kill the process and log warning if it fails
			if err := serverCmd.Process.Kill(); err != nil && verbose {
				mcpInspectLog.Printf("Warning: failed to kill server process %d: %v", serverCmd.Process.Pid, err)
			}
		}
		if err := os.RemoveAll(tmpDir); err != nil && verbose {
			mcpInspectLog.Printf("Warning: failed to clean up temporary directory %s: %v", tmpDir, err)
		}
		return nil, nil, "", fmt.Errorf("safe-inputs HTTP server failed to start within timeout")
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Safe-inputs HTTP server started successfully"))
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Server running on: http://localhost:%d", port)))
	}

	// Create MCP server config for the safe-inputs server
	config := &parser.MCPServerConfig{
		BaseMCPServerConfig: types.BaseMCPServerConfig{
			Type: "http",
			URL:  fmt.Sprintf("http://localhost:%d", port),
			Env:  make(map[string]string),
		},
		Name: "safeinputs",
	}

	return config, serverCmd, tmpDir, nil
}
