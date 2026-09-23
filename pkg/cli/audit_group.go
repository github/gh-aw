package cli

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/parser"
)

// GroupedAuditReport represents audit findings grouped by run and stable finding code.
type GroupedAuditReport struct {
	RunsAnalyzed int                 `json:"runs_analyzed"`
	Entries      []GroupedAuditEntry `json:"entries"`
}

// GroupedAuditEntry summarizes all findings for one [run, audit code] pair.
type GroupedAuditEntry struct {
	RunID               int64            `json:"run_id"`
	Code                AuditFindingCode `json:"code"`
	Occurrences         int              `json:"occurrences"`
	RepresentativeEntry AuditFinding     `json:"representative_entry"`
}

func runAuditGrouped(ctx context.Context, args []string, opts auditCommandOptions) error {
	runRequests, err := resolveGroupedAuditRunRequests(args, opts.repoFlag)
	if err != nil {
		return err
	}
	report := GroupedAuditReport{RunsAnalyzed: len(runRequests)}
	for _, request := range runRequests {
		if err := AuditWorkflowRun(ctx, request.runID, AuditOptions{
			Owner:            request.owner,
			Repo:             request.repo,
			Hostname:         request.hostname,
			OutputDir:        opts.outputDir,
			Verbose:          opts.verbose,
			ArtifactSets:     opts.artifacts,
			ExperimentFilter: opts.experimentFilter,
			VariantFilter:    opts.variantFilter,
			RuntimeFilter:    opts.runtimeFilter,
			EvalsOnly:        opts.evalsOnly,
			Group:            true,
		}); err != nil {
			return err
		}
		auditData, ok := loadGroupedAuditData(opts.outputDir, request.runID)
		if !ok {
			continue
		}
		report.Entries = append(report.Entries, groupAuditFindingsForRun(request.runID, auditData.KeyFindings)...)
	}
	sortGroupedAuditEntries(report.Entries)
	return renderGroupedAuditReport(report, opts)
}

type groupedAuditRunRequest struct {
	runID    int64
	owner    string
	repo     string
	hostname string
}

func resolveGroupedAuditRunRequests(args []string, repoFlag string) ([]groupedAuditRunRequest, error) {
	baseArg, ok := firstAuditArg(args)
	if !ok {
		return nil, errors.New(console.FormatErrorWithSuggestions(
			"at least one run ID or URL is required",
			[]string{"Provide a run ID or URL as a positional argument"},
		))
	}
	baseComponents, err := parser.ParseRunURLExtended(baseArg)
	if err != nil {
		return nil, fmt.Errorf("invalid run %q: %w", baseArg, err)
	}
	if err := applyAuditRepoFlag(repoFlag, baseComponents); err != nil {
		return nil, err
	}
	owner, repo, hostname := baseComponents.Owner, baseComponents.Repo, baseComponents.Host
	seen := make(map[int64]bool, len(args))
	requests := make([]groupedAuditRunRequest, 0, len(args))
	for _, arg := range args {
		components, err := parser.ParseRunURLExtended(arg)
		if err != nil {
			return nil, fmt.Errorf("invalid run %q: %w", arg, err)
		}
		if seen[components.Number] {
			return nil, fmt.Errorf("duplicate run ID %d: each run ID must appear only once", components.Number)
		}
		seen[components.Number] = true
		requests = append(requests, groupedAuditRunRequest{
			runID:    components.Number,
			owner:    firstNonEmptyAuditValue(components.Owner, owner),
			repo:     firstNonEmptyAuditValue(components.Repo, repo),
			hostname: firstNonEmptyAuditValue(components.Host, hostname),
		})
	}
	return requests, nil
}

func firstNonEmptyAuditValue(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func loadGroupedAuditData(outputDir string, runID int64) (AuditData, bool) {
	auditFile := filepath.Join(resolveAuditOutputDir(outputDir, runID), auditFileName)
	data, err := os.ReadFile(auditFile)
	if err != nil {
		return AuditData{}, false
	}
	var auditData AuditData
	if err := json.Unmarshal(data, &auditData); err != nil {
		return AuditData{}, false
	}
	return auditData, true
}

func groupAuditFindingsForRun(runID int64, findings []AuditFinding) []GroupedAuditEntry {
	entriesByCode := make(map[AuditFindingCode]*GroupedAuditEntry)
	for _, finding := range findings {
		entry := entriesByCode[finding.Code]
		if entry == nil {
			entriesByCode[finding.Code] = &GroupedAuditEntry{
				RunID:               runID,
				Code:                finding.Code,
				RepresentativeEntry: finding,
			}
			entry = entriesByCode[finding.Code]
		}
		entry.Occurrences++
	}
	entries := make([]GroupedAuditEntry, 0, len(entriesByCode))
	for _, entry := range entriesByCode {
		entries = append(entries, *entry)
	}
	return entries
}

func sortGroupedAuditEntries(entries []GroupedAuditEntry) {
	slices.SortFunc(entries, func(a, b GroupedAuditEntry) int {
		if runCompare := cmp.Compare(a.RunID, b.RunID); runCompare != 0 {
			return runCompare
		}
		return cmp.Compare(a.Code, b.Code)
	})
}

func renderGroupedAuditReport(report GroupedAuditReport, opts auditCommandOptions) error {
	if opts.jsonOutput || opts.format == "json" {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	if opts.format == "markdown" {
		renderGroupedAuditReportMarkdown(report)
		return nil
	}
	renderGroupedAuditReportPretty(report)
	return nil
}

func renderGroupedAuditReportPretty(report GroupedAuditReport) {
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Grouped audit findings across %d run(s)", report.RunsAnalyzed)))
	if len(report.Entries) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("No audit findings found."))
		return
	}
	for _, entry := range report.Entries {
		fmt.Fprintf(os.Stderr, "- run %d code %s: %d occurrence(s); representative: %s\n",
			entry.RunID, entry.Code, entry.Occurrences, entry.RepresentativeEntry.Title)
	}
}

func renderGroupedAuditReportMarkdown(report GroupedAuditReport) {
	fmt.Fprintf(os.Stdout, "### Grouped Audit Findings\n\n")
	fmt.Fprintf(os.Stdout, "Runs analyzed: %d\n\n", report.RunsAnalyzed)
	if len(report.Entries) == 0 {
		fmt.Fprintln(os.Stdout, "No audit findings found.")
		return
	}
	fmt.Fprintln(os.Stdout, "| Run | Code | Occurrences | Representative |")
	fmt.Fprintln(os.Stdout, "|---:|---|---:|---|")
	for _, entry := range report.Entries {
		fmt.Fprintf(os.Stdout, "| %d | `%s` | %d | %s |\n",
			entry.RunID, entry.Code, entry.Occurrences, escapeMarkdownTableCell(entry.RepresentativeEntry.Title))
	}
}

func escapeMarkdownTableCell(value string) string {
	return strings.ReplaceAll(value, "|", "\\|")
}
