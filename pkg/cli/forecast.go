package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var forecastRunLog = logger.New("cli:forecast_run")

// forecastPeriodDays maps period names to the number of days in a projection window.
var forecastPeriodDays = map[string]int{
	"week":  7,
	"month": 30,
}

type forecastWindow struct {
	now                 time.Time
	start               time.Time
	anchor              time.Time
	validationStartDate string
	validationEndDate   string
}

// RunForecast is the entry point for the forecast command.
func RunForecast(config ForecastConfig) error {
	forecastRunLog.Printf("Running forecast: workflows=%v, days=%d, period=%s, eval=%v", config.WorkflowIDs, config.Days, config.Period, config.EvalMode)
	periodDays, err := validateForecastConfig(&config)
	if err != nil {
		return err
	}
	ctx, cleanup := newForecastContext(config)
	defer cleanup()
	workflowIDs, targetByID, err := prepareForecastInputs(ctx, &config)
	if err != nil {
		return normalizeForecastRunError(err, config)
	}
	if len(workflowIDs) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("No agentic workflows found to forecast"))
		return nil
	}

	window := newForecastWindow(config, periodDays)
	printForecastStart(config, window, len(workflowIDs))
	results, sampleTruncated, err := processForecastWorkflows(ctx, workflowIDs, targetByID, config, window, periodDays)
	if err != nil {
		return err
	}
	sortForecastResults(results)
	output := buildForecastOutput(results, config, window, sampleTruncated)
	if config.JSONOutput {
		return renderForecastJSON(output)
	}
	return renderForecastTable(output, config)
}

func validateForecastConfig(config *ForecastConfig) (int, error) {
	if config.TimeoutMinutes < 0 {
		return 0, fmt.Errorf("invalid timeout value: %d; must be >= 0", config.TimeoutMinutes)
	}
	periodDays, ok := forecastPeriodDays[config.Period]
	if !ok {
		return 0, fmt.Errorf("invalid period %q: must be 'week' or 'month'", config.Period)
	}
	if config.Days != 7 && config.Days != 30 {
		return 0, fmt.Errorf("invalid days value: %d; must be 7 or 30", config.Days)
	}
	if config.SampleSize <= 0 {
		config.SampleSize = 100
	}
	return periodDays, nil
}

func newForecastContext(config ForecastConfig) (context.Context, context.CancelFunc) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	if config.TimeoutMinutes == 0 {
		return ctx, stop
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(config.TimeoutMinutes)*time.Minute)
	return timeoutCtx, func() {
		cancel()
		stop()
	}
}

func prepareForecastInputs(ctx context.Context, config *ForecastConfig) ([]string, map[string]forecastWorkflowTarget, error) {
	if len(config.LogsJSONL) == 0 {
		ids, err := resolveForecastWorkflows(ctx, *config)
		return ids, nil, err
	}
	history, err := loadForecastJSONLHistory(config.LogsJSONL)
	if err != nil {
		return nil, nil, err
	}
	config.history = history
	targets, err := history.targets(config.WorkflowIDs, config.RepoOverride)
	if err != nil {
		return nil, nil, err
	}
	ids := make([]string, 0, len(targets))
	targetByID := make(map[string]forecastWorkflowTarget, len(targets))
	for index, target := range targets {
		key := fmt.Sprintf("jsonl:%d", index)
		ids = append(ids, key)
		targetByID[key] = target
	}
	return ids, targetByID, nil
}

func newForecastWindow(config ForecastConfig, periodDays int) forecastWindow {
	window := forecastWindow{now: time.Now()}
	window.start = window.now.AddDate(0, 0, -config.Days)
	if !config.EvalMode {
		return window
	}
	window.anchor = window.now.AddDate(0, 0, -periodDays)
	window.start = window.anchor.AddDate(0, 0, -config.Days)
	window.validationStartDate = window.anchor.Format("2006-01-02")
	window.validationEndDate = window.now.Format("2006-01-02")
	return window
}

func printForecastStart(config ForecastConfig, window forecastWindow, workflowCount int) {
	if config.EvalMode {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf(
			"Eval mode: training window ends %s; validation window %s → %s",
			window.anchor.Format("2006-01-02"), window.validationStartDate, window.validationEndDate)))
	}
	if !config.Verbose && !config.JSONOutput {
		label := fmt.Sprintf("Forecasting %d workflow(s) using %d-day history → projecting per %s",
			workflowCount, config.Days, config.Period)
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(label))
	}
}

func processForecastWorkflows(ctx context.Context, ids []string, targets map[string]forecastWorkflowTarget, config ForecastConfig, window forecastWindow, periodDays int) ([]ForecastWorkflowResult, bool, error) {
	spinner := console.NewSpinner("Sampling workflow run history…")
	if !config.Verbose {
		spinner.Start()
		defer spinner.Stop()
	}
	results := make([]ForecastWorkflowResult, 0, len(ids))
	sampleTruncated := false
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			emitPartialForecastResults(results, config, window.now)
			return nil, sampleTruncated, normalizeForecastRunError(err, config)
		}
		if !config.Verbose {
			spinner.UpdateMessage(fmt.Sprintf("Sampling %s…", id))
		}
		result, truncated, err := forecastOneWorkflow(ctx, id, targets[id], config, window, periodDays)
		sampleTruncated = sampleTruncated || truncated
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				emitPartialForecastResults(results, config, window.now)
				return nil, sampleTruncated, normalizeForecastRunError(err, config)
			}
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Skipping %s: %v", id, err)))
			continue
		}
		results = append(results, result)
	}
	return results, sampleTruncated, nil
}

func forecastOneWorkflow(ctx context.Context, id string, target forecastWorkflowTarget, config ForecastConfig, window forecastWindow, periodDays int) (ForecastWorkflowResult, bool, error) {
	if config.history != nil {
		end := time.Time{}
		if config.EvalMode {
			end = window.anchor
		}
		return forecastWorkflowFromJSONL(ctx, target, window.start, end, config, periodDays)
	}
	result, err := forecastWorkflow(ctx, id, window.start.Format("2006-01-02"), config, periodDays)
	if err == nil && config.EvalMode {
		result.Evaluation = evaluateForecast(ctx, id, result, window.validationStartDate, window.validationEndDate, config)
	}
	return result, false, err
}

func sortForecastResults(results []ForecastWorkflowResult) {
	slices.SortFunc(results, func(a, b ForecastWorkflowResult) int {
		left, right := a.ProjectedAIC, b.ProjectedAIC
		if a.MonteCarlo != nil {
			left = a.MonteCarlo.P50ProjectedAIC
		}
		if b.MonteCarlo != nil {
			right = b.MonteCarlo.P50ProjectedAIC
		}
		return -cmpFloat64(left, right)
	})
}

func cmpFloat64(left, right float64) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func buildForecastOutput(results []ForecastWorkflowResult, config ForecastConfig, window forecastWindow, sampleTruncated bool) ForecastResult {
	provenance := ForecastHistoryProvenance{Source: "github_api"}
	if config.history != nil {
		provenance = config.history.provenance(window.start, sampleTruncated)
	}
	return ForecastResult{
		Period:         config.Period,
		AsOf:           window.now.UTC().Format(time.RFC3339),
		EvalMode:       config.EvalMode,
		History:        provenance,
		WorkflowRunAPI: aggregateWorkflowRunAPI(results),
		Workflows:      results,
	}
}

func normalizeForecastRunError(err error, config ForecastConfig) error {
	if config.TimeoutMinutes > 0 && errors.Is(err, context.DeadlineExceeded) {
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(
			fmt.Sprintf("Forecast computation timed out after %d minute(s).", config.TimeoutMinutes),
		))
		return &ExitCodeError{Code: 124}
	}
	return err
}
