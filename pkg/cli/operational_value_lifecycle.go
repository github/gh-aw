package cli

import (
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/lifecycle"
)

type valueLifecycleReport struct {
	Coverage  string             `json:"coverage"`
	Delivered *int               `json:"delivered"`
	Adoption  *OutcomeSummary    `json:"adoption"`
	Records   []lifecycle.Record `json:"records"`
}

func reportValueLifecycle(definition valueMeasurementDefinition, start, end time.Time, records map[string]lifecycle.Record) valueLifecycleReport {
	report := valueLifecycleReport{Coverage: "unavailable", Records: []lifecycle.Record{}}
	items := []OutcomeReport{}
	for _, record := range records {
		if !strings.EqualFold(record.Host, definition.Host) || !strings.EqualFold(record.Repository, definition.Repository) ||
			record.Workflow != definition.WorkflowPath || record.Delivery.At.Before(start) || record.Delivery.At.After(end) {
			continue
		}
		report.Records = append(report.Records, record)
		item := OutcomeReport{Type: record.Type}
		applyLifecycleObservation(&item, record)
		items = append(items, item)
	}
	if len(items) > 0 {
		report.Coverage = "retained_subset"
		report.Delivered = new(len(items))
		summary := ComputeOutcomeSummary(items, nil)
		report.Adoption = &summary
	}
	slices.SortFunc(report.Records, func(a, b lifecycle.Record) int {
		return strings.Compare(a.ID, b.ID)
	})
	return report
}
