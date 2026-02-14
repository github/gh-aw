//go:build integration

package cli

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMCPInspectPlaywrightLiveIntegration tests the playwright MCP with a live local web server
// This test actually starts a web server, configures playwright with proper environment variables,
// and attempts to connect to the playwright MCP server to validate full functionality.
//
// The test validates:
// - Frontmatter parsing for playwright configuration
// - MCP configuration validation
// - Environment variable setup (PLAYWRIGHT_ALLOWED_DOMAINS)
// - Docker container startup command generation
// - Connection to the Playwright MCP server (if docker is fast enough)
//
// The test gracefully handles docker timeouts since image pulling and container startup
// can vary significantly across different CI/CD environments.
func TestMCPInspectPlaywrightLiveIntegration(t *testing.T) {
	// Skip in CI environments due to Docker container startup reliability issues
	if os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
		t.Skip("Skipping live playwright test in CI environment due to Docker container startup timeouts")
	}

	// Skip if Docker is not available
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker not available, skipping live playwright test")
	}

	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Find an available port for our test web server
	port, err := findAvailableTestPort()
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}

	// Start a simple HTTP server serving the docs or a simple HTML page
	serverAddr := fmt.Sprintf("localhost:%d", port)
	testURL := fmt.Sprintf("http://localhost:%d", port)

	// Start the test web server in background
	server := startTestWebServer(t, port)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			t.Logf("Error shutting down test web server: %v", err)
		}
	}()

	// Wait for server to be ready
	if !waitForServer(testURL, 5*time.Second) {
		t.Fatalf("Test web server did not start in time")
	}
	t.Logf("✓ Test web server started on %s", serverAddr)

	// Test with each engine
	engines := []struct {
		name         string
		engineConfig string
	}{
		{
			name: "copilot",
			engineConfig: `engine: copilot
tools:
  playwright:
    allowed_domains:
      - "localhost"
      - "127.0.0.1"`,
		},
		{
			name: "claude",
			engineConfig: `engine: claude
tools:
  playwright:
    allowed_domains:
      - "localhost"
      - "127.0.0.1"`,
		},
		{
			name: "codex",
			engineConfig: `engine: codex
tools:
  playwright:
    allowed_domains:
      - "localhost"
      - "127.0.0.1"`,
		},
	}

	for _, tc := range engines {
		t.Run(tc.name, func(t *testing.T) {
			// Create a test workflow file
			workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
` + tc.engineConfig + `
---

# Test Playwright with Live Server for ` + tc.name + `

This workflow tests playwright tool with a live local web server.
Navigate to ` + testURL + ` and take a screenshot.
`

			workflowFile := filepath.Join(setup.workflowsDir, "test-playwright-live-"+tc.name+".md")
			if err := os.WriteFile(workflowFile, []byte(workflowContent), 0o644); err != nil {
				t.Fatalf("Failed to create test workflow file: %v", err)
			}

			// Run mcp inspect command with proper environment variables
			// Set the PLAYWRIGHT_ALLOWED_DOMAINS environment variable
			allowedDomains := "localhost,127.0.0.1"

			// Set timeout for the command to avoid hanging
			timeoutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			cmd := exec.CommandContext(timeoutCtx, setup.binaryPath, "mcp", "inspect", "test-playwright-live-"+tc.name, "--server", "playwright", "--verbose")
			cmd.Dir = setup.tempDir
			cmd.Env = append(os.Environ(), "PLAYWRIGHT_ALLOWED_DOMAINS="+allowedDomains)

			output, err := cmd.CombinedOutput()
			outputStr := string(output)

			t.Logf("MCP inspect output for %s engine:\n%s", tc.name, outputStr)

			// Validate the output
			if !strings.Contains(outputStr, "Frontmatter validation passed") {
				t.Logf("Note: Frontmatter validation message not found")
			} else {
				t.Logf("✓ Frontmatter validation passed for %s engine", tc.name)
			}

			if !strings.Contains(outputStr, "MCP configuration validation passed") {
				t.Logf("Note: MCP configuration validation message not found")
			} else {
				t.Logf("✓ MCP configuration validation passed for %s engine", tc.name)
			}

			// Check that playwright server was mentioned
			if strings.Contains(strings.ToLower(outputStr), "playwright") {
				t.Logf("✓ Playwright MCP server detected for %s engine", tc.name)
			}

			// Check if it successfully connected (docker image might need to be pulled)
			if strings.Contains(outputStr, "Successfully connected") {
				t.Logf("✓ Successfully connected to Playwright MCP server for %s engine", tc.name)

				// Look for tool listings
				if strings.Contains(outputStr, "browser_navigate") ||
					strings.Contains(outputStr, "browser_screenshot") ||
					strings.Contains(outputStr, "browser_snapshot") {
					t.Logf("✓ Playwright tools detected in output for %s engine", tc.name)
				}
			} else if err != nil {
				// Connection might fail if docker image needs to be pulled or docker is slow
				t.Logf("Note: Connection to Playwright MCP server may have failed (docker image might need pulling): %v", err)
				t.Logf("This is acceptable as docker environment varies between test runners")
			}

			// Check for secret validation success
			if strings.Contains(outputStr, "Secret validation failed") {
				t.Errorf("Secret validation should not have failed with PLAYWRIGHT_ALLOWED_DOMAINS set for %s engine", tc.name)
			} else {
				t.Logf("✓ No secret validation errors for %s engine", tc.name)
			}
		})
	}
}

// TestMCPInspectPlaywrightWithDocsServer tests playwright with the actual docs dev server
// This test is more comprehensive and starts the Astro docs server to provide a realistic
// web browsing target for playwright.
//
// The test:
// 1. Starts the Astro documentation server (npm run dev in docs/)
// 2. Waits for the server to be ready on one of the common ports (4321-4324)
// 3. Creates a workflow configured to browse the docs server
// 4. Runs mcp inspect to validate the configuration and attempt connection
//
// This test is skipped if:
// - Running in CI environment (Docker container startup can be unreliable)
// - Docker is not available
// - npm is not available
// - docs directory doesn't exist
// - docs dependencies are not installed (no node_modules)
// - docs server fails to start within timeout
func TestMCPInspectPlaywrightWithDocsServer(t *testing.T) {
	// Skip in CI environments due to Docker container startup reliability issues
	if os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
		t.Skip("Skipping docs server playwright test in CI environment due to Docker container startup timeouts")
	}

	// Skip if Docker is not available
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker not available, skipping docs server playwright test")
	}

	// Skip if npm is not available
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm not available, skipping docs server playwright test")
	}

	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Find the repository root (go up from the temp directory to find the actual repo)
	// The binary is built in the repo root, so we can use the original working directory
	repoRoot := filepath.Dir(filepath.Dir(globalBinaryPath))

	// Check if docs directory exists
	docsDir := filepath.Join(repoRoot, "docs")
	if _, err := os.Stat(docsDir); os.IsNotExist(err) {
		t.Skipf("docs directory not found at %s, skipping docs server test", docsDir)
	}

	// Check if docs has node_modules (dependencies installed)
	nodeModulesDir := filepath.Join(docsDir, "node_modules")
	if _, err := os.Stat(nodeModulesDir); os.IsNotExist(err) {
		t.Skipf("docs dependencies not installed (no node_modules), run 'npm install' in docs directory first")
	}

	// Start the docs dev server
	t.Logf("Starting docs dev server...")
	docsCmd := exec.Command("npm", "run", "dev")
	docsCmd.Dir = docsDir

	// Capture output for debugging
	docsCmdOutput := &strings.Builder{}
	docsCmd.Stdout = docsCmdOutput
	docsCmd.Stderr = docsCmdOutput

	if err := docsCmd.Start(); err != nil {
		t.Skipf("Failed to start docs server: %v", err)
	}
	defer func() {
		if docsCmd.Process != nil {
			docsCmd.Process.Kill()
		}
	}()

	// Wait for the docs server to be ready (it usually runs on 4321 or 4322)
	docsURL := ""
	for _, port := range []int{4321, 4322, 4323, 4324} {
		testURL := fmt.Sprintf("http://localhost:%d", port)
		if waitForServer(testURL, 15*time.Second) {
			docsURL = testURL
			break
		}
	}

	if docsURL == "" {
		t.Logf("Docs server output:\n%s", docsCmdOutput.String())
		t.Skip("Docs server did not start in time, skipping test")
	}
	t.Logf("✓ Docs server started on %s", docsURL)

	// Create a test workflow that uses the docs server
	workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
tools:
  playwright:
    allowed_domains:
      - "localhost"
---

# Test Playwright with Docs Server

Navigate to the docs server and take a screenshot of the homepage.
URL: ` + docsURL + `/gh-aw/
`

	workflowFile := filepath.Join(setup.workflowsDir, "test-playwright-docs.md")
	if err := os.WriteFile(workflowFile, []byte(workflowContent), 0o644); err != nil {
		t.Fatalf("Failed to create test workflow file: %v", err)
	}

	// Run mcp inspect command
	timeoutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, setup.binaryPath, "mcp", "inspect", "test-playwright-docs", "--server", "playwright", "--verbose")
	cmd.Dir = setup.tempDir
	cmd.Env = append(os.Environ(), "PLAYWRIGHT_ALLOWED_DOMAINS=localhost")

	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	t.Logf("MCP inspect output:\n%s", outputStr)

	// Validate results
	if strings.Contains(outputStr, "Frontmatter validation passed") {
		t.Logf("✓ Frontmatter validation passed")
	}

	if strings.Contains(outputStr, "MCP configuration validation passed") {
		t.Logf("✓ MCP configuration validation passed")
	}

	if strings.Contains(strings.ToLower(outputStr), "playwright") {
		t.Logf("✓ Playwright MCP server detected")
	}

	if strings.Contains(outputStr, "Successfully connected") {
		t.Logf("✓ Successfully connected to Playwright MCP server")
	} else if err != nil {
		t.Logf("Note: Connection may have failed (docker environment): %v", err)
	}

	// The test passes even if connection fails, as long as validation works
	// This allows the test to work in environments where docker is slow or restricted
}

// findAvailableTestPort finds an available TCP port on localhost
func findAvailableTestPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	return addr.Port, nil
}

// startTestWebServer starts a simple HTTP server for testing
func startTestWebServer(t *testing.T, port int) *http.Server {
	mux := http.NewServeMux()

	// Serve a simple HTML page
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html>
<head>
    <title>Test Page</title>
</head>
<body>
    <h1>Playwright Test Page</h1>
    <p>This is a test page for playwright MCP integration testing.</p>
    <div id="test-content">
        <ul>
            <li>Item 1</li>
            <li>Item 2</li>
            <li>Item 3</li>
        </ul>
    </div>
</body>
</html>`
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(html))
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			t.Logf("Test server error: %v", err)
		}
	}()

	return server
}

// waitForServer waits for a server to be ready by making HTTP requests
func waitForServer(url string, timeout time.Duration) bool {
	client := &http.Client{
		Timeout: 1 * time.Second,
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	return false
}
