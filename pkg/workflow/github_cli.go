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

// setupGHCommand creates an exec.Cmd for gh CLI with proper token configuration.
// This is the core implementation shared by ExecGH and ExecGHContext.
// When ctx is nil, it uses exec.Command; when ctx is provided, it uses exec.CommandContext.
func setupGHCommand(ctx context.Context, args ...string) *exec.Cmd {
	// Check if GH_TOKEN or GITHUB_TOKEN is available
	ghToken := os.Getenv("GH_TOKEN")
	githubToken := os.Getenv("GITHUB_TOKEN")

	var cmd *exec.Cmd
	if ctx != nil {
		cmd = exec.CommandContext(ctx, "gh", args...)
		if ghToken != "" || githubToken != "" {
			githubCLILog.Printf("Using gh CLI via go-gh/v2 for command with context: gh %v", args)
		} else {
			githubCLILog.Printf("No token available, using default gh CLI with context for command: gh %v", args)
		}
	} else {
		cmd = exec.Command("gh", args...)
		if ghToken != "" || githubToken != "" {
			githubCLILog.Printf("Using gh CLI via go-gh/v2 for command: gh %v", args)
		} else {
			githubCLILog.Printf("No token available, using default gh CLI for command: gh %v", args)
		}
	}

	// Set up environment to ensure token is available
	// Only add GH_TOKEN if it's not set but GITHUB_TOKEN is available
	if ghToken == "" && githubToken != "" {
		githubCLILog.Printf("GH_TOKEN not set, using GITHUB_TOKEN for gh CLI")
		cmd.Env = append(os.Environ(), "GH_TOKEN="+githubToken)
	}

	return cmd
}

// ExecGH wraps gh CLI calls and ensures proper token configuration.
// It uses go-gh/v2 to execute gh commands when GH_TOKEN or GITHUB_TOKEN is available,
// otherwise falls back to direct exec.Command for backward compatibility.
//
// Usage:
//
//	cmd := ExecGH("api", "/user")
//	output, err := cmd.Output()
func ExecGH(args ...string) *exec.Cmd {
	//nolint:staticcheck // Passing nil context to use exec.Command instead of exec.CommandContext
	return setupGHCommand(nil, args...)
}

// ExecGHContext wraps gh CLI calls with context support and ensures proper token configuration.
// Similar to ExecGH but accepts a context for cancellation and timeout support.
//
// Usage:
//
//	cmd := ExecGHContext(ctx, "api", "/user")
//	output, err := cmd.Output()
func ExecGHContext(ctx context.Context, args ...string) *exec.Cmd {
	return setupGHCommand(ctx, args...)
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

// runGHWithSpinner executes a gh CLI command with a spinner and returns the output.
// This is the core implementation shared by RunGH and RunGHCombined.
func runGHWithSpinner(spinnerMessage string, combined bool, args ...string) ([]byte, error) {
	cmd := ExecGH(args...)

	// Show spinner in interactive terminals
	if tty.IsStderrTerminal() {
		spinner := console.NewSpinner(spinnerMessage)
		spinner.Start()
		var output []byte
		var err error
		if combined {
			output, err = cmd.CombinedOutput()
		} else {
			output, err = cmd.Output()
		}
		spinner.Stop()
		return output, err
	}

	if combined {
		return cmd.CombinedOutput()
	}
	return cmd.Output()
}

// RunGH executes a gh CLI command with a spinner and returns the stdout output.
// The spinner is shown in interactive terminals to provide feedback during network operations.
// The spinnerMessage parameter describes what operation is being performed.
//
// Usage:
//
//	output, err := RunGH("Fetching user info...", "api", "/user")
func RunGH(spinnerMessage string, args ...string) ([]byte, error) {
	return runGHWithSpinner(spinnerMessage, false, args...)
}

// RunGHCombined executes a gh CLI command with a spinner and returns combined stdout+stderr output.
// The spinner is shown in interactive terminals to provide feedback during network operations.
// Use this when you need to capture error messages from stderr.
//
// Usage:
//
//	output, err := RunGHCombined("Creating repository...", "repo", "create", "myrepo")
func RunGHCombined(spinnerMessage string, args ...string) ([]byte, error) {
	return runGHWithSpinner(spinnerMessage, true, args...)
}
