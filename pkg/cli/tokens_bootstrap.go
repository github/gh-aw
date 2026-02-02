package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
)

var tokensBootstrapLog = logger.New("cli:tokens_bootstrap")

// tokenSpec describes a recommended token secret for gh-aw
type tokenSpec struct {
	Name        string
	When        string
	Description string
	Optional    bool
}

// getRecommendedTokensForEngine returns token specs based on the workflow engine
func getRecommendedTokensForEngine(engine string) []tokenSpec {
	tokensBootstrapLog.Printf("Getting recommended tokens for engine: %s", engine)
	// Base tokens needed for most workflows
	tokens := []tokenSpec{
		{
			Name:        "GH_AW_GITHUB_TOKEN",
			When:        "Cross-repo Project Ops / remote GitHub tools",
			Description: "Fine-grained or classic PAT with contents/issues/pull-requests read+write on the repos gh-aw will touch.",
			Optional:    false,
		},
	}

	// Engine-specific tokens
	switch engine {
	case "copilot":
		tokens = append(tokens, tokenSpec{
			Name:        "COPILOT_GITHUB_TOKEN",
			When:        "Copilot workflows (CLI, engine, agent tasks, etc.)",
			Description: "PAT with Copilot Requests permission and repo access where Copilot workflows run.",
			Optional:    false,
		})
	case "claude":
		tokens = append(tokens, tokenSpec{
			Name:        "ANTHROPIC_API_KEY",
			When:        "Claude engine workflows",
			Description: "API key from Anthropic Console for Claude API access.",
			Optional:    false,
		})
	case "codex":
		tokens = append(tokens, tokenSpec{
			Name:        "OPENAI_API_KEY",
			When:        "Codex/OpenAI engine workflows",
			Description: "API key from OpenAI for Codex/GPT API access.",
			Optional:    false,
		})
	}

	tokensBootstrapLog.Printf("Collected engine-specific tokens: engine=%s, count=%d", engine, len(tokens))

	// Optional tokens for advanced use cases
	tokens = append(tokens,
		tokenSpec{
			Name:        "GH_AW_AGENT_TOKEN",
			When:        "Assigning agents/bots to issues or pull requests",
			Description: "PAT for agent assignment with issues and pull-requests write on the repos where agents act.",
			Optional:    true,
		},
		tokenSpec{
			Name:        "GH_AW_GITHUB_MCP_SERVER_TOKEN",
			When:        "Isolating MCP server permissions (advanced, optional)",
			Description: "Optional read-mostly token for the GitHub MCP server when you want different scopes than GH_AW_GITHUB_TOKEN.",
			Optional:    true,
		},
	)

	tokensBootstrapLog.Printf("Returning %d total token specs for engine %s", len(tokens), engine)
	return tokens
}

// recommendedTokenSpecs defines the core tokens we surface in tokens.md
// This is kept for backward compatibility and default listing
var recommendedTokenSpecs = []tokenSpec{
	{
		Name:        "GH_AW_GITHUB_TOKEN",
		When:        "Cross-repo Project Ops / remote GitHub tools",
		Description: "Fine-grained or classic PAT with contents/issues/pull-requests read+write on the repos gh-aw will touch.",
		Optional:    false,
	},
	{
		Name:        "COPILOT_GITHUB_TOKEN",
		When:        "Copilot workflows (CLI, engine, agent tasks, etc.)",
		Description: "PAT with Copilot Requests permission and repo access where Copilot workflows run.",
		Optional:    true,
	},
	{
		Name:        "ANTHROPIC_API_KEY",
		When:        "Claude engine workflows",
		Description: "API key from Anthropic Console for Claude API access.",
		Optional:    true,
	},
	{
		Name:        "OPENAI_API_KEY",
		When:        "Codex/OpenAI engine workflows",
		Description: "API key from OpenAI for Codex/GPT API access.",
		Optional:    true,
	},
	{
		Name:        "GH_AW_AGENT_TOKEN",
		When:        "Assigning agents/bots to issues or pull requests",
		Description: "PAT for agent assignment with issues and pull-requests write on the repos where agents act.",
		Optional:    true,
	},
	{
		Name:        "GH_AW_GITHUB_MCP_SERVER_TOKEN",
		When:        "Isolating MCP server permissions (advanced, optional)",
		Description: "Optional read-mostly token for the GitHub MCP server when you want different scopes than GH_AW_GITHUB_TOKEN.",
		Optional:    true,
	},
}

// newSecretsBootstrapSubcommand creates the `secrets bootstrap` subcommand
func newSecretsBootstrapSubcommand() *cobra.Command {
	var engineFlag string
	var ownerFlag string
	var repoFlag string

	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Check and suggest setup for gh aw GitHub token secrets",
		Long: `Check which recommended GitHub token secrets (like GH_AW_GITHUB_TOKEN)
are configured for the current repository, and print least-privilege setup
instructions for any that are missing.

This command is read-only: it does not create tokens or secrets for you.
Instead, it inspects repository secrets (using the GitHub CLI where
available) and prints the exact secrets to add and suggested scopes.

For full details, including precedence rules, see the GitHub Tokens
reference in the documentation.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTokensBootstrap(engineFlag, ownerFlag, repoFlag)
		},
	}

	cmd.Flags().StringVarP(&engineFlag, "engine", "e", "", "Check tokens for specific engine (copilot, claude, codex)")
	cmd.Flags().StringVar(&ownerFlag, "owner", "", "Repository owner (defaults to current repository)")
	cmd.Flags().StringVar(&repoFlag, "repo", "", "Repository name (defaults to current repository)")

	return cmd
}

func runTokensBootstrap(engine, owner, repo string) error {
	secretsLog.Printf("Running tokens bootstrap: engine=%s, owner=%s, repo=%s", engine, owner, repo)
	var repoSlug string
	var err error

	// Determine target repository
	if owner != "" && repo != "" {
		repoSlug = fmt.Sprintf("%s/%s", owner, repo)
	} else if owner != "" || repo != "" {
		return fmt.Errorf("both --owner and --repo must be specified together")
	} else {
		repoSlug, err = GetCurrentRepoSlug()
		if err != nil {
			return fmt.Errorf("failed to detect current repository: %w", err)
		}
	}

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Checking recommended gh-aw token secrets in %s...", repoSlug)))

	// Get tokens based on engine or use all recommended tokens
	var tokensToCheck []tokenSpec
	if engine != "" {
		tokensToCheck = getRecommendedTokensForEngine(engine)
		secretsLog.Printf("Checking tokens for specific engine: %s (%d tokens)", engine, len(tokensToCheck))
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Checking tokens for engine: %s", engine)))
	} else {
		tokensToCheck = recommendedTokenSpecs
		secretsLog.Printf("Checking all recommended tokens: count=%d", len(tokensToCheck))
	}

	missing := make([]tokenSpec, 0, len(tokensToCheck))

	for _, spec := range tokensToCheck {
		exists, err := checkSecretExistsInRepo(spec.Name, repoSlug)
		if err != nil {
			// If we hit a 403 or other error, surface a friendly message and abort
			return fmt.Errorf("unable to inspect repository secrets (gh secret list failed for %s): %w", spec.Name, err)
		}
		if !exists {
			missing = append(missing, spec)
		}
	}

	if len(missing) == 0 {
		secretsLog.Print("All required tokens present")
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("All recommended gh-aw token secrets are present in this repository."))
		return nil
	}

	secretsLog.Printf("Found missing tokens: count=%d", len(missing))
	// Separate required and optional missing secrets
	var requiredMissing, optionalMissing []tokenSpec
	for _, spec := range missing {
		if spec.Optional {
			optionalMissing = append(optionalMissing, spec)
		} else {
			requiredMissing = append(requiredMissing, spec)
		}
	}

	// Extract owner and repo from slug for command examples
	parts := splitRepoSlug(repoSlug)
	cmdOwner := parts[0]
	cmdRepo := parts[1]

	if len(requiredMissing) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage("Required gh-aw token secrets are missing:"))
		for _, spec := range requiredMissing {
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Secret: %s", spec.Name)))
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("When needed: %s", spec.When)))
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Recommended scopes: %s", spec.Description)))
			fmt.Fprintln(os.Stderr, console.FormatCommandMessage(fmt.Sprintf("gh aw secrets set %s --owner %s --repo %s", spec.Name, cmdOwner, cmdRepo)))
		}
	}

	if len(optionalMissing) > 0 {
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Optional gh-aw token secrets are missing:"))
		for _, spec := range optionalMissing {
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Secret: %s (optional)", spec.Name)))
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("When needed: %s", spec.When)))
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Recommended scopes: %s", spec.Description)))
			fmt.Fprintln(os.Stderr, console.FormatCommandMessage(fmt.Sprintf("gh aw secrets set %s --owner %s --repo %s", spec.Name, cmdOwner, cmdRepo)))
		}
	}

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("For detailed token behavior and precedence, see the GitHub Tokens reference in the documentation."))

	return nil
}

// checkSecretExistsInRepo checks if a secret exists in a specific repository
func checkSecretExistsInRepo(secretName, repoSlug string) (bool, error) {
	secretsLog.Printf("Checking if secret exists in %s: %s", repoSlug, secretName)

	// Use gh CLI to list repository secrets
	output, err := workflow.RunGH("Listing secrets...", "secret", "list", "--repo", repoSlug, "--json", "name")
	if err != nil {
		// Check if it's a 403 error by examining the error
		if exitError, ok := err.(*exec.ExitError); ok {
			if strings.Contains(string(exitError.Stderr), "403") {
				return false, fmt.Errorf("403 access denied")
			}
		}
		return false, fmt.Errorf("failed to list secrets: %w", err)
	}

	// Parse the JSON output
	var secrets []struct {
		Name string `json:"name"`
	}

	if err := json.Unmarshal(output, &secrets); err != nil {
		return false, fmt.Errorf("failed to parse secrets list: %w", err)
	}

	// Check if our secret exists
	for _, secret := range secrets {
		if secret.Name == secretName {
			return true, nil
		}
	}

	return false, nil
}

// splitRepoSlug splits "owner/repo" into [owner, repo]
// Uses repoutil.SplitRepoSlug internally but provides backward-compatible array return
func splitRepoSlug(slug string) [2]string {
	owner, repo, err := repoutil.SplitRepoSlug(slug)
	if err != nil {
		// Fallback behavior for invalid format
		return [2]string{slug, ""}
	}
	return [2]string{owner, repo}
}
