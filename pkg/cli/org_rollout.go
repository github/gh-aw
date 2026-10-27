package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/stringutil"
)

const orgRolloutResultPrefix = "gh-aw-rollout-result:"

type orgRolloutResult struct {
	Repo    string          `json:"repo,omitempty"`
	Status  string          `json:"status"`
	URL     string          `json:"url,omitempty"`
	Error   string          `json:"error,omitempty"`
	Preview *orgRepoPreview `json:"preview,omitempty"`
	Stop    bool            `json:"stop,omitempty"`
}

type orgRolloutRow struct {
	repo, status string
	prs          []orgRolloutPR
	selected     bool
}

type orgRolloutEvent struct {
	index  int
	result orgRolloutResult
	done   bool
}

func emitOrgRolloutResult(result orgRolloutResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encoding rollout result: %w", err)
	}
	if _, err := fmt.Fprintln(os.Stdout, orgRolloutResultPrefix+string(data)); err != nil {
		return fmt.Errorf("writing rollout result: %w", err)
	}
	return nil
}

func emitOrgRolloutPreview(preview orgRepoPreview, include bool) error {
	status := "unchanged"
	if include {
		status = "planned"
	}
	return emitOrgRolloutResult(orgRolloutResult{Status: status, Preview: &preview})
}

func runOrgRollout(ctx context.Context, org string, globs []string, cbs orgRunCallbacks, createPR, createIssue, verbose bool, opts orgRolloutOptions) error {
	if err := validateRepoGlobs(globs); err != nil {
		return err
	}
	if createPR && createIssue {
		return errors.New("--create-pull-request and --create-issue are mutually exclusive")
	}
	opts.dryRun = opts.dryRun || (!createPR && !createIssue)
	if !opts.dryRun && !opts.interactive && !cbs.AutoYes && isRunningInCIFn() {
		return errors.New("unattended org or enterprise creation requires --yes")
	}
	if opts.interactive && createIssue {
		return errors.New("--interactive supports PR rollouts and dry-run previews, not --create-issue")
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	orgs := []string{org}
	if opts.enterprise != "" {
		var err error
		orgs, err = discoverEnterpriseOrgs(ctx, opts.enterprise)
		if err != nil {
			return err
		}
	}
	if createIssue && !opts.dryRun {
		legacy := context.WithValue(ctx, orgRolloutKey{}, (*orgRolloutOptions)(nil))
		for _, organization := range orgs {
			if err := runCommandForOrg(legacy, organization, globs, cbs, false, true, verbose); err != nil {
				return err
			}
		}
		return nil
	}
	rows, err := discoverOrgRolloutRows(ctx, orgs, globs, cbs, opts, verbose)
	if err != nil || len(rows) == 0 {
		return err
	}
	cacheDir, err := newOrgRolloutCache()
	if err != nil {
		return err
	}
	opts.cacheDir = cacheDir
	defer func() {
		if err := os.RemoveAll(cacheDir); err != nil {
			fmt.Fprintln(os.Stderr, "Could not remove rollout API cache: "+err.Error())
		}
	}()
	var results []orgRolloutResult
	if opts.interactive {
		results, err = runOrgRolloutTUI(ctx, rows, opts)
	} else {
		results, err = runOrgRolloutPlain(ctx, rows, opts, cbs.AutoYes)
	}
	return reportOrgRolloutResults(ctx, results, cbs, opts, err)
}

func newOrgRolloutCache() (string, error) {
	root, err := gitutil.FindGitRoot()
	if err != nil {
		return "", fmt.Errorf("org and enterprise rollouts require a Git checkout: %w", err)
	}
	directory, err := ensureUpdateTargetRepoGitignore(root)
	if err != nil {
		return "", err
	}
	directory, err = os.MkdirTemp(directory, "rollout-api-cache-")
	if err != nil {
		return "", fmt.Errorf("creating shared rollout API cache: %w", err)
	}
	return directory, nil
}

func discoverOrgRolloutRows(ctx context.Context, orgs, globs []string, cbs orgRunCallbacks, opts orgRolloutOptions, verbose bool) ([]orgRolloutRow, error) {
	var repos []string
	for _, organization := range orgs {
		if !isValidOrgSlug(organization) {
			return nil, invalidOrgSlugError(organization)
		}
		fmt.Fprintln(os.Stderr, "Discovering repositories in "+organization+"...")
		found, err := cbs.SearchFn(ctx, organization, verbose)
		if err != nil {
			return nil, err
		}
		repos = append(repos, filterOrgRepos(found, globs)...)
	}
	slices.Sort(repos)
	repos = slices.Compact(repos)
	if len(repos) == 0 {
		fmt.Fprintln(os.Stderr, "No matching repositories found")
		return nil, nil
	}
	prs, err := discoverRolloutPRs(ctx, repos, rolloutMarker(opts.command))
	if err != nil {
		return nil, fmt.Errorf("discovering existing rollout PRs: %w", err)
	}
	rows := make([]orgRolloutRow, 0, len(repos))
	for _, repo := range repos {
		row := orgRolloutRow{repo: repo, status: "ready", selected: true, prs: prs[repo]}
		if len(row.prs) > 0 {
			row.status = "existing PR"
			row.selected = opts.replace
			if opts.replace {
				row.status = "replace existing PR"
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func runOrgRolloutPlain(ctx context.Context, rows []orgRolloutRow, opts orgRolloutOptions, autoYes bool) ([]orgRolloutResult, error) {
	var results []orgRolloutResult
	for index, value := range rows {
		row := value
		fmt.Fprintf(os.Stderr, "%s: %s\n", row.repo, row.status)
		for _, pr := range row.prs {
			fmt.Fprintf(os.Stderr, "  #%d %s\n", pr.Number, pr.URL)
		}
		if row.selected && !opts.dryRun && !autoYes {
			selected, err := confirmOrgAction(orgRepoPreview{Repo: row.repo}, "create a pull request", false)
			if err != nil {
				return nil, err
			}
			row.selected = selected
		}
		rows[index] = row
	}
	for event := range startOrgRolloutJobs(ctx, rows, opts, time.Second, runOrgRolloutWorker) {
		if event.done {
			break
		}
		result := event.result
		fmt.Fprintf(os.Stderr, "%s: %s %s\n", result.Repo, result.Status, result.URL)
		if result.Preview != nil && opts.command == "upgrade" {
			fmt.Fprintln(os.Stderr, "  compilers: "+formatUpgradeCompilerVersions(*result.Preview))
		}
		if result.Status != "running" {
			results = append(results, result)
		}
	}
	return results, nil
}

func reportOrgRolloutResults(ctx context.Context, results []orgRolloutResult, cbs orgRunCallbacks, opts orgRolloutOptions, runErr error) error {
	var previews []orgRepoPreview
	var failures []error
	if runErr != nil {
		failures = append(failures, runErr)
	}
	for _, result := range results {
		if result.Preview != nil && (result.Status == "planned" || opts.command == "upgrade") {
			previews = append(previews, *result.Preview)
		}
		if result.Error != "" {
			failures = append(failures, fmt.Errorf("%s: %s", result.Repo, result.Error))
		}
		if opts.interactive {
			fmt.Fprintf(os.Stderr, "%s: %s %s\n", result.Repo, result.Status, result.URL)
		}
	}
	if opts.dryRun || (opts.command == "upgrade" && len(previews) > 0) {
		cbs.ReportFn(previews, !opts.dryRun)
	}
	if ctx.Err() != nil {
		failures = append(failures, ctx.Err())
	}
	return errors.Join(failures...)
}

// Workers are separate processes: the existing update/upgrade pipeline uses
// os.Chdir and process-global compiler state. Goroutines must not run it directly.
func startOrgRolloutJobs(ctx context.Context, rows []orgRolloutRow, opts orgRolloutOptions, gap time.Duration,
	run func(context.Context, string, orgRolloutOptions) orgRolloutResult,
) <-chan orgRolloutEvent {
	rows = slices.Clone(rows)
	events := make(chan orgRolloutEvent)
	go func() {
		defer close(events)
		admission, stopAdmission := context.WithCancel(ctx)
		defer stopAdmission()
		var workers sync.WaitGroup
		defer workers.Wait()
		defer func() {
			if recovered := recover(); recovered != nil {
				events <- orgRolloutEvent{index: -1, result: orgRolloutResult{Status: "failed", Error: fmt.Sprintf("rollout scheduler panic: %v", recovered)}}
			}
		}()
		slots := make(chan struct{}, opts.concurrency)
		lastBudgetCheck := time.Time{}
		for index, row := range rows {
			if !row.selected {
				continue
			}
			if admission.Err() != nil {
				break
			}
			select {
			case slots <- struct{}{}:
			case <-admission.Done():
			}
			if admission.Err() != nil {
				break
			}
			if time.Since(lastBudgetCheck) >= 30*time.Second {
				if err := waitForOrgRateLimitFn(ctx, "core", false); err != nil {
					<-slots
					events <- orgRolloutEvent{index: index, result: orgRolloutResult{Repo: row.repo, Status: "failed", Error: err.Error()}}
					break
				}
				lastBudgetCheck = time.Now()
			}
			events <- orgRolloutEvent{index: index, result: orgRolloutResult{Repo: row.repo, Status: "running"}}
			workers.Go(func() {
				defer func() { <-slots }()
				result := run(ctx, row.repo, opts)
				result.Repo = row.repo
				if result.Stop {
					stopAdmission()
				}
				events <- orgRolloutEvent{index: index, result: result}
			})
			timer := time.NewTimer(gap)
			select {
			case <-admission.Done():
			case <-timer.C:
			}
			timer.Stop()
		}
		workers.Wait()
		events <- orgRolloutEvent{done: true}
	}()
	return events
}

type orgRolloutOutput struct {
	tail  []byte
	limit int
}

func (output *orgRolloutOutput) Write(data []byte) (int, error) {
	output.tail = append(output.tail, data...)
	limit := output.limit
	if limit == 0 {
		limit = 32768
	}
	if len(output.tail) > limit {
		output.tail = output.tail[len(output.tail)-limit:]
	}
	return len(data), nil
}

func runOrgRolloutWorker(ctx context.Context, repo string, opts orgRolloutOptions) orgRolloutResult {
	executable, err := os.Executable()
	if err != nil {
		return orgRolloutResult{Status: "failed", Error: err.Error()}
	}
	args := append(slices.Clone(opts.args), "--repo="+repo)
	if opts.dryRun {
		args = append(args, "--dry-run")
	} else {
		args = append(args, "--create-pull-request")
	}
	if opts.replace {
		args = append(args, "--replace-existing")
	}
	if opts.cacheDir != "" {
		args = append(args, "--rollout-cache-dir="+opts.cacheDir)
	}
	if len(opts.workflows) > 0 {
		args = append(args, "--")
		args = append(args, opts.workflows...)
	}
	cmd := exec.CommandContext(ctx, executable, args...)
	configureOrgRolloutProcess(cmd)
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "NO_COLOR=1", "GH_HOST="+getHostFromOriginRemote())
	output, diagnostic := &orgRolloutOutput{limit: 8 * 1024 * 1024}, &orgRolloutOutput{}
	cmd.Stdout, cmd.Stderr = output, diagnostic
	cmd.WaitDelay = 5 * time.Second
	err = cmd.Run()
	result := orgRolloutResult{}
	text := string(output.tail)
	if index := strings.LastIndex(text, orgRolloutResultPrefix); index >= 0 {
		line, _, _ := strings.Cut(text[index+len(orgRolloutResultPrefix):], "\n")
		if decodeErr := json.Unmarshal([]byte(line), &result); decodeErr != nil {
			return orgRolloutResult{Status: "failed", Error: fmt.Sprintf("invalid worker result: %v", decodeErr)}
		}
	} else {
		result = orgRolloutResult{Status: "failed", Error: "worker exited without a rollout result"}
	}
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("%v\n%s", err, stringutil.StripANSI(string(diagnostic.tail)))
		message := strings.ToLower(result.Error)
		result.Stop = strings.Contains(message, "http 429") || strings.Contains(message, "http 401") ||
			strings.Contains(message, "secondary rate limit") || strings.Contains(message, "api rate limit exceeded")
	}
	return result
}
