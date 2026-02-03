package cli

import (
	"fmt"
	"os"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/spf13/cobra"
)

var completionLog = logger.New("cli:completion")

// NewCompletionCommand creates the completion command with install subcommand
func NewCompletionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [shell]",
		Short: "Generate shell completion scripts for gh aw commands",
		Long: `Generate shell completion scripts to enable tab completion for gh aw commands.

Tab completion provides:
- Command name completion (add, compile, run, etc.)
- Workflow name completion for commands that accept workflow arguments
- Engine name completion for --engine flag (copilot, claude, codex, custom)
- Directory path completion for --dir flag
- Helpful descriptions for workflows when available

Supported shells: bash, zsh, fish, powershell

Examples:
  # Install completions automatically (detects your shell)
  gh aw completion install

  # Generate completion script for bash
  gh aw completion bash > ~/.bash_completion.d/gh-aw
  source ~/.bash_completion.d/gh-aw

  # Generate completion script for zsh
  gh aw completion zsh > "${fpath[1]}/_gh-aw"
  compinit

  # Generate completion script for fish
  gh aw completion fish > ~/.config/fish/completions/gh-aw.fish

  # Generate completion script for PowerShell
  gh aw completion powershell | Out-String | Invoke-Expression

  # Add to PowerShell profile for persistent completions
  echo 'gh aw completion powershell | Out-String | Invoke-Expression' >> $PROFILE`,
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			shell := args[0]
			completionLog.Printf("Generating %s completion script", shell)

			switch shell {
			case "bash":
				return cmd.Root().GenBashCompletion(os.Stdout)
			case "zsh":
				return cmd.Root().GenZshCompletion(os.Stdout)
			case "fish":
				return cmd.Root().GenFishCompletion(os.Stdout, true)
			case "powershell":
				return cmd.Root().GenPowerShellCompletion(os.Stdout)
			default:
				return fmt.Errorf("unsupported shell: %s", shell)
			}
		},
	}

	// Add install subcommand
	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Install shell completion for the detected shell",
		Long: `Automatically install shell completion for your current shell.

This command detects your shell (bash, zsh, fish, or PowerShell) and installs
the completion script to the appropriate location. After installation, restart
your shell or source your shell configuration file.

Supported shells:
  - Bash:       Installs to ~/.bash_completion.d/ or /etc/bash_completion.d/
  - Zsh:        Installs to ~/.zsh/completions/
  - Fish:       Installs to ~/.config/fish/completions/
  - PowerShell: Provides instructions to add to PowerShell profile

Examples:
  gh aw completion install           # Auto-detect and install
  gh aw completion install --verbose # Show detailed installation steps`,
		RunE: func(cmd *cobra.Command, args []string) error {
			verbose, _ := cmd.Flags().GetBool("verbose")
			return InstallShellCompletion(verbose, cmd.Root())
		},
	}

	// Add uninstall subcommand
	uninstallCmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall shell completion for the detected shell",
		Long: `Automatically uninstall shell completion for your current shell.

This command detects your shell (bash, zsh, fish, or PowerShell) and removes
the completion script from the appropriate location. After uninstallation,
restart your shell for changes to take effect.

Supported shells:
  - Bash:       Removes from ~/.bash_completion.d/ or /etc/bash_completion.d/
  - Zsh:        Removes from ~/.zsh/completions/
  - Fish:       Removes from ~/.config/fish/completions/
  - PowerShell: Provides instructions to remove from PowerShell profile

Examples:
  gh aw completion uninstall           # Auto-detect and uninstall
  gh aw completion uninstall --verbose # Show detailed uninstallation steps`,
		RunE: func(cmd *cobra.Command, args []string) error {
			verbose, _ := cmd.Flags().GetBool("verbose")
			return UninstallShellCompletion(verbose)
		},
	}

	cmd.AddCommand(installCmd)
	cmd.AddCommand(uninstallCmd)

	return cmd
}
