package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/spf13/cobra"
)

// NewParseCommand creates a local, parse-only frontmatter JSON command.
func NewParseCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "parse <file|->",
		Short: "Parse workflow Markdown frontmatter as JSON",
		Long: `Parse a local Markdown file, or exact Markdown content from stdin using "-".
Print the parser's frontmatter as a JSON object without compiling, validating the
workflow schema, resolving imports, installing, or executing the workflow.
Missing or empty frontmatter produces {}. Malformed frontmatter is an error.`,
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` parse .github/workflows/example.md
  ` + string(constants.CLIExtensionPrefix) + ` parse - < .github/workflows/example.md
  git show <commit>:.github/workflows/example.md | ` + string(constants.CLIExtensionPrefix) + ` parse -`,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		// Parsing must not load project configuration through the root hook.
		PersistentPreRun: func(*cobra.Command, []string) {},
		RunE: func(cmd *cobra.Command, args []string) error {
			var content []byte
			var err error
			if args[0] == "-" {
				content, err = io.ReadAll(cmd.InOrStdin())
			} else {
				content, err = os.ReadFile(args[0])
			}
			if err != nil {
				return fmt.Errorf("failed to read Markdown from %q: %w", args[0], err)
			}

			result, err := parser.ExtractFrontmatterFromContent(string(content))
			if err != nil {
				return err
			}
			if err := json.NewEncoder(cmd.OutOrStdout()).Encode(result.Frontmatter); err != nil {
				return fmt.Errorf("failed to write frontmatter JSON: %w", err)
			}
			return nil
		},
	}
	cmd.SetOut(os.Stdout)
	return cmd
}
