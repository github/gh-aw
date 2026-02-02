package cli

import (
	"github.com/github/gh-aw/pkg/constants"
	"github.com/spf13/cobra"
)

// NewStatusCommand creates the status command
func NewStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status [pattern]",
		Short: "Show status of all agentic workflows in the repository",
		Long: `Show status of all agentic workflows in the repository.

Displays a table with workflow name, AI engine, compilation status, enabled/disabled state,
and time remaining until expiration (if stop-after is configured).

The optional pattern argument filters workflows by name (case-insensitive substring match).

Examples:
  ` + string(constants.CLIExtensionPrefix) + ` status                          # Show all workflow status
  ` + string(constants.CLIExtensionPrefix) + ` status ci-                       # Show workflows with 'ci-' in name
  ` + string(constants.CLIExtensionPrefix) + ` status --json                    # Output in JSON format
  ` + string(constants.CLIExtensionPrefix) + ` status --ref main                # Show latest run status for main branch
  ` + string(constants.CLIExtensionPrefix) + ` status --label automation        # Show workflows with 'automation' label
  ` + string(constants.CLIExtensionPrefix) + ` status --repo owner/other-repo   # Check status in different repository`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var pattern string
			if len(args) > 0 {
				pattern = args[0]
			}
			verbose, _ := cmd.Flags().GetBool("verbose")
			jsonFlag, _ := cmd.Flags().GetBool("json")
			ref, _ := cmd.Flags().GetString("ref")
			labelFilter, _ := cmd.Flags().GetString("label")
			repoOverride, _ := cmd.Flags().GetString("repo")
			return StatusWorkflows(pattern, verbose, jsonFlag, ref, labelFilter, repoOverride)
		},
	}

	addJSONFlag(cmd)
	cmd.Flags().StringP("repo", "r", "", "Target repository (owner/repo format). Defaults to current repository")
	cmd.Flags().String("ref", "", "Filter runs by branch or tag name (e.g., main, v1.0.0)")
	cmd.Flags().String("label", "", "Filter workflows by label")

	// Register completions for status command
	cmd.ValidArgsFunction = CompleteWorkflowNames

	return cmd
}
