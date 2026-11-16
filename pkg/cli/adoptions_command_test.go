//go:build !integration

package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogsAdoptionsDoesNotShadowGlobalVerboseFlag(t *testing.T) {
	t.Parallel()
	cmd := NewLogsCommand()
	require.NotNil(t, cmd)

	assert.Nil(t, cmd.Flags().Lookup("verbose"), "logs should not define a local --verbose flag")

	root := &cobra.Command{Use: "gh aw"}
	root.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose output showing detailed information")
	root.AddCommand(cmd)

	inherited := cmd.InheritedFlags().Lookup("verbose")
	require.NotNil(t, inherited, "logs should inherit global --verbose flag")
	assert.Equal(t, "Enable verbose output showing detailed information", inherited.Usage)
}

func TestLogsAdoptionsOutputFlagShowsDefault(t *testing.T) {
	t.Parallel()
	cmd := NewLogsCommand()
	require.NotNil(t, cmd)

	outputFlag := cmd.Flags().Lookup("output")
	require.NotNil(t, outputFlag)
	assert.Equal(t, defaultLogsOutputDir, outputFlag.DefValue)
	assert.Contains(t, cmd.UsageString(), `Output directory for generated files (default ".github/aw/logs")`)
}
