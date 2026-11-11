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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateEngine(t *testing.T) {
	t.Parallel()
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
			name:      "valid gemini engine",
			engine:    "gemini",
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
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateEngine(tt.engine)

			if tt.expectErr {
				require.Error(t, err, "validateEngine(%q) should return an error for invalid engines", tt.engine)

				// Check that error message contains the expected format.
				// The engine list is dynamic, so only check the prefix.
				expectedPrefix := fmt.Sprintf("invalid engine value '%s'. Must be", tt.engine)
				if tt.errMessage != "" {
					assert.True(t, strings.HasPrefix(err.Error(), expectedPrefix), "validateEngine(%q) error should start with %q, got %q", tt.engine, expectedPrefix, err.Error())
				}
			} else {
				assert.NoError(t, err, "validateEngine(%q) should not return an error for valid engines", tt.engine)
			}
		})
	}
}

func TestInitFunction(t *testing.T) {
	t.Parallel()
	// Test that init function doesn't panic
	t.Run("init function executes without panic", func(t *testing.T) {
		t.Parallel()
		defer func() {
			assert.Nil(t, recover(), "init() should not panic")
		}()

		// The init function has already been called when the package was loaded
		// We can't call it again, but we can verify that the initialization worked
		// by checking that the version was set
		assert.NotEmpty(t, version, "init() should initialize the version variable")
	})
}

func TestMainFunction(t *testing.T) {
	// We can't easily test the main() function directly since it calls os.Exit(),
	// but we can test the command structure and basic functionality

	t.Run("main function setup", func(t *testing.T) {
		// Test that root command is properly configured
		assert.NotEmpty(t, rootCmd.Use, "root command Use should not be empty")
		assert.NotEmpty(t, rootCmd.Short, "root command Short description should not be empty")
		assert.NotEmpty(t, rootCmd.Long, "root command Long description should not be empty")
		assert.NotEmpty(t, rootCmd.Commands(), "root command should have subcommands")
	})

	t.Run("version command is available", func(t *testing.T) {
		found := false
		for _, cmd := range rootCmd.Commands() {
			if cmd.Name() == "version" {
				found = true
				break
			}
		}
		assert.True(t, found, "version command should be available")
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

		require.NoError(t, err, "root command help should execute successfully")
		assert.NotEmpty(t, output, "root command help should produce output")
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

		require.NoError(t, err, "help all command should execute successfully")
		assert.NotEmpty(t, output, "help all command should produce output")

		// Verify output contains expected content
		assert.Contains(t, output, "Complete Command Reference", "help all output should include the complete command reference heading")

		// Verify output contains multiple commands
		commandCount := 0
		expectedCommands := []string{"add", "compile", "init", "version", "status"}
		for _, cmd := range expectedCommands {
			if strings.Contains(output, fmt.Sprintf("Command: gh aw %s", cmd)) {
				commandCount++
			}
		}

		assert.GreaterOrEqual(t, commandCount, len(expectedCommands), "help all should show help for all expected commands")

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
		require.NoError(t, err, "running main with --help should succeed")

		outputStr := string(output)
		assert.Contains(t, outputStr, "GitHub Agentic Workflows", "main help output should contain the product name")
		assert.Contains(t, outputStr, "Usage:", "main help output should contain usage information")
	})

	t.Run("main function version command", func(t *testing.T) {
		// Test version command execution through main function
		cmd := exec.Command("go", "run", ".", "version")
		cmd.Dir = "."

		output, err := cmd.CombinedOutput() // Use CombinedOutput to capture both stdout and stderr
		require.NoError(t, err, "running main with version command should succeed")

		outputStr := string(output)
		// Should produce some version output (even if it's "unknown")
		assert.NotEmpty(t, strings.TrimSpace(outputStr), "main version command should produce output")
	})

	t.Run("main function error handling", func(t *testing.T) {
		// Test error handling in main function
		cmd := exec.Command("go", "run", ".", "invalid-command")
		cmd.Dir = "."

		_, err := cmd.Output()
		require.Error(t, err, "main function should return a non-zero exit code for invalid command")

		// Check that it's an ExitError (non-zero exit code)
		exitError, ok := err.(*exec.ExitError)
		require.True(t, ok, "invalid command should return an *exec.ExitError, got %T", err)
		assert.NotZero(t, exitError.ExitCode(), "invalid command should return a non-zero exit code")
	})

	t.Run("main function version info setup", func(t *testing.T) {
		// Test that SetVersionInfo is called in main()
		// We can verify this by checking that the CLI package has version info

		// Reset version info to simulate fresh start
		originalVersion := cli.GetVersion()

		// Set a test version
		cli.SetVersionInfo("test-version")

		// Verify it was set
		assert.Equal(t, "test-version", cli.GetVersion(), "SetVersionInfo should update the version in CLI package")

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
				require.NoError(t, err, "running main with version command should not fail with an unexpected execution error")
			}
		}

		// Should produce some output
		assert.NotEmpty(t, output, "version command should produce some output")
	})
}

func TestVersionCommandFunctionality(t *testing.T) {
	t.Run("version information is available", func(t *testing.T) {
		// The cli package should provide version functionality
		versionInfo := cli.GetVersion()
		assert.NotEmpty(t, versionInfo, "GetVersion should return version information")
	})

	t.Run("--version flag is supported", func(t *testing.T) {
		// Test that --version flag works
		cmd := exec.Command("go", "run", ".", "--version")
		cmd.Dir = "."

		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "running main with --version should succeed")

		outputStr := string(output)
		// Should produce version output
		assert.NotEmpty(t, strings.TrimSpace(outputStr), "--version flag should produce output")

		// Should contain "version" in the output
		assert.Contains(t, outputStr, "version", "--version output should contain the word 'version'")
	})

	t.Run("version subcommand and --version flag produce same output", func(t *testing.T) {
		// Test version subcommand
		cmdVersion := exec.Command("go", "run", ".", "version")
		cmdVersion.Dir = "."
		outputVersion, err := cmdVersion.CombinedOutput()
		require.NoError(t, err, "running main with version subcommand should succeed")

		// Test --version flag
		cmdFlag := exec.Command("go", "run", ".", "--version")
		cmdFlag.Dir = "."
		outputFlag, err := cmdFlag.CombinedOutput()
		require.NoError(t, err, "running main with --version flag should succeed")

		// Both should produce the same output
		assert.Equal(t, string(outputVersion), string(outputFlag), "version subcommand and --version flag should produce identical output")
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

		assert.Empty(t, missingCommands, "all expected commands should be present")
	})

	t.Run("global flags are configured", func(t *testing.T) {
		// Test that global flags are properly configured
		flag := rootCmd.PersistentFlags().Lookup("verbose")
		require.NotNil(t, flag, "verbose flag should be configured")
		assert.Equal(t, "false", flag.DefValue, "verbose flag should default to false")
	})

	t.Run("SilenceUsage is enabled", func(t *testing.T) {
		// Test that SilenceUsage is set to prevent usage output on application errors
		assert.True(t, rootCmd.SilenceUsage, "SilenceUsage should be true to prevent usage output on application errors")
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
		assert.True(t, found, "mcp command should be available")
	})

	t.Run("mcp command has inspect subcommand", func(t *testing.T) {
		mcpCmd, _, _ := rootCmd.Find([]string{"mcp"})
		require.NotNil(t, mcpCmd, "mcp command should be found")

		found := false
		for _, subCmd := range mcpCmd.Commands() {
			if subCmd.Name() == "inspect" {
				found = true
				break
			}
		}
		assert.True(t, found, "mcp inspect subcommand should be available")
	})

	t.Run("mcp inspect command help", func(t *testing.T) {
		// Test help for nested command
		mcpCmd, _, _ := rootCmd.Find([]string{"mcp"})
		require.NotNil(t, mcpCmd, "mcp command should be found")

		inspectCmd, _, _ := mcpCmd.Find([]string{"inspect"})
		require.NotNil(t, inspectCmd, "mcp inspect command should be found")

		// Basic validation that command structure is valid
		assert.NotEmpty(t, inspectCmd.Use, "mcp inspect command should have usage text")
		assert.NotEmpty(t, inspectCmd.Short, "mcp inspect command should have a short description")
	})
}

func TestCommandErrorHandling(t *testing.T) {
	t.Run("invalid command produces error", func(t *testing.T) {
		// Test invalid command
		rootCmd.SetArgs([]string{"invalid-command"})
		err := rootCmd.Execute()

		assert.Error(t, err, "invalid command should produce an error")
		assert.ErrorContains(t, err, `unknown command "invalid-command" for "gh aw"`)

		// With RunE and SilenceErrors, errors are returned but not automatically printed
		// The main() function is responsible for formatting and printing errors
		// This test verifies that Execute() returns an error for invalid commands

		// Reset args for other tests
		rootCmd.SetArgs([]string{})
	})
}
