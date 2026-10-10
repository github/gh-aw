package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/spf13/cobra"
)

var tokensBootstrapLog = logger.New("cli:tokens_bootstrap")

// newSecretsBootstrapSubcommand creates the `secrets bootstrap` subcommand
func newSecretsBootstrapSubcommand() *cobra.Command {
	var engineFlag string
	var nonInteractiveFlag bool

	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Analyze workflows and interactively configure required secrets",
		Long: `Analyze all workflows in the repository to determine which secrets
are required, check which ones are already configured, and interactively
prompt for any missing required secrets.

Only required secrets are prompted for. Optional secrets are not shown.`,
		Example: `  gh aw secrets bootstrap                        # Check and set up all required secrets
  gh aw secrets bootstrap --non-interactive      # Display missing secrets without prompting
  gh aw secrets bootstrap --engine copilot       # Check secrets for a specific engine`,
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, _ := cmd.Flags().GetString("repo")
			return runTokensBootstrap(engineFlag, repo, nonInteractiveFlag)
		},
	}

	cmd.Flags().BoolVar(&nonInteractiveFlag, "non-interactive", false, "Check secrets without prompting (display-only mode)")
	cmd.Flags().StringVarP(&engineFlag, "engine", "e", "", "Check tokens for specific engine ("+engineFlagHelpList+")")
	addRepoFlag(cmd)

	return cmd
}

func runTokensBootstrap(engine, repo string, nonInteractive bool) error {
	tokensBootstrapLog.Printf("Running tokens bootstrap: engine=%s, repo=%s, nonInteractive=%v", engine, repo, nonInteractive)

	// Configure the gh CLI host from the git remote before any gh calls so that
	// secret discovery and upload both target the correct host.
	configureDefaultGHHostFromOriginRemoteIfUnset()

	var repoSlug string
	var err error

	// Determine target repository
	if repo != "" {
		repoSlug = repo
	} else {
		repoSlug, err = GetCurrentRepoSlug()
		if err != nil {
			return fmt.Errorf("failed to detect current repository: %w", err)
		}
	}

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Analyzing workflows in %s...", repoSlug)))

	// Discover workflows in the repository
	requirements, err := getSecretRequirements(engine)
	if err != nil {
		return fmt.Errorf("failed to analyze workflows: %w", err)
	}

	tokensBootstrapLog.Printf("Collected %d required secrets from workflows", len(requirements))

	// Check existing secrets in repository
	existingSecrets, err := getExistingSecretsInRepo(repoSlug)
	if err != nil {
		// If we can't check existing secrets (e.g., no gh auth), continue with empty map
		tokensBootstrapLog.Printf("Could not check existing secrets: %v", err)
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Unable to check existing repository secrets. Will assume all secrets need to be configured."))
		existingSecrets = make(map[string]struct {
		})
	}

	// Filter to only required secrets that are missing
	missing := getMissingRequiredSecrets(requirements, existingSecrets)

	// Always display summary table of all required secrets with their status
	displaySecretsSummaryTable(requirements, existingSecrets)

	if len(missing) == 0 {
		tokensBootstrapLog.Print("All required secrets present")
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("All required secrets are configured."))
		return nil
	}

	tokensBootstrapLog.Printf("Found %d missing required secrets", len(missing))

	// In non-interactive mode, just display what's missing
	if nonInteractive {
		displayMissingSecrets(missing, repoSlug, existingSecrets)
		return nil
	}

	// Interactive mode: prompt for missing secrets
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %d missing required secret(s). You will be prompted to provide them.", len(missing))))
	fmt.Fprintln(os.Stderr, "")

	config := EngineSecretConfig{
		RepoSlug:             repoSlug,
		ExistingSecrets:      existingSecrets,
		IncludeSystemSecrets: true,
		IncludeOptional:      false,
	}

	// Prompt for each missing secret
	for _, req := range missing {
		if err := promptForSecret(req, config); err != nil {
			return fmt.Errorf("failed to collect secret %s: %w", req.Name, err)
		}
	}

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("All required secrets have been configured."))

	return nil
}

// getSecretRequirements discovers all workflows and collects their required secrets
func getSecretRequirements(engineFilter string) ([]SecretRequirement, error) {
	tokensBootstrapLog.Printf("Discovering workflows (engine filter: %s)", engineFilter)

	var allRequirements []SecretRequirement

	// If engine is explicitly specified, we can bootstrap without workflows
	if engineFilter != "" {
		tokensBootstrapLog.Printf("Engine explicitly specified, bootstrapping for %s regardless of workflows", engineFilter)
		// Get engine-specific secrets and system secrets (including optional)
		allRequirements = getSecretRequirementsForEngine(engineFilter, true, true)
	} else {
		// Discover workflow files
		workflowFiles, err := getMarkdownWorkflowFiles("")
		if err != nil {
			return nil, fmt.Errorf("failed to discover workflows: %w", err)
		}

		if len(workflowFiles) == 0 {
			return nil, errors.New("no workflow files found in .github/workflows/")
		}

		tokensBootstrapLog.Printf("Found %d workflow files, extracting secrets", len(workflowFiles))

		// Use getRequiredSecretsForWorkflows to collect and deduplicate secrets
		allRequirements = getSecretsRequirementsForWorkflows(workflowFiles)
	}

	tokensBootstrapLog.Printf("Returning %d deduplicated secret requirements", len(allRequirements))
	return allRequirements, nil
}
