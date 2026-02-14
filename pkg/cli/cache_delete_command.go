package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
)

var cacheDeleteLog = logger.New("cli:cache_delete")

// NewCacheDeleteCommand creates the cache delete command
func NewCacheDeleteCommand() *cobra.Command {
	var deleteAll bool
	var cacheKey string
	var ref string
	var force bool

	cmd := &cobra.Command{
		Use:   "delete [workflow]",
		Short: "Delete cache artifacts for a workflow",
		Long: `Delete GitHub Actions cache artifacts for agentic workflows using cache-memory.

This command deletes all cache artifacts that workflows created when using the cache-memory
feature. By default, it prompts for confirmation before deleting all matching caches.
Use --force to skip the confirmation prompt.

` + WorkflowIDExplanation + `

Examples:
  ` + string(constants.CLIExtensionPrefix) + ` cache delete my-workflow                      # Delete all matching caches (with confirmation)
  ` + string(constants.CLIExtensionPrefix) + ` cache delete my-workflow --force              # Delete all without confirmation
  ` + string(constants.CLIExtensionPrefix) + ` cache delete my-workflow -k memory-custom     # Delete caches with custom key prefix
  ` + string(constants.CLIExtensionPrefix) + ` cache delete my-workflow -r refs/heads/main   # Delete caches for specific ref`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			workflowID := args[0]
			verbose, _ := cmd.Flags().GetBool("verbose")

			config := CacheDeleteConfig{
				WorkflowID: workflowID,
				DeleteAll:  deleteAll,
				CacheKey:   cacheKey,
				Ref:        ref,
				Force:      force,
				Verbose:    verbose,
			}

			return RunCacheDelete(config)
		},
	}

	cmd.Flags().BoolVarP(&deleteAll, "all", "a", false, "Delete all caches matching the pattern (default behavior)")
	cmd.Flags().StringVarP(&cacheKey, "key", "k", "", "Filter by cache key prefix")
	cmd.Flags().StringVarP(&ref, "ref", "r", "", "Filter by ref (e.g., refs/heads/main)")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompt")

	return cmd
}

// CacheDeleteConfig holds configuration for cache deletion
type CacheDeleteConfig struct {
	WorkflowID string
	DeleteAll  bool
	CacheKey   string
	Ref        string
	Force      bool
	Verbose    bool
}

// RunCacheDelete executes the cache deletion logic
func RunCacheDelete(config CacheDeleteConfig) error {
	cacheDeleteLog.Printf("Starting cache delete: workflow=%s, deleteAll=%t", config.WorkflowID, config.DeleteAll)

	// Normalize workflow ID (handles paths, .md extension, etc.)
	workflowID := normalizeWorkflowID(config.WorkflowID)

	// Validate that the workflow exists
	if err := validateWorkflowName(workflowID); err != nil {
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(err.Error()))
		return fmt.Errorf("workflow validation failed: %w", err)
	}

	// Determine cache key pattern to search for
	keyPattern := config.CacheKey
	if keyPattern == "" {
		// Default: search for caches with keys matching memory-<workflow>-
		keyPattern = fmt.Sprintf("memory-%s-", workflowID)
	}

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Searching for caches with key prefix: %s", keyPattern)))

	// List caches to see what we're about to delete
	caches, err := listCaches(keyPattern, 100, config.Verbose)
	if err != nil {
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(err.Error()))
		return fmt.Errorf("failed to list caches: %w", err)
	}

	if len(caches) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("No caches found with key prefix: %s", keyPattern)))
		return nil
	}

	// Determine which caches to delete
	// Default behavior: delete all caches (unless explicitly limited)
	cachesToDelete := caches

	// Confirmation prompt (unless --force is used)
	if !config.Force {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("About to delete %d cache(s):", len(cachesToDelete))))
		for _, cache := range cachesToDelete {
			listItem := fmt.Sprintf("%s (ID: %d, Size: %s)", cache.Key, cache.ID, console.FormatFileSize(cache.SizeInBytes))
			fmt.Fprintln(os.Stderr, console.FormatListItem(listItem))
		}
		fmt.Fprintf(os.Stderr, "\n%s", console.FormatPromptMessage("Proceed with deletion? [y/N]: "))

		var response string
		fmt.Scanln(&response)
		response = strings.ToLower(strings.TrimSpace(response))
		if response != "y" && response != "yes" {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Deletion cancelled"))
			return nil
		}
	}

	// Delete caches with throttling protection
	successCount := 0
	failedCount := 0

	// Start spinner for bulk operations
	var spinner *console.SpinnerWrapper
	if !config.Verbose && len(cachesToDelete) > 1 {
		spinner = console.NewSpinner(fmt.Sprintf("Deleting %d cache(s)...", len(cachesToDelete)))
		spinner.Start()
	}

	for i, cache := range cachesToDelete {
		if config.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("[%d/%d] Deleting cache: %s (ID: %d)",
				i+1, len(cachesToDelete), cache.Key, cache.ID)))
		}

		if err := deleteCache(cache.ID, config.Ref, config.Verbose); err != nil {
			cacheDeleteLog.Printf("Failed to delete cache %d: %v", cache.ID, err)
			failedCount++

			// Check for rate limiting
			if strings.Contains(err.Error(), "rate limit") || strings.Contains(err.Error(), "403") {
				if spinner != nil {
					spinner.Stop()
				}
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Hit API rate limit after deleting %d cache(s). Stopping.", successCount)))
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Wait a few minutes before retrying, or delete remaining caches individually."))
				break
			}

			if !config.Verbose {
				// Only show error for non-verbose mode
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to delete cache %d: %v", cache.ID, err)))
			}
			continue
		}

		successCount++

		// Add small delay between deletes to avoid rate limiting
		if i < len(cachesToDelete)-1 && len(cachesToDelete) > 5 {
			time.Sleep(200 * time.Millisecond)
		}
	}

	if spinner != nil {
		spinner.Stop()
	}

	if successCount > 0 {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Successfully deleted %d cache(s)", successCount)))
	}
	if failedCount > 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to delete %d cache(s)", failedCount)))
	}
	if successCount == 0 && failedCount == 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("No caches were deleted"))
	}

	return nil
}

// deleteCache deletes a single cache by ID
func deleteCache(cacheID int64, ref string, verbose bool) error {
	cacheDeleteLog.Printf("Deleting cache: cacheID=%d, ref=%s", cacheID, ref)

	args := []string{
		"cache", "delete",
		fmt.Sprintf("%d", cacheID),
	}

	if ref != "" {
		args = append(args, "--ref", ref)
	}

	// Perform the cache deletion. Spinner handling is managed by callers.
	_, err := workflow.RunGHCombined("Deleting cache...", args...)
	if err != nil {
		return fmt.Errorf("gh cache delete failed: %w", err)
	}

	cacheDeleteLog.Printf("Successfully deleted cache: cacheID=%d", cacheID)
	return nil
}

// CacheEntry represents a GitHub Actions cache
type CacheEntry struct {
	ID             int64  `json:"id"`
	Key            string `json:"key"`
	Ref            string `json:"ref"`
	SizeInBytes    int64  `json:"size_in_bytes"`
	CreatedAt      string `json:"created_at"`
	LastAccessedAt string `json:"last_accessed_at"`
}

// listCaches retrieves cache entries from GitHub Actions cache API
func listCaches(keyPrefix string, limit int, verbose bool) ([]CacheEntry, error) {
	cacheDeleteLog.Printf("Listing caches: keyPrefix=%s, limit=%d", keyPrefix, limit)

	// Use spinner for listing
	spinner := console.NewSpinner("Searching for caches...")
	if !verbose {
		spinner.Start()
	}

	// Use gh CLI to list caches
	args := []string{
		"cache", "list",
		"--key", keyPrefix,
		"--limit", fmt.Sprintf("%d", limit),
		"--json", "id,key,ref,sizeInBytes,createdAt,lastAccessedAt",
	}

	output, err := workflow.RunGHCombined("Listing caches...", args...)

	if !verbose {
		if err != nil {
			spinner.Stop()
		} else {
			spinner.StopWithMessage("✓ Found caches")
		}
	}

	if err != nil {
		return nil, fmt.Errorf("gh cache list failed: %w", err)
	}

	// Parse JSON output
	var caches []CacheEntry
	if err := json.Unmarshal(output, &caches); err != nil {
		return nil, fmt.Errorf("failed to parse cache list: %w", err)
	}

	cacheDeleteLog.Printf("Found %d caches", len(caches))
	return caches, nil
}
