//go:build !integration

package main

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/cli"
	"github.com/spf13/cobra"
)

// TestCapitalizationConsistency verifies that command descriptions follow Option 2:
// - Use lowercase "agentic workflows" when referring generically to workflow files/functionality
// - Use capitalized "Agentic Workflows" only when explicitly referring to the product as a whole
func TestCapitalizationConsistency(t *testing.T) {

	// Test root command uses product name with capital
	if !strings.Contains(rootCmd.Short, "GitHub Agentic Workflows") {
		t.Errorf("Root command Short should use capitalized product name 'GitHub Agentic Workflows', got: %s", rootCmd.Short)
	}
	if !strings.Contains(rootCmd.Long, "GitHub Agentic Workflows") {
		t.Errorf("Root command Long should use capitalized product name 'GitHub Agentic Workflows', got: %s", rootCmd.Long)
	}

	// Version command is allowed to not have the product name in descriptions,
	// since it's output in the Run function instead.

	// Define commands that should use lowercase "agentic workflows" (generic usage)
	genericWorkflowCommands := []*cobra.Command{
		enableCmd,
		disableCmd,
		runCmd,
		cli.NewStatusCommand(),
		cli.NewInitCommand(),
		cli.NewLogsCommand(),
		cli.NewTrialCommand(validateEngine),
	}

	for _, cmd := range genericWorkflowCommands {
		// Directly check for incorrect usage of "Agentic Workflow" without "GitHub" prefix
		if strings.Contains(cmd.Short, "Agentic Workflow") && !strings.Contains(cmd.Short, "GitHub Agentic Workflow") {
			t.Errorf("Command '%s' Short description should use lowercase 'agentic workflow' for generic usage, not 'Agentic Workflow'. Got: %s", cmd.Name(), cmd.Short)
		}
		if strings.Contains(cmd.Long, "Agentic Workflow") && !strings.Contains(cmd.Long, "GitHub Agentic Workflow") {
			t.Errorf("Command '%s' Long description should use lowercase 'agentic workflow' for generic usage, not 'Agentic Workflow'. Got: %s", cmd.Name(), cmd.Long)
		}
	}
}

// TestMCPCommandCapitalization specifically tests MCP subcommands
func TestMCPCommandCapitalization(t *testing.T) {
	mcpCmd := cli.NewMCPCommand()

	// MCP command Long description should use lowercase "agentic workflows"
	if strings.Contains(mcpCmd.Long, "Agentic Workflows") && !strings.Contains(mcpCmd.Long, "GitHub Agentic Workflows") {
		t.Errorf("MCP command Long should use lowercase 'agentic workflows', got: %s", mcpCmd.Long)
	}

	// Check all MCP subcommands
	for _, subCmd := range mcpCmd.Commands() {
		if strings.Contains(subCmd.Short, "Agentic Workflows") && !strings.Contains(subCmd.Short, "GitHub Agentic Workflows") {
			t.Errorf("MCP subcommand '%s' Short should use lowercase 'agentic workflows', got: %s", subCmd.Name(), subCmd.Short)
		}
		if strings.Contains(subCmd.Long, "Agentic Workflows") && !strings.Contains(subCmd.Long, "GitHub Agentic Workflows") {
			t.Errorf("MCP subcommand '%s' Long should use lowercase 'agentic workflows', got: %s", subCmd.Name(), subCmd.Long)
		}
	}
}

// TestTechnicalTermsCapitalization verifies that technical terms remain capitalized
func TestTechnicalTermsCapitalization(t *testing.T) {
	// Technical terms that should remain capitalized
	technicalTerms := []string{"Markdown", "YAML", "MCP"}

	// Commands to check for technical term capitalization
	commandsToCheck := []*cobra.Command{
		compileCmd,
		newCmd,
	}

	// Check all commands and their subcommands
	for _, cmd := range commandsToCheck {
		checkCommandForTechnicalTerms(t, cmd, technicalTerms)

		// Also check subcommands
		for _, subCmd := range cmd.Commands() {
			checkCommandForTechnicalTerms(t, subCmd, technicalTerms)
		}
	}
}

// checkCommandForTechnicalTerms verifies technical terms are properly capitalized in a command
func checkCommandForTechnicalTerms(t *testing.T, cmd *cobra.Command, technicalTerms []string) {
	for _, term := range technicalTerms {
		lowerTerm := strings.ToLower(term)

		// Check Short description
		if strings.Contains(cmd.Short, lowerTerm) && !strings.Contains(cmd.Short, term) {
			t.Errorf("Command '%s' Short should capitalize technical term '%s', but found lowercase '%s'. Short: %s",
				cmd.Name(), term, lowerTerm, cmd.Short)
		}

		// Check Long description
		if strings.Contains(cmd.Long, lowerTerm) && !strings.Contains(cmd.Long, term) {
			t.Errorf("Command '%s' Long should capitalize technical term '%s', but found lowercase '%s'",
				cmd.Name(), term, lowerTerm)
		}
	}
}
