package workflow

import (
	"bytes"
	"context"
	"os"
	"os/exec"

	"github.com/cli/go-gh/v2"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/tty"
)

var githubCLILog = logger.New("workflow:github_cli")

// ExecGH wraps gh CLI calls and ensures proper token configuration.
// It uses go-gh/v2 to execute gh commands when GH_TOKEN or GITHUB_TOKEN is available,
// otherwise falls back to direct exec.Command for backward compatibility.
//
// Usage:
//
//	cmd := ExecGH("api", "/user")
//	output, err := cmd.Output()
func ExecGH(args ...string) *exec.Cmd {
	// Check if GH_TOKEN or GITHUB_TOKEN is available
	ghToken := os.Getenv("GH_TOKEN")
	githubToken := os.Getenv("GITHUB_TOKEN")

	// If we have a token, use go-gh/v2 which handles authentication properly
	if ghToken != "" || githubToken != "" {
		githubCLILog.Printf("Using gh CLI via go-gh/v2 for command: gh %v", args)

		// Create a command that will execute via go-gh
		// We return an exec.Cmd for backward compatibility with existing code
		cmd := exec.Command("gh", args...)

		// Set up environment to ensure token is available
		if ghToken == "" && githubToken != "" {
			githubCLILog.Printf("GH_TOKEN not set, using GITHUB_TOKEN for gh CLI")
			cmd.Env = append(os.Environ(), "GH_TOKEN="+githubToken)
		}

		return cmd
	}

	// If no token is available, use default gh CLI behavior
	githubCLILog.Printf("No token available, using default gh CLI for command: gh %v", args)
	return exec.Command("gh", args...)
}

// ExecGHContext wraps gh CLI calls with context support and ensures proper token configuration.
// Similar to ExecGH but accepts a context for cancellation and timeout support.
//
// Usage:
//
//	cmd := ExecGHContext(ctx, "api", "/user")
//	output, err := cmd.Output()
func ExecGHContext(ctx context.Context, args ...string) *exec.Cmd {
	// Check if GH_TOKEN or GITHUB_TOKEN is available
	ghToken := os.Getenv("GH_TOKEN")
	githubToken := os.Getenv("GITHUB_TOKEN")

	// If we have a token, use go-gh/v2 which handles authentication properly
	if ghToken != "" || githubToken != "" {
		githubCLILog.Printf("Using gh CLI via go-gh/v2 for command with context: gh %v", args)

		// Create a command that will execute via go-gh with context
		cmd := exec.CommandContext(ctx, "gh", args...)

		// Set up environment to ensure token is available
		if ghToken == "" && githubToken != "" {
			githubCLILog.Printf("GH_TOKEN not set, using GITHUB_TOKEN for gh CLI")
			cmd.Env = append(os.Environ(), "GH_TOKEN="+githubToken)
		}

		return cmd
	}

	// If no token is available, use default gh CLI behavior
	githubCLILog.Printf("No token available, using default gh CLI with context for command: gh %v", args)
	return exec.CommandContext(ctx, "gh", args...)
}

// ExecGHWithOutput executes a gh CLI command using go-gh/v2 and returns stdout, stderr, and error.
// This is a convenience wrapper that directly uses go-gh/v2's Exec function.
//
// Usage:
//
//	stdout, stderr, err := ExecGHWithOutput("api", "/user")
func ExecGHWithOutput(args ...string) (stdout, stderr bytes.Buffer, err error) {
	githubCLILog.Printf("Executing gh CLI command via go-gh/v2: gh %v", args)
	return gh.Exec(args...)
}

// RunGH executes a gh CLI command with a spinner and returns the stdout output.
// The spinner is shown in interactive terminals to provide feedback during network operations.
// The spinnerMessage parameter describes what operation is being performed.
//
// Usage:
//
//	output, err := RunGH("Fetching user info...", "api", "/user")
func RunGH(spinnerMessage string, args ...string) ([]byte, error) {
	cmd := ExecGH(args...)

	// Show spinner in interactive terminals
	if tty.IsStderrTerminal() {
		spinner := console.NewSpinner(spinnerMessage)
		spinner.Start()
		output, err := cmd.Output()
		spinner.Stop()
		return output, err
	}

	return cmd.Output()
}

// RunGHCombined executes a gh CLI command with a spinner and returns combined stdout+stderr output.
// The spinner is shown in interactive terminals to provide feedback during network operations.
// Use this when you need to capture error messages from stderr.
//
// Usage:
//
//	output, err := RunGHCombined("Creating repository...", "repo", "create", "myrepo")
func RunGHCombined(spinnerMessage string, args ...string) ([]byte, error) {
	cmd := ExecGH(args...)

	// Show spinner in interactive terminals
	if tty.IsStderrTerminal() {
		spinner := console.NewSpinner(spinnerMessage)
		spinner.Start()
		output, err := cmd.CombinedOutput()
		spinner.Stop()
		return output, err
	}

	return cmd.CombinedOutput()
}
