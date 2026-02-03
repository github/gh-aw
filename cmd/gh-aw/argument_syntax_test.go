//go:build integration

package main

import (
	"regexp"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/cli"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestArgumentSyntaxConsistency verifies that command argument syntax is consistent with validators
func TestArgumentSyntaxConsistency(t *testing.T) {
	tests := []struct {
		name           string
		command        *cobra.Command
		expectedUse    string
		argsValidator  string // Description of the Args validator
		shouldValidate func(*cobra.Command) error
	}{
		// Commands with required arguments (using angle brackets <>)
		{
			name:           "audit command requires run-id",
			command:        cli.NewAuditCommand(),
			expectedUse:    "audit <run-id>",
			argsValidator:  "ExactArgs(1)",
			shouldValidate: func(cmd *cobra.Command) error { return cmd.Args(cmd, []string{"123456"}) },
		},
		{
			name:           "trial command requires workflow-spec",
			command:        cli.NewTrialCommand(validateEngine),
			expectedUse:    "trial <workflow-spec>...",
			argsValidator:  "MinimumNArgs(1)",
			shouldValidate: func(cmd *cobra.Command) error { return cmd.Args(cmd, []string{"test"}) },
		},
		{
			name:           "add command requires workflow",
			command:        cli.NewAddCommand(validateEngine),
			expectedUse:    "add <workflow>...",
			argsValidator:  "MinimumNArgs(1)",
			shouldValidate: func(cmd *cobra.Command) error { return cmd.Args(cmd, []string{"test"}) },
		},

		// Commands with optional arguments (using square brackets [])
		{
			name:           "run command has optional workflow",
			command:        runCmd,
			expectedUse:    "run [workflow]...",
			argsValidator:  "no validator (all optional)",
			shouldValidate: func(cmd *cobra.Command) error { return nil },
		},
		{
			name:           "new command has optional workflow",
			command:        newCmd,
			expectedUse:    "new [workflow]",
			argsValidator:  "MaximumNArgs(1)",
			shouldValidate: func(cmd *cobra.Command) error { return cmd.Args(cmd, []string{}) },
		},
		{
			name:           "remove command has optional pattern",
			command:        removeCmd,
			expectedUse:    "remove [pattern]",
			argsValidator:  "no validator (all optional)",
			shouldValidate: func(cmd *cobra.Command) error { return nil },
		},
		{
			name:           "enable command has optional workflow",
			command:        enableCmd,
			expectedUse:    "enable [workflow]...",
			argsValidator:  "no validator (all optional)",
			shouldValidate: func(cmd *cobra.Command) error { return nil },
		},
		{
			name:           "disable command has optional workflow",
			command:        disableCmd,
			expectedUse:    "disable [workflow]...",
			argsValidator:  "no validator (all optional)",
			shouldValidate: func(cmd *cobra.Command) error { return nil },
		},
		{
			name:           "compile command has optional workflow",
			command:        compileCmd,
			expectedUse:    "compile [workflow]...",
			argsValidator:  "no validator (all optional)",
			shouldValidate: func(cmd *cobra.Command) error { return nil },
		},
		{
			name:           "logs command has optional workflow",
			command:        cli.NewLogsCommand(),
			expectedUse:    "logs [workflow]",
			argsValidator:  "no validator (all optional)",
			shouldValidate: func(cmd *cobra.Command) error { return nil },
		},
		{
			name:           "fix command has optional workflow",
			command:        cli.NewFixCommand(),
			expectedUse:    "fix [workflow]...",
			argsValidator:  "no validator (all optional)",
			shouldValidate: func(cmd *cobra.Command) error { return nil },
		},
		{
			name:           "update command has optional workflow",
			command:        cli.NewUpdateCommand(validateEngine),
			expectedUse:    "update [workflow]...",
			argsValidator:  "no validator (all optional)",
			shouldValidate: func(cmd *cobra.Command) error { return nil },
		},
		{
			name:           "status command has optional pattern",
			command:        cli.NewStatusCommand(),
			expectedUse:    "status [pattern]",
			argsValidator:  "no validator (all optional)",
			shouldValidate: func(cmd *cobra.Command) error { return nil },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup step - ensure command is valid
			require.NotNil(t, tt.command, "Test case %q requires valid command", tt.name)

			// Check Use pattern
			use := tt.command.Use
			assert.Equal(t, tt.expectedUse, use,
				"Command %q should have expected Use syntax", tt.command.Name())

			// Validate the Use pattern format
			assert.True(t, isValidUseSyntax(use),
				"Command %q Use=%q should follow valid CLI syntax patterns",
				tt.command.Name(), use)

			// Skip validation check if not provided
			if tt.shouldValidate == nil {
				return
			}

			// Validate that the Args validator works as expected
			err := tt.shouldValidate(tt.command)
			require.NoError(t, err,
				"Args validator (%s) should accept valid test arguments for command %q",
				tt.argsValidator, tt.command.Name())
		})
	}
}

// TestMCPSubcommandArgumentSyntax verifies MCP subcommands have consistent syntax
func TestMCPSubcommandArgumentSyntax(t *testing.T) {
	mcpCmd := cli.NewMCPCommand()

	tests := []struct {
		name        string
		subcommand  string
		expectedUse string
	}{
		{
			name:        "mcp list has optional workflow",
			subcommand:  "list",
			expectedUse: "list [workflow]",
		},
		{
			name:        "mcp inspect has optional workflow",
			subcommand:  "inspect",
			expectedUse: "inspect [workflow]",
		},
		{
			name:        "mcp add has optional workflow and server",
			subcommand:  "add",
			expectedUse: "add [workflow] [server]",
		},
		{
			name:        "mcp list-tools requires server with optional workflow",
			subcommand:  "list-tools",
			expectedUse: "list-tools <server> [workflow]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Find the subcommand - setup step
			foundCmd := findSubcommand(mcpCmd, tt.subcommand)
			require.NotNil(t, foundCmd,
				"Test requires MCP subcommand %q to exist in command list",
				tt.subcommand)

			use := foundCmd.Use
			assert.Equal(t, tt.expectedUse, use,
				"MCP subcommand %q should have expected Use syntax", tt.subcommand)

			// Validate the Use pattern format
			assert.True(t, isValidUseSyntax(use),
				"MCP subcommand %q Use=%q should follow valid syntax pattern",
				tt.subcommand, use)
		})
	}
}

// TestPRSubcommandArgumentSyntax verifies PR subcommands have consistent syntax
func TestPRSubcommandArgumentSyntax(t *testing.T) {
	prCmd := cli.NewPRCommand()

	tests := []struct {
		name        string
		subcommand  string
		expectedUse string
	}{
		{
			name:        "pr transfer requires pr-url",
			subcommand:  "transfer",
			expectedUse: "transfer <pr-url>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Find the subcommand - setup step
			foundCmd := findSubcommand(prCmd, tt.subcommand)
			require.NotNil(t, foundCmd,
				"Test requires PR subcommand %q to exist in command list",
				tt.subcommand)

			use := foundCmd.Use
			assert.Equal(t, tt.expectedUse, use,
				"PR subcommand %q should have expected Use syntax", tt.subcommand)

			// Validate the Use pattern format
			assert.True(t, isValidUseSyntax(use),
				"PR subcommand %q Use=%q should follow valid syntax pattern",
				tt.subcommand, use)
		})
	}
}

// findSubcommand finds a subcommand by name in a command
func findSubcommand(cmd *cobra.Command, name string) *cobra.Command {
	for _, subcmd := range cmd.Commands() {
		if subcmd.Name() == name {
			return subcmd
		}
	}
	return nil
}

// isValidUseSyntax validates the Use syntax pattern
func isValidUseSyntax(use string) bool {
	// Pattern: command-name [<required>|[optional]] [...]
	// Required arguments use angle brackets: <arg>
	// Optional arguments use square brackets: [arg]
	// Multiple arguments indicated with ellipsis: ...

	parts := strings.Fields(use)
	if len(parts) == 0 {
		return false
	}

	// First part should be the command name (no brackets or special chars except hyphen)
	commandName := parts[0]
	if !regexp.MustCompile(`^[a-z][a-z0-9-]*$`).MatchString(commandName) {
		return false
	}

	// Check argument patterns
	for i := 1; i < len(parts); i++ {
		arg := parts[i]

		// Check for valid patterns:
		// - <arg>     (required)
		// - <arg>...  (required multiple)
		// - [arg]     (optional)
		// - [arg]...  (optional multiple)

		validPatterns := []string{
			`^<[a-z][a-z0-9-]*>$`,         // <required>
			`^<[a-z][a-z0-9-]*>\.\.\.$`,   // <required>...
			`^\[[a-z][a-z0-9-]*\]$`,       // [optional]
			`^\[[a-z][a-z0-9-]*\]\.\.\.$`, // [optional]...
		}

		isValid := false
		for _, pattern := range validPatterns {
			if regexp.MustCompile(pattern).MatchString(arg) {
				isValid = true
				break
			}
		}

		if !isValid {
			return false
		}
	}

	return true
}

// TestArgumentNamingConventions verifies that argument names follow conventions
func TestArgumentNamingConventions(t *testing.T) {
	// Collect all commands
	commands := []*cobra.Command{
		newCmd,
		removeCmd,
		enableCmd,
		disableCmd,
		compileCmd,
		runCmd,
		cli.NewAddCommand(validateEngine),
		cli.NewUpdateCommand(validateEngine),
		cli.NewTrialCommand(validateEngine),
		cli.NewLogsCommand(),
		cli.NewAuditCommand(),
		cli.NewFixCommand(),
		cli.NewStatusCommand(),
		cli.NewMCPCommand(),
		cli.NewPRCommand(),
	}

	// Also collect subcommands
	for _, cmd := range commands {
		commands = append(commands, cmd.Commands()...)
	}

	// Define naming conventions
	conventions := map[string]string{
		"workflow":      "Workflow-related commands should use 'workflow' for consistency",
		"pattern":       "Filter/search commands should use 'pattern' or 'filter'",
		"run-id":        "Audit command should use 'run-id' for clarity",
		"workflow-spec": "Trial command should use 'workflow-spec' to indicate special format",
		"pr-url":        "PR transfer should use 'pr-url' for clarity",
		"server":        "MCP commands should use 'server' for MCP server names",
	}

	for _, cmd := range commands {
		use := cmd.Use
		parts := strings.Fields(use)

		for i := 1; i < len(parts); i++ {
			arg := parts[i]

			// Extract the argument name (remove brackets and ellipsis)
			argName := arg
			argName = strings.TrimPrefix(argName, "<")
			argName = strings.TrimPrefix(argName, "[")
			argName = strings.TrimSuffix(argName, "...")
			argName = strings.TrimSuffix(argName, ">")
			argName = strings.TrimSuffix(argName, "]")

			// Verify argument name follows conventions
			if reason, exists := conventions[argName]; exists {
				t.Logf("✓ Command %q uses conventional argument name %q: %s", cmd.Name(), argName, reason)
			}

			// Argument names should be lowercase with hyphens
			assert.Regexp(t, `^[a-z][a-z0-9-]*$`, argName,
				"Command %q argument %q should use lowercase with hyphens only",
				cmd.Name(), argName)
		}
	}
}
