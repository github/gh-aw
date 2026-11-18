package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/lifecycle"
)

type valueDailyObservation struct {
	Snapshot            valueSnapshot `json:"snapshot"`
	RetainedAcceptances *int          `json:"retained_human_acceptances"`
	AcceptanceCoverage  string        `json:"acceptance_coverage"`
}

func collectValueDaily(git valueGit, measurement valueMeasurement, tip string, report operationalValueReport, records map[string]lifecycle.Record) ([]valueDailyObservation, error) {
	counts := make(map[time.Time]int)
	for _, record := range records {
		a := record.Adoption
		if !strings.EqualFold(record.Host, report.Measurement.Host) || !strings.EqualFold(record.Repository, report.Measurement.Repository) ||
			record.Workflow != report.Measurement.WorkflowPath || record.Delivery.State != "delivered" ||
			a.State != "accepted" || a.Coverage != "available" || a.Source != "github-api" ||
			a.AcceptanceKind != "explicit_human" || !a.HumanVerified || a.At.Before(report.Start) || a.At.After(report.End) {
			continue
		}
		// Bucket by the decision clock, not the delivery or collection clock.
		at := a.At.UTC()
		noon := at.Truncate(24 * time.Hour).Add(12 * time.Hour)
		if at.After(noon) {
			noon = noon.AddDate(0, 0, 1)
		}
		counts[noon]++
	}
	known := make(map[time.Time]valueSnapshot, len(report.Weekly))
	for _, snapshot := range report.Weekly {
		known[snapshot.At] = snapshot
	}
	var rows []valueDailyObservation
	for at := report.Start; !at.After(report.End); at = at.AddDate(0, 0, 1) {
		snapshot, ok := known[at]
		if !ok {
			var err error
			snapshot, err = git.snapshot(measurement, tip, at)
			if err != nil {
				return nil, fmt.Errorf("daily measurement at %s: %w", at.Format(time.RFC3339), err)
			}
		}
		row := valueDailyObservation{Snapshot: snapshot, AcceptanceCoverage: "unknown"}
		if n := counts[at]; n > 0 {
			row.RetainedAcceptances, row.AcceptanceCoverage = &n, "retained_subset"
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func renderValueDaily(out io.Writer, measurement valueMeasurement, daily []valueDailyObservation) error {
	if len(daily) == 0 {
		return nil
	}
	type row struct {
		Day      string `console:"header:Snapshot (UTC noon)"`
		Metric   string `console:"header:Measurement"`
		Count    string `console:"header:Retained human acceptances"`
		Coverage string `console:"header:Acceptance coverage"`
	}
	rows := make([]row, 0, len(daily))
	for _, obs := range daily {
		count := "unknown"
		if obs.RetainedAcceptances != nil {
			count = strconv.Itoa(*obs.RetainedAcceptances)
		}
		rows = append(rows, row{obs.Snapshot.At.Format(time.DateOnly), measurement.format(obs.Snapshot), count, obs.AcceptanceCoverage})
	}
	_, err := fmt.Fprint(out, console.RenderStructStdout(rows))
	return err
}
