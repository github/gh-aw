//go:build !integration

package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestHelpFlagConsistency verifies that all commands have consistent --help flag
// descriptions starting with "Show help for gh aw" (matching the root command).
func TestHelpFlagConsistency(t *testing.T) {
	var checkCmd func(cmd *cobra.Command)
	checkCmd = func(cmd *cobra.Command) {
		t.Run("command "+cmd.CommandPath()+" has consistent help flag", func(t *testing.T) {
			cmd.InitDefaultHelpFlag()
			f := cmd.Flags().Lookup("help")
			if f == nil {
				t.Skip("Command has no help flag")
			}
			want := "Show help for gh aw"
			if !strings.HasPrefix(f.Usage, want) {
				t.Errorf("Command %q help flag Usage = %q, want prefix %q", cmd.CommandPath(), f.Usage, want)
			}
		})
		for _, sub := range cmd.Commands() {
			checkCmd(sub)
		}
	}
	// Ensure the custom help command (registered via SetHelpCommand) is part of
	// the command tree so it is covered by this check.
	rootCmd.InitDefaultHelpCmd()
	checkCmd(rootCmd)
}
