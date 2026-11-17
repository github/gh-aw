package cli

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/spf13/cobra"
)

func cliCommandPath(cmd *cobra.Command) string {
	path := cmd.CommandPath()
	if rootName := cmd.Root().Name(); rootName == "gh" {
		path = strings.TrimPrefix(path, rootName)
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return string(constants.CLIExtensionPrefix)
	}
	return string(constants.CLIExtensionPrefix) + " " + path
}

// newLegacyGHGuardSubcommand rejects legacy-style nested "gh" invocations such as
// "gh aw <command> gh --help" so they do not silently render parent help.
func newLegacyGHGuardSubcommand() *cobra.Command {
	return &cobra.Command{
		Use:                "gh",
		Hidden:             true,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return fmt.Errorf("unknown command %q for %q", cmd.Name(), cliCommandPath(cmd.Parent()))
		},
	}
}
