package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/spf13/cobra"
)

func newLogsEpisodeCommand() *cobra.Command {
	opts := EpisodeRetrievalOptions{
		StartDate: "-7d", MaxRuns: 100, MaxAPIRequests: 500,
		Timeout: 5 * time.Minute, OutputDir: defaultLogsOutputDir,
	}
	var repository string
	var jsonOutput bool
	var view string
	cmd := &cobra.Command{
		Use:   "episode <run-id-or-url>",
		Short: "Retrieve a canonical episode within explicit collection bounds",
		Long: `Retrieve compact info and usage artifacts for a seed run and related runs.
Membership requires the seed's recorded context.episode_id. Recorded parents and
roots are visited first, then runs in the time window are scanned for descendants.
Only the seed repository and explicitly allowed repositories on the same host are
queried. Missing evidence and collection limits are reported; coverage is never
claimed complete. The run cap includes nonmembers and failed candidate lookups.
The API cap counts every REST request, including artifact pages and downloads,
but not the subsequent transfer from GitHub's artifact storage redirect.
Reports are cached automatically. Use --view hops to inspect recorded invocations
from the same snapshot without another retrieval. Use --refresh to collect again;
immutable artifacts are reused. --json always returns the standard logs report.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateEpisodeView(view); err != nil {
				return err
			}
			outcomesDir := getStringFlag(cmd, "outcomes-dir")
			if outcomesDir != "" && !opts.Outcomes {
				return errors.New("--outcomes-dir requires --outcomes")
			}
			if len(args) > 0 {
				if err := resolveEpisodeSeed(&opts, args[0], repository); err != nil {
					return err
				}
			} else {
				return errors.New("expected one seed run ID or URL")
			}
			data, err := RetrieveEpisodeLogs(cmd.Context(), opts)
			if err != nil {
				return err
			}
			if opts.Outcomes {
				if err := collectEpisodeOutcomes(cmd.Context(), &data, outcomesDir, getBoolFlag(cmd, "verbose")); err != nil {
					return err
				}
			}
			return renderEpisodeLogs(cmd.OutOrStdout(), data, view, jsonOutput)
		},
	}

	addEpisodeRetrievalFlags(cmd, &opts, &repository, &jsonOutput)
	cmd.Flags().StringVar(&view, "view", "summary", "Episode view: summary or hops (JSON always includes both)")
	cmd.Flags().BoolVar(&opts.Refresh, "refresh", false, "Refresh the cached episode snapshot using the current collection bounds")
	cmd.Flags().BoolVar(&opts.Outcomes, "outcomes", false, "Evaluate current outcomes for episode members (additional live API requests beyond retrieval bounds)")
	cmd.Flags().String("outcomes-dir", "", "Write outcome JSONL observations to this directory (requires --outcomes)")
	return cmd
}

func renderEpisodeLogs(w io.Writer, data LogsData, view string, jsonOutput bool) error {
	if err := renderEpisodeView(w, data, view, jsonOutput); err != nil {
		return err
	}
	if !jsonOutput {
		return renderLogsOutcomes(w, data)
	}
	return nil
}

func addEpisodeRetrievalFlags(cmd *cobra.Command, opts *EpisodeRetrievalOptions, repository *string, jsonOutput *bool) {
	flags := cmd.Flags()
	flags.StringVarP(repository, "repo", "r", "", "Seed repository: [HOST/]owner/repo (required for a numeric run ID)")
	flags.StringSliceVar(&opts.AllowRepositories, "allow-repo", nil, "Additional owner/repo allowed for retrieval on the seed host (repeatable)")
	flags.StringVar(&opts.StartDate, "start-date", opts.StartDate, "Earliest candidate creation date (YYYY-MM-DD, RFC3339, or relative date)")
	flags.StringVar(&opts.EndDate, "end-date", "", "Latest candidate creation date (defaults to now)")
	flags.IntVar(&opts.MaxRuns, "max-runs", opts.MaxRuns, "Maximum distinct candidate runs examined, including the seed and nonmembers")
	flags.IntVar(&opts.MaxAPIRequests, "max-api-requests", opts.MaxAPIRequests, "Maximum GitHub REST requests for the entire retrieval")
	flags.DurationVar(&opts.Timeout, "timeout", opts.Timeout, "Wall-clock retrieval deadline")
	flags.StringVarP(&opts.OutputDir, "output", "o", opts.OutputDir, "Directory for compact artifacts")
	flags.BoolVarP(jsonOutput, "json", "j", false, "Output the standard logs report with retrieval bounds and gaps as JSON")
}

func renderEpisodeRetrieval(w io.Writer, data LogsData) error {
	if err := renderLogsCompactToWriter(w, data); err != nil {
		return err
	}
	retrieval := data.EpisodeRetrieval
	if retrieval == nil {
		return errors.New("episode retrieval report is missing its collection bounds")
	}
	if _, err := fmt.Fprintf(w, "\nEpisode %s: %d selected runs; %d/%d candidates examined; %d/%d REST requests.\nRepositories: %s\nWindow: %s through %s\n%s\n",
		retrieval.EpisodeID, len(data.Runs), retrieval.RunsExamined, retrieval.MaxRuns, retrieval.APIRequests, retrieval.MaxAPIRequests,
		strings.Join(retrieval.Repositories, ", "), retrieval.StartDate.Format(time.RFC3339), retrieval.EndDate.Format(time.RFC3339), data.Message); err != nil {
		return fmt.Errorf("write episode retrieval bounds: %w", err)
	}
	if len(retrieval.StopReasons) > 0 {
		if _, err := fmt.Fprintln(w, "Collection stopped: "+strings.Join(retrieval.StopReasons, ", ")); err != nil {
			return fmt.Errorf("write episode retrieval limits: %w", err)
		}
	}
	for _, issue := range retrieval.Issues {
		if _, err := fmt.Fprintf(w, "%s run %d: %s\n", issue.Repository, issue.RunID, issue.Message); err != nil {
			return fmt.Errorf("write episode retrieval gap: %w", err)
		}
	}
	return nil
}

func resolveEpisodeSeed(opts *EpisodeRetrievalOptions, seed, repository string) error {
	components, err := parser.ParseRunURLExtended(seed)
	if err != nil || components.Number <= 0 {
		return fmt.Errorf("invalid seed run %q: use a positive run ID or a full GitHub Actions run URL", seed)
	}
	opts.SeedRunID = components.Number
	if components.Owner != "" {
		urlRepository := fmt.Sprintf("%s/%s", components.Owner, components.Repo)
		fromURL := fmt.Sprintf("%s/%s", components.Host, urlRepository)
		requested, requestedHost := repoutil.NormalizeRepoForAPI(repository)
		if repository != "" && (!strings.EqualFold(requested, urlRepository) || requestedHost != "" && !strings.EqualFold(requestedHost, components.Host)) {
			return fmt.Errorf("--repo %q conflicts with seed repository %q", repository, fromURL)
		}
		repository = fromURL
	}
	if repository == "" {
		return errors.New("pass --repo [HOST/]owner/repo for a numeric seed run ID, or provide a full run URL")
	}
	opts.Repository = repository
	return nil
}
