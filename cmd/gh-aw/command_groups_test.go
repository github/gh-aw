//go:build integration

package main

import (
	"testing"

	"github.com/spf13/cobra"
)

// TestCommandGroupAssignments verifies that commands are assigned to appropriate groups
func TestCommandGroupAssignments(t *testing.T) {
	tests := []struct {
		name            string
		commandName     string
		expectedGroup   string
		shouldHaveGroup bool
	}{
		// Setup Commands
		{name: "init command in setup group", commandName: "init", expectedGroup: "setup", shouldHaveGroup: true},
		{name: "new command in setup group", commandName: "new", expectedGroup: "setup", shouldHaveGroup: true},
		{name: "add command in setup group", commandName: "add", expectedGroup: "setup", shouldHaveGroup: true},
		{name: "remove command in setup group", commandName: "remove", expectedGroup: "setup", shouldHaveGroup: true},
		{name: "update command in setup group", commandName: "update", expectedGroup: "setup", shouldHaveGroup: true},
		{name: "secrets command in setup group", commandName: "secrets", expectedGroup: "setup", shouldHaveGroup: true},

		// Development Commands
		{name: "compile command in development group", commandName: "compile", expectedGroup: "development", shouldHaveGroup: true},
		{name: "mcp command in development group", commandName: "mcp", expectedGroup: "development", shouldHaveGroup: true},
		{name: "status command in development group", commandName: "status", expectedGroup: "development", shouldHaveGroup: true},
		{name: "fix command in development group", commandName: "fix", expectedGroup: "development", shouldHaveGroup: true},

		// Execution Commands
		{name: "run command in execution group", commandName: "run", expectedGroup: "execution", shouldHaveGroup: true},
		{name: "enable command in execution group", commandName: "enable", expectedGroup: "execution", shouldHaveGroup: true},
		{name: "disable command in execution group", commandName: "disable", expectedGroup: "execution", shouldHaveGroup: true},
		{name: "trial command in execution group", commandName: "trial", expectedGroup: "execution", shouldHaveGroup: true},

		// Analysis Commands
		{name: "logs command in analysis group", commandName: "logs", expectedGroup: "analysis", shouldHaveGroup: true},
		{name: "audit command in analysis group", commandName: "audit", expectedGroup: "analysis", shouldHaveGroup: true},

		// Utilities
		{name: "mcp-server command in utilities group", commandName: "mcp-server", expectedGroup: "utilities", shouldHaveGroup: true},
		{name: "pr command in utilities group", commandName: "pr", expectedGroup: "utilities", shouldHaveGroup: true},

		// Commands without groups (intentionally)
		{name: "version command without group", commandName: "version", expectedGroup: "", shouldHaveGroup: false},
		// Note: help command is special in Cobra and managed separately, so we don't test it here
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Find the command
			var foundCmd *cobra.Command
			for _, cmd := range rootCmd.Commands() {
				if cmd.Name() == tt.commandName {
					foundCmd = cmd
					break
				}
			}

			if foundCmd == nil {
				t.Fatalf("Command %q not found", tt.commandName)
			}

			// Check group assignment
			if tt.shouldHaveGroup {
				if foundCmd.GroupID == "" {
					t.Errorf("Command %q should have a group assigned but has no GroupID", tt.commandName)
				} else if foundCmd.GroupID != tt.expectedGroup {
					t.Errorf("Command %q has GroupID=%q, expected %q", tt.commandName, foundCmd.GroupID, tt.expectedGroup)
				}
			} else {
				if foundCmd.GroupID != "" {
					t.Errorf("Command %q should not have a group (GroupID=%q), but expected no group", tt.commandName, foundCmd.GroupID)
				}
			}
		})
	}
}

// TestCommandGroupsExist verifies that all expected command groups exist
func TestCommandGroupsExist(t *testing.T) {
	expectedGroups := map[string]string{
		"setup":       "Setup Commands:",
		"development": "Development Commands:",
		"execution":   "Execution Commands:",
		"analysis":    "Analysis Commands:",
		"utilities":   "Utilities:",
	}

	groups := rootCmd.Groups()
	foundGroups := make(map[string]bool)

	for _, group := range groups {
		foundGroups[group.ID] = true

		// Check if the group title matches expected
		if expectedTitle, exists := expectedGroups[group.ID]; exists {
			if group.Title != expectedTitle {
				t.Errorf("Group %q has title=%q, expected %q", group.ID, group.Title, expectedTitle)
			}
		}
	}

	// Verify all expected groups exist
	for groupID := range expectedGroups {
		if !foundGroups[groupID] {
			t.Errorf("Expected group %q not found", groupID)
		}
	}
}

// TestNoCommandsInAdditionalCommandsWithGroups verifies that commands that should have groups
// are not appearing in the "Additional Commands" section
func TestNoCommandsInAdditionalCommandsWithGroups(t *testing.T) {
	// Commands that should NOT be in Additional Commands (should have groups)
	commandsShouldHaveGroups := []string{"remove", "update", "trial", "mcp-server", "pr"}

	for _, cmdName := range commandsShouldHaveGroups {
		t.Run("command "+cmdName+" has group", func(t *testing.T) {
			var foundCmd *cobra.Command
			for _, cmd := range rootCmd.Commands() {
				if cmd.Name() == cmdName {
					foundCmd = cmd
					break
				}
			}

			if foundCmd == nil {
				t.Fatalf("Command %q not found", cmdName)
			}

			if foundCmd.GroupID == "" {
				t.Errorf("Command %q should have a group assigned to avoid appearing in 'Additional Commands'", cmdName)
			}
		})
	}
}
