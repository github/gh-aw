package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow"
)

var mcpInspectorLog = logger.New("cli:mcp_inspect_inspector")

// spawnMCPInspector launches the official @modelcontextprotocol/inspector tool
// and spawns any stdio MCP servers beforehand
func spawnMCPInspector(workflowFile string, serverFilter string, verbose bool) error {
	mcpInspectorLog.Printf("Spawning MCP inspector: workflow_file=%s, server_filter=%s", workflowFile, serverFilter)
	// Check if npx is available
	if _, err := exec.LookPath("npx"); err != nil {
		return fmt.Errorf("npx not found. Please install Node.js and npm to use the MCP inspector: %w", err)
	}

	var mcpConfigs []parser.MCPServerConfig
	var serverProcesses []*exec.Cmd
	var wg sync.WaitGroup

	// If workflow file is specified, extract MCP configurations and start servers
	if workflowFile != "" {
		// Resolve the workflow file path (supports shared workflows)
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

		// Use the compiler to parse the workflow file
		// This automatically handles imports, merging, and validation
		compiler := workflow.NewCompiler(
			workflow.WithVerbose(verbose),
		)
		workflowData, err := compiler.ParseWorkflowFile(workflowPath)
		if err != nil {
			return err
		}

		// Build frontmatter map from WorkflowData for MCP extraction
		// This includes all merged imports and tools
		frontmatterForMCP := buildFrontmatterFromWorkflowData(workflowData)

		// Extract MCP configurations from the merged frontmatter
		mcpConfigs, err = parser.ExtractMCPConfigurations(frontmatterForMCP, serverFilter)
		if err != nil {
			mcpInspectorLog.Printf("Failed to extract MCP configurations: %v", err)
			return err
		}

		mcpInspectorLog.Printf("Extracted %d MCP server configurations from workflow", len(mcpConfigs))

		if len(mcpConfigs) > 0 {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %d MCP server(s) in workflow:", len(mcpConfigs))))
			for _, config := range mcpConfigs {
				fmt.Fprintf(os.Stderr, "  • %s (%s)\n", config.Name, config.Type)
			}
			fmt.Fprintln(os.Stderr)

			// Start stdio MCP servers in the background
			stdioServers := []parser.MCPServerConfig{}
			for _, config := range mcpConfigs {
				if config.Type == "stdio" {
					stdioServers = append(stdioServers, config)
				}
			}

			if len(stdioServers) > 0 {
				mcpInspectorLog.Printf("Starting %d stdio MCP servers", len(stdioServers))
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Starting stdio MCP servers..."))

				for _, config := range stdioServers {
					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Starting server: %s", config.Name)))
					}

					// Create the command for the MCP server
					var cmd *exec.Cmd
					if config.Container != "" {
						// Docker container mode
						args := append([]string{"run", "--rm", "-i"}, config.Args...)
						cmd = exec.Command("docker", args...)
					} else {
						// Direct command mode
						if config.Command == "" {
							fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Skipping server %s: no command specified", config.Name)))
							continue
						}
						cmd = exec.Command(config.Command, config.Args...)
					}

					// Set environment variables
					cmd.Env = os.Environ()
					for key, value := range config.Env {
						// Resolve environment variable references
						resolvedValue := os.ExpandEnv(value)
						cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, resolvedValue))
					}

					// Start the server process
					if err := cmd.Start(); err != nil {
						mcpInspectorLog.Printf("Failed to start MCP server %s: %v", config.Name, err)
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to start server %s: %v", config.Name, err)))
						continue
					}

					mcpInspectorLog.Printf("Started MCP server %s (PID: %d, type: %s)", config.Name, cmd.Process.Pid, config.Type)
					serverProcesses = append(serverProcesses, cmd)

					// Monitor the process in the background
					wg.Add(1)
					go func(serverCmd *exec.Cmd, serverName string) {
						defer wg.Done()
						if err := serverCmd.Wait(); err != nil && verbose {
							fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Server %s exited with error: %v", serverName, err)))
						}
					}(cmd, config.Name)

					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Started server: %s (PID: %d)", config.Name, cmd.Process.Pid)))
					}
				}

				// Give servers a moment to start up
				time.Sleep(2 * time.Second)
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("All stdio servers started successfully"))
			}

			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Configuration details for MCP inspector:"))
			for _, config := range mcpConfigs {
				fmt.Fprintf(os.Stderr, "\n📡 %s (%s):\n", config.Name, config.Type)
				switch config.Type {
				case "stdio":
					if config.Container != "" {
						fmt.Fprintf(os.Stderr, "  Container: %s\n", config.Container)
					} else {
						fmt.Fprintf(os.Stderr, "  Command: %s\n", config.Command)
						if len(config.Args) > 0 {
							fmt.Fprintf(os.Stderr, "  Args: %s\n", strings.Join(config.Args, " "))
						}
					}
				case "http":
					fmt.Fprintf(os.Stderr, "  URL: %s\n", config.URL)
				}
				if len(config.Env) > 0 {
					fmt.Fprintf(os.Stderr, "  Environment Variables: %v\n", config.Env)
				}
			}
			fmt.Fprintln(os.Stderr)
		} else {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("No MCP servers found in workflow"))
			return nil
		}
	}

	// Set up cleanup function for stdio servers
	defer func() {
		if len(serverProcesses) > 0 {
			mcpInspectorLog.Printf("Cleaning up %d MCP server processes", len(serverProcesses))
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Cleaning up MCP servers..."))
			for i, cmd := range serverProcesses {
				if cmd.Process != nil {
					if err := cmd.Process.Kill(); err != nil && verbose {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to kill server process %d: %v", cmd.Process.Pid, err)))
					}
				}
				// Give each process a chance to clean up
				if i < len(serverProcesses)-1 {
					time.Sleep(100 * time.Millisecond)
				}
			}
			// Wait for all background goroutines to finish (with timeout)
			done := make(chan struct{})
			go func() {
				wg.Wait()
				close(done)
			}()

			select {
			case <-done:
				// All finished
			case <-time.After(5 * time.Second):
				// Timeout waiting for cleanup
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Timeout waiting for server cleanup"))
				}
			}
		}
	}()

	mcpInspectorLog.Print("Launching @modelcontextprotocol/inspector")
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Launching @modelcontextprotocol/inspector..."))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Visit http://localhost:5173 after the inspector starts"))
	if len(serverProcesses) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("%d stdio MCP server(s) are running in the background", len(serverProcesses))))
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Configure them in the inspector using the details shown above"))
	}

	cmd := exec.Command("npx", "@modelcontextprotocol/inspector")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	err := cmd.Run()
	if err != nil {
		mcpInspectorLog.Printf("MCP inspector exited with error: %v", err)
	} else {
		mcpInspectorLog.Print("MCP inspector exited successfully")
	}
	return err
}
