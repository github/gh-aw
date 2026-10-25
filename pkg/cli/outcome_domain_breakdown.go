package cli

import (
	"cmp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var domainBreakdownLog = logger.New("cli:domain_breakdown")

// DomainBreakdown provides metrics for a single label/domain from outcomes.
type DomainBreakdown struct {
	Label                  string  `json:"label" console:"header:Domain"`
	Attempted              int     `json:"attempted" console:"header:Attempted"`
	Accepted               int     `json:"accepted" console:"header:Accepted"`
	Rejected               int     `json:"rejected" console:"header:Rejected"`
	Pending                int     `json:"pending" console:"header:Pending"`
	TotalObjectiveValue    int     `json:"total_objective_value" console:"header:Total Value"`
	AcceptedObjectiveValue int     `json:"accepted_objective_value" console:"header:Accepted Value"`
	ObjectiveEfficiency    float64 `json:"objective_efficiency,omitempty" console:"header:Efficiency"`
	AcceptanceRate         float64 `json:"acceptance_rate,omitempty" console:"header:Acceptance Rate"`
}

// ComputeDomainBreakdowns aggregates outcome metrics by label/domain.
// Returns a slice of DomainBreakdown sorted by total_objective_value descending.
func ComputeDomainBreakdowns(reports []OutcomeReport) []DomainBreakdown {
	if len(reports) == 0 {
		return []DomainBreakdown{}
	}

	// Map domain label → metrics
	domains := make(map[string]*DomainBreakdown)

	for _, report := range reports {
		accumulateOutcomeDomainBreakdown(domains, report)
	}
	return finalizeDomainBreakdowns(domains)
}

func accumulateOutcomeDomainBreakdown(domains map[string]*DomainBreakdown, report OutcomeReport) {
	// If outcome has objective labels, aggregate by each label
	for _, label := range report.ObjectiveLabels {
		normalizedLabel := strings.ToLower(strings.TrimSpace(label))
		if _, exists := domains[normalizedLabel]; !exists {
			domains[normalizedLabel] = &DomainBreakdown{
				Label: label,
			}
		}
		domain := domains[normalizedLabel]
		domain.Attempted++
		domain.TotalObjectiveValue += report.ObjectiveValue

		switch report.OutcomeStatus {
		case OutcomeStatusAccepted:
			domain.Accepted++
			domain.AcceptedObjectiveValue += report.ObjectiveValue
		case OutcomeStatusRejected:
			domain.Rejected++
		case OutcomeStatusPending:
			domain.Pending++
		}
	}

	// If outcome has NO objective labels, create "unmapped" entry
	if len(report.ObjectiveLabels) == 0 && report.ObjectiveValue == 0 {
		if _, exists := domains["unmapped"]; !exists {
			domains["unmapped"] = &DomainBreakdown{
				Label: "unmapped",
			}
		}
		domain := domains["unmapped"]
		domain.Attempted++

		switch report.OutcomeStatus {
		case OutcomeStatusAccepted:
			domain.Accepted++
		case OutcomeStatusRejected:
			domain.Rejected++
		case OutcomeStatusPending:
			domain.Pending++
		}
	}
}

func finalizeDomainBreakdowns(domains map[string]*DomainBreakdown) []DomainBreakdown {
	// Compute efficiency metrics for each domain
	result := make([]DomainBreakdown, 0, len(domains))
	for _, domain := range domains {
		if domain.Attempted > 0 {
			domain.AcceptanceRate = float64(domain.Accepted) / float64(domain.Attempted)
		}
		if domain.TotalObjectiveValue > 0 {
			domain.ObjectiveEfficiency = float64(domain.AcceptedObjectiveValue) / float64(domain.TotalObjectiveValue)
		}
		result = append(result, *domain)
	}

	// Sort by total_objective_value descending
	slices.SortFunc(result, func(a, b DomainBreakdown) int {
		if a.TotalObjectiveValue != b.TotalObjectiveValue {
			return cmp.Compare(b.TotalObjectiveValue, a.TotalObjectiveValue)
		}
		return strings.Compare(a.Label, b.Label)
	})

	domainBreakdownLog.Printf("Computed domain breakdowns: domains=%d, total_attempted=%d", len(result), countTotalAttempted(result))
	return result
}

func countTotalAttempted(breakdowns []DomainBreakdown) int {
	total := 0
	for _, d := range breakdowns {
		total += d.Attempted
	}
	return total
}
