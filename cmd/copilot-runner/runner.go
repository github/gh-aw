// This file provides the core runner logic for the copilot-runner binary.
//
// The runner manages the Copilot SDK client lifecycle:
//  1. Creates an SDK client with the Copilot CLI path
//  2. Creates a session with configured tools, MCP servers, and system prompt
//  3. Sends the prompt and waits for completion
//  4. Collects metrics from SDK events
//  5. Writes structured JSON output and conversation log
//
// Note: The Copilot SDK (github.com/github/copilot-sdk/go) is a Technical Preview.
// This runner provides a bridge between gh-aw's compiled workflows and the SDK's
// programmatic interface. Once the SDK stabilizes, the runner can be simplified.

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

// Runner manages the Copilot SDK session execution.
type Runner struct {
	config  *RunnerConfig
	metrics *MetricsCollector
}

// NewRunner creates a new Runner with the given configuration.
func NewRunner(config *RunnerConfig) *Runner {
	return &Runner{
		config:  config,
		metrics: NewMetricsCollector(),
	}
}

// Run executes the Copilot session and returns the structured output.
func (r *Runner) Run(ctx context.Context) (RunnerOutput, error) {
	// Set up signal handling for graceful shutdown
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Read the prompt
	prompt, err := r.readPrompt()
	if err != nil {
		return r.metrics.BuildOutput(false, ""), fmt.Errorf("failed to read prompt: %w", err)
	}

	fmt.Fprintf(os.Stderr, "[copilot-runner] Starting Copilot SDK session\n")
	fmt.Fprintf(os.Stderr, "[copilot-runner] CLI path: %s\n", r.config.CLIPath)
	if r.config.Model != "" {
		fmt.Fprintf(os.Stderr, "[copilot-runner] Model: %s\n", r.config.Model)
	}
	fmt.Fprintf(os.Stderr, "[copilot-runner] Available tools: %v\n", r.config.AvailableTools)
	fmt.Fprintf(os.Stderr, "[copilot-runner] Prompt length: %d bytes\n", len(prompt))

	// Since the Copilot SDK is not yet available as a Go dependency,
	// we fall back to invoking the Copilot CLI directly with the configured parameters.
	// This provides the same structured config approach while maintaining compatibility.
	//
	// TODO: Replace this with actual SDK client calls once github.com/github/copilot-sdk/go
	// is available as a public Go module:
	//
	//   client := copilot.NewClient(&copilot.ClientOptions{
	//       CLIPath:     r.config.CLIPath,
	//       GithubToken: os.Getenv("COPILOT_GITHUB_TOKEN"),
	//       Cwd:         r.config.WorkingDirectory,
	//       LogLevel:    "debug",
	//   })
	//   client.Start(ctx)
	//   defer client.Stop()
	//
	//   session, _ := client.CreateSession(ctx, &copilot.SessionConfig{
	//       Model:          r.config.Model,
	//       AvailableTools: r.config.AvailableTools,
	//       ExcludedTools:  r.config.ExcludedTools,
	//       MCPServers:     mcpServers,
	//   })
	//
	//   response, _ := session.SendAndWait(ctx, copilot.MessageOptions{
	//       Prompt: prompt,
	//   })

	// For now, construct and execute the CLI command based on the config
	output, err := r.executeCLIFallback(ctx, prompt)
	if err != nil {
		r.metrics.RecordError(err.Error())
		return r.metrics.BuildOutput(false, ""), err
	}

	return r.metrics.BuildOutput(true, output), nil
}

// readPrompt reads the prompt from the configured prompt file.
func (r *Runner) readPrompt() (string, error) {
	data, err := os.ReadFile(r.config.PromptFile)
	if err != nil {
		return "", fmt.Errorf("failed to read prompt file %s: %w", r.config.PromptFile, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// executeCLIFallback invokes the Copilot CLI directly as a fallback
// until the SDK Go module is publicly available.
func (r *Runner) executeCLIFallback(ctx context.Context, prompt string) (string, error) {
	var args []string

	// Prepend CLIArgs (e.g., for "node ./node_modules/.bin/copilot" invocation)
	if len(r.config.CLIArgs) > 0 {
		args = append(args, r.config.CLIArgs...)
	}

	// Add directories
	for _, dir := range r.config.AddDirs {
		args = append(args, "--add-dir", dir)
	}

	// Add log settings
	args = append(args, "--log-level", "all")
	args = append(args, "--log-dir", r.config.LogDir)

	// Disable built-in MCPs if configured
	if r.config.DisableBuiltinMCPs {
		args = append(args, "--disable-builtin-mcps")
	}

	// Add model
	if r.config.Model != "" {
		args = append(args, "--model", r.config.Model)
	}

	// Add agent
	if r.config.Agent != "" {
		args = append(args, "--agent", r.config.Agent)
	}

	// Map SDK tool names back to CLI --allow-tool flags
	for _, tool := range r.config.AvailableTools {
		if tool == "*" {
			args = append(args, "--allow-all-tools")
			break
		}
		cliToolName := mapSDKToolToCLI(tool)
		args = append(args, "--allow-tool", cliToolName)
	}

	// Allow all paths if configured
	if r.config.AllowAllPaths {
		args = append(args, "--allow-all-paths")
	}

	// Add share file
	if r.config.ShareFile != "" {
		args = append(args, "--share", r.config.ShareFile)
	}

	// Add prompt
	args = append(args, "--prompt", prompt)

	fmt.Fprintf(os.Stderr, "[copilot-runner] Executing CLI fallback with %d args\n", len(args))

	// Execute the CLI command
	// #nosec G204 -- CLIPath is from the config file which is generated by the compiler
	cmd := newCommand(ctx, r.config.CLIPath, args...)
	cmd.setStdout(os.Stdout)
	cmd.setStderr(os.Stderr)

	if r.config.WorkingDirectory != "" {
		cmd.setDir(r.config.WorkingDirectory)
	}

	if err := cmd.run(); err != nil {
		return "", fmt.Errorf("copilot CLI execution failed: %w", err)
	}

	return "CLI execution completed successfully", nil
}

// mapSDKToolToCLI maps SDK tool names back to CLI --allow-tool values.
// Handles both simple names ("bash" -> "shell") and parameterized forms
// ("bash(git)" -> "shell(git)") for granular command permissions.
func mapSDKToolToCLI(sdkTool string) string {
	// Handle parameterized bash tools: bash(<cmd>) -> shell(<cmd>)
	if strings.HasPrefix(sdkTool, "bash(") && strings.HasSuffix(sdkTool, ")") {
		inner := sdkTool[5 : len(sdkTool)-1]
		return "shell(" + inner + ")"
	}
	switch sdkTool {
	case "bash":
		return "shell"
	case "edit":
		return "write"
	default:
		return sdkTool
	}
}
