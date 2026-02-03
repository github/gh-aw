package cli

import (
	"github.com/github/gh-aw/pkg/logger"
	"github.com/spf13/cobra"
)

var secretsCommandLog = logger.New("cli:secrets_command")

// NewSecretsCommand creates the main secrets command with subcommands
func NewSecretsCommand() *cobra.Command {
	secretsCommandLog.Print("Creating secrets command with subcommands")
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage repository secrets and GitHub tokens",
		Long: `Manage GitHub Actions secrets and tokens for GitHub Agentic Workflows.

This command provides tools for managing secrets required by agentic workflows, including
AI API keys (Anthropic, OpenAI, GitHub Copilot) and GitHub tokens for workflow execution.

Available subcommands:
  • set       - Create or update individual secrets
  • bootstrap - Validate and configure all required secrets for workflows

Use 'gh aw init --tokens' to check which secrets are configured for your repository.

Examples:
  gh aw secrets set MY_SECRET --value "secret123"    # Set a secret directly
  gh aw secrets bootstrap                             # Check all required secrets
  gh aw init --tokens --engine copilot                # Validate Copilot tokens`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	// Add subcommands
	cmd.AddCommand(newSecretsSetSubcommand())
	cmd.AddCommand(newSecretsBootstrapSubcommand())

	return cmd
}
