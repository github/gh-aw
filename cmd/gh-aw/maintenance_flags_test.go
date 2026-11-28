package main

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMaintenanceToggleRepositoryFlags(t *testing.T) {
	for _, cmd := range []*cobra.Command{enableCmd, disableCmd} {
		t.Run(cmd.Name(), func(t *testing.T) {
			flag := cmd.Flags().Lookup("repo")
			require.NotNil(t, flag)
			assert.Equal(t, "r", flag.Shorthand)
			assert.Equal(t, "", flag.DefValue)
			require.NotNil(t, cmd.RunE, "errors must be returned to the CLI entry point")
			assert.Contains(t, cmd.Long, "no local checkout is required")
			assert.Contains(t, cmd.Long, "exact")
		})
	}
}
