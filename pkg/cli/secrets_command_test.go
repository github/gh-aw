//go:build !integration

package cli

import (
	"io"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSecretsCommand(t *testing.T) {
	t.Parallel()
	cmd := NewSecretsCommand()

	require.NotNil(t, cmd, "NewSecretsCommand should not return nil")
	assert.Equal(t, "secrets", cmd.Use, "Command use should be 'secrets'")
	assert.Equal(t, "Manage GitHub Actions secrets for agentic workflows", cmd.Short, "Command short description should match")
	assert.Contains(t, cmd.Long, "Manage GitHub Actions secrets", "Command long description should contain expected text")
	assert.Contains(t, cmd.Long, "for agentic workflows", "Command long description should use lowercase product phrasing")
	assert.NotContains(t, cmd.Long, "GitHub Agentic Workflows", "Command long description should avoid inconsistent capitalization")

	// Verify subcommands are added
	assert.True(t, cmd.HasSubCommands(), "Secrets command should have subcommands")
	subcommands := cmd.Commands()
	assert.GreaterOrEqual(t, len(subcommands), 2, "Should have at least 2 subcommands (set, bootstrap)")

	// Verify specific subcommands exist
	var hasSetSubcommand, hasBootstrapSubcommand bool
	for _, subcmd := range subcommands {
		if subcmd.Use == "set <secret-name>" || subcmd.Name() == "set" {
			hasSetSubcommand = true
		}
		if subcmd.Use == "bootstrap" || subcmd.Name() == "bootstrap" {
			hasBootstrapSubcommand = true
		}
	}
	assert.True(t, hasSetSubcommand, "Should have 'set' subcommand")
	assert.True(t, hasBootstrapSubcommand, "Should have 'bootstrap' subcommand")
}

func TestSecretsCommandHelp(t *testing.T) {
	t.Parallel()
	cmd := NewSecretsCommand()

	// Verify RunE returns help when command is run without subcommand
	err := cmd.RunE(cmd, []string{})
	require.NoError(t, err, "Running secrets command without subcommand should show help without error")
}

func TestSecretsCommandStructure(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		expectedUse    string
		commandCreator func() any
	}{
		{
			name:        "secrets command exists",
			expectedUse: "secrets",
			commandCreator: func() any {
				return NewSecretsCommand()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.commandCreator()
			require.NotNil(t, cmd, "Command should not be nil")
		})
	}
}

func TestSecretsBootstrapEngineFlagUsage(t *testing.T) {
	t.Parallel()
	cmd := NewSecretsCommand()

	var bootstrapCmd *cobra.Command
	for _, subcmd := range cmd.Commands() {
		if subcmd.Name() == "bootstrap" {
			bootstrapCmd = subcmd
			break
		}
	}

	require.NotNil(t, bootstrapCmd, "bootstrap subcommand should exist")

	engineFlag := bootstrapCmd.Flags().Lookup("engine")
	require.NotNil(t, engineFlag, "--engine flag should exist on bootstrap")

	// Assert the full shared engine list is present so future additions are detected.
	expectedEngines := []string{"copilot", "claude", "codex", "gemini", "pi", "agy"}
	for _, engine := range expectedEngines {
		assert.Contains(t, engineFlag.Usage, engine, "--engine help should include %s engine", engine)
	}
}

func TestSecretsCommandUnknownSubcommandReturnsError(t *testing.T) {
	t.Parallel()
	cmd := NewSecretsCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"unknown-cmd"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Equal(t, `unknown command "unknown-cmd" for "secrets"`, err.Error())
}
