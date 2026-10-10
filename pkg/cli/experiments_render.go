package cli

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/sliceutil"
)

func printExperimentDetails(d *ExperimentDetails) {
	fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("Experiment workflow: "+d.WorkflowID))
	fmt.Fprintf(os.Stderr, "  Branch:     %s\n", d.Branch)
	fmt.Fprintf(os.Stderr, "  Total runs: %d\n", d.TotalRuns)

	if len(d.Experiments) > 0 {
		for _, exp := range d.Experiments {
			// Sort variants for deterministic display.
			type kv struct {
				k string
				v int
			}
			pairs := make([]kv, 0, len(exp.Variants))
			for k, v := range exp.Variants {
				pairs = append(pairs, kv{k, v})
			}
			slices.SortFunc(pairs, func(a, b kv) int {
				return strings.Compare(a.k, b.k)
			})
			rows := make([][]string, 0, len(pairs))
			for _, p := range pairs {
				pct := 0
				if exp.Total > 0 {
					pct = p.v * 100 / exp.Total
				}
				rows = append(rows, []string{p.k, strconv.Itoa(p.v), strconv.Itoa(pct) + "%"})
			}
			if len(rows) > 0 {
				fmt.Fprintf(os.Stderr, "\n%s", console.RenderTableStderr(console.TableConfig{
					Title:    fmt.Sprintf("%s (total: %d)", exp.Name, exp.Total),
					Headers:  []string{"Variant", "Count", "Percent"},
					Rows:     rows,
					MaxWidth: console.DefaultTableWidth,
				}))
			}
		}
	} else {
		fmt.Fprintln(os.Stderr, "\nNo experiment data found (state.jsonl/state.json not present or empty).")
	}

	printExperimentAnalyses(d.Analyses)

	if len(d.RecentRuns) > 0 {
		rows := make([][]string, 0, len(d.RecentRuns))
		for _, run := range d.RecentRuns {
			date := run.Timestamp
			if len(date) >= 10 {
				date = date[:10]
			}
			rows = append(rows, []string{date, run.RunID, formatAssignments(run.Assignments)})
		}
		fmt.Fprintf(os.Stderr, "\n%s", console.RenderTableStderr(console.TableConfig{
			Title:    "Recent runs",
			Headers:  []string{"Date", "Run ID", "Assignments"},
			Rows:     rows,
			MaxWidth: console.DefaultTableWidth,
		}))
	}
}

// formatAssignments formats a map of experiment→variant as "k=v, k=v" sorted by key.
func formatAssignments(assignments map[string]string) string {
	if len(assignments) == 0 {
		return "-"
	}
	keys := sliceutil.SortedKeys(assignments)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+assignments[k])
	}
	return strings.Join(parts, ", ")
}
