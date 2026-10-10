// This file implements the "view" command, which downloads artifacts for a
// workflow run (reusing the helpers from audit/logs) and renders a unified
// MCP Gateway + AWF Firewall + Agent event timeline directly in the console.
//
// Usage:
//
//	gh aw view <run-id-or-url>
//
// The output simulates the chronological activity log that would be visible
// while observing a Copilot CLI session, but is produced entirely offline
// from the downloaded artifacts.

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/spf13/cobra"
)

var viewLog = logger.New("cli:view")

// NewViewCommand creates the view command.
func NewViewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view <run-id-or-url>",
		Short: "Render unified timeline and safe outputs for a workflow run",
		Long: `Download artifacts for a workflow run and render a unified, chronologically
ordered activity timeline together with any safe outputs in the console.

The timeline merges events from three sources:
  - MCP Gateway logs  (gateway.jsonl / rpc-messages.jsonl)
  - AWF Firewall logs (audit.jsonl)
  - Agent session logs (events.jsonl)

The result simulates what you would see when watching a Copilot CLI session
live, providing a readable, complete log of all agentic activity, followed
by any safe outputs created during the run (e.g., issues, pull requests,
comments) and a link to the GitHub Actions workflow run page.

The run argument accepts the same formats as the "audit" command:
  - A numeric run ID                     (e.g., 1234567890)
  - A GitHub Actions run URL             (e.g., https://github.com/owner/repo/actions/runs/1234567890)
  - A GitHub Enterprise run URL

Artifacts are downloaded to the default logs directory and cached; repeated
invocations for the same run ID will read from the local cache without
re-downloading.`,
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` view 1234567890
  ` + string(constants.CLIExtensionPrefix) + ` view https://github.com/owner/repo/actions/runs/1234567890
  ` + string(constants.CLIExtensionPrefix) + ` view 1234567890 --repo owner/repo
  ` + string(constants.CLIExtensionPrefix) + ` view 1234567890 -o ./my-logs
  ` + string(constants.CLIExtensionPrefix) + ` view 1234567890 -v`,
		Args:   cobra.ExactArgs(1),
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			verbose, _ := cmd.Flags().GetBool("verbose")
			outputDir, _ := cmd.Flags().GetString("output")
			repoFlag, _ := cmd.Flags().GetString("repo")

			runIDOrURL := args[0]

			components, err := parser.ParseRunURLExtended(runIDOrURL)
			if err != nil {
				return err
			}

			// Apply --repo flag when owner/repo were not inferred from a URL.
			if repoFlag != "" && components.Owner == "" {
				parts := strings.SplitN(repoFlag, "/", 2)
				if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
					return fmt.Errorf("invalid repository format %q: expected 'owner/repo'", repoFlag)
				}
				components.Owner = parts[0]
				components.Repo = parts[1]
			}

			if outputDir == "" {
				outputDir = defaultLogsOutputDir
			}

			return ViewWorkflowRun(cmd.Context(), components.Number, ViewOptions{
				Owner:     components.Owner,
				Repo:      components.Repo,
				Hostname:  components.Host,
				OutputDir: outputDir,
				Verbose:   verbose,
			})
		},
	}

	addOutputFlag(cmd, defaultLogsOutputDir)
	addRepoFlag(cmd)
	RegisterDirFlagCompletion(cmd, "output")

	return cmd
}

// ViewOptions holds configuration for the view command.
type ViewOptions struct {
	Owner     string
	Repo      string
	Hostname  string
	OutputDir string
	Verbose   bool
}

// ViewWorkflowRun downloads artifacts for the given run (if not already cached)
// and renders the unified event timeline, safe outputs, and a link to the run page.
func ViewWorkflowRun(ctx context.Context, runID int64, opts ViewOptions) error {
	viewLog.Printf("Starting view for run %d (owner=%s, repo=%s, hostname=%s)", runID, opts.Owner, opts.Repo, opts.Hostname)

	// Auto-detect GHES host from git remote when not explicitly provided.
	hostname := opts.Hostname
	if hostname == "" {
		hostname = getHostFromOriginRemote()
		if hostname != "github.com" {
			viewLog.Printf("Auto-detected GHES host from git remote: %s", hostname)
		}
	}

	runDir := filepath.Join(opts.OutputDir, fmt.Sprintf("run-%d", runID))
	if absDir, err := filepath.Abs(runDir); err == nil {
		runDir = absDir
	}

	if opts.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Viewing run %d...", runID)))
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Run directory: "+runDir))
	}

	// Download artifacts when the run directory does not yet contain the JSONL
	// log files we need.  We deliberately pass a nil artifact filter so that all
	// artifacts are downloaded — the timeline relies on whichever JSONL files
	// happen to be present; no single one is strictly required.
	if err := downloadRunArtifacts(ctx, downloadArtifactsOptions{runID: runID, outputDir: runDir, verbose: opts.Verbose, owner: opts.Owner, repo: opts.Repo, hostname: hostname}); err != nil {
		if !errors.Is(err, ErrNoArtifacts) {
			return fmt.Errorf("failed to download artifacts for run %d: %w", runID, err)
		}
		// No artifacts is non-fatal: the run may still have useful events in the
		// workflow logs or the directory may have been populated by a previous run.
		if opts.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("No artifacts attached to this run; timeline may be empty."))
		}
	}

	// Collect and merge events from all available JSONL sources.
	events, err := BuildUnifiedTimeline(runDir, opts.Verbose)
	if err != nil {
		return fmt.Errorf("failed to build timeline for run %d: %w", runID, err)
	}

	if len(events) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("No timeline events found for run %d.", runID)))
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Ensure the workflow has gateway.jsonl, audit.jsonl, or events.jsonl artifacts."))
	} else {
		output := renderUnifiedTimelineStream(events)
		if output != "" {
			fmt.Fprint(os.Stdout, output)
		}
	}

	// Render safe outputs if any were created during the run.
	renderViewSafeOutputs(runDir)

	// Finish with a link to the GitHub Actions run page.
	runURL := buildRunHTMLURL(hostname, opts.Owner, opts.Repo, runID)
	if runURL != "" {
		fmt.Fprintln(os.Stdout, console.FormatInfoMessageStdout(runURL))
	}

	return nil
}

// buildRunHTMLURL constructs the GitHub Actions HTML URL for a workflow run.
// Returns an empty string when owner or repo are unknown.
func buildRunHTMLURL(hostname, owner, repo string, runID int64) string {
	if owner == "" || repo == "" {
		return ""
	}
	if hostname == "" {
		hostname = "github.com"
	}
	return fmt.Sprintf("https://%s/%s/%s/actions/runs/%d", hostname, owner, repo, runID)
}

// renderViewSafeOutputs reads safe-output-items.jsonl from runDir and prints a
// human-friendly summary of every item that was created during the run.
// A missing or empty manifest is silently ignored.
func renderViewSafeOutputs(runDir string) {
	items := extractCreatedItemsFromManifest(runDir)
	if len(items) == 0 {
		return
	}

	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, console.FormatSectionHeaderStdout("Safe Outputs"))
	for _, item := range items {
		line := "  " + item.Type
		if item.URL != "" {
			line += "  " + item.URL
		} else if item.Repo != "" && item.Number > 0 {
			line += fmt.Sprintf("  %s#%d", item.Repo, item.Number)
		}
		fmt.Fprintln(os.Stdout, line)
	}
}
