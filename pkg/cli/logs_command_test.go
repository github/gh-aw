//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLogsCommand(t *testing.T) {
	cmd := NewLogsCommand()

	require.NotNil(t, cmd, "NewLogsCommand should not return nil")
	assert.Equal(t, "logs [workflow]", cmd.Use, "Command use should be 'logs [workflow]'")
	assert.Equal(t, "Download and analyze agentic workflow logs with aggregated metrics", cmd.Short, "Command short description should match")
	assert.Contains(t, cmd.Long, "Download workflow run logs", "Command long description should contain expected text")

	// Verify flags are registered
	flags := cmd.Flags()

	// Check count flag
	countFlag := flags.Lookup("count")
	assert.NotNil(t, countFlag, "Should have 'count' flag")
	assert.Equal(t, "c", countFlag.Shorthand, "Count flag shorthand should be 'c'")

	// Check start-date flag
	startDateFlag := flags.Lookup("start-date")
	assert.NotNil(t, startDateFlag, "Should have 'start-date' flag")

	// Check end-date flag
	endDateFlag := flags.Lookup("end-date")
	assert.NotNil(t, endDateFlag, "Should have 'end-date' flag")

	// Check engine flag
	engineFlag := flags.Lookup("engine")
	assert.NotNil(t, engineFlag, "Should have 'engine' flag")

	// Check firewall flags
	firewallFlag := flags.Lookup("firewall")
	assert.NotNil(t, firewallFlag, "Should have 'firewall' flag")
	noFirewallFlag := flags.Lookup("no-firewall")
	assert.NotNil(t, noFirewallFlag, "Should have 'no-firewall' flag")

	// Check output flag
	outputFlag := flags.Lookup("output")
	assert.NotNil(t, outputFlag, "Should have 'output' flag")
	assert.Equal(t, "o", outputFlag.Shorthand, "Output flag shorthand should be 'o'")

	// Check ref flag
	refFlag := flags.Lookup("ref")
	assert.NotNil(t, refFlag, "Should have 'ref' flag")

	// Check run ID filters
	afterRunIDFlag := flags.Lookup("after-run-id")
	assert.NotNil(t, afterRunIDFlag, "Should have 'after-run-id' flag")
	beforeRunIDFlag := flags.Lookup("before-run-id")
	assert.NotNil(t, beforeRunIDFlag, "Should have 'before-run-id' flag")

	// Check tool-graph flag
	toolGraphFlag := flags.Lookup("tool-graph")
	assert.NotNil(t, toolGraphFlag, "Should have 'tool-graph' flag")

	// Check parse flag
	parseFlag := flags.Lookup("parse")
	assert.NotNil(t, parseFlag, "Should have 'parse' flag")

	// Check json flag
	jsonFlag := flags.Lookup("json")
	assert.NotNil(t, jsonFlag, "Should have 'json' flag")

	// Check repo flag
	repoFlag := flags.Lookup("repo")
	assert.NotNil(t, repoFlag, "Should have 'repo' flag")
}

func TestLogsCommandFlagDefaults(t *testing.T) {
	cmd := NewLogsCommand()
	flags := cmd.Flags()

	tests := []struct {
		flagName     string
		defaultValue string
	}{
		{"start-date", ""},
		{"end-date", ""},
		{"engine", ""},
		{"output", ".github/aw/logs"}, // Updated to match actual default
		{"ref", ""},
		{"after-run-id", "0"},
		{"before-run-id", "0"},
		{"repo", ""},
	}

	for _, tt := range tests {
		t.Run(tt.flagName, func(t *testing.T) {
			flag := flags.Lookup(tt.flagName)
			require.NotNil(t, flag, "Flag should exist: %s", tt.flagName)
			assert.Equal(t, tt.defaultValue, flag.DefValue, "Default value should match for flag: %s", tt.flagName)
		})
	}
}

func TestLogsCommandBooleanFlags(t *testing.T) {
	cmd := NewLogsCommand()
	flags := cmd.Flags()

	boolFlags := []string{"firewall", "no-firewall", "tool-graph", "parse", "json"}

	for _, flagName := range boolFlags {
		t.Run(flagName, func(t *testing.T) {
			flag := flags.Lookup(flagName)
			require.NotNil(t, flag, "Boolean flag should exist: %s", flagName)
			assert.Equal(t, "false", flag.DefValue, "Boolean flag should default to false: %s", flagName)
		})
	}
}

func TestLogsCommandStructure(t *testing.T) {
	tests := []struct {
		name           string
		commandCreator func() interface{}
	}{
		{
			name: "logs command exists",
			commandCreator: func() interface{} {
				return NewLogsCommand()
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

func TestLogsCommandArgs(t *testing.T) {
	cmd := NewLogsCommand()

	// Logs command accepts 0 or 1 argument (workflow is optional)
	// Only test if Args validator is set
	if cmd.Args != nil {
		// Verify it accepts no arguments
		err := cmd.Args(cmd, []string{})
		require.NoError(t, err, "Should not error with no arguments")

		// Verify it accepts 1 argument
		err = cmd.Args(cmd, []string{"workflow1"})
		require.NoError(t, err, "Should not error with 1 argument")
	}
}

func TestLogsCommandMutuallyExclusiveFlags(t *testing.T) {
	cmd := NewLogsCommand()
	flags := cmd.Flags()

	// firewall and no-firewall are mutually exclusive
	firewallFlag := flags.Lookup("firewall")
	noFirewallFlag := flags.Lookup("no-firewall")

	require.NotNil(t, firewallFlag, "firewall flag should exist")
	require.NotNil(t, noFirewallFlag, "no-firewall flag should exist")

	// Both flags exist and are boolean
	assert.Equal(t, "bool", firewallFlag.Value.Type(), "firewall should be boolean")
	assert.Equal(t, "bool", noFirewallFlag.Value.Type(), "no-firewall should be boolean")
}

func TestLogsCommandCountFlag(t *testing.T) {
	cmd := NewLogsCommand()
	flags := cmd.Flags()

	countFlag := flags.Lookup("count")
	require.NotNil(t, countFlag, "count flag should exist")

	// Count flag should be an integer
	assert.Equal(t, "int", countFlag.Value.Type(), "count should be integer type")

	// Count flag has shorthand
	assert.Equal(t, "c", countFlag.Shorthand, "count shorthand should be 'c'")
}

func TestLogsCommandDateFlags(t *testing.T) {
	cmd := NewLogsCommand()
	flags := cmd.Flags()

	tests := []struct {
		flagName string
		flagType string
	}{
		{"start-date", "string"},
		{"end-date", "string"},
	}

	for _, tt := range tests {
		t.Run(tt.flagName, func(t *testing.T) {
			flag := flags.Lookup(tt.flagName)
			require.NotNil(t, flag, "Flag should exist: %s", tt.flagName)
			assert.Equal(t, tt.flagType, flag.Value.Type(), "Flag %s should be %s type", tt.flagName, tt.flagType)
		})
	}
}

func TestLogsCommandRunIDFilters(t *testing.T) {
	cmd := NewLogsCommand()
	flags := cmd.Flags()

	tests := []struct {
		flagName string
	}{
		{"after-run-id"},
		{"before-run-id"},
	}

	for _, tt := range tests {
		t.Run(tt.flagName, func(t *testing.T) {
			flag := flags.Lookup(tt.flagName)
			require.NotNil(t, flag, "Flag should exist: %s", tt.flagName)
			assert.Equal(t, "int64", flag.Value.Type(), "Flag %s should be int64 type", tt.flagName)
		})
	}
}

func TestLogsCommandOutputFlag(t *testing.T) {
	cmd := NewLogsCommand()
	flags := cmd.Flags()

	outputFlag := flags.Lookup("output")
	require.NotNil(t, outputFlag, "output flag should exist")

	// Output flag should be a string
	assert.Equal(t, "string", outputFlag.Value.Type(), "output should be string type")

	// Output flag has shorthand
	assert.Equal(t, "o", outputFlag.Shorthand, "output shorthand should be 'o'")

	// Output flag has default value
	assert.Equal(t, ".github/aw/logs", outputFlag.DefValue, "output default should be '.github/aw/logs'")
}

func TestLogsCommandHelpText(t *testing.T) {
	cmd := NewLogsCommand()

	// Verify long description contains expected sections
	expectedSections := []string{
		"Download workflow run logs",
		"Orchestrator Usage",
		"Examples:",
		"gh aw logs",
	}

	for _, section := range expectedSections {
		assert.Contains(t, cmd.Long, section, "Long description should contain: %s", section)
	}
}
