package cli

import (
	"cmp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var domainBreakdownLog = logger.New("cli:domain_breakdown")

// DomainBreakdown provides metrics for a single label/domain from adoptions.
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

// ComputeDomainBreakdowns aggregates adoption metrics by label/domain.
// Returns a slice of DomainBreakdown sorted by total_objective_value descending.
func ComputeDomainBreakdowns(reports []AdoptionReport) []DomainBreakdown {
	if len(reports) == 0 {
		return []DomainBreakdown{}
	}

	domains := make(map[string]*DomainBreakdown)
	for _, report := range reports {
		for _, label := range report.ObjectiveLabels {
			normalizedLabel := strings.ToLower(strings.TrimSpace(label))
			domain := ensureDomainBreakdown(domains, normalizedLabel, label)
			addReportToDomain(domain, report, true)
		}
		if len(report.ObjectiveLabels) == 0 && report.ObjectiveValue == 0 {
			addReportToDomain(ensureDomainBreakdown(domains, "unmapped", "unmapped"), report, false)
		}
	}

	result := finalizeDomainBreakdowns(domains)
	domainBreakdownLog.Printf("Computed domain breakdowns: domains=%d, total_attempted=%d", len(result), countTotalAttempted(result))
	return result
}

func ensureDomainBreakdown(domains map[string]*DomainBreakdown, key, label string) *DomainBreakdown {
	if domains[key] == nil {
		domains[key] = &DomainBreakdown{Label: label}
	}
	return domains[key]
}

func addReportToDomain(domain *DomainBreakdown, report AdoptionReport, includeObjectiveValue bool) {
	domain.Attempted++
	if includeObjectiveValue {
		domain.TotalObjectiveValue += report.ObjectiveValue
	}
	switch report.AdoptionStatus {
	case AdoptionStatusAccepted:
		domain.Accepted++
		if includeObjectiveValue {
			domain.AcceptedObjectiveValue += report.ObjectiveValue
		}
	case AdoptionStatusRejected:
		domain.Rejected++
	case AdoptionStatusPending:
		domain.Pending++
	}
}

func finalizeDomainBreakdowns(domains map[string]*DomainBreakdown) []DomainBreakdown {
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
	slices.SortFunc(result, func(a, b DomainBreakdown) int {
		if a.TotalObjectiveValue != b.TotalObjectiveValue {
			return cmp.Compare(b.TotalObjectiveValue, a.TotalObjectiveValue)
		}
		return strings.Compare(a.Label, b.Label)
	})
	return result
}

func countTotalAttempted(breakdowns []DomainBreakdown) int {
	total := 0
	for _, d := range breakdowns {
		total += d.Attempted
	}
	return total
}
