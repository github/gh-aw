//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSecretsCommand(t *testing.T) {
	cmd := NewSecretsCommand()

	require.NotNil(t, cmd, "NewSecretsCommand should not return nil")
	assert.Equal(t, "secrets", cmd.Use, "Command use should be 'secrets'")
	assert.Equal(t, "Manage repository secrets and GitHub tokens", cmd.Short, "Command short description should match")
	assert.Contains(t, cmd.Long, "Manage GitHub Actions secrets", "Command long description should contain expected text")

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
	cmd := NewSecretsCommand()

	// Verify RunE returns help when command is run without subcommand
	err := cmd.RunE(cmd, []string{})
	assert.NoError(t, err, "Running secrets command without subcommand should show help without error")
}

func TestSecretsCommandStructure(t *testing.T) {
	tests := []struct {
		name           string
		expectedUse    string
		commandCreator func() interface{}
	}{
		{
			name:        "secrets command exists",
			expectedUse: "secrets",
			commandCreator: func() interface{} {
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
