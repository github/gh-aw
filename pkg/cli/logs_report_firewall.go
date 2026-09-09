package cli

import (
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var firewallReportLog = logger.New("cli:logs_report_firewall")

// AccessLogSummary contains aggregated access log analysis
type AccessLogSummary struct {
	FirewallSummaryBase
	ByWorkflow map[string]*DomainAnalysis `json:"by_workflow,omitempty" console:"-"`
}

// FirewallSummaryBase contains shared aggregated request and domain fields.
type FirewallSummaryBase struct {
	TotalRequests   int      `json:"total_requests" console:"header:Total Requests"`
	AllowedRequests int      `json:"allowed_requests" console:"header:Allowed"`
	BlockedRequests int      `json:"blocked_requests" console:"header:Blocked"`
	AllowedDomains  []string `json:"allowed_domains" console:"-"`
	BlockedDomains  []string `json:"blocked_domains" console:"-"`
}

// FirewallLogSummary contains aggregated firewall log data
type FirewallLogSummary struct {
	FirewallSummaryBase
	RequestsByDomain map[string]DomainRequestStats `json:"requests_by_domain,omitempty" console:"-"`
	Domains          []FirewallDomainRecord        `json:"domains,omitempty" console:"-"`
	ByWorkflow       map[string]*FirewallAnalysis  `json:"by_workflow,omitempty" console:"-"`
}

// FirewallDomainRecord describes requests to a domain with one firewall decision.
type FirewallDomainRecord struct {
	Domain   string `json:"domain"`
	Accepted bool   `json:"accepted"`
	Arity    int    `json:"arity"`
}

// domainAggregation holds the result of aggregating domain statistics
type domainAggregation struct {
	allAllowedDomains map[string]struct{}
	allBlockedDomains map[string]struct{}
	totalRequests     int
	allowedCount      int
	blockedCount      int
}

// aggregateDomainStats aggregates domain statistics across runs
// This is a shared helper for both access log and firewall log summaries
func aggregateDomainStats(processedRuns []ProcessedRun, getAnalysis func(*ProcessedRun) (allowedDomains, blockedDomains []string, totalRequests, allowedCount, blockedCount int, exists bool)) *domainAggregation {
	firewallReportLog.Printf("Aggregating domain stats across %d runs", len(processedRuns))
	agg := &domainAggregation{
		allAllowedDomains: make(map[string]struct{}),
		allBlockedDomains: make(map[string]struct{}),
	}

	for i := range processedRuns {
		allowedDomains, blockedDomains, totalRequests, allowedCount, blockedCount, exists := getAnalysis(&processedRuns[i])
		if !exists {
			continue
		}

		agg.totalRequests += totalRequests
		agg.allowedCount += allowedCount
		agg.blockedCount += blockedCount

		for _, domain := range allowedDomains {
			agg.allAllowedDomains[domain] = struct{}{}
		}
		for _, domain := range blockedDomains {
			agg.allBlockedDomains[domain] = struct{}{}
		}
	}

	firewallReportLog.Printf("Domain aggregation complete: %d allowed, %d blocked, %d total requests",
		len(agg.allAllowedDomains), len(agg.allBlockedDomains), agg.totalRequests)
	return agg
}

// convertDomainsToSortedSlices converts domain maps to sorted slices
func convertDomainsToSortedSlices(allowedMap, blockedMap map[string]struct{}) (allowed, blocked []string) {
	return sliceutil.SortedKeys(allowedMap), sliceutil.SortedKeys(blockedMap)
}

// buildAccessLogSummary aggregates access log data across all runs
func buildAccessLogSummary(processedRuns []ProcessedRun) *AccessLogSummary {
	byWorkflow := make(map[string]*DomainAnalysis)

	// Use shared aggregation helper
	agg := aggregateDomainStats(processedRuns, func(pr *ProcessedRun) ([]string, []string, int, int, int, bool) {
		if pr.AccessAnalysis == nil {
			return nil, nil, 0, 0, 0, false
		}
		byWorkflow[pr.Run.WorkflowName] = pr.AccessAnalysis
		return pr.AccessAnalysis.AllowedDomains,
			pr.AccessAnalysis.BlockedDomains,
			pr.AccessAnalysis.TotalRequests,
			pr.AccessAnalysis.AllowedRequests,
			pr.AccessAnalysis.BlockedRequests,
			true
	})

	if agg.totalRequests == 0 {
		return nil
	}

	allowedDomains, blockedDomains := convertDomainsToSortedSlices(agg.allAllowedDomains, agg.allBlockedDomains)

	return &AccessLogSummary{
		FirewallSummaryBase: FirewallSummaryBase{
			TotalRequests:   agg.totalRequests,
			AllowedRequests: agg.allowedCount,
			BlockedRequests: agg.blockedCount,
			AllowedDomains:  allowedDomains,
			BlockedDomains:  blockedDomains,
		},
		ByWorkflow: byWorkflow,
	}
}

// buildFirewallLogSummary aggregates firewall log data across all runs
func buildFirewallLogSummary(processedRuns []ProcessedRun) *FirewallLogSummary {
	allRequestsByDomain := make(map[string]DomainRequestStats)
	byWorkflow := make(map[string]*FirewallAnalysis)

	// Use shared aggregation helper
	agg := aggregateDomainStats(processedRuns, func(pr *ProcessedRun) ([]string, []string, int, int, int, bool) {
		if pr.FirewallAnalysis == nil {
			return nil, nil, 0, 0, 0, false
		}
		byWorkflow[pr.Run.WorkflowName] = pr.FirewallAnalysis

		// Aggregate request stats by domain (firewall-specific)
		for domain, stats := range pr.FirewallAnalysis.RequestsByDomain {
			existing := allRequestsByDomain[domain]
			existing.Allowed += stats.Allowed
			existing.Blocked += stats.Blocked
			allRequestsByDomain[domain] = existing
		}

		return pr.FirewallAnalysis.AllowedDomains,
			pr.FirewallAnalysis.BlockedDomains,
			pr.FirewallAnalysis.TotalRequests,
			pr.FirewallAnalysis.AllowedRequests,
			pr.FirewallAnalysis.BlockedRequests,
			true
	})

	if agg.totalRequests == 0 {
		return nil
	}

	allowedDomains, blockedDomains := convertDomainsToSortedSlices(agg.allAllowedDomains, agg.allBlockedDomains)
	domains := make([]FirewallDomainRecord, 0, len(allRequestsByDomain))
	for _, domain := range sliceutil.SortedKeys(allRequestsByDomain) {
		stats := allRequestsByDomain[domain]
		if stats.Allowed > 0 {
			domains = append(domains, FirewallDomainRecord{Domain: domain, Accepted: true, Arity: stats.Allowed})
		}
		if stats.Blocked > 0 {
			domains = append(domains, FirewallDomainRecord{Domain: domain, Accepted: false, Arity: stats.Blocked})
		}
	}

	return &FirewallLogSummary{
		FirewallSummaryBase: FirewallSummaryBase{
			TotalRequests:   agg.totalRequests,
			AllowedRequests: agg.allowedCount,
			BlockedRequests: agg.blockedCount,
			AllowedDomains:  allowedDomains,
			BlockedDomains:  blockedDomains,
		},
		RequestsByDomain: allRequestsByDomain,
		Domains:          domains,
		ByWorkflow:       byWorkflow,
	}
}

// buildRedactedDomainsSummary aggregates redacted domains data across all runs
func buildRedactedDomainsSummary(processedRuns []ProcessedRun) *RedactedDomainsLogSummary {
	allDomainsSet := make(map[string]struct {
	})
	byWorkflow := make(map[string]*RedactedDomainsAnalysis)
	hasData := false

	for _, pr := range processedRuns {
		if pr.RedactedDomainsAnalysis == nil {
			continue
		}
		hasData = true
		byWorkflow[pr.Run.WorkflowName] = pr.RedactedDomainsAnalysis

		// Collect all unique domains
		for _, domain := range pr.RedactedDomainsAnalysis.Domains {
			allDomainsSet[domain] = struct {
			}{}
		}
	}

	if !hasData {
		return nil
	}

	return &RedactedDomainsLogSummary{
		TotalDomains: len(allDomainsSet),
		Domains:      sliceutil.SortedKeys(allDomainsSet),
		ByWorkflow:   byWorkflow,
	}
}
