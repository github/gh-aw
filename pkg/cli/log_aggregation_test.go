//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// Test that DomainAnalysis implements LogAnalysis interface
func TestDomainAnalysisImplementsLogAnalysis(t *testing.T) {
	var _ LogAnalysis = (*DomainAnalysis)(nil)
}

// Test that FirewallAnalysis implements LogAnalysis interface
func TestFirewallAnalysisImplementsLogAnalysis(t *testing.T) {
	var _ LogAnalysis = (*FirewallAnalysis)(nil)
}

func TestDomainAnalysisGettersSetters(t *testing.T) {
	analysis := &DomainAnalysis{
		DomainBuckets: DomainBuckets{
			AllowedDomains: []string{"example.com", "test.com"},
			BlockedDomains: []string{"blocked.com"},
		},
	}

	// Test getters
	allowed := analysis.GetAllowedDomains()
	if len(allowed) != 2 {
		t.Errorf("Expected 2 allowed domains, got %d", len(allowed))
	}

	denied := analysis.GetBlockedDomains()
	if len(denied) != 1 {
		t.Errorf("Expected 1 blocked domain, got %d", len(denied))
	}

	// Test setters
	newAllowed := []string{"new1.com", "new2.com", "new3.com"}
	analysis.SetAllowedDomains(newAllowed)
	if len(analysis.AllowedDomains) != 3 {
		t.Errorf("Expected 3 allowed domains after set, got %d", len(analysis.AllowedDomains))
	}

	newDenied := []string{"denied1.com", "denied2.com"}
	analysis.SetBlockedDomains(newDenied)
	if len(analysis.BlockedDomains) != 2 {
		t.Errorf("Expected 2 blocked domains after set, got %d", len(analysis.BlockedDomains))
	}
}

func TestDomainAnalysisAddMetrics(t *testing.T) {
	analysis1 := &DomainAnalysis{
		TotalRequests: 10,
		AllowedCount:  6,
		BlockedCount:  4,
	}

	analysis2 := &DomainAnalysis{
		TotalRequests: 5,
		AllowedCount:  3,
		BlockedCount:  2,
	}

	analysis1.AddMetrics(analysis2)

	if analysis1.TotalRequests != 15 {
		t.Errorf("Expected TotalRequests 15, got %d", analysis1.TotalRequests)
	}
	if analysis1.AllowedCount != 9 {
		t.Errorf("Expected AllowedCount 9, got %d", analysis1.AllowedCount)
	}
	if analysis1.BlockedCount != 6 {
		t.Errorf("Expected BlockedCount 6, got %d", analysis1.BlockedCount)
	}
}

func TestFirewallAnalysisGettersSetters(t *testing.T) {
	analysis := &FirewallAnalysis{
		DomainBuckets: DomainBuckets{
			AllowedDomains: []string{"api.github.com:443", "api.npmjs.org:443"},
			BlockedDomains: []string{"blocked.example.com:443"},
		},
		RequestsByDomain: make(map[string]DomainRequestStats),
	}

	// Test getters
	allowed := analysis.GetAllowedDomains()
	if len(allowed) != 2 {
		t.Errorf("Expected 2 allowed domains, got %d", len(allowed))
	}

	denied := analysis.GetBlockedDomains()
	if len(denied) != 1 {
		t.Errorf("Expected 1 blocked domain, got %d", len(denied))
	}

	// Test setters
	newAllowed := []string{"new1.com:443", "new2.com:443"}
	analysis.SetAllowedDomains(newAllowed)
	if len(analysis.AllowedDomains) != 2 {
		t.Errorf("Expected 2 allowed domains after set, got %d", len(analysis.AllowedDomains))
	}

	newDenied := []string{"denied1.com:443"}
	analysis.SetBlockedDomains(newDenied)
	if len(analysis.BlockedDomains) != 1 {
		t.Errorf("Expected 1 blocked domain after set, got %d", len(analysis.BlockedDomains))
	}
}

func TestFirewallAnalysisAddMetrics(t *testing.T) {
	analysis1 := &FirewallAnalysis{
		TotalRequests:   10,
		AllowedRequests: 6,
		BlockedRequests: 4,
		RequestsByDomain: map[string]DomainRequestStats{
			"api.github.com:443": {Allowed: 3, Blocked: 1},
		},
	}

	analysis2 := &FirewallAnalysis{
		TotalRequests:   5,
		AllowedRequests: 3,
		BlockedRequests: 2,
		RequestsByDomain: map[string]DomainRequestStats{
			"api.github.com:443": {Allowed: 2, Blocked: 0},
			"api.npmjs.org:443":  {Allowed: 1, Blocked: 2},
		},
	}

	analysis1.AddMetrics(analysis2)

	if analysis1.TotalRequests != 15 {
		t.Errorf("Expected TotalRequests 15, got %d", analysis1.TotalRequests)
	}
	if analysis1.AllowedRequests != 9 {
		t.Errorf("Expected AllowedRequests 9, got %d", analysis1.AllowedRequests)
	}
	if analysis1.BlockedRequests != 6 {
		t.Errorf("Expected BlockedRequests 6, got %d", analysis1.BlockedRequests)
	}

	// Check merged domain stats
	stats := analysis1.RequestsByDomain["api.github.com:443"]
	if stats.Allowed != 5 {
		t.Errorf("Expected api.github.com:443 Allowed 5, got %d", stats.Allowed)
	}
	if stats.Blocked != 1 {
		t.Errorf("Expected api.github.com:443 Denied 1, got %d", stats.Blocked)
	}

	npmStats := analysis1.RequestsByDomain["api.npmjs.org:443"]
	if npmStats.Allowed != 1 {
		t.Errorf("Expected api.npmjs.org:443 Allowed 1, got %d", npmStats.Allowed)
	}
	if npmStats.Blocked != 2 {
		t.Errorf("Expected api.npmjs.org:443 Denied 2, got %d", npmStats.Blocked)
	}
}

func TestAggregateLogFilesWithAccessLogs(t *testing.T) {
	// Create a temporary directory for the test
	tempDir := testutil.TempDir(t, "test-*")
	accessLogsDir := filepath.Join(tempDir, "access.log")
	err := os.MkdirAll(accessLogsDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create access.log directory: %v", err)
	}

	// Create test access log content for multiple files
	fetchLogContent := `1701234567.123    180 192.168.1.100 TCP_MISS/200 1234 GET http://example.com/api/data - HIER_DIRECT/93.184.216.34 text/html
1701234568.456    250 192.168.1.100 TCP_HIT/200 5678 GET http://api.github.com/repos - HIER_DIRECT/140.82.112.6 application/json`

	browserLogContent := `1701234569.789    120 192.168.1.100 TCP_DENIED/403 0 CONNECT github.com:443 - HIER_NONE/- -
1701234570.012    0 192.168.1.100 TCP_DENIED/403 0 GET http://malicious.site/evil - HIER_NONE/- -`

	// Write separate log files
	fetchLogPath := filepath.Join(accessLogsDir, "access-fetch.log")
	err = os.WriteFile(fetchLogPath, []byte(fetchLogContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test access-fetch.log: %v", err)
	}

	browserLogPath := filepath.Join(accessLogsDir, "access-browser.log")
	err = os.WriteFile(browserLogPath, []byte(browserLogContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test access-browser.log: %v", err)
	}

	// Test aggregation using the helper
	analysis, err := aggregateLogFiles(
		accessLogsDir,
		"access-*.log",
		false,
		parseSquidAccessLog,
		func() *DomainAnalysis {
			return &DomainAnalysis{
				DomainBuckets: DomainBuckets{
					AllowedDomains: []string{},
					BlockedDomains: []string{},
				},
			}
		},
	)

	if err != nil {
		t.Fatalf("Failed to aggregate access logs: %v", err)
	}

	// Verify aggregated results
	if analysis.TotalRequests != 4 {
		t.Errorf("Expected 4 total requests, got %d", analysis.TotalRequests)
	}

	if analysis.AllowedCount != 2 {
		t.Errorf("Expected 2 allowed requests, got %d", analysis.AllowedCount)
	}

	if analysis.BlockedCount != 2 {
		t.Errorf("Expected 2 denied requests, got %d", analysis.BlockedCount)
	}

	// Check allowed domains
	expectedAllowed := 2
	if len(analysis.AllowedDomains) != expectedAllowed {
		t.Errorf("Expected %d allowed domains, got %d", expectedAllowed, len(analysis.AllowedDomains))
	}

	// Check blocked domains
	expectedDenied := 2
	if len(analysis.BlockedDomains) != expectedDenied {
		t.Errorf("Expected %d blocked domains, got %d", expectedDenied, len(analysis.BlockedDomains))
	}
}

func TestAggregateLogFilesWithFirewallLogs(t *testing.T) {
	// Create a temporary directory for the test
	tempDir := testutil.TempDir(t, "test-*")
	logsDir := filepath.Join(tempDir, "firewall-logs")
	err := os.MkdirAll(logsDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create firewall-logs directory: %v", err)
	}

	// Create test log content for multiple files
	log1Content := `1761332530.474 172.30.0.20:35288 api.github.com:443 140.82.112.22:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.github.com:443 "-"
1761332531.123 172.30.0.20:35289 allowed.example.com:443 140.82.112.23:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT allowed.example.com:443 "-"`

	log2Content := `1761332532.456 172.30.0.20:35290 blocked.example.com:443 140.82.112.24:443 1.1 CONNECT 403 NONE_NONE:HIER_NONE blocked.example.com:443 "-"
1761332533.789 172.30.0.20:35291 denied.test.com:443 140.82.112.25:443 1.1 CONNECT 403 TCP_DENIED:HIER_NONE denied.test.com:443 "-"`

	// Write separate log files
	log1Path := filepath.Join(logsDir, "firewall-1.log")
	err = os.WriteFile(log1Path, []byte(log1Content), 0644)
	if err != nil {
		t.Fatalf("Failed to create test firewall-1.log: %v", err)
	}

	log2Path := filepath.Join(logsDir, "firewall-2.log")
	err = os.WriteFile(log2Path, []byte(log2Content), 0644)
	if err != nil {
		t.Fatalf("Failed to create test firewall-2.log: %v", err)
	}

	// Test aggregation using the helper
	analysis, err := aggregateLogFiles(
		logsDir,
		"*.log",
		false,
		parseFirewallLog,
		func() *FirewallAnalysis {
			return &FirewallAnalysis{
				DomainBuckets: DomainBuckets{
					AllowedDomains: []string{},
					BlockedDomains: []string{},
				},
				RequestsByDomain: make(map[string]DomainRequestStats),
			}
		},
	)

	if err != nil {
		t.Fatalf("Failed to aggregate firewall logs: %v", err)
	}

	// Verify aggregated results
	if analysis.TotalRequests != 4 {
		t.Errorf("TotalRequests: got %d, want 4", analysis.TotalRequests)
	}

	if analysis.AllowedRequests != 2 {
		t.Errorf("AllowedRequests: got %d, want 2", analysis.AllowedRequests)
	}

	if analysis.BlockedRequests != 2 {
		t.Errorf("BlockedRequests: got %d, want 2", analysis.BlockedRequests)
	}

	// Check domains
	expectedAllowed := 2
	if len(analysis.AllowedDomains) != expectedAllowed {
		t.Errorf("AllowedDomains count: got %d, want %d", len(analysis.AllowedDomains), expectedAllowed)
	}

	expectedDenied := 2
	if len(analysis.BlockedDomains) != expectedDenied {
		t.Errorf("BlockedDomains count: got %d, want %d", len(analysis.BlockedDomains), expectedDenied)
	}
}

func TestAggregateLogFilesNoFiles(t *testing.T) {
	// Create a temporary directory with no log files
	tempDir := testutil.TempDir(t, "test-*")

	// Test with access logs
	analysis, err := aggregateLogFiles(
		tempDir,
		"access-*.log",
		false,
		parseSquidAccessLog,
		func() *DomainAnalysis {
			return &DomainAnalysis{
				DomainBuckets: DomainBuckets{
					AllowedDomains: []string{},
					BlockedDomains: []string{},
				},
			}
		},
	)

	if err != nil {
		t.Fatalf("Expected no error for empty directory, got %v", err)
	}

	if analysis != nil {
		t.Errorf("Expected nil analysis for no files, got %+v", analysis)
	}
}

func TestAggregateLogFilesWithParseErrors(t *testing.T) {
	// Create a temporary directory for the test
	tempDir := testutil.TempDir(t, "test-*")
	logsDir := filepath.Join(tempDir, "logs")
	err := os.MkdirAll(logsDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create logs directory: %v", err)
	}

	// Create a valid log file
	validLogContent := `1701234567.123    180 192.168.1.100 TCP_MISS/200 1234 GET http://example.com/api/data - HIER_DIRECT/93.184.216.34 text/html`
	validLogPath := filepath.Join(logsDir, "access-valid.log")
	err = os.WriteFile(validLogPath, []byte(validLogContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create valid log file: %v", err)
	}

	// Create an invalid log file (malformed content)
	invalidLogPath := filepath.Join(logsDir, "access-invalid.log")
	err = os.WriteFile(invalidLogPath, []byte("invalid log content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create invalid log file: %v", err)
	}

	// Test aggregation - should skip invalid file and continue
	analysis, err := aggregateLogFiles(
		logsDir,
		"access-*.log",
		false,
		parseSquidAccessLog,
		func() *DomainAnalysis {
			return &DomainAnalysis{
				DomainBuckets: DomainBuckets{
					AllowedDomains: []string{},
					BlockedDomains: []string{},
				},
			}
		},
	)

	if err != nil {
		t.Fatalf("Failed to aggregate logs with errors: %v", err)
	}

	// Should have aggregated the valid file
	if analysis.TotalRequests != 1 {
		t.Errorf("Expected 1 total request from valid file, got %d", analysis.TotalRequests)
	}
}
