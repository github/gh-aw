package cli

import (
	"encoding/json"
	"fmt"
	"math"
	"os"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var forecastRenderLog = logger.New("cli:forecast_render")

// renderForecastJSON outputs the forecast result as pretty-printed JSON.
func renderForecastJSON(output ForecastResult) error {
	forecastRenderLog.Printf("Rendering forecast as JSON: workflows=%d, eval_mode=%v", len(output.Workflows), output.EvalMode)
	b, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal forecast JSON: %w", err)
	}
	fmt.Fprintln(os.Stdout, string(b))
	return nil
}

// renderForecastTable renders the forecast result as a human-readable table.
func renderForecastTable(output ForecastResult, config ForecastConfig) error {
	forecastRenderLog.Printf("Rendering forecast table: workflows=%d, days=%d, eval_mode=%v", len(output.Workflows), config.Days, output.EvalMode)

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(
		fmt.Sprintf("Workflow Forecast — weekly & monthly projections (based on last %d days of history)", config.Days)))
	fmt.Fprintln(os.Stderr, "")

	anyUnreliable := false
	var totalWeeklyP50, totalMonthlyP50 float64
	var totalRuns int
	rows := make([]forecastTableRow, 0, len(output.Workflows)+1)
	for _, wf := range output.Workflows {
		unreliableMark := ""

		weeklyP50 := wf.WeeklyProjectedAIC
		if mc := wf.WeeklyMonteCarlo; mc != nil {
			weeklyP50 = mc.P50ProjectedAIC
			if !mc.IsReliable {
				anyUnreliable = true
				unreliableMark = "*"
			}
		}
		monthlyP50 := wf.MonthlyProjectedAIC
		if mc := wf.MonthlyMonteCarlo; mc != nil {
			monthlyP50 = mc.P50ProjectedAIC
		}
		// Skip NaN/Inf so a single unreliable projection can't poison the totals.
		if !math.IsNaN(weeklyP50) && !math.IsInf(weeklyP50, 0) {
			totalWeeklyP50 += weeklyP50
		}
		if !math.IsNaN(monthlyP50) && !math.IsInf(monthlyP50, 0) {
			totalMonthlyP50 += monthlyP50
		}
		totalRuns += wf.SampledRuns

		row := forecastTableRow{
			Workflow:    wf.WorkflowID + unreliableMark,
			Runs:        wf.SampledRuns,
			P50PerRun:   formatForecastAIC(wf.P50AIC),
			P95PerRun:   formatForecastAIC(wf.P95AIC),
			WeeklyP50:   formatForecastAIC(weeklyP50),
			MonthlyP50:  formatForecastAIC(monthlyP50),
			SuccessRate: formatForecastPercent(wf.SuccessRate, wf.SampledRuns > 0),
		}
		rows = append(rows, row)
	}

	forecastRenderLog.Printf("Forecast aggregates: total_weekly_p50=%.3f, total_monthly_p50=%.3f, any_unreliable=%v", totalWeeklyP50, totalMonthlyP50, anyUnreliable)

	// Append a totals row when more than one workflow is present.
	if len(output.Workflows) > 1 {
		rows = append(rows, forecastTableRow{
			Workflow:   "TOTAL",
			Runs:       totalRuns,
			WeeklyP50:  formatForecastAIC(totalWeeklyP50),
			MonthlyP50: formatForecastAIC(totalMonthlyP50),
		})
	}

	fmt.Fprint(os.Stderr, console.RenderStruct(rows))
	fmt.Fprintln(os.Stderr, "")

	// Show detailed per-run samples section only when specific workflows were requested.
	if len(config.WorkflowIDs) > 0 {
		printRunSamplesSection(output.Workflows)
	}

	// Show experiment variant details when present.
	for _, wf := range output.Workflows {
		if len(wf.ExperimentVariants) > 0 {
			printVariantBreakdown(wf)
		}
	}

	// Show backtesting evaluation table in --eval mode.
	if output.EvalMode {
		printEvalBreakdown(output.Workflows)
	}

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(
		"Cost/projection figures are AI Credits (AIC) — the gh-aw cost metric."))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(
		"AIC = AI Credits. P50 AIC/Run = per-run median AIC; P95 AIC/Run = 95th-percentile per-run AIC; Weekly/Monthly AIC = projected P50 usage."))
	if anyUnreliable {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
			fmt.Sprintf("* Fewer than %d sampled runs — projections may be unreliable.", minObservationsForReliableForecast)))
	}
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
		"All forecasts are estimates derived from historical samples and may be inaccurate."))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(
		fmt.Sprintf("Run '%s forecast --json' for full output including P10/P90 confidence intervals.", string(constants.CLIExtensionPrefix))))
	return nil
}

// printRunSamplesSection prints a detailed table of the sampled runs used in the forecast,
// including the run ID, date, and raw AIC for each run.  Workflows with no samples are skipped.
func printRunSamplesSection(workflows []ForecastWorkflowResult) {
	type runRow struct {
		RunID string `json:"run_id" console:"header:Run ID"`
		Date  string `json:"date"   console:"header:Date"`
		AIC   string `json:"aic"    console:"header:AIC"`
	}

	hasSamples := false
	for _, wf := range workflows {
		if len(wf.RunSamples) > 0 {
			hasSamples = true
			break
		}
	}
	if !hasSamples {
		return
	}

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Sampled runs used in computation:"))
	for _, wf := range workflows {
		if len(wf.RunSamples) == 0 {
			continue
		}
		fmt.Fprintf(os.Stderr, "  %s (%d run(s)):\n", wf.WorkflowID, len(wf.RunSamples))
		rows := make([]runRow, 0, len(wf.RunSamples))
		for _, s := range wf.RunSamples {
			rows = append(rows, runRow{
				RunID: fmt.Sprintf("#%d", s.RunID),
				Date:  s.Date,
				AIC:   formatForecastAIC(s.AIC),
			})
		}
		fmt.Fprint(os.Stderr, console.RenderStruct(rows))
		fmt.Fprintln(os.Stderr, "")
	}
}

// printEvalBreakdown renders the backtesting comparison table.
func printEvalBreakdown(workflows []ForecastWorkflowResult) {
	type evalRow struct {
		Workflow    string `json:"workflow"       console:"header:Workflow"`
		ActualRuns  int    `json:"actual_runs"    console:"header:Actual Runs"`
		ActualAIC   string `json:"actual_aic"     console:"header:Actual AIC"`
		ForecastP50 string `json:"forecast_p50"   console:"header:Forecast P50"`
		ErrorAbs    string `json:"error_abs"      console:"header:Error (abs)"`
		ErrorPct    string `json:"error_pct"      console:"header:Error %"`
		InCI        string `json:"in_ci"          console:"header:In 80% CI?"`
	}

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Backtesting evaluation (actual vs forecasted):"))
	var rows []evalRow
	for _, wf := range workflows {
		ev := wf.Evaluation
		if ev == nil {
			continue
		}
		p50 := wf.ProjectedAIC
		if mc := wf.MonteCarlo; mc != nil {
			p50 = mc.P50ProjectedAIC
		}
		inCI := "No"
		if ev.InCI {
			inCI = "Yes ✓"
		}
		rows = append(rows, evalRow{
			Workflow:    wf.WorkflowID,
			ActualRuns:  ev.ActualRuns,
			ActualAIC:   formatForecastAIC(ev.ActualAIC),
			ForecastP50: formatForecastAIC(p50),
			ErrorAbs:    formatForecastSignedAIC(ev.P50ErrorAbs),
			ErrorPct:    fmt.Sprintf("%.1f%%", ev.P50ErrorPct),
			InCI:        inCI,
		})
	}
	fmt.Fprint(os.Stderr, console.RenderStruct(rows))
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(
		"Training window ended at the forecast anchor; validation window is the following projection period."))
}

func printVariantBreakdown(wf ForecastWorkflowResult) {
	type variantRow struct {
		Experiment string `json:"experiment" console:"header:Experiment"`
		Variant    string `json:"variant"    console:"header:Variant"`
		Runs       int    `json:"runs"       console:"header:Runs"`
		Fraction   string `json:"fraction"   console:"header:Fraction"`
	}

	fmt.Fprintf(os.Stderr, "  Experiment variants for %s:\n", wf.WorkflowID)
	varRows := make([]variantRow, 0, len(wf.ExperimentVariants))
	for _, v := range wf.ExperimentVariants {
		varRows = append(varRows, variantRow{
			Experiment: v.ExperimentName,
			Variant:    v.Variant,
			Runs:       v.RunCount,
			Fraction:   formatForecastPercent(v.Fraction, wf.SampledRuns > 0),
		})
	}
	fmt.Fprint(os.Stderr, console.RenderStruct(varRows))
	fmt.Fprintln(os.Stderr, "")
}

// ── Format helpers ───────────────────────────────────────────────────────────

// formatForecastPercent formats v as a percentage string.
// hasData must be false when the underlying sample is empty (no runs), in which
// case "N/A" is returned; otherwise the value (including 0%) is formatted.
func formatForecastPercent(v float64, hasData bool) string {
	if !hasData {
		return "N/A"
	}
	return fmt.Sprintf("%.0f%%", v*100)
}

func formatForecastAIC(value float64) string {
	if value <= 0 {
		return "-"
	}
	if value < 1 {
		return fmt.Sprintf("%.3f", value)
	}
	if value < 10 {
		return fmt.Sprintf("%.2f", value)
	}
	if value < 1000 {
		return fmt.Sprintf("%.0f", value)
	}
	if value < 1_000_000 {
		return fmt.Sprintf("%.1fK", value/1000)
	}
	return fmt.Sprintf("%.2fM", value/1_000_000)
}

// formatForecastSignedAIC formats a signed AIC value, preserving
// the sign so callers can display positive/negative deltas (e.g., error abs).
func formatForecastSignedAIC(value float64) string {
	if value == 0 {
		return "0"
	}
	sign := ""
	v := value
	if value < 0 {
		sign = "-"
		v = math.Abs(value)
	}
	return sign + formatForecastAIC(v)
}

func roundForecastAIC(value float64) float64 {
	return math.Round(value*1000) / 1000
}
