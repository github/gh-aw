package cli

import (
	"fmt"
	"os"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/spf13/cobra"
)

var cacheListLog = logger.New("cli:cache_list")

// NewCacheListCommand creates the cache list command
func NewCacheListCommand() *cobra.Command {
	var limit int
	var cacheKey string
	var ref string

	cmd := &cobra.Command{
		Use:   "list [workflow]",
		Short: "List cache artifacts for a workflow",
		Long: `List GitHub Actions cache artifacts for agentic workflows using cache-memory.

This command lists cache artifacts that workflows created when using the cache-memory
feature. By default, it lists all caches with keys matching the workflow name pattern
'memory-<workflow>-*'.

If no workflow is specified, lists all caches in the repository.

` + WorkflowIDExplanation + `

Examples:
  ` + string(constants.CLIExtensionPrefix) + ` cache list                              # List all caches in repository
  ` + string(constants.CLIExtensionPrefix) + ` cache list my-workflow                  # List caches for specific workflow
  ` + string(constants.CLIExtensionPrefix) + ` cache list my-workflow -L 10            # Limit to 10 most recent
  ` + string(constants.CLIExtensionPrefix) + ` cache list my-workflow -k memory-custom # Filter by custom cache key
  ` + string(constants.CLIExtensionPrefix) + ` cache list -r refs/heads/main           # Filter by branch ref`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			workflowID := ""
			if len(args) > 0 {
				workflowID = args[0]
			}
			verbose, _ := cmd.Flags().GetBool("verbose")

			config := CacheListConfig{
				WorkflowID: workflowID,
				Limit:      limit,
				CacheKey:   cacheKey,
				Ref:        ref,
				Verbose:    verbose,
			}

			return RunCacheList(config)
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "L", 30, "Maximum number of caches to list")
	cmd.Flags().StringVarP(&cacheKey, "key", "k", "", "Filter by cache key prefix")
	cmd.Flags().StringVarP(&ref, "ref", "r", "", "Filter by ref (e.g., refs/heads/main)")

	return cmd
}

// CacheListConfig holds configuration for cache listing
type CacheListConfig struct {
	WorkflowID string
	Limit      int
	CacheKey   string
	Ref        string
	Verbose    bool
}

// RunCacheList executes the cache listing logic
func RunCacheList(config CacheListConfig) error {
	cacheListLog.Printf("Starting cache list: workflow=%s, limit=%d", config.WorkflowID, config.Limit)

	// Determine cache key pattern to search for
	keyPattern := config.CacheKey
	if keyPattern == "" && config.WorkflowID != "" {
		// Normalize workflow ID (handles paths, .md extension, etc.)
		workflowID := normalizeWorkflowID(config.WorkflowID)
		// Default: search for caches with keys matching memory-<workflow>-
		keyPattern = fmt.Sprintf("memory-%s-", workflowID)
	}

	if keyPattern != "" {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Searching for caches with key prefix: %s", keyPattern)))
	} else {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Listing all caches in repository"))
	}

	// List caches using gh CLI with optional ref filter
	caches, err := listCachesWithRef(keyPattern, config.Ref, config.Limit, config.Verbose)
	if err != nil {
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(err.Error()))
		return fmt.Errorf("failed to list caches: %w", err)
	}

	if len(caches) == 0 {
		if keyPattern != "" {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("No caches found with key prefix: %s", keyPattern)))
		} else {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("No caches found in repository"))
		}
		return nil
	}

	// Display caches in a formatted table
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Found %d cache(s)", len(caches))))
	fmt.Fprintln(os.Stderr, "")

	// Build table configuration for console.RenderTable
	headers := []string{"ID", "KEY", "REF", "SIZE", "LAST ACCESSED"}
	var rows [][]string

	for _, cache := range caches {
		// Format size using console helper
		sizeStr := console.FormatFileSize(cache.SizeInBytes)

		// Truncate key if too long
		key := cache.Key
		if len(key) > 50 {
			key = key[:47] + "..."
		}

		// Truncate ref if too long
		ref := cache.Ref
		if len(ref) > 25 {
			ref = ref[:22] + "..."
		}

		// Format last accessed time
		lastAccessed := formatTime(cache.LastAccessedAt)

		row := []string{
			fmt.Sprintf("%d", cache.ID),
			key,
			ref,
			sizeStr,
			lastAccessed,
		}
		rows = append(rows, row)
	}

	// Render table using console package helper
	tableConfig := console.TableConfig{
		Headers: headers,
		Rows:    rows,
	}

	fmt.Fprint(os.Stderr, console.RenderTable(tableConfig))

	return nil
}

// listCachesWithRef retrieves cache entries with optional ref filter
func listCachesWithRef(keyPrefix string, ref string, limit int, verbose bool) ([]CacheEntry, error) {
	cacheListLog.Printf("Listing caches: keyPrefix=%s, ref=%s, limit=%d", keyPrefix, ref, limit)

	// Use spinner for listing
	spinner := console.NewSpinner("Searching for caches...")
	if !verbose {
		spinner.Start()
	}

	// Call listCaches which already handles the listing
	output, err := listCaches(keyPrefix, limit, verbose)

	if err != nil {
		if !verbose {
			spinner.Stop()
		}
		return nil, err
	}

	// Filter by ref if specified (manual filtering since not all gh CLI versions support --ref)
	if ref != "" {
		var filtered []CacheEntry
		for _, cache := range output {
			if cache.Ref == ref {
				filtered = append(filtered, cache)
			}
		}
		output = filtered
	}

	// Stop spinner without message (avoid duplicate)
	if !verbose {
		spinner.Stop()
	}

	cacheListLog.Printf("Found %d caches after filtering", len(output))
	return output, nil
}

// formatTime formats ISO 8601 timestamp into human-readable format
func formatTime(timestamp string) string {
	if timestamp == "" {
		return "N/A"
	}
	// Just return a simplified version for now
	// Could parse and format more nicely if needed
	if len(timestamp) > 19 {
		return timestamp[:19]
	}
	return timestamp
}
