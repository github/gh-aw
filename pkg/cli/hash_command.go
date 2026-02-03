package cli

import (
	"fmt"
	"os"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/spf13/cobra"
)

var hashLog = logger.New("cli:hash")

// NewHashCommand creates the hash-frontmatter command
func NewHashCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hash-frontmatter <workflow>",
		Short: "Compute frontmatter hash for a workflow",
		Long: `Compute a deterministic SHA-256 hash of workflow frontmatter.

The hash includes:
- All frontmatter fields from the main workflow
- Frontmatter from all imported workflows (BFS traversal)
- Template expressions containing env. or vars. from the markdown body
- Version information (gh-aw, awf, agents)

The hash can be used to detect configuration changes between compilation and execution.

Examples:
  gh aw hash-frontmatter my-workflow.md
  gh aw hash-frontmatter .github/workflows/audit-workflows.md`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			workflowPath := args[0]
			return RunHashFrontmatter(workflowPath)
		},
	}

	return cmd
}

// RunHashFrontmatter computes and prints the frontmatter hash for a workflow
func RunHashFrontmatter(workflowPath string) error {
	hashLog.Printf("Computing frontmatter hash for: %s", workflowPath)

	// Check if file exists
	if _, err := os.Stat(workflowPath); os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(fmt.Sprintf("workflow file not found: %s", workflowPath)))
		return fmt.Errorf("workflow file not found: %s", workflowPath)
	}

	// Compute hash
	cache := parser.NewImportCache("")
	hash, err := parser.ComputeFrontmatterHashFromFile(workflowPath, cache)
	if err != nil {
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(err.Error()))
		return err
	}

	// Print hash to stdout (for easy parsing by scripts)
	fmt.Println(hash)

	return nil
}
