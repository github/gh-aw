package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/stringutil"
)

var accessLogLog = logger.New("cli:access_log")

// AccessLogEntry represents a parsed squid access log entry
type AccessLogEntry struct {
	Timestamp string
	Duration  string
	ClientIP  string
	Status    string
	Size      string
	Method    string
	URL       string
	User      string
	Hierarchy string
	Type      string
}

// DomainAnalysis represents analysis of domains from access logs
type DomainAnalysis struct {
	AnalysisBase
}

// domainAnalysisWire is the stable JSON schema for DomainAnalysis.
// It preserves the original "allowed_count"/"blocked_count" field names so that
// cached RunSummary.access_analysis JSON and AccessLogSummary.by_workflow values
// remain backward-compatible after the AnalysisBase refactor, which renamed those
// fields to AllowedRequests/BlockedRequests internally.
type domainAnalysisWire struct {
	TotalRequests  int      `json:"total_requests"`
	AllowedCount   int      `json:"allowed_count"`
	BlockedCount   int      `json:"blocked_count"`
	AllowedDomains []string `json:"allowed_domains,omitempty"`
	BlockedDomains []string `json:"blocked_domains,omitempty"`
}

// MarshalJSON emits the original "allowed_count"/"blocked_count" wire names so
// existing consumers of the access-analysis JSON do not see a silent field rename.
func (d DomainAnalysis) MarshalJSON() ([]byte, error) {
	return json.Marshal(domainAnalysisWire{
		TotalRequests:  d.TotalRequests,
		AllowedCount:   d.AllowedRequests,
		BlockedCount:   d.BlockedRequests,
		AllowedDomains: d.AllowedDomains,
		BlockedDomains: d.BlockedDomains,
	})
}

// UnmarshalJSON accepts the original "allowed_count"/"blocked_count" wire names,
// keeping round-trip compatibility with cached JSON produced before the refactor.
func (d *DomainAnalysis) UnmarshalJSON(data []byte) error {
	var wire domainAnalysisWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	d.TotalRequests = wire.TotalRequests
	d.AllowedRequests = wire.AllowedCount
	d.BlockedRequests = wire.BlockedCount
	d.AllowedDomains = wire.AllowedDomains
	d.BlockedDomains = wire.BlockedDomains
	return nil
}

// AddMetrics adds metrics from another analysis
func (d *DomainAnalysis) AddMetrics(other LogAnalysis) {
	if otherDomain, ok := other.(*DomainAnalysis); ok {
		d.addBaseMetrics(&otherDomain.AnalysisBase)
	}
}

// parseSquidAccessLog parses a squid access log file and extracts domain information
func parseSquidAccessLog(logPath string, verbose bool) (*DomainAnalysis, error) {
	accessLogLog.Printf("Parsing squid access log: %s", logPath)

	file, err := os.Open(logPath)
	if err != nil {
		accessLogLog.Printf("Failed to open access log %s: %v", logPath, err)
		return nil, fmt.Errorf("failed to open access log: %w", err)
	}
	defer file.Close()

	analysis := &DomainAnalysis{}

	allowedDomainsSet := make(map[string]struct {
	})
	blockedDomainsSet := make(map[string]struct {
	})

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		processSquidAccessLogLine(strings.TrimSpace(scanner.Text()), verbose, analysis, allowedDomainsSet, blockedDomainsSet)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading access log: %w", err)
	}

	// Sort domains for consistent output
	sort.Strings(analysis.AllowedDomains)
	sort.Strings(analysis.BlockedDomains)

	accessLogLog.Printf("Parsed access log: total_requests=%d, allowed=%d, blocked=%d, unique_allowed_domains=%d, unique_blocked_domains=%d",
		analysis.TotalRequests, analysis.AllowedRequests, analysis.BlockedRequests, len(analysis.AllowedDomains), len(analysis.BlockedDomains))

	return analysis, nil
}

func processSquidAccessLogLine(line string, verbose bool, analysis *DomainAnalysis, allowedDomainsSet, blockedDomainsSet map[string]struct{}) {
	if line == "" || strings.HasPrefix(line, "#") {
		return
	}

	entry, err := parseSquidLogLine(line)
	if err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to parse log line: %v", err)))
		}
		return
	}

	analysis.TotalRequests++
	domain := stringutil.ExtractDomainFromURL(entry.URL)
	if domain == "" {
		return
	}

	if isAllowedSquidStatus(entry.Status) {
		analysis.AllowedRequests++
		addUniqueDomain(allowedDomainsSet, domain, &analysis.AllowedDomains)
		return
	}

	analysis.BlockedRequests++
	addUniqueDomain(blockedDomainsSet, domain, &analysis.BlockedDomains)
}

func isAllowedSquidStatus(statusCode string) bool {
	return statusCode == "TCP_HIT/200" || statusCode == "TCP_MISS/200" ||
		statusCode == "TCP_REFRESH_MODIFIED/200" || statusCode == "TCP_IMS_HIT/304" ||
		strings.Contains(statusCode, "/200") || strings.Contains(statusCode, "/206") ||
		strings.Contains(statusCode, "/304")
}

func addUniqueDomain(domainSet map[string]struct{}, domain string, domains *[]string) {
	if setutil.Contains(domainSet, domain) {
		return
	}
	domainSet[domain] = struct{}{}
	*domains = append(*domains, domain)
}

// parseSquidLogLine parses a single squid access log line
// Squid log format: timestamp duration client status size method url user hierarchy type
func parseSquidLogLine(line string) (*AccessLogEntry, error) {
	fields := strings.Fields(line)
	if len(fields) < 10 {
		return nil, fmt.Errorf("invalid log line format: expected at least 10 fields, got %d", len(fields))
	}

	return &AccessLogEntry{
		Timestamp: fields[0],
		Duration:  fields[1],
		ClientIP:  fields[2],
		Status:    fields[3],
		Size:      fields[4],
		Method:    fields[5],
		URL:       fields[6],
		User:      fields[7],
		Hierarchy: fields[8],
		Type:      fields[9],
	}, nil
}

// analyzeAccessLogs analyzes access logs in a run directory
func analyzeAccessLogs(runDir string, verbose bool) (*DomainAnalysis, error) {
	accessLogLog.Printf("Analyzing access logs in: %s", runDir)

	// Check for access log files in access.log directory (legacy path)
	accessLogsDir := filepath.Join(runDir, "access.log")
	if _, err := os.Stat(accessLogsDir); err == nil {
		accessLogLog.Printf("Found access logs directory: %s", accessLogsDir)
		return analyzeMultipleAccessLogs(accessLogsDir, verbose)
	}

	// Check for access logs in sandbox/firewall/logs/ directory (new path after artifact download)
	// Firewall logs are uploaded from /tmp/gh-aw/sandbox/firewall/logs/ and the common parent
	// /tmp/gh-aw/ is stripped during artifact upload, resulting in sandbox/firewall/logs/ after download
	sandboxFirewallLogsDir := filepath.Join(runDir, "sandbox", "firewall", "logs")
	if _, err := os.Stat(sandboxFirewallLogsDir); err == nil {
		accessLogLog.Printf("Found firewall logs directory: %s", sandboxFirewallLogsDir)
		return analyzeMultipleAccessLogs(sandboxFirewallLogsDir, verbose)
	}

	// No access logs found
	accessLogLog.Printf("No access logs directory found in: %s", runDir)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No access logs found in "+runDir))
	}
	return nil, nil
}

// analyzeMultipleAccessLogs analyzes multiple separate access log files
func analyzeMultipleAccessLogs(accessLogsDir string, verbose bool) (*DomainAnalysis, error) {
	return aggregateLogFiles(
		accessLogsDir,
		"access-*.log",
		verbose,
		parseSquidAccessLog,
		func() *DomainAnalysis {
			return &DomainAnalysis{}
		},
	)
}

// formatDomainWithEcosystem formats a domain with its ecosystem identifier if found
