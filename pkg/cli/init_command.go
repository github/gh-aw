package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
)

var initCommandLog = logger.New("cli:init_command")

const initCommandLong = `Initialize the repository for agentic workflows by configuring repository settings.

This command performs non-interactive repository setup and does not prompt for
engine selection or secret configuration.

This command:
- Configures .gitattributes to mark .lock.yml files as generated
- Creates the dispatcher skill at .github/skills/agentic-workflows/SKILL.md
- Removes old prompt files from .github/prompts/ if they exist
- Configures VSCode settings (.vscode/settings.json)
- Generates/updates .github/workflows/agentics-maintenance.yml if any workflows use the expires field for discussions or issues

With --engine copilot:
- Creates the custom agent at .github/agents/agentic-workflows.md

With --engine copilot (without --no-mcp):
- Creates .github/workflows/copilot-setup-steps.yml with gh-aw installation steps
- Creates .github/mcp.json with gh-aw MCP server configuration

With --no-mcp flag:
- Skips creating GitHub Copilot Agent MCP server configuration files

With --no-skill flag:
- Skips creating the dispatcher skill

With --no-agent flag:
- Skips creating the custom agent

With --codespaces flag:
- Updates existing .devcontainer/devcontainer.json if present, otherwise creates new file at default location
- Configures permissions for current repo: actions:write, contents:write, discussions:write, issues:write, pull-requests:write, workflows:write
- Configures permissions for additional repos (in same org): actions:read, contents:read, discussions:read, issues:read, pull-requests:read, workflows:read
- Adds GitHub Copilot extensions and gh aw CLI installation
- Use without a value (--codespaces) for current repo only, or with comma-separated repos (--codespaces=repo1,repo2)

With --completions flag:
- Automatically detects your shell (bash, zsh, fish, or PowerShell)
- Installs shell completion configuration for the CLI
- Provides instructions for enabling completions in your shell

After running this command, you can:
- Add workflows from the catalog with: ` + string(constants.CLIExtensionPrefix) + ` add <workflow-name>
- Create new workflows from scratch with: ` + string(constants.CLIExtensionPrefix) + ` new <workflow-name>`

const initCommandExample = `  ` + string(constants.CLIExtensionPrefix) + ` init                                # Initialize repository with defaults
  ` + string(constants.CLIExtensionPrefix) + ` init -v                             # Initialize with verbose output
  ` + string(constants.CLIExtensionPrefix) + ` init --engine copilot               # Add Copilot MCP and agent files
  ` + string(constants.CLIExtensionPrefix) + ` init --engine claude                # Initialize for the Claude engine
  ` + string(constants.CLIExtensionPrefix) + ` init --no-mcp                       # Skip MCP configuration
  ` + string(constants.CLIExtensionPrefix) + ` init --no-skill                     # Skip dispatcher skill creation
  ` + string(constants.CLIExtensionPrefix) + ` init --no-agent                     # Skip custom agent creation
  ` + string(constants.CLIExtensionPrefix) + ` init --codespaces                   # Configure Codespaces for current repo only
  ` + string(constants.CLIExtensionPrefix) + ` init --codespaces=repo1,repo2       # Codespaces with additional repos
  ` + string(constants.CLIExtensionPrefix) + ` init --completions                  # Install shell completions
  ` + string(constants.CLIExtensionPrefix) + ` init --create-pull-request          # Initialize and create a pull request
  ` + string(constants.CLIExtensionPrefix) + ` init --pr --auto-merge              # Initialize and enable pull request auto-merge`

func runInitCommand(cmd *cobra.Command, args []string) error {
	verbose, _ := cmd.Flags().GetBool("verbose")
	mcpFlag, _ := cmd.Flags().GetBool("mcp")
	noMcp, _ := cmd.Flags().GetBool("no-mcp")
	noSkill, _ := cmd.Flags().GetBool("no-skill")
	noAgent, _ := cmd.Flags().GetBool("no-agent")
	engineOverride, _ := cmd.Flags().GetString("engine")
	codespaceReposStr, _ := cmd.Flags().GetString("codespaces")
	codespaceEnabled := cmd.Flags().Changed("codespaces")
	completions, _ := cmd.Flags().GetBool("completions")
	createPRFlag, _ := cmd.Flags().GetBool("create-pull-request")
	prFlagAlias, _ := cmd.Flags().GetBool("pr")
	createPR := createPRFlag || prFlagAlias // Support both --create-pull-request and --pr
	autoMerge, _ := cmd.Flags().GetBool("auto-merge")
	if err := requireAutoMergePR(autoMerge, createPR); err != nil {
		return err
	}

	if err := validateInitEngine(engineOverride); err != nil {
		return err
	}

	// Determine MCP state: default true, unless --no-mcp is specified
	// --mcp flag is kept for backward compatibility (hidden from help)
	mcp := !noMcp
	if cmd.Flags().Changed("mcp") {
		// If --mcp is explicitly set, use it (backward compatibility)
		mcp = mcpFlag
	}

	// Trim the codespace repos string after parsing. With NoOptDefVal set, bare
	// --codespaces becomes the current-repo sentinel while explicit values use
	// --codespaces=<repo1,repo2>.
	codespaceRepos := parseInitCodespaceRepos(codespaceReposStr)

	initCommandLog.Printf("Executing init command: verbose=%v, skill=%v, agent=%v, mcp=%v, codespaces=%v, codespaceEnabled=%v, completions=%v, createPR=%v", verbose, !noSkill, !noAgent, mcp, codespaceRepos, codespaceEnabled, completions, createPR)
	opts := InitOptions{
		Ctx:              cmd.Context(),
		Verbose:          verbose,
		Engine:           engineOverride,
		Skill:            !noSkill,
		Agent:            !noAgent,
		MCP:              mcp,
		CodespaceRepos:   codespaceRepos,
		CodespaceEnabled: codespaceEnabled,
		Completions:      completions,
		CreatePR:         createPR,
		AutoMerge:        autoMerge,
		PRMessage:        cmd.Flag("pr-message").Value.String(),
		RootCmd:          cmd.Root(),
	}
	if err := InitRepository(opts); err != nil {
		initCommandLog.Printf("Init command failed: %v", err)
		return err
	}
	initCommandLog.Print("Init command completed successfully")
	return nil
}

func validateInitEngine(engineOverride string) error {
	if engineOverride != "" {
		registry := workflow.GetGlobalEngineRegistry()
		if !registry.IsValidEngine(engineOverride) {
			supportedEngines := registry.GetSupportedEngines()
			sort.Strings(supportedEngines)
			return fmt.Errorf("invalid engine value '%s'. Must be one of: %s", engineOverride, strings.Join(supportedEngines, ", "))
		}
	}
	return nil
}

func parseInitCodespaceRepos(value string) []string {
	value = strings.TrimSpace(value)
	var repos []string
	if value != "" {
		repos = strings.Split(value, ",")
		for i, repo := range repos {
			repos[i] = strings.TrimSpace(repo)
		}
	}
	return repos
}

// NewInitCommand creates the init command
func NewInitCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "init",
		Short:   "Initialize the repository for agentic workflows",
		Long:    initCommandLong,
		Example: initCommandExample,
		RunE:    runInitCommand,
	}

	addEngineFlag(cmd)
	cmd.Flags().Bool("no-mcp", false, "Skip configuring gh-aw MCP server integration for GitHub Copilot Agent")
	cmd.Flags().Bool("no-skill", false, "Skip creating the agentic-workflows dispatcher skill")
	cmd.Flags().Bool("no-agent", false, "Skip creating the agentic-workflows custom agent")
	cmd.Flags().String("codespaces", "", "Create devcontainer.json for GitHub Codespaces with agentic workflow support. Specify comma-separated repository names in the same organization (e.g., repo1,repo2), or use without a value for the current repo only")
	// Allow --codespaces without a value (e.g. "gh aw init --codespaces").
	// pflag treats NoOptDefVal="" as "flag requires a value", so we use a single space as
	// the no-value sentinel. The existing strings.TrimSpace call in RunE normalises it to ""
	// (current repo only), while still distinguishing flag-present from flag-absent via Changed.
	cmd.Flags().Lookup("codespaces").NoOptDefVal = " "
	cmd.Flags().Bool("completions", false, "Install shell completion for the detected shell (bash, zsh, fish, or PowerShell)")
	cmd.Flags().Bool("create-pull-request", false, "Create a pull request with the initialization changes")
	addPRMessageFlag(cmd)
	cmd.Flags().Bool("pr", false, "Alias for --create-pull-request")
	cmd.Flags().Bool("auto-merge", false, "Enable auto-merge on the created pull request (requires --pr or --create-pull-request)")
	_ = cmd.Flags().MarkHidden("pr") // Hide the short alias from help output
	cmd.Flags().Bool("mcp", false, "Configure GitHub Copilot Agent MCP server integration (deprecated; use --engine copilot)")
	// Hide the deprecated --mcp flag from help (kept for backward compatibility)
	_ = cmd.Flags().MarkHidden("mcp")

	return cmd
}
