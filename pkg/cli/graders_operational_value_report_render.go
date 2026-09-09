package cli

import (
	"encoding/json"
	"fmt"
	"html"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/constants"
)

type operationalValueReportArtifactPaths struct {
	JSON     string `json:"json"`
	SVG      string `json:"svg"`
	Markdown string `json:"markdown"`
}

func writeOperationalValueReportArtifacts(report operationalValueReport, outputDir string) (operationalValueReportArtifactPaths, error) {
	if err := os.MkdirAll(outputDir, constants.DirPermPublic); err != nil {
		return operationalValueReportArtifactPaths{}, fmt.Errorf("cannot create operational-value report directory: %w", err)
	}
	base := report.WorkflowID + "-operational-value"
	paths := operationalValueReportArtifactPaths{
		JSON: filepath.Join(outputDir, base+".json"), SVG: filepath.Join(outputDir, base+".svg"), Markdown: filepath.Join(outputDir, base+".md"),
	}
	jsonData, err := marshalIndentJSONOrWrap(report, "operational-value report")
	if err != nil {
		return operationalValueReportArtifactPaths{}, err
	}
	svgData := renderOperationalValueReportSVG(report)
	markdownData := renderOperationalValueReportMarkdown(report, filepath.Base(paths.JSON), filepath.Base(paths.SVG))
	for path, data := range map[string][]byte{paths.JSON: jsonData, paths.SVG: svgData, paths.Markdown: markdownData} {
		if err := writeFileAtomically(path, data); err != nil {
			return operationalValueReportArtifactPaths{}, fmt.Errorf("cannot write operational-value report %s: %w", path, err)
		}
	}
	return paths, nil
}

func renderOperationalValueReportSVG(report operationalValueReport) []byte { //nolint:largefunc // SVG emission preserves deterministic visual element ordering.
	const left, right = 120.0, 1160.0
	const outcomeTop = 245.0
	const primaryTop, primaryBottom = 730.0, 840.0
	hasDiagnostics := len(report.Diagnostics) > 0
	outcomeBottom := 555.0
	if !hasDiagnostics {
		outcomeBottom = primaryBottom
	}
	changeExtent := 1.0
	if hasDiagnostics {
		changeExtent = operationalValueReportChangeExtent(report)
	}
	start, _ := time.Parse(time.RFC3339, report.Window.StartAt)
	end, _ := time.Parse(time.RFC3339, report.Window.EndAt)
	span := end.Sub(start).Seconds()
	if span <= 0 {
		span = 1
	}
	xFor := func(value time.Time) float64 {
		position := value.Sub(start).Seconds() / span
		return left + max(0, min(1, position))*(right-left)
	}
	outcomeYFor := func(value float64) float64 {
		if hasDiagnostics {
			return outcomeTop + (changeExtent-value)/(2*changeExtent)*(outcomeBottom-outcomeTop)
		}
		return outcomeBottom - value*(outcomeBottom-outcomeTop)
	}
	primaryYFor := func(value float64) float64 { return primaryBottom - value*(primaryBottom-primaryTop) }
	var svg strings.Builder
	headlineChange := operationalValueReportHeadlineChange(report)
	svg.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1280 920" role="img" aria-labelledby="title description">` + "\n")
	fmt.Fprintf(&svg, "<title id=\"title\">%s operational value over time</title>\n", html.EscapeString(report.WorkflowName))
	fmt.Fprintf(&svg, "<desc id=\"description\">Outcome changes relative to the first post-adoption observation and a separate weekly per-run attainment trend under evaluator %s.</desc>\n", html.EscapeString(shortOperationalValueDigest(report.Evaluator.SHA256)))
	svg.WriteString(`<style>:root{--fg:#24292f;--muted:#57606a;--bg:#fff;--subtle:#f6f8fa;--border:#d0d7de;--good:#1a7f37;--bad:#cf222e;--outcome-1:#1a7f37;--outcome-2:#0969da;--outcome-3:#8250df;--outcome-4:#1b7c83;--primary:#bf8700;--adoption:#57606a}@media(prefers-color-scheme:dark){:root{--fg:#f0f6fc;--muted:#8c959f;--bg:#0d1117;--subtle:#161b22;--border:#30363d;--good:#3fb950;--bad:#ff7b72;--outcome-1:#3fb950;--outcome-2:#58a6ff;--outcome-3:#d2a8ff;--outcome-4:#56d4dd;--primary:#d29922;--adoption:#8c959f}}text{font-family:ui-sans-serif,system-ui,sans-serif;fill:var(--fg);letter-spacing:0}.title{font-size:28px;font-weight:700}.headline{font-size:22px;font-weight:650}.section{font-size:18px;font-weight:650}.metric-name{font-size:14px;font-weight:600;fill:var(--muted)}.metric-value{font-size:28px;font-weight:700}.metric-change{font-size:15px;font-weight:600}.good{fill:var(--good)}.bad{fill:var(--bad)}.neutral{fill:var(--muted)}.subtitle{font-size:15px;fill:var(--muted)}.axis{font-size:13px;fill:var(--muted)}.grid{stroke:var(--border);stroke-width:1}.outcome,.primary,.primary-trend{fill:none;stroke-linejoin:round;stroke-linecap:round}.outcome{stroke-width:5}.outcome-1{stroke:var(--outcome-1)}.outcome-2{stroke:var(--outcome-2);stroke-dasharray:10 5}.outcome-3{stroke:var(--outcome-3);stroke-dasharray:3 5}.outcome-4{stroke:var(--outcome-4);stroke-dasharray:12 4 3 4}.primary{stroke:var(--primary);stroke-width:3}.primary-raw{stroke-width:2;opacity:.35}.primary-trend{stroke:var(--primary);stroke-width:4}.primary-label{fill:var(--primary)}.gain-zone{fill:var(--good);opacity:.035}.loss-zone{fill:var(--bad);opacity:.035}.gain-area{fill:var(--good);opacity:.16}.loss-area{fill:var(--bad);opacity:.16}.change-baseline{stroke:var(--fg);stroke-width:2;stroke-dasharray:7 5}.endpoint{stroke:var(--bg);stroke-width:3}.endpoint-1{fill:var(--outcome-1)}.endpoint-2{fill:var(--outcome-2)}.endpoint-3{fill:var(--outcome-3)}.endpoint-4{fill:var(--outcome-4)}.endpoint-label-1{fill:var(--outcome-1)}.endpoint-label-2{fill:var(--outcome-2)}.endpoint-label-3{fill:var(--outcome-3)}.endpoint-label-4{fill:var(--outcome-4)}.adoption{stroke:var(--adoption);stroke-width:2;stroke-dasharray:5 5}</style>` + "\n")
	svg.WriteString(`<rect width="1280" height="920" fill="var(--bg)"/>` + "\n")
	fmt.Fprintf(&svg, "<text x=\"120\" y=\"44\" class=\"title\">%s</text>\n", html.EscapeString(report.WorkflowName))
	fmt.Fprintf(&svg, "<text x=\"120\" y=\"78\" class=\"headline %s\">%s</text>\n", operationalValueReportTrendClass(headlineChange), html.EscapeString(operationalValueReportTrendHeadline(report)))
	fmt.Fprintf(&svg, "<text x=\"1160\" y=\"44\" text-anchor=\"end\" class=\"subtitle\">%d runs · evaluator %s</text>\n", report.Coverage.RunCount, html.EscapeString(shortOperationalValueDigest(report.Evaluator.SHA256)))
	for index, diagnostic := range report.Diagnostics {
		if index >= 2 || diagnostic.Summary.First == nil || diagnostic.Summary.Latest == nil {
			continue
		}
		x := 120.0 + float64(index)*520
		fmt.Fprintf(&svg, "<text x=\"%.1f\" y=\"116\" class=\"metric-name\">%s</text>\n", x, html.EscapeString(diagnostic.Metric.Name))
		fmt.Fprintf(&svg, "<text x=\"%.1f\" y=\"151\" class=\"metric-value\">%s → %s</text>\n", x, formatOperationalValuePercent(*diagnostic.Summary.First), formatOperationalValuePercent(*diagnostic.Summary.Latest))
		if diagnostic.Summary.Change != nil {
			fmt.Fprintf(&svg, "<text x=\"%.1f\" y=\"176\" class=\"metric-change %s\">%s since adoption</text>\n", x, operationalValueReportTrendClass(diagnostic.Summary.Change), formatOperationalValuePointChange(*diagnostic.Summary.Change))
		}
	}
	if !hasDiagnostics && report.Summary.First != nil && report.Summary.Latest != nil {
		fmt.Fprintf(&svg, "<text x=\"120\" y=\"116\" class=\"metric-name\">Primary operational attainment</text>\n")
		fmt.Fprintf(&svg, "<text x=\"120\" y=\"151\" class=\"metric-value\">%s → %s</text>\n", formatOperationalValuePercent(*report.Summary.First), formatOperationalValuePercent(*report.Summary.Latest))
		if report.Summary.Change != nil {
			fmt.Fprintf(&svg, "<text x=\"120\" y=\"176\" class=\"metric-change %s\">%s since adoption</text>\n", operationalValueReportTrendClass(report.Summary.Change), formatOperationalValuePointChange(*report.Summary.Change))
		}
	}
	sectionTitle := "Outcome change since adoption"
	sectionDescription := "Percentage-point change from each metric's first observed value"
	if !hasDiagnostics {
		sectionTitle = "Operational attainment"
		sectionDescription = "Weekly primary mean · higher is better"
	}
	fmt.Fprintf(&svg, "<text x=\"120\" y=\"218\" class=\"section\">%s</text>\n", sectionTitle)
	fmt.Fprintf(&svg, "<text x=\"1160\" y=\"218\" text-anchor=\"end\" class=\"subtitle\">%s</text>\n", sectionDescription)
	if hasDiagnostics {
		zeroY := outcomeYFor(0)
		fmt.Fprintf(&svg, "<rect class=\"gain-zone\" x=\"%.1f\" y=\"%.1f\" width=\"%.1f\" height=\"%.1f\"/><rect class=\"loss-zone\" x=\"%.1f\" y=\"%.1f\" width=\"%.1f\" height=\"%.1f\"/>\n", left, outcomeTop, right-left, zeroY-outcomeTop, left, zeroY, right-left, outcomeBottom-zeroY)
	}
	for index := range 5 {
		value := float64(index) / 4
		label := fmt.Sprintf("%.0f%%", value*100)
		if hasDiagnostics {
			value = changeExtent - float64(index)*changeExtent/2
			label = formatOperationalValueAxisPointChange(value)
		}
		y := outcomeYFor(value)
		fmt.Fprintf(&svg, "<line class=\"grid\" x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\"/><text x=\"102\" y=\"%.1f\" text-anchor=\"end\" class=\"axis\">%s</text>\n", left, y, right, y, y+5, label)
	}
	for index := range 7 {
		at := start.Add(time.Duration(float64(end.Sub(start)) * float64(index) / 6))
		x := xFor(at)
		fmt.Fprintf(&svg, "<line class=\"grid\" x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\"/><text x=\"%.1f\" y=\"%.1f\" text-anchor=\"middle\" class=\"axis\">%s</text>\n", x, outcomeTop, x, outcomeBottom, x, outcomeBottom+27, at.Format("Jan 02"))
	}
	fmt.Fprintf(&svg, "<line class=\"adoption\" x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\"/><text x=\"%.1f\" y=\"238\" class=\"axis\">Adoption</text>\n", left, outcomeTop, left, outcomeBottom, left+8)
	if hasDiagnostics {
		zeroY := outcomeYFor(0)
		fmt.Fprintf(&svg, "<line class=\"change-baseline\" x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\"/><text x=\"%.1f\" y=\"%.1f\" text-anchor=\"end\" class=\"axis\">0 pts = first observed value</text>\n", left, zeroY, right, zeroY, right-8, zeroY-9)
	}
	if hasDiagnostics && report.Diagnostics[0].Summary.First != nil {
		diagnostic := report.Diagnostics[0]
		points := make([]string, 0, len(diagnostic.Weekly)+2)
		hasGain, hasLoss := false, false
		var firstX, lastX float64
		for _, week := range diagnostic.Weekly {
			if week.Value == nil {
				continue
			}
			weekStart, _ := time.Parse(time.RFC3339, week.WeekStart)
			weekEnd, _ := time.Parse(time.RFC3339, week.WeekEnd)
			x := xFor(weekStart.Add(weekEnd.Sub(weekStart) / 2))
			if len(points) == 0 {
				firstX = x
			}
			lastX = x
			change := *week.Value - *diagnostic.Summary.First
			hasGain = hasGain || change > 0
			hasLoss = hasLoss || change < 0
			points = append(points, fmt.Sprintf("%.1f,%.1f", x, outcomeYFor(change)))
		}
		if len(points) > 1 {
			zeroY := outcomeYFor(0)
			fmt.Fprintf(&svg, "<defs><clipPath id=\"gain-region\"><rect x=\"%.1f\" y=\"%.1f\" width=\"%.1f\" height=\"%.1f\"/></clipPath><clipPath id=\"loss-region\"><rect x=\"%.1f\" y=\"%.1f\" width=\"%.1f\" height=\"%.1f\"/></clipPath></defs>\n", left, outcomeTop, right-left, zeroY-outcomeTop, left, zeroY, right-left, outcomeBottom-zeroY)
			areaPoints := fmt.Sprintf("%.1f,%.1f %s %.1f,%.1f", firstX, zeroY, strings.Join(points, " "), lastX, zeroY)
			if hasGain {
				fmt.Fprintf(&svg, "<polygon class=\"gain-area\" clip-path=\"url(#gain-region)\" points=\"%s\"><title>Gain from adoption for %s</title></polygon>\n", areaPoints, html.EscapeString(diagnostic.Metric.Name))
			}
			if hasLoss {
				fmt.Fprintf(&svg, "<polygon class=\"loss-area\" clip-path=\"url(#loss-region)\" points=\"%s\"><title>Loss from adoption for %s</title></polygon>\n", areaPoints, html.EscapeString(diagnostic.Metric.Name))
			}
		}
	}
	for index, diagnostic := range report.Diagnostics {
		points := make([]string, 0, len(diagnostic.Weekly))
		var firstX, firstY, lastX, lastY, lastChange float64
		if diagnostic.Summary.First == nil {
			continue
		}
		for _, week := range diagnostic.Weekly {
			if week.Value == nil {
				continue
			}
			weekStart, _ := time.Parse(time.RFC3339, week.WeekStart)
			weekEnd, _ := time.Parse(time.RFC3339, week.WeekEnd)
			x := xFor(weekStart.Add(weekEnd.Sub(weekStart) / 2))
			lastChange = *week.Value - *diagnostic.Summary.First
			y := outcomeYFor(lastChange)
			if len(points) == 0 {
				firstX, firstY = x, y
			}
			lastX, lastY = x, y
			points = append(points, fmt.Sprintf("%.1f,%.1f", x, y))
		}
		if len(points) > 1 {
			fmt.Fprintf(&svg, "<polyline class=\"outcome outcome-%d\" points=\"%s\"/>\n", index%4+1, strings.Join(points, " "))
			fmt.Fprintf(&svg, "<circle class=\"endpoint endpoint-%d\" cx=\"%.1f\" cy=\"%.1f\" r=\"6\"/><circle class=\"endpoint endpoint-%d\" cx=\"%.1f\" cy=\"%.1f\" r=\"6\"/>\n", index%4+1, firstX, firstY, index%4+1, lastX, lastY)
			labelY := max(outcomeTop+18, min(outcomeBottom-8, lastY-10))
			fmt.Fprintf(&svg, "<text x=\"%.1f\" y=\"%.1f\" text-anchor=\"end\" class=\"metric-change endpoint-label-%d\">%s</text>\n", lastX-10, labelY, index%4+1, formatOperationalValuePointChange(lastChange))
		}
	}
	if !hasDiagnostics {
		points := make([]string, 0, len(report.Weekly))
		for _, week := range report.Weekly {
			if week.Mean == nil {
				continue
			}
			weekStart, _ := time.Parse(time.RFC3339, week.WeekStart)
			weekEnd, _ := time.Parse(time.RFC3339, week.WeekEnd)
			x := xFor(weekStart.Add(weekEnd.Sub(weekStart) / 2))
			points = append(points, fmt.Sprintf("%.1f,%.1f", x, outcomeYFor(*week.Mean)))
		}
		if len(points) > 1 {
			fmt.Fprintf(&svg, "<polyline class=\"primary\" points=\"%s\"/>\n", strings.Join(points, " "))
		}
	}
	for index, diagnostic := range report.Diagnostics {
		x := 130.0 + float64(index%2)*530
		fmt.Fprintf(&svg, "<line class=\"outcome outcome-%d\" x1=\"%.1f\" y1=\"620\" x2=\"%.1f\" y2=\"620\"/><text x=\"%.1f\" y=\"626\" class=\"axis\">%s</text>\n", index%4+1, x, x+36, x+48, html.EscapeString(diagnostic.Metric.Name))
	}
	if hasDiagnostics {
		fmt.Fprintf(&svg, "<text x=\"120\" y=\"682\" class=\"section\">Per-run operational attainment</text>\n")
		fmt.Fprintf(&svg, "<text x=\"120\" y=\"706\" class=\"subtitle\">Weekly values (thin) · 4-week rolling mean (bold) · separate from repository health</text>\n")
		for index := range 3 {
			value := float64(index) / 2
			y := primaryYFor(value)
			fmt.Fprintf(&svg, "<line class=\"grid\" x1=\"%.1f\" y1=\"%.1f\" x2=\"%.1f\" y2=\"%.1f\"/><text x=\"102\" y=\"%.1f\" text-anchor=\"end\" class=\"axis\">%.0f%%</text>\n", left, y, right, y, y+5, value*100)
		}
		primaryPoints := make([]string, 0, len(report.Weekly))
		trendPoints := make([]string, 0, len(report.Weekly))
		var lastTrendX, lastTrendY, lastTrendValue float64
		for weekIndex, week := range report.Weekly {
			if week.Mean == nil {
				continue
			}
			weekStart, _ := time.Parse(time.RFC3339, week.WeekStart)
			weekEnd, _ := time.Parse(time.RFC3339, week.WeekEnd)
			x := xFor(weekStart.Add(weekEnd.Sub(weekStart) / 2))
			primaryPoints = append(primaryPoints, fmt.Sprintf("%.1f,%.1f", x, primaryYFor(*week.Mean)))
			windowStart := max(0, weekIndex-3)
			total, count := 0.0, 0
			for previousIndex := windowStart; previousIndex <= weekIndex; previousIndex++ {
				if report.Weekly[previousIndex].Mean != nil {
					total += *report.Weekly[previousIndex].Mean
					count++
				}
			}
			if count > 0 {
				lastTrendX = x
				lastTrendValue = total / float64(count)
				lastTrendY = primaryYFor(lastTrendValue)
				trendPoints = append(trendPoints, fmt.Sprintf("%.1f,%.1f", lastTrendX, lastTrendY))
			}
		}
		if len(primaryPoints) > 1 {
			fmt.Fprintf(&svg, "<polyline class=\"primary primary-raw\" points=\"%s\"/>\n", strings.Join(primaryPoints, " "))
		}
		if len(trendPoints) > 1 {
			fmt.Fprintf(&svg, "<polyline class=\"primary-trend\" points=\"%s\"/>\n", strings.Join(trendPoints, " "))
			labelY := max(primaryTop+15, min(primaryBottom-6, lastTrendY-8))
			fmt.Fprintf(&svg, "<text x=\"%.1f\" y=\"%.1f\" text-anchor=\"end\" class=\"metric-change primary-label\">4-week avg %s</text>\n", lastTrendX-8, labelY, formatOperationalValuePercent(lastTrendValue))
		}
	}
	fmt.Fprintf(&svg, "<text x=\"120\" y=\"888\" class=\"subtitle\">Observed association only; the report does not establish that the workflow caused these changes.</text>\n")
	svg.WriteString("</svg>\n")
	return []byte(svg.String())
}

func renderOperationalValueReportMarkdown(report operationalValueReport, jsonName, svgName string) []byte { //nolint:largefunc // Markdown emission preserves deterministic report section ordering.
	var markdown strings.Builder
	fmt.Fprintf(&markdown, "# %s operational value\n\n", sanitizeOperationalValueMarkdown(report.WorkflowName))
	fmt.Fprintf(&markdown, "![%s operational value timeline](%s)\n\n", sanitizeOperationalValueMarkdown(report.WorkflowName), svgName)
	markdown.WriteString("## Summary\n\n")
	fmt.Fprintf(&markdown, "- **Operational value:** %s\n", sanitizeOperationalValueMarkdown(report.OperationalValue))
	fmt.Fprintf(&markdown, "- **History:** %s through %s\n", report.Window.StartAt, report.Window.EndAt)
	fmt.Fprintf(&markdown, "- **Coverage:** %d of %d runs produced numeric values; %d unavailable; %d errors\n", report.Coverage.NumericCount, report.Coverage.RunCount, report.Coverage.UnavailableCount, report.Coverage.ErrorCount)
	fmt.Fprintf(&markdown, "- **Current evaluator:** `%s`\n", report.Evaluator.SHA256)
	fmt.Fprintf(&markdown, "- **Weekly cache:** %d hits; %d runs evaluated in this invocation\n", report.Coverage.WeeklyCacheHits, report.Coverage.EvaluatedCount)
	if report.Summary.Latest != nil {
		fmt.Fprintf(&markdown, "- **Latest value:** %s", formatOperationalValue(*report.Summary.Latest))
		if report.Summary.Change != nil {
			fmt.Fprintf(&markdown, " (%s from the first numeric observation)", formatSignedOperationalValue(*report.Summary.Change))
		}
		markdown.WriteString("\n")
	}
	if report.Baseline.Value != nil {
		fmt.Fprintf(&markdown, "- **Frozen baseline:** %s", formatOperationalValue(*report.Baseline.Value))
		if report.Summary.LatestDeltaFromBaseline != nil {
			fmt.Fprintf(&markdown, " (latest delta %s)", formatSignedOperationalValue(*report.Summary.LatestDeltaFromBaseline))
		}
		markdown.WriteString("\n")
	}
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Summary.Latest == nil {
			continue
		}
		fmt.Fprintf(&markdown, "- **Diagnostic, %s:** %s", sanitizeOperationalValueMarkdown(diagnostic.Metric.Name), formatOperationalValue(*diagnostic.Summary.Latest))
		if diagnostic.Summary.Change != nil {
			fmt.Fprintf(&markdown, " (%s across observed runs)", formatSignedOperationalValue(*diagnostic.Summary.Change))
		}
		markdown.WriteString("\n")
	}
	fmt.Fprintf(&markdown, "- **Structured report:** [%s](%s)\n\n", jsonName, jsonName)

	markdown.WriteString("## Weekly History\n\n")
	markdown.WriteString("| Week | Runs | Distinct opportunities | Primary mean | Primary range")
	for _, diagnostic := range report.Diagnostics {
		fmt.Fprintf(&markdown, " | %s", sanitizeOperationalValueMarkdown(diagnostic.Metric.Name))
	}
	markdown.WriteString(" |\n|---|---:|---:|---:|---:")
	for range report.Diagnostics {
		markdown.WriteString("|---:")
	}
	markdown.WriteString("|\n")
	for weekIndex, week := range report.Weekly {
		mean, valueRange := "missing", "missing"
		if week.Mean != nil {
			mean = formatOperationalValue(*week.Mean)
			valueRange = formatOperationalValue(*week.Minimum) + "-" + formatOperationalValue(*week.Maximum)
		}
		fmt.Fprintf(&markdown, "| %s | %d | %d | %s | %s", strings.TrimSuffix(week.WeekStart, "T00:00:00Z"), week.RunCount, week.DistinctOpportunityCount, mean, valueRange)
		for _, diagnostic := range report.Diagnostics {
			value := "missing"
			if weekIndex < len(diagnostic.Weekly) && diagnostic.Weekly[weekIndex].Value != nil {
				value = formatOperationalValue(*diagnostic.Weekly[weekIndex].Value)
			}
			fmt.Fprintf(&markdown, " | %s", value)
		}
		markdown.WriteString(" |\n")
	}

	markdown.WriteString("\n## Frozen Contract\n\n")
	fmt.Fprintf(&markdown, "- **Opportunity:** %s\n", sanitizeOperationalValueMarkdown(report.EvaluatorDefinition().Evidence.Opportunity))
	fmt.Fprintf(&markdown, "- **Assignment:** %s\n", sanitizeOperationalValueMarkdown(report.EvaluatorDefinition().Evidence.Assignment))
	fmt.Fprintf(&markdown, "- **Accepted evidence:** %s\n", sanitizeOperationalValueMarkdown(report.EvaluatorDefinition().Evidence.Accepted))
	fmt.Fprintf(&markdown, "- **Collection:** %s\n", sanitizeOperationalValueMarkdown(report.EvaluatorDefinition().Evidence.Collection))
	fmt.Fprintf(&markdown, "- **Maturation:** %s\n", sanitizeOperationalValueMarkdown(report.EvaluatorDefinition().Evidence.Maturation))
	fmt.Fprintf(&markdown, "- **Zero rule:** %s\n", sanitizeOperationalValueMarkdown(report.EvaluatorDefinition().Evidence.ZeroRule))
	fmt.Fprintf(&markdown, "- **Missing rule:** %s\n", sanitizeOperationalValueMarkdown(report.EvaluatorDefinition().Evidence.MissingRule))
	fmt.Fprintf(&markdown, "- **Primary metric:** `%s` = %s\n", report.EvaluatorDefinition().PrimaryMetric.ID, sanitizeOperationalValueMarkdown(report.EvaluatorDefinition().PrimaryMetric.Formula))
	for _, metric := range report.EvaluatorDefinition().DiagnosticMetrics {
		fmt.Fprintf(&markdown, "- **Diagnostic metric:** `%s` (%s) = %s\n", metric.ID, metric.Aggregation, sanitizeOperationalValueMarkdown(metric.Formula))
	}

	markdown.WriteString("\n## Coverage Notes\n\n")
	fmt.Fprintf(&markdown, "%d distinct opportunity keys were observed; %d additional numeric observations repeated a key. Repeated observations remain visible in the plot but are not treated as independent within a weekly mean.\n\n", report.Coverage.DistinctOpportunityCount, report.Coverage.DuplicateOpportunityCount)
	if report.Coverage.ErrorCount > 0 {
		markdown.WriteString("### Evaluation Errors\n\n")
		for _, observation := range report.Observations {
			if observation.Status == "error" {
				fmt.Fprintf(&markdown, "- Run `%s`: %s\n", observation.Run.ID, sanitizeOperationalValueMarkdown(observation.Message))
			}
		}
		markdown.WriteString("\n")
	}
	markdown.WriteString("## Interpretation\n\n")
	markdown.WriteString(report.Caveat + " Diagnostic metrics provide context but are not combined with the primary value. Pre-grader runs have no archived case or event payload, so their cases are reconstructed only when the evaluator supports assignment from the run subject. Missing evidence is never treated as zero.\n")
	return []byte(markdown.String())
}

func (report operationalValueReport) EvaluatorDefinition() operationalValueReportDefinition {
	var definition operationalValueReportDefinition
	if err := json.Unmarshal(report.Evaluator.Definition, &definition); err != nil {
		return operationalValueReportDefinition{}
	}
	return definition
}

func sanitizeOperationalValueMarkdown(value string) string {
	return strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ", "|", "\\|").Replace(value))
}

func formatOperationalValue(value float64) string {
	return strconv.FormatFloat(math.Round(value*10000)/10000, 'f', -1, 64)
}

func formatOperationalValuePercent(value float64) string {
	return strconv.FormatFloat(math.Round(value*1000)/10, 'f', 1, 64) + "%"
}

func formatOperationalValuePointChange(value float64) string {
	points := math.Round(value*1000) / 10
	if points > 0 {
		return "+" + strconv.FormatFloat(points, 'f', 1, 64) + " pts"
	}
	return strconv.FormatFloat(points, 'f', 1, 64) + " pts"
}

func formatOperationalValueAxisPointChange(value float64) string {
	points := math.Round(value*1000) / 10
	if math.Abs(points) < 0.05 {
		return "0 pts"
	}
	if points > 0 {
		return "+" + strconv.FormatFloat(points, 'f', -1, 64) + " pts"
	}
	return strconv.FormatFloat(points, 'f', -1, 64) + " pts"
}

func operationalValueReportChangeExtent(report operationalValueReport) float64 {
	maximumChange := 0.0
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Summary.First == nil {
			continue
		}
		for _, week := range diagnostic.Weekly {
			if week.Value != nil {
				maximumChange = max(maximumChange, math.Abs(*week.Value-*diagnostic.Summary.First))
			}
		}
	}
	return max(0.1, min(1, math.Ceil(maximumChange*10)/10))
}

func operationalValueReportTrendHeadline(report operationalValueReport) string {
	if len(report.Diagnostics) == 0 {
		return operationalValueReportTrendStatement("Operational attainment", report.Summary.Change)
	}
	diagnostic := report.Diagnostics[0]
	return operationalValueReportTrendStatement(diagnostic.Metric.Name, diagnostic.Summary.Change)
}

func operationalValueReportTrendStatement(name string, change *float64) string {
	if change == nil {
		return name + " since adoption"
	}
	switch {
	case *change > 0.005:
		return name + " improved since adoption"
	case *change < -0.005:
		return name + " declined since adoption"
	default:
		return name + " held steady since adoption"
	}
}

func operationalValueReportHeadlineChange(report operationalValueReport) *float64 {
	if len(report.Diagnostics) > 0 {
		return report.Diagnostics[0].Summary.Change
	}
	return report.Summary.Change
}

func operationalValueReportTrendClass(change *float64) string {
	if change == nil || math.Abs(*change) <= 0.005 {
		return "neutral"
	}
	if *change > 0 {
		return "good"
	}
	return "bad"
}

func formatSignedOperationalValue(value float64) string {
	if value > 0 {
		return "+" + formatOperationalValue(value)
	}
	return formatOperationalValue(value)
}

func shortOperationalValueDigest(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}
