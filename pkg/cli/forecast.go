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

// RunForecast is the entry point for the forecast command.
func RunForecast(config ForecastConfig) error {
	forecastRunLog.Printf("Running forecast: workflows=%v, days=%d, period=%s, eval=%v", config.WorkflowIDs, config.Days, config.Period, config.EvalMode)
	if config.TimeoutMinutes < 0 {
		return fmt.Errorf("invalid timeout value: %d; must be >= 0", config.TimeoutMinutes)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if config.TimeoutMinutes > 0 {
		timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(config.TimeoutMinutes)*time.Minute)
		defer cancel()
		ctx = timeoutCtx
	}

	// Validate period.
	periodDays, ok := forecastPeriodDays[config.Period]
	if !ok {
		return fmt.Errorf("invalid period %q: must be 'week' or 'month'", config.Period)
	}
	if config.Days != 7 && config.Days != 30 {
		return fmt.Errorf("invalid days value: %d; must be 7 or 30", config.Days)
	}
	if config.SampleSize <= 0 {
		config.SampleSize = 100
	}

	// Resolve the list of workflow IDs to forecast.
	workflowIDs, err := resolveForecastWorkflows(ctx, config)
	if err != nil {
		return normalizeForecastRunError(err, config)
	}
	if len(workflowIDs) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("No agentic workflows found to forecast"))
		return nil
	}

	now := time.Now()

	// In eval mode, shift the entire date range back by one period so we can
	// compare the forecast against the actual runs in the most recent period.
	//
	//  ┌──────────────────────────────────────────────────────────────────┐
	//  │  [anchor - days ... anchor]  training  │  [anchor ... now]  val  │
	//  └──────────────────────────────────────────────────────────────────┘
	//   anchor = now - periodDays
	//
	// Normal mode: startDate = now - days (no anchor shift).
	var anchor time.Time
	var validationStartDate, validationEndDate string
	if config.EvalMode {
		anchor = now.AddDate(0, 0, -periodDays)
		validationStartDate = anchor.Format("2006-01-02")
		validationEndDate = now.Format("2006-01-02")
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(
			fmt.Sprintf("Eval mode: training window ends %s; validation window %s → %s",
				anchor.Format("2006-01-02"), validationStartDate, validationEndDate)))
	}

	startDate := now.AddDate(0, 0, -config.Days).Format("2006-01-02")
	if config.EvalMode {
		// Training window ends at the anchor, not now.
		startDate = anchor.AddDate(0, 0, -config.Days).Format("2006-01-02")
	}

	if !config.Verbose && !config.JSONOutput {
		label := fmt.Sprintf("Forecasting %d workflow(s) using %d-day history → projecting per %s",
			len(workflowIDs), config.Days, config.Period)
		fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage(label))
	}

	spinner := console.NewSpinner("Sampling workflow run history…")
	if !config.Verbose {
		spinner.Start()
	}

	results := make([]ForecastWorkflowResult, 0, len(workflowIDs))
	for _, wfID := range workflowIDs {
		if err := ctx.Err(); err != nil {
			if !config.Verbose {
				spinner.Stop()
			}
			emitPartialForecastResults(results, config, now)
			return normalizeForecastRunError(err, config)
		}
		if !config.Verbose {
			spinner.UpdateMessage(fmt.Sprintf("Sampling %s…", wfID))
		}

		// forecastWorkflow uses the shifted startDate; in eval mode we also pass the
		// anchor so the function knows where the training window ends.
		result, err := forecastWorkflow(ctx, wfID, startDate, config, periodDays)
		if err != nil {
			// context.Canceled typically indicates user interruption (Ctrl-C), while
			// context.DeadlineExceeded indicates the configured forecast timeout.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				if !config.Verbose {
					spinner.Stop()
				}
				emitPartialForecastResults(results, config, now)
				return normalizeForecastRunError(err, config)
			}
			if !config.Verbose {
				spinner.Stop()
			}
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
				fmt.Sprintf("Skipping %s: %v", wfID, err)))
			if !config.Verbose {
				spinner.Start()
			}
			continue
		}

		// In eval mode, fetch the validation-window runs and attach evaluation metrics.
		if config.EvalMode {
			result.Evaluation = evaluateForecast(ctx, wfID, result, validationStartDate, validationEndDate, config)
		}

		results = append(results, result)
	}

	if !config.Verbose {
		spinner.Stop()
	}

	// Sort results by Monte Carlo P50 (or point estimate when MC unavailable) descending.
	slices.SortFunc(results, func(a, b ForecastWorkflowResult) int {
		pi := a.ProjectedAIC
		if mc := a.MonteCarlo; mc != nil {
			pi = mc.P50ProjectedAIC
		}
		pj := b.ProjectedAIC
		if mc := b.MonteCarlo; mc != nil {
			pj = mc.P50ProjectedAIC
		}
		if pi > pj {
			return -1
		}
		if pi < pj {
			return 1
		}
		return 0
	})

	output := ForecastResult{
		Period:    config.Period,
		AsOf:      now.UTC().Format(time.RFC3339),
		EvalMode:  config.EvalMode,
		Workflows: results,
	}

	if config.JSONOutput {
		return renderForecastJSON(output)
	}
	return renderForecastTable(output, config)
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
