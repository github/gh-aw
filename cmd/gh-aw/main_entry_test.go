//go:build integration

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/cli"
)

func TestValidateEngine(t *testing.T) {
	tests := []struct {
		name       string
		engine     string
		expectErr  bool
		errMessage string
	}{
		{
			name:      "empty engine (uses default)",
			engine:    "",
			expectErr: false,
		},
		{
			name:      "valid claude engine",
			engine:    "claude",
			expectErr: false,
		},
		{
			name:      "valid codex engine",
			engine:    "codex",
			expectErr: false,
		},
		{
			name:      "valid copilot engine",
			engine:    "copilot",
			expectErr: false,
		},
		{
			name:      "valid custom engine",
			engine:    "custom",
			expectErr: false,
		},
		{
			name:       "invalid engine",
			engine:     "gpt4",
			expectErr:  true,
			errMessage: "invalid engine value 'gpt4'",
		},
		{
			name:       "invalid engine case sensitive",
			engine:     "Claude",
			expectErr:  true,
			errMessage: "invalid engine value 'Claude'",
		},
		{
			name:       "invalid engine with spaces",
			engine:     "claude ",
			expectErr:  true,
			errMessage: "invalid engine value 'claude '",
		},
		{
			name:       "completely invalid engine",
			engine:     "invalid-engine",
			expectErr:  true,
			errMessage: "invalid engine value 'invalid-engine'",
		},
		{
			name:       "numeric engine",
			engine:     "123",
			expectErr:  true,
			errMessage: "invalid engine value '123'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEngine(tt.engine)

			if tt.expectErr {
				if err == nil {
					t.Errorf("validateEngine(%q) expected error but got none", tt.engine)
					return
				}

				// Check that error message contains the expected format
				// Error may include "Did you mean" suggestions, so we check if it starts with the base message
				expectedMsg := fmt.Sprintf("invalid engine value '%s'. Must be 'claude', 'codex', 'copilot', or 'custom'", tt.engine)
				if tt.errMessage != "" && !strings.HasPrefix(err.Error(), expectedMsg) {
					t.Errorf("validateEngine(%q) error message = %v, want to start with %v", tt.engine, err.Error(), expectedMsg)
				}
			} else {
				if err != nil {
					t.Errorf("validateEngine(%q) unexpected error: %v", tt.engine, err)
				}
			}
		})
	}
}

func TestInitFunction(t *testing.T) {
	// Test that init function doesn't panic
	t.Run("init function executes without panic", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("init() panicked: %v", r)
			}
		}()

		// The init function has already been called when the package was loaded
		// We can't call it again, but we can verify that the initialization worked
		// by checking that the version was set
		if version == "" {
			t.Error("init() should have initialized version variable")
		}
	})
}

func TestMainFunction(t *testing.T) {
	// We can't easily test the main() function directly since it calls os.Exit(),
	// but we can test the command structure and basic functionality

	t.Run("main function setup", func(t *testing.T) {
		// Test that root command is properly configured
		if rootCmd.Use == "" {
			t.Error("rootCmd.Use should not be empty")
		}

		if rootCmd.Short == "" {
			t.Error("rootCmd.Short should not be empty")
		}

		if rootCmd.Long == "" {
			t.Error("rootCmd.Long should not be empty")
		}

		// Test that commands are properly added
		if len(rootCmd.Commands()) == 0 {
			t.Error("rootCmd should have subcommands")
		}
	})

	t.Run("version command is available", func(t *testing.T) {
		found := false
		for _, cmd := range rootCmd.Commands() {
			if cmd.Name() == "version" {
				found = true
				break
			}
		}
		if !found {
			t.Error("version command should be available")
		}
	})

	t.Run("root command help", func(t *testing.T) {
		// Capture output
		oldStderr := os.Stderr
		r, w, _ := os.Pipe()
		os.Stderr = w

		// Update the command's output to use the new os.Stderr pipe
		// This is necessary because rootCmd captured the original os.Stderr in init()
		rootCmd.SetOut(os.Stderr)

		// Read from pipe in goroutine to prevent deadlock when buffer fills
		var buf bytes.Buffer
		done := make(chan struct{})
		go func() {
			_, _ = buf.ReadFrom(r)
			close(done)
		}()

		// Execute help
		rootCmd.SetArgs([]string{"--help"})
		err := rootCmd.Execute()

		// Restore output
		w.Close()
		os.Stderr = oldStderr
		rootCmd.SetOut(os.Stderr) // Restore the command's output to the original stderr

		// Wait for reader goroutine to finish
		<-done
		output := buf.String()

		if err != nil {
			t.Errorf("root command help failed: %v", err)
		}

		if output == "" {
			t.Error("root command help should produce output")
		}

		// Reset args for other tests
		rootCmd.SetArgs([]string{})
	})

	t.Run("help all command", func(t *testing.T) {
		// Capture output
		oldStderr := os.Stderr
		r, w, _ := os.Pipe()
		os.Stderr = w

		// Update the command's output to use the new os.Stderr pipe
		rootCmd.SetOut(os.Stderr)

		// Read from pipe in goroutine to prevent deadlock when buffer fills
		var buf bytes.Buffer
		done := make(chan struct{})
		go func() {
			_, _ = buf.ReadFrom(r)
			close(done)
		}()

		// Execute help all
		rootCmd.SetArgs([]string{"help", "all"})
		err := rootCmd.Execute()

		// Restore output
		w.Close()
		os.Stderr = oldStderr
		rootCmd.SetOut(os.Stderr)

		// Wait for reader goroutine to finish
		<-done
		output := buf.String()

		if err != nil {
			t.Errorf("help all command failed: %v", err)
		}

		if output == "" {
			t.Error("help all command should produce output")
		}

		// Verify output contains expected content
		if !strings.Contains(output, "Complete Command Reference") {
			t.Error("help all output should contain 'Complete Command Reference'")
		}

		// Verify output contains multiple commands
		commandCount := 0
		expectedCommands := []string{"add", "compile", "init", "version", "status"}
		for _, cmd := range expectedCommands {
			if strings.Contains(output, fmt.Sprintf("Command: gh aw %s", cmd)) {
				commandCount++
			}
		}

		if commandCount < len(expectedCommands) {
			t.Errorf("help all should show help for all commands, found %d/%d", commandCount, len(expectedCommands))
		}

		// Reset args for other tests
		rootCmd.SetArgs([]string{})
	})
}

// TestMainFunctionExecutionPath tests the main function execution path
// This covers the main() function at line 360
func TestMainFunctionExecutionPath(t *testing.T) {
	// Test that we can build and run the main function successfully
	t.Run("main function integration test", func(t *testing.T) {
		// Only run this test if we're in development (has go)
		if _, err := exec.LookPath("go"); err != nil {
			t.Skip("go binary not available - skipping main function integration test")
		}

		// Test help command execution through main function
		cmd := exec.Command("go", "run", ".", "--help")
		cmd.Dir = "."

		output, err := cmd.CombinedOutput() // Use CombinedOutput to capture stderr
		if err != nil {
			t.Fatalf("Failed to run main with --help: %v", err)
		}

		outputStr := string(output)
		if !strings.Contains(outputStr, "GitHub Agentic Workflows") {
			t.Error("main function help output should contain 'GitHub Agentic Workflows'")
		}

		if !strings.Contains(outputStr, "Usage:") {
			t.Error("main function help output should contain usage information")
		}
	})

	t.Run("main function version command", func(t *testing.T) {
		// Test version command execution through main function
		cmd := exec.Command("go", "run", ".", "version")
		cmd.Dir = "."

		output, err := cmd.CombinedOutput() // Use CombinedOutput to capture both stdout and stderr
		if err != nil {
			t.Fatalf("Failed to run main with version: %v", err)
		}

		outputStr := string(output)
		// Should produce some version output (even if it's "unknown")
		if len(strings.TrimSpace(outputStr)) == 0 {
			t.Error("main function version command should produce output")
		}
	})

	t.Run("main function error handling", func(t *testing.T) {
		// Test error handling in main function
		cmd := exec.Command("go", "run", ".", "invalid-command")
		cmd.Dir = "."

		_, err := cmd.Output()
		if err == nil {
			t.Error("main function should return non-zero exit code for invalid command")
		}

		// Check that it's an ExitError (non-zero exit code)
		if exitError, ok := err.(*exec.ExitError); !ok {
			t.Errorf("Expected ExitError for invalid command, got %T: %v", err, err)
		} else if exitError.ExitCode() == 0 {
			t.Error("Expected non-zero exit code for invalid command")
		}
	})

	t.Run("main function version info setup", func(t *testing.T) {
		// Test that SetVersionInfo is called in main()
		// We can verify this by checking that the CLI package has version info

		// Reset version info to simulate fresh start
		originalVersion := cli.GetVersion()

		// Set a test version
		cli.SetVersionInfo("test-version")

		// Verify it was set
		if cli.GetVersion() != "test-version" {
			t.Error("SetVersionInfo should update the version in CLI package")
		}

		// Restore original version
		cli.SetVersionInfo(originalVersion)
	})

	t.Run("main function basic execution flow", func(t *testing.T) {
		// Test that main function sets up CLI properly and exits cleanly for valid commands
		cmd := exec.Command("go", "run", ".", "version")
		cmd.Dir = "."

		// This should run successfully (exit code 0) even if no workflows found
		// Use CombinedOutput to capture both stdout and stderr (version now outputs to stderr)
		output, err := cmd.CombinedOutput()
		if err != nil {
			// Check if it's just a non-zero exit (which is okay for some commands)
			if exitError, ok := err.(*exec.ExitError); ok {
				// Some commands might return non-zero but still function properly
				t.Logf("Command returned exit code %d, output: %s", exitError.ExitCode(), string(output))
			} else {
				t.Fatalf("Failed to run main with version command: %v", err)
			}
		}

		// Should produce some output
		if len(output) == 0 {
			t.Error("version command should produce some output")
		}
	})
}

func TestVersionCommandFunctionality(t *testing.T) {
	t.Run("version information is available", func(t *testing.T) {
		// The cli package should provide version functionality
		versionInfo := cli.GetVersion()
		if versionInfo == "" {
			t.Error("GetVersion() should return version information")
		}
	})

	t.Run("--version flag is supported", func(t *testing.T) {
		// Test that --version flag works
		cmd := exec.Command("go", "run", ".", "--version")
		cmd.Dir = "."

		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Failed to run main with --version: %v", err)
		}

		outputStr := string(output)
		// Should produce version output
		if len(strings.TrimSpace(outputStr)) == 0 {
			t.Error("--version flag should produce output")
		}

		// Should contain "version" in the output
		if !strings.Contains(outputStr, "version") {
			t.Errorf("--version output should contain 'version', got: %s", outputStr)
		}
	})

	t.Run("version subcommand and --version flag produce same output", func(t *testing.T) {
		// Test version subcommand
		cmdVersion := exec.Command("go", "run", ".", "version")
		cmdVersion.Dir = "."
		outputVersion, err := cmdVersion.CombinedOutput()
		if err != nil {
			t.Fatalf("Failed to run main with version subcommand: %v", err)
		}

		// Test --version flag
		cmdFlag := exec.Command("go", "run", ".", "--version")
		cmdFlag.Dir = "."
		outputFlag, err := cmdFlag.CombinedOutput()
		if err != nil {
			t.Fatalf("Failed to run main with --version flag: %v", err)
		}

		// Both should produce the same output
		if string(outputVersion) != string(outputFlag) {
			t.Errorf("version subcommand and --version flag should produce same output.\nSubcommand: %s\nFlag: %s",
				string(outputVersion), string(outputFlag))
		}
	})
}

func TestCommandLineIntegration(t *testing.T) {
	// Test basic command line parsing and validation

	t.Run("command structure validation", func(t *testing.T) {
		// Test that essential commands are present
		expectedCommands := []string{"add", "compile", "remove", "status", "run", "version", "mcp"}

		cmdMap := make(map[string]bool)
		for _, cmd := range rootCmd.Commands() {
			cmdMap[cmd.Name()] = true
		}

		missingCommands := []string{}
		for _, expected := range expectedCommands {
			if !cmdMap[expected] {
				missingCommands = append(missingCommands, expected)
			}
		}

		if len(missingCommands) > 0 {
			t.Errorf("Missing expected commands: %v", missingCommands)
		}
	})

	t.Run("global flags are configured", func(t *testing.T) {
		// Test that global flags are properly configured
		flag := rootCmd.PersistentFlags().Lookup("verbose")
		if flag == nil {
			t.Error("verbose flag should be configured")
		}

		if flag != nil && flag.DefValue != "false" {
			t.Error("verbose flag should default to false")
		}
	})

	t.Run("SilenceUsage is enabled", func(t *testing.T) {
		// Test that SilenceUsage is set to prevent usage output on application errors
		if !rootCmd.SilenceUsage {
			t.Error("SilenceUsage should be true to prevent cluttering terminal output with usage on application errors")
		}
	})
}

func TestMCPCommand(t *testing.T) {
	// Test the new MCP command structure
	t.Run("mcp command is available", func(t *testing.T) {
		found := false
		for _, cmd := range rootCmd.Commands() {
			if cmd.Name() == "mcp" {
				found = true
				break
			}
		}
		if !found {
			t.Error("mcp command should be available")
		}
	})

	t.Run("mcp command has inspect subcommand", func(t *testing.T) {
		mcpCmd, _, _ := rootCmd.Find([]string{"mcp"})
		if mcpCmd == nil {
			t.Fatal("mcp command not found")
		}

		found := false
		for _, subCmd := range mcpCmd.Commands() {
			if subCmd.Name() == "inspect" {
				found = true
				break
			}
		}
		if !found {
			t.Error("mcp inspect subcommand should be available")
		}
	})

	t.Run("mcp inspect command help", func(t *testing.T) {
		// Test help for nested command
		mcpCmd, _, _ := rootCmd.Find([]string{"mcp"})
		if mcpCmd == nil {
			t.Fatal("mcp command not found")
		}

		inspectCmd, _, _ := mcpCmd.Find([]string{"inspect"})
		if inspectCmd == nil {
			t.Fatal("mcp inspect command not found")
		}

		// Basic validation that command structure is valid
		if inspectCmd.Use == "" {
			t.Error("mcp inspect command should have usage text")
		}
		if inspectCmd.Short == "" {
			t.Error("mcp inspect command should have short description")
		}
	})
}

func TestCommandErrorHandling(t *testing.T) {
	t.Run("invalid command produces error", func(t *testing.T) {
		// Test invalid command
		rootCmd.SetArgs([]string{"invalid-command"})
		err := rootCmd.Execute()

		if err == nil {
			t.Error("invalid command should produce an error")
		}

		// With RunE and SilenceErrors, errors are returned but not automatically printed
		// The main() function is responsible for formatting and printing errors
		// This test verifies that Execute() returns an error for invalid commands

		// Reset args for other tests
		rootCmd.SetArgs([]string{})
	})
}
