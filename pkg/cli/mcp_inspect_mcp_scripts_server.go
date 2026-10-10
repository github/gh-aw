package cli

import (
	"context"
	"errors"
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
	// Port range for mcp-scripts HTTP server
	mcpScriptsStartPort = 3000
	mcpScriptsPortRange = 10
	// mcpScriptsServerStartupDelay paces readiness checks while the mcp-scripts server boots.
	mcpScriptsServerStartupDelay = 200 * time.Millisecond
)

// findAvailablePort finds an available port starting from the given port
func findAvailablePort(startPort int, verbose bool) int {
	for port := startPort; port < startPort+mcpScriptsPortRange; port++ {
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

var errMCPScriptsServerStartupTimeout = errors.New("mcp-scripts HTTP server failed to start within timeout")

func isServerReady(client *http.Client, req *http.Request, port int, verbose bool) bool {
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			mcpInspectLog.Printf("Warning: failed to close response body: %v", closeErr)
		}
	}()
	if verbose {
		mcpInspectLog.Printf("Server is ready on port %d", port)
	}
	return true
}

// waitForServerReady waits for the HTTP server to be ready by polling the endpoint.
func waitForServerReady(ctx context.Context, port int, timeout time.Duration, verbose bool) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{
		Timeout: 1 * time.Second,
	}
	url := fmt.Sprintf("http://localhost:%d/", port)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			mcpInspectLog.Printf("Failed to create request: %v", err)
			return err
		}
		if isServerReady(client, req, port, verbose) {
			return nil
		}
		timer := time.NewTimer(mcpScriptsServerStartupDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}

	mcpInspectLog.Printf("Server did not become ready within timeout")
	return errMCPScriptsServerStartupTimeout
}

// startMCPScriptsHTTPServer starts the mcp-scripts HTTP MCP server
func startMCPScriptsHTTPServer(dir string, port int, verbose bool) (*exec.Cmd, error) {
	mcpInspectLog.Printf("Starting mcp-scripts HTTP server on port %d", port)

	mcpServerPath := filepath.Join(dir, "mcp-server.cjs")

	cmd := exec.Command("node", mcpServerPath)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("GH_AW_MCP_SCRIPTS_PORT=%d", port),
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
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Started mcp-scripts server (PID: %d)", cmd.Process.Pid)))
	}

	return cmd, nil
}

// startMCPScriptsServer starts the mcp-scripts HTTP server and returns the MCP config
func startMCPScriptsServer(ctx context.Context, mcpScriptsConfig *workflow.MCPScriptsConfig, verbose bool) (*parser.RegistryMCPServerConfig, *exec.Cmd, string, error) {
	mcpInspectLog.Printf("Starting mcp-scripts server with %d tools", len(mcpScriptsConfig.Tools))

	// Check if node is available
	if _, err := exec.LookPath("node"); err != nil {
		errMsg := "node not found. Please install Node.js to run the mcp-scripts MCP server"
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(errMsg))
		return nil, nil, "", fmt.Errorf("node not found. Please install Node.js to run the mcp-scripts MCP server: %w", err)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %d mcp-script tool(s) to configure", len(mcpScriptsConfig.Tools))))
	}

	// Create temporary directory for mcp-scripts files
	tmpDir, err := os.MkdirTemp("", "gh-aw-mcp-scripts-*")
	if err != nil {
		errMsg := fmt.Sprintf("failed to create temporary directory: %v", err)
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(errMsg))
		return nil, nil, "", fmt.Errorf("failed to create temporary directory: %w", err)
	}

	if verbose {
		if _, err := fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Created temporary directory: "+tmpDir)); err != nil {
			mcpInspectLog.Printf("Warning: failed to write to stderr: %v", err)
		}
	}

	// Write mcp-scripts files to temporary directory
	if err := writeMCPScriptsFiles(tmpDir, mcpScriptsConfig, verbose); err != nil {
		// Clean up temporary directory on error
		if err := os.RemoveAll(tmpDir); err != nil && verbose {
			mcpInspectLog.Printf("Warning: failed to clean up temporary directory %s: %v", tmpDir, err)
		}
		errMsg := fmt.Sprintf("failed to write mcp-scripts files: %v", err)
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(errMsg))
		return nil, nil, "", fmt.Errorf("failed to write mcp-scripts files: %w", err)
	}

	// Find an available port for the HTTP server
	port := findAvailablePort(mcpScriptsStartPort, verbose)
	if port == 0 {
		if err := os.RemoveAll(tmpDir); err != nil && verbose {
			mcpInspectLog.Printf("Warning: failed to clean up temporary directory %s: %v", tmpDir, err)
		}
		errMsg := "failed to find an available port for the HTTP server"
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(errMsg))
		return nil, nil, "", errors.New("failed to find an available port for the HTTP server")
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Using port %d for mcp-scripts HTTP server", port)))
	}

	// Start the HTTP server
	serverCmd, err := startMCPScriptsHTTPServer(tmpDir, port, verbose)
	if err != nil {
		// Clean up temporary directory on error
		if rmErr := os.RemoveAll(tmpDir); rmErr != nil && verbose {
			mcpInspectLog.Printf("Warning: failed to clean up temporary directory %s: %v", tmpDir, rmErr)
		}
		return nil, nil, "", fmt.Errorf("failed to start mcp-scripts HTTP server: %w", err)
	}

	// Wait for the server to start up
	if err := waitForServerReady(ctx, port, 5*time.Second, verbose); err != nil {
		if serverCmd.Process != nil {
			// Kill the process and log warning if it fails
			if err := serverCmd.Process.Kill(); err != nil && verbose {
				mcpInspectLog.Printf("Warning: failed to kill server process %d: %v", serverCmd.Process.Pid, err)
			}
		}
		if err := os.RemoveAll(tmpDir); err != nil && verbose {
			mcpInspectLog.Printf("Warning: failed to clean up temporary directory %s: %v", tmpDir, err)
		}
		return nil, nil, "", err
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("MCP Scripts HTTP server started successfully"))
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Server running on: http://localhost:%d", port)))
	}

	// Create MCP server config for the mcp-scripts server
	config := &parser.RegistryMCPServerConfig{
		BaseMCPServerConfig: types.BaseMCPServerConfig{
			Type: "http",
			URL:  fmt.Sprintf("http://localhost:%d", port),
			Env:  make(map[string]string),
		},
		Name: "mcpscripts",
	}

	return config, serverCmd, tmpDir, nil
}
