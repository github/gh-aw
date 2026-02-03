//go:build !integration

package main

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/cli"
	"github.com/spf13/cobra"
)

// TestShortDescriptionConsistency verifies that all command Short descriptions
// follow CLI conventions:
// - No trailing punctuation (periods, exclamation marks, question marks)
// - This is a common convention for CLI tools (e.g., Git, kubectl, gh)
func TestShortDescriptionConsistency(t *testing.T) {

	// Create commands that have subcommands
	mcpCmd := cli.NewMCPCommand()
	prCmd := cli.NewPRCommand()

	// Collect all commands to test (including the parent commands with subcommands)
	allCommands := []*cobra.Command{
		rootCmd,
		newCmd,
		removeCmd,
		enableCmd,
		disableCmd,
		compileCmd,
		runCmd,
		versionCmd,
		cli.NewStatusCommand(),
		cli.NewInitCommand(),
		cli.NewLogsCommand(),
		cli.NewTrialCommand(validateEngine),
		cli.NewAddCommand(validateEngine),
		cli.NewUpdateCommand(validateEngine),
		cli.NewAuditCommand(),
		mcpCmd,
		cli.NewMCPServerCommand(),
		prCmd,
	}

	// Add MCP subcommands
	allCommands = append(allCommands, mcpCmd.Commands()...)

	// Add PR subcommands
	allCommands = append(allCommands, prCmd.Commands()...)

	// Check each command's Short description
	for _, cmd := range allCommands {
		t.Run("command "+cmd.Name()+" has no trailing punctuation", func(t *testing.T) {
			short := cmd.Short
			if short == "" {
				t.Skip("Command has no Short description")
			}
			if len(short) == 0 {
				t.Skip("Command has empty Short description")
			}

			// Check for trailing punctuation
			lastChar := short[len(short)-1:]
			if lastChar == "." || lastChar == "!" || lastChar == "?" {
				t.Errorf("Command '%s' Short description should not end with punctuation. Got: %q", cmd.Name(), short)
			}
		})
	}
}

// TestLongDescriptionHasSentences verifies that Long descriptions use proper
// sentences with punctuation, in contrast to Short descriptions.
// This is a documentation test that logs informational messages rather than failing.
func TestLongDescriptionHasSentences(t *testing.T) {
	// Sample commands that have Long descriptions
	commandsWithLong := []*cobra.Command{
		rootCmd,
		newCmd,
		removeCmd,
		enableCmd,
		disableCmd,
		compileCmd,
		runCmd,
		cli.NewMCPCommand(),
	}

	for _, cmd := range commandsWithLong {
		t.Run("command "+cmd.Name()+" Long description uses sentences", func(t *testing.T) {
			long := strings.TrimSpace(cmd.Long)
			if long == "" {
				t.Skip("Command has no Long description")
			}

			// Long descriptions should typically contain sentence-ending punctuation
			// This is just informational logging, not a strict requirement
			// (Long descriptions may use various punctuation styles: periods, colons, etc.)
			if !strings.Contains(long, ".") && !strings.Contains(long, ":") {
				t.Logf("Note: Command '%s' Long description may benefit from sentence punctuation", cmd.Name())
			}
		})
	}
}
