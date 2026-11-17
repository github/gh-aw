package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/github/gh-aw/pkg/workflow"
)

type lifecycleAPIBudgetKey struct{}
type lifecycleLogsBudgetKey struct{}
type lifecycleNetworkContextKey struct{}

func lifecycleLogsContext(ctx context.Context, minutes, seconds, maxRequests int) (context.Context, context.CancelFunc) {
	active, cancel, _, _ := buildLogsDownloadContext(ctx, minutes, seconds, false)
	if cancel != nil {
		ctx = context.WithValue(ctx, lifecycleNetworkContextKey{}, active)
	}
	if maxRequests != 0 {
		ctx = context.WithValue(ctx, lifecycleLogsBudgetKey{}, maxRequests)
	}
	return ctx, cancel
}

type lifecycleAPIBudget struct {
	mu     sync.Mutex
	report *EpisodeRetrieval
}

func (b *lifecycleAPIBudget) reserve(repository string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	repo, host := repoutil.NormalizeRepoForAPI(repository)
	if host == "" {
		host = "github.com"
	}
	if !strings.EqualFold(host, b.report.Host) || !slices.Contains(b.report.Repositories, strings.ToLower(repo)) {
		return errors.New("adoption target is outside the episode's allowed repositories")
	}
	if b.report.APIRequests >= b.report.MaxAPIRequests {
		if !slices.Contains(b.report.StopReasons, "api_request_limit") {
			b.report.StopReasons = append(b.report.StopReasons, "api_request_limit")
		}
		return errEpisodeAPILimit
	}
	b.report.APIRequests++
	return nil
}

func requestLifecycleAPI(ctx context.Context, repository string, args []string) ([]byte, error) {
	budget, bounded := ctx.Value(lifecycleAPIBudgetKey{}).(*lifecycleAPIBudget)
	maxRequests, boundedLogs := ctx.Value(lifecycleLogsBudgetKey{}).(int)
	apiCtx := ctx
	if deadline, ok := ctx.Value(lifecycleNetworkContextKey{}).(context.Context); ok {
		apiCtx = deadline
	}
	fetch := func(arguments []string) ([]byte, error) {
		if err := apiCtx.Err(); err != nil {
			return nil, err
		}
		if bounded {
			if err := budget.reserve(repository); err != nil {
				return nil, err
			}
		}
		if boundedLogs {
			quota, err := fetchRateLimitForHostFunc(apiCtx, outcomeAPIHostForRepo(repository))
			if err != nil {
				return nil, fmt.Errorf("check adoption API quota: %w", err)
			}
			maximum, err := resolveMaxGitHubAPIRateLimit(maxRequests, quota.Limit)
			if err != nil {
				return nil, err
			}
			if quota.Used+1 > maximum {
				return nil, stopForLogsRateLimit(quota, maximum, time.Unix(quota.Reset, 0))
			}
		}
		return workflow.RunGHContext(apiCtx, "Checking adoption...", arguments...)
	}
	if (!bounded && !boundedLogs) || !slices.Contains(args, "--paginate") {
		return fetch(args)
	}
	return requestLifecyclePages(args, fetch)
}

func requestLifecyclePages(args []string, fetch func([]string) ([]byte, error)) ([]byte, error) {
	// Each page must reserve a request; gh's automatic pagination hides this cost.
	var singlePage []string
	for _, arg := range args {
		if arg != "--paginate" && arg != "--slurp" {
			singlePage = append(singlePage, arg)
		}
	}
	if len(singlePage) < 2 {
		return nil, errors.New("bounded adoption pagination requires a REST resource")
	}
	if singlePage[0] != "api" || singlePage[1] == "graphql" { //nolint:uncheckedsliceindex // The length guard above requires both arguments.
		return nil, errors.New("bounded adoption pagination requires a REST resource")
	}
	target, err := url.Parse(singlePage[1]) //nolint:uncheckedsliceindex // The REST argument pair was validated above.
	if err != nil {
		return nil, err
	}
	var pages []json.RawMessage
	for page := 1; ; page++ {
		query := target.Query()
		query.Set("page", strconv.Itoa(page))
		query.Set("per_page", "100")
		target.RawQuery = query.Encode()
		singlePage[1] = target.String() //nolint:uncheckedsliceindex // The REST argument pair was validated above.
		body, err := fetch(singlePage)
		if err != nil {
			return nil, err
		}
		var rows []json.RawMessage
		if err := json.Unmarshal(body, &rows); err != nil {
			return nil, fmt.Errorf("decode bounded adoption page: %w", err)
		}
		pages = append(pages, body)
		if len(rows) < 100 {
			return json.Marshal(pages)
		}
	}
}
