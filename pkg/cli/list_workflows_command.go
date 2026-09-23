package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/spf13/cobra"
)

var listWorkflowsLog = logger.New("cli:list_workflows")

// WorkflowListItem represents a single workflow for list output
type WorkflowListItem struct {
	Workflow string   `json:"workflow" console:"header:workflow"`
	EngineID string   `json:"engine_id" console:"header:engine"`
	Compiled string   `json:"compiled" console:"header:compiled"`
	Labels   []string `json:"labels,omitempty" console:"-"`
	On       any      `json:"on,omitempty" console:"-"`
}

// NewListCommand creates the list command
func NewListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list [pattern]",
		Short: "List agentic workflows in the repository",
		Long: `List all agentic workflows in a repository without checking their status.

Displays a simplified table with workflow name, AI engine, and compilation status.
Unlike 'status', this command does not check GitHub workflow state or time remaining.

The optional pattern argument filters workflows by name (case-insensitive substring match).
It accepts workflow IDs (basename without .md) or full filenames.`,
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` list                              # List all workflows in current repo
  ` + string(constants.CLIExtensionPrefix) + ` list --repo github/gh-aw          # List workflows from github/gh-aw repo
  ` + string(constants.CLIExtensionPrefix) + ` list --repo org/repo --path workflows  # List from custom path
  ` + string(constants.CLIExtensionPrefix) + ` list --dir custom/workflows        # List from custom local directory
  ` + string(constants.CLIExtensionPrefix) + ` list ci-                           # List workflows with 'ci-' in name
  ` + string(constants.CLIExtensionPrefix) + ` list --repo github/gh-aw ci-      # List workflows from github/gh-aw with 'ci-' in name
  ` + string(constants.CLIExtensionPrefix) + ` list --json                        # Output in JSON format
  ` + string(constants.CLIExtensionPrefix) + ` list --stale                       # List workflows with stale or missing lock files
  ` + string(constants.CLIExtensionPrefix) + ` list --label automation            # List workflows with 'automation' label`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var pattern string
			if len(args) > 0 {
				pattern = args[0]
			}

			repo, _ := cmd.Flags().GetString("repo")
			path, _ := cmd.Flags().GetString("path")
			dir, _ := cmd.Flags().GetString("dir")
			verbose, _ := cmd.Flags().GetBool("verbose")
			jsonFlag, _ := cmd.Flags().GetBool("json")
			labelFilter, _ := cmd.Flags().GetString("label")
			staleOnly, _ := cmd.Flags().GetBool("stale")

			// --dir overrides the local workflow directory when no remote repo is specified.
			// When --repo is set, --path is used for the remote repository path instead.
			if dir != "" && repo == "" {
				path = dir
			}
			return RunListWorkflows(cmd.Context(), repo, path, pattern, verbose, jsonFlag, labelFilter, staleOnly)
		},
	}

	addRepoFlag(cmd)
	addJSONFlag(cmd)
	cmd.Flags().String("label", "", "Filter workflows by label")
	cmd.Flags().Bool("stale", false, "List workflows with stale or missing lock files (local only)")
	cmd.Flags().String("path", constants.GetWorkflowDir(), "Path to workflows directory in the remote repository (used with --repo)")
	cmd.Flags().StringP("dir", "d", "", "Workflow directory (default: $GH_AW_WORKFLOWS_DIR or .github/workflows; ignored when --repo is set)")

	// Register completions for list command
	cmd.ValidArgsFunction = CompleteWorkflowNames
	RegisterDirFlagCompletion(cmd, "dir")

	return cmd
}

// RunListWorkflows lists workflows without checking GitHub status
func RunListWorkflows(ctx context.Context, repo, path, pattern string, verbose bool, jsonOutput bool, labelFilter string, staleOnly bool) error {
	listWorkflowsLog.Printf("Listing workflows: repo=%s, path=%s, pattern=%s, jsonOutput=%v, labelFilter=%s, staleOnly=%v", repo, path, pattern, jsonOutput, labelFilter, staleOnly)
	if repo != "" && staleOnly {
		return errors.New("--stale cannot be used with --repo because remote workflow compilation status is unavailable")
	}

	mdFiles, isRemote, err := listWorkflowFiles(ctx, repo, path, pattern, verbose, jsonOutput)
	if err != nil {
		listWorkflowsLog.Printf("Failed to get markdown workflow files: %v", err)
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(err.Error()))
		return nil
	}

	listWorkflowsLog.Printf("Found %d markdown workflow files", len(mdFiles))
	if len(mdFiles) == 0 {
		if jsonOutput {
			// Output empty array for JSON
			output := []WorkflowListItem{}
			jsonBytes, _ := json.MarshalIndent(output, "", "  ")
			fmt.Fprintln(os.Stdout, string(jsonBytes))
			return nil
		}
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No workflow files found."))
		return nil
	}

	if verbose && !jsonOutput {
		fmt.Fprintf(os.Stderr, "Found %d markdown workflow files\n", len(mdFiles))
	}

	return outputWorkflowList(buildWorkflowList(mdFiles, path, pattern, labelFilter, staleOnly, isRemote), jsonOutput)
}

func listWorkflowFiles(ctx context.Context, repo, path, pattern string, verbose, jsonOutput bool) ([]string, bool, error) {
	if repo != "" {
		if verbose && !jsonOutput {
			fmt.Fprintf(os.Stderr, "Listing workflow files from %s\n", repo)
		}
		files, err := getRemoteWorkflowFiles(ctx, repo, path, verbose, jsonOutput)
		return files, true, err
	}

	if verbose && !jsonOutput {
		fmt.Fprintln(os.Stderr, "Listing workflow files")
		if pattern != "" {
			fmt.Fprintf(os.Stderr, "Filtering by pattern: %s\n", pattern)
		}
	}
	files, err := getMarkdownWorkflowFiles(path)
	return files, false, err
}

func buildWorkflowList(mdFiles []string, path, pattern, labelFilter string, staleOnly, isRemote bool) []WorkflowListItem {
	workflows := make([]WorkflowListItem, 0)
	// Shared import cache across all iterations to avoid re-creating it for every workflow
	importCache := parser.NewImportCache("")

	for _, file := range mdFiles {
		name := extractWorkflowNameFromPath(file)

		if pattern != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(pattern)) {
			continue
		}
		if isRemote {
			workflows = append(workflows, remoteWorkflowListItem(name))
			continue
		}
		item, include := localWorkflowListItem(file, name, labelFilter, staleOnly, importCache)
		if include {
			workflows = append(workflows, item)
		}
	}
	return workflows
}

func remoteWorkflowListItem(name string) WorkflowListItem {
	return WorkflowListItem{
		Workflow: name,
		EngineID: "N/A",
		Compiled: "N/A",
	}
}

func localWorkflowListItem(file, name, labelFilter string, staleOnly bool, importCache *parser.ImportCache) (WorkflowListItem, bool) {
	lockFile := stringutil.MarkdownToLockFile(file)
	compiled := "N/A"
	if _, err := os.Stat(lockFile); err == nil {
		compiled = isCompiledUpToDateWithCache(file, lockFile, importCache)
	}
	if staleOnly && compiled == "Yes" {
		return WorkflowListItem{}, false
	}

	onField, labels := workflowFrontmatterFields(file)
	if labelFilter != "" && !slices.ContainsFunc(labels, func(label string) bool {
		return strings.EqualFold(label, labelFilter)
	}) {
		return WorkflowListItem{}, false
	}
	return WorkflowListItem{
		Workflow: name,
		EngineID: extractEngineIDFromFile(file),
		Compiled: compiled,
		Labels:   labels,
		On:       onField,
	}, true
}

func workflowFrontmatterFields(file string) (any, []string) {
	content, err := os.ReadFile(file)
	if err != nil {
		return nil, nil
	}
	result, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil || result.Frontmatter == nil {
		return nil, nil
	}
	var labels []string
	if labelsField, ok := result.Frontmatter["labels"]; ok {
		if labelsArray, ok := labelsField.([]any); ok {
			for _, label := range labelsArray {
				if labelStr, ok := label.(string); ok {
					labels = append(labels, labelStr)
				}
			}
		}
	}
	return result.Frontmatter["on"], labels
}

func outputWorkflowList(workflows []WorkflowListItem, jsonOutput bool) error {
	if jsonOutput {
		jsonBytes, err := marshalIndentJSONOrWrap(workflows, "workflow list")
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, string(jsonBytes))
		return nil
	}

	// Print workflow count message for text output
	workflowCount := len(workflows)
	if workflowCount == 1 {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Found 1 workflow"))
	} else {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Found %d workflows", workflowCount)))
	}

	// Render the table using struct-based rendering
	fmt.Fprint(os.Stderr, console.RenderStruct(workflows))

	return nil
}

// getRemoteWorkflowFiles fetches the list of workflow files from a remote repository
func getRemoteWorkflowFiles(ctx context.Context, repoSpec, workflowPath string, verbose bool, jsonOutput bool) ([]string, error) {
	listWorkflowsLog.Printf("Fetching remote workflow files: repoSpec=%s, path=%s", repoSpec, workflowPath)
	// Parse repo spec: owner/repo[@ref]
	var owner, repo, ref string
	repoPart, ref, hasRef := strings.Cut(repoSpec, "@")
	if !hasRef {
		ref = "main" // default to main branch
	}

	// Parse owner/repo
	owner, repo, err := repoutil.SplitRepoSlug(repoPart)
	if err != nil {
		return nil, fmt.Errorf("invalid repository format: %s (expected owner/repo or owner/repo@ref)", repoSpec)
	}

	if verbose && !jsonOutput {
		fmt.Fprintf(os.Stderr, "Fetching workflow files from %s/%s@%s (path: %s)\n", owner, repo, ref, workflowPath)
	}

	// Use the parser package to list workflow files
	listWorkflowsLog.Printf("Listing remote workflow files: owner=%s, repo=%s, ref=%s, path=%s", owner, repo, ref, workflowPath)
	files, err := parser.ListWorkflowFiles(ctx, owner, repo, ref, workflowPath)
	if err != nil {
		listWorkflowsLog.Printf("Failed to list remote workflow files: %v", err)
		return nil, fmt.Errorf("failed to list workflow files from %s/%s: %w", owner, repo, err)
	}

	listWorkflowsLog.Printf("Found %d remote workflow files", len(files))
	return files, nil
}
