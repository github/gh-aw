package cli

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	ghmapping "github.com/github/gh-aw/pkg/github"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
)

var outcomesHistoryLog = logger.New("cli:outcomes_history")

const (
	historySourceIssues = "issues"
	historySourcePRs    = "prs"
	historySourceAll    = "all"
)

var outcomesHistoryRunGH = workflow.RunGH

type OutcomesHistoryConfig struct {
	RepoOverride string
	JSONOutput   bool
	Limit        int
	Source       string
}

type historicalObjectiveItem struct {
	Kind            string   `json:"kind"`
	Number          int      `json:"number"`
	Title           string   `json:"title"`
	URL             string   `json:"url"`
	ClosedAt        string   `json:"closed_at,omitempty"`
	MergedAt        string   `json:"merged_at,omitempty"`
	ObjectiveLabels []string `json:"objective_labels"`
	ObjectiveValue  int      `json:"objective_value"`
}

type historicalObjectiveBucket struct {
	Label            string `json:"label"`
	Count            int    `json:"count"`
	MappedValue      int    `json:"mapped_value"`
	ContributedValue int    `json:"contributed_value"`
}

type historicalObjectiveReport struct {
	Source              string                      `json:"source"`
	SampleSize          int                         `json:"sample_size"`
	ScoredItems         int                         `json:"scored_items"`
	TotalObjectiveValue int                         `json:"total_objective_value"`
	ObjectiveBuckets    []historicalObjectiveBucket `json:"objective_buckets"`
	RepresentativeItems []historicalObjectiveItem   `json:"representative_items"`
}

type historicalObjectivesData struct {
	Repo   string                     `json:"repo"`
	Limit  int                        `json:"limit"`
	Issues *historicalObjectiveReport `json:"issues,omitempty"`
	PRs    *historicalObjectiveReport `json:"prs,omitempty"`
}

type historicalGitHubItem struct {
	Number   int    `json:"number"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	ClosedAt string `json:"closedAt,omitempty"`
	MergedAt string `json:"mergedAt,omitempty"`
	Labels   []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

func NewOutcomesHistorySubcommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "history",
		Short: "Score recent issues and merged PRs against the objective mapping",
		Long: `Score recent issues and merged pull requests against the objective mapping.

This gives a quick local historical view of what kinds of work the repository
has been closing or merging under the current objective mapping.`,
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` outcomes history
  ` + string(constants.CLIExtensionPrefix) + ` outcomes history --source issues --limit 100
  ` + string(constants.CLIExtensionPrefix) + ` outcomes history --repo owner/repo --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonOutput, _ := cmd.Flags().GetBool("json")
			repoOverride, _ := cmd.Flags().GetString("repo")
			limit, _ := cmd.Flags().GetInt("limit")
			source, _ := cmd.Flags().GetString("source")

			return RunOutcomesHistory(OutcomesHistoryConfig{
				RepoOverride: repoOverride,
				JSONOutput:   jsonOutput,
				Limit:        limit,
				Source:       source,
			})
		},
	}

	addJSONFlag(cmd)
	addRepoFlag(cmd)
	cmd.Flags().Int("limit", 200, "Maximum number of items to inspect per source")
	cmd.Flags().String("source", historySourceAll, "History source to inspect: issues, prs, or all")

	return cmd
}

func RunOutcomesHistory(config OutcomesHistoryConfig) error {
	repo := config.RepoOverride
	if repo == "" {
		slug, err := GetCurrentRepoSlug()
		if err != nil {
			return fmt.Errorf("could not determine repository: %w", err)
		}
		repo = slug
	}

	if config.Limit <= 0 {
		config.Limit = 200
	}

	source := strings.TrimSpace(config.Source)
	if source == "" {
		source = historySourceAll
	}
	source = strings.ToLower(source)
	if source != historySourceAll && source != historySourceIssues && source != historySourcePRs {
		return fmt.Errorf("invalid --source %q: expected issues, prs, or all", config.Source)
	}

	outcomesHistoryLog.Printf("Running outcomes history: repo=%s, source=%s, limit=%d, json=%v", repo, source, config.Limit, config.JSONOutput)

	mapping := ghmapping.LoadObjectiveMapping()
	data := historicalObjectivesData{Repo: repo, Limit: config.Limit}

	if source == historySourceAll || source == historySourceIssues {
		issues, err := fetchHistoricalGitHubItems(repo, config.Limit, historySourceIssues)
		if err != nil {
			return err
		}
		report := buildHistoricalObjectiveReport(historySourceIssues, issues, mapping)
		data.Issues = &report
	}

	if source == historySourceAll || source == historySourcePRs {
		prs, err := fetchHistoricalGitHubItems(repo, config.Limit, historySourcePRs)
		if err != nil {
			return err
		}
		report := buildHistoricalObjectiveReport(historySourcePRs, prs, mapping)
		data.PRs = &report
	}

	if config.JSONOutput {
		out, err := marshalIndentJSONOrWrap(data, "outcomes history")
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, string(out))
		return nil
	}

	fmt.Fprintln(os.Stderr, console.FormatSectionHeader(fmt.Sprintf("Objective history for %s (limit %d)", repo, config.Limit)))
	if data.Issues != nil {
		renderHistoricalObjectiveReport(*data.Issues)
	}
	if data.PRs != nil {
		renderHistoricalObjectiveReport(*data.PRs)
	}

	return nil
}

func fetchHistoricalGitHubItems(repo string, limit int, source string) ([]historicalGitHubItem, error) {
	args := []string{"--repo", repo, "--limit", strconv.Itoa(limit), "--json", "number,title,labels,url"}
	spinner := "Listing closed issues..."
	command := []string{"issue", "list", "--state", "closed"}

	if source == historySourcePRs {
		spinner = "Listing merged pull requests..."
		command = []string{"pr", "list", "--state", "merged"}
		args[len(args)-1] = "number,title,labels,url,mergedAt"
	} else {
		args[len(args)-1] = "number,title,labels,url,closedAt"
	}

	output, err := outcomesHistoryRunGH(spinner, append(command, args...)...)
	if err != nil {
		return nil, fmt.Errorf("failed to list %s for %s: %w", source, repo, err)
	}

	var items []historicalGitHubItem
	if err := json.Unmarshal(output, &items); err != nil {
		return nil, fmt.Errorf("failed to parse %s listing JSON: %w", source, err)
	}
	outcomesHistoryLog.Printf("Fetched %d %s items for %s", len(items), source, repo)
	return items, nil
}

func buildHistoricalObjectiveReport(source string, items []historicalGitHubItem, mapping *ghmapping.ObjectiveMapping) historicalObjectiveReport {
	rows := make([]historicalObjectiveItem, 0, len(items))
	bucketCounts := map[string]int{}
	totalObjectiveValue := 0
	scoredItems := 0

	for _, item := range items {
		labels := make([]string, 0, len(item.Labels))
		for _, label := range item.Labels {
			labels = append(labels, label.Name)
		}

		objectiveLabels := mapping.FilterObjectiveLabels(labels)
		objectiveValue := mapping.ComputeObjectiveValue(labels)
		if objectiveValue > 0 {
			scoredItems++
		}
		totalObjectiveValue += objectiveValue

		for _, label := range objectiveLabels {
			normalized := strings.ToLower(strings.TrimSpace(label))
			bucketCounts[normalized]++
		}

		rows = append(rows, historicalObjectiveItem{
			Kind:            source,
			Number:          item.Number,
			Title:           item.Title,
			URL:             item.URL,
			ClosedAt:        item.ClosedAt,
			MergedAt:        item.MergedAt,
			ObjectiveLabels: objectiveLabels,
			ObjectiveValue:  objectiveValue,
		})
	}

	buckets := make([]historicalObjectiveBucket, 0, len(bucketCounts))
	for label, count := range bucketCounts {
		mappedValue := mapping.LabelToValue[label]
		buckets = append(buckets, historicalObjectiveBucket{
			Label:            label,
			Count:            count,
			MappedValue:      mappedValue,
			ContributedValue: mappedValue * count,
		})
	}

	slices.SortFunc(buckets, func(a, b historicalObjectiveBucket) int {
		if a.ContributedValue != b.ContributedValue {
			return cmp.Compare(b.ContributedValue, a.ContributedValue)
		}
		if a.Count != b.Count {
			return cmp.Compare(b.Count, a.Count)
		}
		if a.MappedValue != b.MappedValue {
			return cmp.Compare(b.MappedValue, a.MappedValue)
		}
		return strings.Compare(a.Label, b.Label)
	})

	slices.SortFunc(rows, func(a, b historicalObjectiveItem) int {
		if a.ObjectiveValue != b.ObjectiveValue {
			return cmp.Compare(b.ObjectiveValue, a.ObjectiveValue)
		}
		leftTime := a.ClosedAt
		if leftTime == "" {
			leftTime = a.MergedAt
		}
		rightTime := b.ClosedAt
		if rightTime == "" {
			rightTime = b.MergedAt
		}
		return strings.Compare(leftTime, rightTime)
	})

	representative := make([]historicalObjectiveItem, 0, min(len(rows), 15))
	for _, row := range rows {
		if row.ObjectiveValue <= 0 {
			continue
		}
		representative = append(representative, row)
		if len(representative) == 15 {
			break
		}
	}

	outcomesHistoryLog.Printf("Built %s report: scored=%d/%d, totalValue=%d, buckets=%d", source, scoredItems, len(items), totalObjectiveValue, len(buckets))
	return historicalObjectiveReport{
		Source:              source,
		SampleSize:          len(items),
		ScoredItems:         scoredItems,
		TotalObjectiveValue: totalObjectiveValue,
		ObjectiveBuckets:    buckets,
		RepresentativeItems: representative,
	}
}

func renderHistoricalObjectiveReport(report historicalObjectiveReport) {
	fmt.Fprintf(os.Stderr, "\n%s\n", console.FormatSectionHeader(strings.ToUpper(report.Source)))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Sample size: %d", report.SampleSize)))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Scored items: %d", report.ScoredItems)))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Total objective value: %d", report.TotalObjectiveValue)))

	if len(report.ObjectiveBuckets) > 0 {
		bucketRows := make([][]string, 0, min(len(report.ObjectiveBuckets), 8))
		for _, bucket := range report.ObjectiveBuckets[:min(len(report.ObjectiveBuckets), 8)] {
			bucketRows = append(bucketRows, []string{
				bucket.Label,
				strconv.Itoa(bucket.Count),
				strconv.Itoa(bucket.MappedValue),
				strconv.Itoa(bucket.ContributedValue),
			})
		}
		fmt.Fprint(os.Stderr, console.RenderTable(console.TableConfig{
			Title:   "Top objective buckets",
			Headers: []string{"Bucket", "Count", "Mapped Value", "Contributed Value"},
			Rows:    bucketRows,
		}))
	}

	if len(report.RepresentativeItems) > 0 {
		itemRows := make([][]string, 0, min(len(report.RepresentativeItems), 5))
		for _, item := range report.RepresentativeItems[:min(len(report.RepresentativeItems), 5)] {
			itemRows = append(itemRows, []string{
				fmt.Sprintf("#%d", item.Number),
				strconv.Itoa(item.ObjectiveValue),
				item.Title,
			})
		}
		fmt.Fprint(os.Stderr, console.RenderTable(console.TableConfig{
			Title:   "Representative items",
			Headers: []string{"Number", "Value", "Title"},
			Rows:    itemRows,
		}))
	}
}
