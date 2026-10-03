package cli

import (
	"fmt"
	"os"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/spf13/cobra"
)

// NewSessionsCommand creates the experimental command for retrieving unified agent sessions.
func NewSessionsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "Experimental: download unified agent sessions from workflow runs",
	}
	cmd.SetOut(os.Stdout)
	downloadCmd := &cobra.Command{
		Use:   "download <run-id-or-url>",
		Short: "Experimental: download a unified agent session as JSONL or Markdown",
		Long: `Experimental: download the unified agent session from a workflow run.

Prefers aw_session.jsonl in the usage artifact. If it is absent, reconstructs
the unified session from the agent artifact using the same parsers as audit.
Reconstructed sessions include only evidence present in the downloaded artifacts.

Writes JSONL to standard output by default. Use --format markdown for a
privacy-preserving summary, or --output to write the text to a file.
Node.js is required for reconstruction and Markdown rendering.`,
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` sessions download 1234567890
  ` + string(constants.CLIExtensionPrefix) + ` sessions download 1234567890 --repo owner/repo --format markdown
  ` + string(constants.CLIExtensionPrefix) + ` sessions download https://github.com/owner/repo/actions/runs/1234567890 -o session.jsonl`,
		Args: cobra.ExactArgs(1),
		RunE: runSessionsDownloadCommand,
	}
	addRepoFlag(downloadCmd)
	downloadCmd.Flags().String("format", "jsonl", "Output format: jsonl or markdown")
	downloadCmd.Flags().StringP("output", "o", "", "Write session text to a file instead of standard output")
	_ = downloadCmd.RegisterFlagCompletionFunc("format", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return []string{"jsonl", "markdown"}, cobra.ShellCompDirectiveNoFileComp
	})
	cmd.AddCommand(downloadCmd)
	return cmd
}

func runSessionsDownloadCommand(cmd *cobra.Command, args []string) error {
	format, _ := cmd.Flags().GetString("format")
	if format != "jsonl" && format != "markdown" {
		return fmt.Errorf("invalid session format %q: expected jsonl or markdown", format)
	}
	components, err := parser.ParseRunURLExtended(args[0]) //nolint:uncheckedsliceindex // cobra.ExactArgs(1)
	if err != nil {
		return err
	}
	if components.Number <= 0 {
		return fmt.Errorf("invalid run ID %d: expected a positive run ID", components.Number)
	}
	repoFlag, _ := cmd.Flags().GetString("repo")
	if repoFlag != "" && components.Owner == "" {
		ownerRepo, host := repoutil.NormalizeRepoForAPI(repoFlag)
		owner, repo, err := repoutil.SplitRepoSlug(ownerRepo)
		if err != nil {
			return err
		}
		components.Owner, components.Repo, components.Host = owner, repo, host
	}
	verbose, _ := cmd.Flags().GetBool("verbose")
	content, err := downloadSession(cmd.Context(), components, format, verbose)
	if err != nil {
		return err
	}
	output, _ := cmd.Flags().GetString("output")
	if output != "" {
		if err := writeFileAtomically(output, content); err != nil {
			return fmt.Errorf("failed to write session to %s: %w", output, err)
		}
		return nil
	}
	_, err = cmd.OutOrStdout().Write(content)
	return err
}
