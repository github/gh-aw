package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/spf13/cobra"
)

// NewParseCommand creates a local, parse-only frontmatter JSON command.
func NewParseCommand() *cobra.Command {
	var baseDir string
	cmd := &cobra.Command{
		Use:   "parse <file|->",
		Short: "Parse workflow Markdown frontmatter as JSON",
		Long: `Parse a local Markdown file, or exact Markdown content from stdin using "-".
Print the parser's frontmatter as a JSON object. When imports are present,
resolved-imports lists their transitive closure in dependency order.
Imports from stdin are resolved relative to the current directory or --base-dir.
This does not validate the workflow schema, compile, install, or execute workflows.
Missing or empty frontmatter produces {}. Malformed frontmatter is an error.`,
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` parse .github/workflows/example.md
  ` + string(constants.CLIExtensionPrefix) + ` parse - < .github/workflows/example.md
  git show <commit>:.github/workflows/example.md | ` + string(constants.CLIExtensionPrefix) + ` parse -`,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		// Parsing must not load project configuration through the root hook.
		PersistentPreRun: func(*cobra.Command, []string) {},
		RunE: func(cmd *cobra.Command, args []string) error {
			var source string
			if len(args) > 0 {
				source = args[0]
			} else {
				return errors.New("a Markdown file path or '-' for stdin is required")
			}
			var content []byte
			var err error
			if source == "-" {
				content, err = io.ReadAll(cmd.InOrStdin())
			} else {
				content, err = os.ReadFile(source)
			}
			if err != nil {
				return fmt.Errorf("failed to read Markdown from %q: %w", source, err)
			}

			result, err := parser.ExtractFrontmatterFromContent(string(content))
			if err != nil {
				return err
			}
			if err := resolveParseImports(result.Frontmatter, source, baseDir); err != nil {
				return err
			}
			if err := json.NewEncoder(cmd.OutOrStdout()).Encode(result.Frontmatter); err != nil {
				return fmt.Errorf("failed to write frontmatter JSON: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&baseDir, "base-dir", "", "Base directory for resolving imports (default: file directory or current directory for stdin)")
	cmd.SetOut(os.Stdout)
	return cmd
}

func resolveParseImports(frontmatter map[string]any, source, baseDir string) error {
	if _, exists := frontmatter["imports"]; !exists {
		return nil
	}
	if _, exists := frontmatter["resolved-imports"]; exists {
		return errors.New("frontmatter field 'resolved-imports' conflicts with parse output")
	}
	if baseDir == "" {
		baseDir = "."
		if source != "-" {
			baseDir = filepath.Dir(source)
		}
	}
	baseDir, err := filepath.Abs(baseDir)
	if err != nil {
		return fmt.Errorf("failed to resolve import base directory: %w", err)
	}
	cacheDir, err := os.MkdirTemp("", "gh-aw-parse-imports-")
	if err != nil {
		return fmt.Errorf("failed to create import cache: %w", err)
	}
	defer os.RemoveAll(cacheDir)
	cache := parser.NewImportCache(cacheDir)
	cache.SetRequireStablePaths(true)
	imports, err := parser.ProcessImportsFromFrontmatterWithSource(frontmatter, baseDir, cache, "", "")
	if err != nil {
		var cycle *parser.ImportCycleError
		if errors.As(err, &cycle) {
			return parser.FormatImportCycleError(cycle)
		}
		return err
	}
	if len(imports.RepositoryImports) > 0 {
		return fmt.Errorf("cannot resolve repository imports in parse: %v", imports.RepositoryImports)
	}
	for path, content := range imports.MetadataDocuments {
		if strings.HasPrefix(path, parser.BuiltinPathPrefix) {
			_, err = parser.ExtractFrontmatterFromBuiltinFile(path, []byte(content))
		} else {
			_, err = parser.ExtractFrontmatterFromContent(content)
		}
		if err != nil {
			return fmt.Errorf("failed to parse imported file %q: %w", path, err)
		}
	}
	resolved := imports.ImportedFiles
	if resolved == nil {
		resolved = []string{}
	}
	frontmatter["resolved-imports"] = resolved
	return nil
}
