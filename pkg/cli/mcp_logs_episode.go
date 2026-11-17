package cli

import (
	"context"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

func validateMCPEpisodeLogsArgs(args logsArgs) error {
	if args.EpisodeRun == "" {
		if args.EpisodeRepository != "" || len(args.AllowRepositories) > 0 || args.MaxAPIRequests != 0 || args.Refresh {
			return errors.New("episode_repository, allow_repositories, max_api_requests, and refresh require episode_run")
		}
		return nil
	}
	if args.WorkflowName != "" || args.Engine != "" || args.Runtime != "" || args.Branch != "" ||
		args.Firewall || args.NoFirewall || args.FilteredIntegrity || args.Graders ||
		args.AfterRunID != 0 || args.BeforeRunID != 0 || len(args.IgnoreWorkflowRuns) > 0 ||
		args.MaxGitHubAPIRateLimit != 0 || args.MaxStorageMB != nil || args.PruneOlderRuns {
		return errors.New("episode_run supports only repository allowlists, dates, count, timeout, max_api_requests, delivery, adoption, and compact info/usage artifacts; workflow discovery filters are not supported")
	}
	if args.Count < 0 || args.MaxAPIRequests < 0 || args.Timeout < 0 {
		return errors.New("episode retrieval count, max_api_requests, and timeout must be positive when specified")
	}
	if slices.ContainsFunc(args.Artifacts, func(set string) bool { return set != "info" && set != "usage" }) {
		return errors.New("episode retrieval supports only compact info and usage artifacts")
	}
	return nil
}

func buildEpisodeLogsCommandArgs(ctx context.Context, args logsArgs) ([]string, int, int) {
	count := effectiveMCPLogsToolCount(args.Count)
	timeout := effectiveMCPLogsToolTimeoutMinutes(args.Timeout, count, "", "")
	apiRequests := args.MaxAPIRequests
	if apiRequests == 0 {
		apiRequests = 500
	}
	cmdArgs := []string{
		"logs", "episode", "--json", "--output", constants.TmpAwMcpLogsDir,
		"--max-runs", strconv.Itoa(count), "--max-api-requests", strconv.Itoa(apiRequests),
		"--timeout", strconv.Itoa(timeout) + "m",
	}
	if seconds, ok := effectiveMCPLogsToolSoftTimeoutSeconds(ctx, timeout); ok {
		cmdArgs = append(cmdArgs, "--timeout", strconv.Itoa(seconds)+"s")
	}
	if args.Refresh {
		cmdArgs = append(cmdArgs, "--refresh")
	}
	if args.Delivery {
		cmdArgs = append(cmdArgs, "--delivery")
	}
	if args.Adoption {
		cmdArgs = append(cmdArgs, "--adoption")
	}
	repository := args.EpisodeRepository
	if repository == "" && !strings.Contains(args.EpisodeRun, "/") {
		repository = os.Getenv("GITHUB_REPOSITORY") //nolint:osgetenvlibrary
	}
	if repository != "" {
		cmdArgs = append(cmdArgs, "--repo", repository)
	}
	for _, allowed := range args.AllowRepositories {
		cmdArgs = append(cmdArgs, "--allow-repo", allowed)
	}
	if args.StartDate != "" {
		cmdArgs = append(cmdArgs, "--start-date", args.StartDate)
	}
	if args.EndDate != "" {
		cmdArgs = append(cmdArgs, "--end-date", args.EndDate)
	}
	cmdArgs = append(cmdArgs, "--", args.EpisodeRun)
	return cmdArgs, count, timeout
}
