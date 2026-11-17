package cli

import (
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/spf13/cobra"
)

var secretsCommandLog = logger.New("cli:secrets_command")

// NewSecretsCommand creates the main secrets command with subcommands
func NewSecretsCommand() *cobra.Command {
	secretsCommandLog.Print("Creating secrets command with subcommands")
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage GitHub Actions secrets for agentic workflows",
		Long: `Manage GitHub Actions secrets for agentic workflows.

This command provides tools for managing secrets required by agentic workflows, including
AI API keys (Anthropic, OpenAI, GitHub Copilot) and GitHub tokens for workflow execution.

Available subcommands:
  - set       - Create or update a repository secret
  - bootstrap - Analyze workflows and interactively configure required secrets`,
		Example: `  gh aw secrets set MY_SECRET --value "secret123"    # Set a secret directly
  gh aw secrets bootstrap                             # Check all required secrets`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("unknown command %q for %q", args[0], cliCommandPath(cmd))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	// Add subcommands
	cmd.AddCommand(newLegacyGHGuardSubcommand())
	cmd.AddCommand(newSecretsSetSubcommand())
	cmd.AddCommand(newSecretsBootstrapSubcommand())

	return cmd
}
