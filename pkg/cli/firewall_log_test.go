//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"

	"github.com/github/gh-aw/pkg/workflow"
)

func TestParseFirewallLogLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		line     string
		expected *FirewallLogEntry
	}{
		{
			name: "valid log line with all fields",
			line: `1761332530.474 172.30.0.20:35288 api.enterprise.githubcopilot.com:443 140.82.112.22:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.enterprise.githubcopilot.com:443 "-"`,
			expected: &FirewallLogEntry{
				Timestamp:    "1761332530.474",
				ClientIPPort: "172.30.0.20:35288",
				Domain:       "api.enterprise.githubcopilot.com:443",
				DestIPPort:   "140.82.112.22:443",
				Proto:        "1.1",
				Method:       "CONNECT",
				Status:       "200",
				Decision:     "TCP_TUNNEL:HIER_DIRECT",
				URL:          "api.enterprise.githubcopilot.com:443",
				UserAgent:    "-",
			},
		},
		{
			name: "log line with placeholder values",
			line: `1761332530.500 - - - - - 0 NONE_NONE:HIER_NONE - "-"`,
			expected: &FirewallLogEntry{
				Timestamp:    "1761332530.500",
				ClientIPPort: "-",
				Domain:       "-",
				DestIPPort:   "-",
				Proto:        "-",
				Method:       "-",
				Status:       "0",
				Decision:     "NONE_NONE:HIER_NONE",
				URL:          "-",
				UserAgent:    "-",
			},
		},
		{
			name:     "empty line",
			line:     "",
			expected: nil,
		},
		{
			name:     "comment line",
			line:     "# This is a comment",
			expected: nil,
		},
		{
			name:     "invalid timestamp (non-numeric)",
			line:     `WARNING: 172.30.0.20:35288 api.github.com:443 140.82.112.22:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.github.com:443 "-"`,
			expected: nil,
		},
		{
			name: "non-standard client IP:port format is accepted",
			line: `1761332530.474 Accepting api.github.com:443 140.82.112.22:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.github.com:443 "-"`,
			expected: &FirewallLogEntry{
				Timestamp:    "1761332530.474",
				ClientIPPort: "Accepting",
				Domain:       "api.github.com:443",
				DestIPPort:   "140.82.112.22:443",
				Proto:        "1.1",
				Method:       "CONNECT",
				Status:       "200",
				Decision:     "TCP_TUNNEL:HIER_DIRECT",
				URL:          "api.github.com:443",
				UserAgent:    "-",
			},
		},
		{
			name: "non-standard domain format is accepted",
			line: `1761332530.474 172.30.0.20:35288 DNS 140.82.112.22:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.github.com:443 "-"`,
			expected: &FirewallLogEntry{
				Timestamp:    "1761332530.474",
				ClientIPPort: "172.30.0.20:35288",
				Domain:       "DNS",
				DestIPPort:   "140.82.112.22:443",
				Proto:        "1.1",
				Method:       "CONNECT",
				Status:       "200",
				Decision:     "TCP_TUNNEL:HIER_DIRECT",
				URL:          "api.github.com:443",
				UserAgent:    "-",
			},
		},
		{
			name: "non-standard dest IP:port format is accepted",
			line: `1761332530.474 172.30.0.20:35288 api.github.com:443 Local 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.github.com:443 "-"`,
			expected: &FirewallLogEntry{
				Timestamp:    "1761332530.474",
				ClientIPPort: "172.30.0.20:35288",
				Domain:       "api.github.com:443",
				DestIPPort:   "Local",
				Proto:        "1.1",
				Method:       "CONNECT",
				Status:       "200",
				Decision:     "TCP_TUNNEL:HIER_DIRECT",
				URL:          "api.github.com:443",
				UserAgent:    "-",
			},
		},
		{
			name: "non-numeric status code is accepted",
			line: `1761332530.474 172.30.0.20:35288 api.github.com:443 140.82.112.22:443 1.1 CONNECT Swap TCP_TUNNEL:HIER_DIRECT api.github.com:443 "-"`,
			expected: &FirewallLogEntry{
				Timestamp:    "1761332530.474",
				ClientIPPort: "172.30.0.20:35288",
				Domain:       "api.github.com:443",
				DestIPPort:   "140.82.112.22:443",
				Proto:        "1.1",
				Method:       "CONNECT",
				Status:       "Swap",
				Decision:     "TCP_TUNNEL:HIER_DIRECT",
				URL:          "api.github.com:443",
				UserAgent:    "-",
			},
		},
		{
			name: "decision format without colon is accepted",
			line: `1761332530.474 172.30.0.20:35288 api.github.com:443 140.82.112.22:443 1.1 CONNECT 200 Waiting api.github.com:443 "-"`,
			expected: &FirewallLogEntry{
				Timestamp:    "1761332530.474",
				ClientIPPort: "172.30.0.20:35288",
				Domain:       "api.github.com:443",
				DestIPPort:   "140.82.112.22:443",
				Proto:        "1.1",
				Method:       "CONNECT",
				Status:       "200",
				Decision:     "Waiting",
				URL:          "api.github.com:443",
				UserAgent:    "-",
			},
		},
		{
			name:     "fewer than 10 fields",
			line:     `WARNING: Something went wrong`,
			expected: nil,
		},
		{
			name: "line with pipe character in domain position is accepted",
			line: `1761332530.474 172.30.0.20:35288 pinger|test 140.82.112.22:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.github.com:443 "-"`,
			expected: &FirewallLogEntry{
				Timestamp:    "1761332530.474",
				ClientIPPort: "172.30.0.20:35288",
				Domain:       "pinger|test",
				DestIPPort:   "140.82.112.22:443",
				Proto:        "1.1",
				Method:       "CONNECT",
				Status:       "200",
				Decision:     "TCP_TUNNEL:HIER_DIRECT",
				URL:          "api.github.com:443",
				UserAgent:    "-",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := parseFirewallLogLine(tt.line)

			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %+v", result)
				}
				return
			}

			if result == nil {
				t.Fatalf("expected result, got nil")
			}

			if result.Timestamp != tt.expected.Timestamp {
				t.Errorf("Timestamp: got %q, want %q", result.Timestamp, tt.expected.Timestamp)
			}
			if result.ClientIPPort != tt.expected.ClientIPPort {
				t.Errorf("ClientIPPort: got %q, want %q", result.ClientIPPort, tt.expected.ClientIPPort)
			}
			if result.Domain != tt.expected.Domain {
				t.Errorf("Domain: got %q, want %q", result.Domain, tt.expected.Domain)
			}
			if result.DestIPPort != tt.expected.DestIPPort {
				t.Errorf("DestIPPort: got %q, want %q", result.DestIPPort, tt.expected.DestIPPort)
			}
			if result.Proto != tt.expected.Proto {
				t.Errorf("Proto: got %q, want %q", result.Proto, tt.expected.Proto)
			}
			if result.Method != tt.expected.Method {
				t.Errorf("Method: got %q, want %q", result.Method, tt.expected.Method)
			}
			if result.Status != tt.expected.Status {
				t.Errorf("Status: got %q, want %q", result.Status, tt.expected.Status)
			}
			if result.Decision != tt.expected.Decision {
				t.Errorf("Decision: got %q, want %q", result.Decision, tt.expected.Decision)
			}
			if result.URL != tt.expected.URL {
				t.Errorf("URL: got %q, want %q", result.URL, tt.expected.URL)
			}
			if result.UserAgent != tt.expected.UserAgent {
				t.Errorf("UserAgent: got %q, want %q", result.UserAgent, tt.expected.UserAgent)
			}
		})
	}
}

func TestIsRequestAllowed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		decision string
		status   string
		expected bool
	}{
		{
			name:     "status 200",
			decision: "TCP_TUNNEL:HIER_DIRECT",
			status:   "200",
			expected: true,
		},
		{
			name:     "status 206",
			decision: "TCP_TUNNEL:HIER_DIRECT",
			status:   "206",
			expected: true,
		},
		{
			name:     "status 304",
			decision: "TCP_TUNNEL:HIER_DIRECT",
			status:   "304",
			expected: true,
		},
		{
			name:     "status 403",
			decision: "NONE_NONE:HIER_NONE",
			status:   "403",
			expected: false,
		},
		{
			name:     "status 407",
			decision: "NONE_NONE:HIER_NONE",
			status:   "407",
			expected: false,
		},
		{
			name:     "TCP_TUNNEL decision",
			decision: "TCP_TUNNEL:HIER_DIRECT",
			status:   "0",
			expected: true,
		},
		{
			name:     "TCP_HIT decision",
			decision: "TCP_HIT:HIER_DIRECT",
			status:   "0",
			expected: true,
		},
		{
			name:     "TCP_MISS decision",
			decision: "TCP_MISS:HIER_DIRECT",
			status:   "0",
			expected: true,
		},
		{
			name:     "NONE_NONE decision",
			decision: "NONE_NONE:HIER_NONE",
			status:   "0",
			expected: false,
		},
		{
			name:     "TCP_DENIED decision",
			decision: "TCP_DENIED:HIER_NONE",
			status:   "0",
			expected: false,
		},
		{
			name:     "unknown decision and status",
			decision: "UNKNOWN:HIER_UNKNOWN",
			status:   "500",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := isRequestAllowed(tt.decision, tt.status)
			if result != tt.expected {
				t.Errorf("isRequestAllowed(%q, %q) = %v, want %v", tt.decision, tt.status, result, tt.expected)
			}
		})
	}
}

func TestParseFirewallLog(t *testing.T) {
	t.Parallel()
	// Create a temporary directory for the test
	tempDir := testutil.TempDir(t, "test-*")

	// Create test firewall log content
	testLogContent := `1761332530.474 172.30.0.20:35288 api.enterprise.githubcopilot.com:443 140.82.112.22:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.enterprise.githubcopilot.com:443 "-"
1761332531.123 172.30.0.20:35289 blocked.example.com:443 140.82.112.23:443 1.1 CONNECT 403 NONE_NONE:HIER_NONE blocked.example.com:443 "-"
1761332532.456 172.30.0.20:35290 api.github.com:443 140.82.112.6:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.github.com:443 "Mozilla/5.0"
1761332533.789 172.30.0.20:35291 denied.test.com:443 140.82.112.24:443 1.1 CONNECT 403 TCP_DENIED:HIER_NONE denied.test.com:443 "-"
# This is a comment line
`

	// Write test log file
	logPath := filepath.Join(tempDir, "firewall.log")
	err := os.WriteFile(logPath, []byte(testLogContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test firewall.log: %v", err)
	}

	// Test parsing
	analysis, err := parseFirewallLog(logPath, false)
	if err != nil {
		t.Fatalf("Failed to parse firewall log: %v", err)
	}

	// Verify results
	if analysis.TotalRequests != 4 {
		t.Errorf("TotalRequests: got %d, want 4", analysis.TotalRequests)
	}

	if analysis.AllowedRequests != 2 {
		t.Errorf("AllowedRequests: got %d, want 2", analysis.AllowedRequests)
	}

	if analysis.BlockedRequests != 2 {
		t.Errorf("BlockedRequests: got %d, want 2", analysis.BlockedRequests)
	}

	// Check allowed domains
	expectedAllowed := []string{"api.enterprise.githubcopilot.com:443", "api.github.com:443"}
	if len(analysis.AllowedDomains) != len(expectedAllowed) {
		t.Errorf("AllowedDomains count: got %d, want %d", len(analysis.AllowedDomains), len(expectedAllowed))
	}

	// Check blocked domains
	expectedDenied := []string{"blocked.example.com:443", "denied.test.com:443"}
	if len(analysis.BlockedDomains) != len(expectedDenied) {
		t.Errorf("BlockedDomains count: got %d, want %d", len(analysis.BlockedDomains), len(expectedDenied))
	}

	// Check request stats by domain
	if stats, ok := analysis.RequestsByDomain["api.github.com:443"]; ok {
		if stats.Allowed != 1 {
			t.Errorf("api.github.com:443 Allowed: got %d, want 1", stats.Allowed)
		}
		if stats.Blocked != 0 {
			t.Errorf("api.github.com:443 Blocked: got %d, want 0", stats.Blocked)
		}
	} else {
		t.Error("api.github.com:443 not found in RequestsByDomain")
	}

	if stats, ok := analysis.RequestsByDomain["blocked.example.com:443"]; ok {
		if stats.Allowed != 0 {
			t.Errorf("blocked.example.com:443 Allowed: got %d, want 0", stats.Allowed)
		}
		if stats.Blocked != 1 {
			t.Errorf("blocked.example.com:443 Blocked: got %d, want 1", stats.Blocked)
		}
	} else {
		t.Error("blocked.example.com:443 not found in RequestsByDomain")
	}
}

func TestParseFirewallLogMalformedLines(t *testing.T) {
	t.Parallel()
	// Create a temporary directory for the test
	tempDir := testutil.TempDir(t, "test-*")

	// Create test firewall log with various malformed lines
	testLogContent := `# Comment line
1761332530.474 172.30.0.20:35288 api.github.com:443 140.82.112.22:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.github.com:443 "-"
WARNING: Something went wrong
Invalid line with not enough fields
1761332531.123 INVALID_IP api.github.com:443 140.82.112.23:443 1.1 CONNECT 403 NONE_NONE:HIER_NONE api.github.com:443 "-"
1761332532.456 172.30.0.20:35290 api.npmjs.org:443 140.82.112.6:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.npmjs.org:443 "-"
`

	// Write test log file
	logPath := filepath.Join(tempDir, "firewall.log")
	err := os.WriteFile(logPath, []byte(testLogContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test firewall.log: %v", err)
	}

	// Test parsing - should only parse valid lines
	analysis, err := parseFirewallLog(logPath, false)
	if err != nil {
		t.Fatalf("Failed to parse firewall log: %v", err)
	}

	// Should have parsed 3 valid lines (relaxed validation accepts INVALID_IP like JavaScript parser)
	// Lines with valid timestamps and 10 fields are accepted, even if field formats are non-standard
	if analysis.TotalRequests != 3 {
		t.Errorf("TotalRequests: got %d, want 3 (non-standard formats accepted)", analysis.TotalRequests)
	}

	if analysis.AllowedRequests != 2 {
		t.Errorf("AllowedRequests: got %d, want 2", analysis.AllowedRequests)
	}

	if analysis.BlockedRequests != 1 {
		t.Errorf("BlockedRequests: got %d, want 1", analysis.BlockedRequests)
	}
}

func TestParseFirewallLogPartialMissingFields(t *testing.T) {
	t.Parallel()
	// Create a temporary directory for the test
	tempDir := testutil.TempDir(t, "test-*")

	// Create test firewall log with partial/missing fields
	testLogContent := `1761332530.474 172.30.0.20:35288 api.github.com:443 140.82.112.22:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.github.com:443 "-"
1761332531.123 - - - - - 0 NONE_NONE:HIER_NONE - "-"
1761332532.456 172.30.0.20:35290 test.example.com:443 - 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT test.example.com:443 "-"
`

	// Write test log file
	logPath := filepath.Join(tempDir, "firewall.log")
	err := os.WriteFile(logPath, []byte(testLogContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test firewall.log: %v", err)
	}

	// Test parsing
	analysis, err := parseFirewallLog(logPath, false)
	if err != nil {
		t.Fatalf("Failed to parse firewall log: %v", err)
	}

	// All 3 lines are valid (placeholders "-" are acceptable)
	if analysis.TotalRequests != 3 {
		t.Errorf("TotalRequests: got %d, want 3", analysis.TotalRequests)
	}

	// Check that placeholder domain "-" is tracked
	if stats, ok := analysis.RequestsByDomain["-"]; ok {
		if stats.Blocked != 1 {
			t.Errorf("Placeholder domain '-' Blocked: got %d, want 1", stats.Blocked)
		}
	}
}

func TestParseFirewallLogIptablesDropped(t *testing.T) {
	t.Parallel()
	// Create a temporary directory for the test
	tempDir := testutil.TempDir(t, "test-*")

	// Simulate iptables-dropped traffic: domain="-" but destIPPort has the actual destination.
	// This occurs when iptables drops packets before they reach the Squid proxy, so Squid
	// only sees the IP layer info and logs domain as "-".
	testLogContent := `1761332530.474 172.30.0.20:35288 api.github.com:443 140.82.112.22:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.github.com:443 "-"
1761332531.123 172.30.0.20:35289 - 8.8.8.8:53 - - 0 NONE_NONE:HIER_NONE - "-"
1761332532.456 172.30.0.20:35290 - 1.2.3.4:443 - - 0 NONE_NONE:HIER_NONE - "-"
1761332533.789 172.30.0.20:35291 - 1.2.3.4:443 - - 0 NONE_NONE:HIER_NONE - "-"
1761332534.012 172.30.0.20:35292 - - - - 0 NONE_NONE:HIER_NONE - "-"
`

	// Write test log file
	logPath := filepath.Join(tempDir, "firewall.log")
	err := os.WriteFile(logPath, []byte(testLogContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test firewall.log: %v", err)
	}

	// Test parsing
	analysis, err := parseFirewallLog(logPath, false)
	if err != nil {
		t.Fatalf("Failed to parse firewall log: %v", err)
	}

	if analysis.TotalRequests != 5 {
		t.Errorf("TotalRequests: got %d, want 5", analysis.TotalRequests)
	}
	if analysis.AllowedRequests != 1 {
		t.Errorf("AllowedRequests: got %d, want 1", analysis.AllowedRequests)
	}
	if analysis.BlockedRequests != 4 {
		t.Errorf("BlockedRequests: got %d, want 4", analysis.BlockedRequests)
	}

	// Iptables-dropped entries with destIPPort should use destIPPort as the key
	if stats, ok := analysis.RequestsByDomain["8.8.8.8:53"]; !ok {
		t.Error("8.8.8.8:53 should be in RequestsByDomain (iptables-dropped fallback)")
	} else if stats.Blocked != 1 {
		t.Errorf("8.8.8.8:53 Blocked: got %d, want 1", stats.Blocked)
	}

	if stats, ok := analysis.RequestsByDomain["1.2.3.4:443"]; !ok {
		t.Error("1.2.3.4:443 should be in RequestsByDomain (iptables-dropped fallback)")
	} else if stats.Blocked != 2 {
		t.Errorf("1.2.3.4:443 Blocked: got %d, want 2", stats.Blocked)
	}

	// Entries where both domain and destIPPort are "-" should use the unknownDomain sentinel.
	if stats, ok := analysis.RequestsByDomain[unknownDomain]; !ok {
		t.Errorf("%q should be in RequestsByDomain for truly-unknown entries", unknownDomain)
	} else if stats.Blocked != 1 {
		t.Errorf("%q Blocked: got %d, want 1", unknownDomain, stats.Blocked)
	}

	// "-" should NOT appear as a key in RequestsByDomain after the sentinel replacement.
	if _, ok := analysis.RequestsByDomain["-"]; ok {
		t.Error("\"-\" should not be in RequestsByDomain; it should be replaced by the unknownDomain sentinel")
	}

	// BlockedDomains should include the real IPs, not just "-"
	blockedSet := make(map[string]bool)
	for _, d := range analysis.BlockedDomains {
		blockedSet[d] = true
	}
	if !blockedSet["8.8.8.8:53"] {
		t.Error("BlockedDomains should contain 8.8.8.8:53 (iptables-dropped fallback)")
	}
	if !blockedSet["1.2.3.4:443"] {
		t.Error("BlockedDomains should contain 1.2.3.4:443 (iptables-dropped fallback)")
	}
	// Neither the "-" placeholder nor the unknownDomain sentinel should appear in BlockedDomains.
	if blockedSet["-"] {
		t.Error("BlockedDomains should NOT contain \"-\" placeholder")
	}
	if blockedSet[unknownDomain] {
		t.Errorf("BlockedDomains should NOT contain %q sentinel", unknownDomain)
	}
}

func TestParseFirewallLogUnknownAllowedDomain(t *testing.T) {
	t.Parallel()
	// Verify that when both domain and destIPPort are "-" and the request is classified as
	// allowed, the unknownDomain sentinel is excluded from AllowedDomains.
	// This is an unlikely but possible edge case (e.g. Squid internally marks a
	// packet as allowed before iptables drops it at the network layer).
	tempDir := testutil.TempDir(t, "test-*")

	// Status 200 + TCP_TUNNEL:HIER_DIRECT = allowed; domain and destIPPort both "-"
	testLogContent := `1761332530.474 172.30.0.20:35288 - - - - 200 TCP_TUNNEL:HIER_DIRECT - "-"
`
	logPath := filepath.Join(tempDir, "firewall.log")
	err := os.WriteFile(logPath, []byte(testLogContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test firewall.log: %v", err)
	}

	analysis, err := parseFirewallLog(logPath, false)
	if err != nil {
		t.Fatalf("Failed to parse firewall log: %v", err)
	}

	if analysis.AllowedRequests != 1 {
		t.Errorf("AllowedRequests: got %d, want 1", analysis.AllowedRequests)
	}

	// The unknownDomain sentinel should appear in RequestsByDomain but NOT in AllowedDomains.
	if stats, ok := analysis.RequestsByDomain[unknownDomain]; !ok {
		t.Errorf("%q should be in RequestsByDomain", unknownDomain)
	} else if stats.Allowed != 1 {
		t.Errorf("%q Allowed: got %d, want 1", unknownDomain, stats.Allowed)
	}

	for _, d := range analysis.AllowedDomains {
		if d == unknownDomain || d == "-" {
			t.Errorf("AllowedDomains should NOT contain %q placeholder/sentinel", d)
		}
	}
}

func TestAnalyzeMultipleFirewallLogs(t *testing.T) {
	t.Parallel()
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

	// Test analysis of multiple logs
	analysis, err := analyzeMultipleFirewallLogs(logsDir, false)
	if err != nil {
		t.Fatalf("Failed to analyze multiple firewall logs: %v", err)
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

func TestSanitizeWorkflowName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "lowercase conversion",
			input:    "MyWorkflow",
			expected: "myworkflow",
		},
		{
			name:     "spaces to dashes",
			input:    "My Workflow Name",
			expected: "my-workflow-name",
		},
		{
			name:     "colons to dashes",
			input:    "workflow:test",
			expected: "workflow-test",
		},
		{
			name:     "slashes to dashes",
			input:    "workflow/test",
			expected: "workflow-test",
		},
		{
			name:     "backslashes to dashes",
			input:    "workflow\\test",
			expected: "workflow-test",
		},
		{
			name:     "special characters to dashes",
			input:    "workflow@#$test",
			expected: "workflow-test",
		},
		{
			name:     "preserve dots and underscores",
			input:    "workflow.test_name",
			expected: "workflow.test_name",
		},
		{
			name:     "complex name",
			input:    "My Workflow: Test/Build",
			expected: "my-workflow-test-build",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := workflow.SanitizeWorkflowName(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeWorkflowName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestAnalyzeFirewallLogsWithWorkflowSuffix(t *testing.T) {
	t.Parallel()
	// Create a temporary directory structure that mimics actual workflow artifact layout
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a directory with workflow-specific suffix (like squid-logs-smoke-copilot-firewall)
	logsDir := filepath.Join(tmpDir, "squid-logs-smoke-copilot-firewall")
	err := os.MkdirAll(logsDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create logs directory: %v", err)
	}

	// Create a sample access.log file
	accessLog := filepath.Join(logsDir, "access.log")
	logContent := `1761332530.474 172.30.0.20:35288 api.enterprise.githubcopilot.com:443 140.82.112.22:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.enterprise.githubcopilot.com:443 "-"
1761332531.123 172.30.0.20:35289 blocked.example.com:443 140.82.112.23:443 1.1 CONNECT 403 NONE_NONE:HIER_NONE blocked.example.com:443 "-"
1761332532.456 172.30.0.20:35290 api.github.com:443 140.82.112.5:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.github.com:443 "-"
`
	err = os.WriteFile(accessLog, []byte(logContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write access.log: %v", err)
	}

	// Analyze the logs - this should find the squid-logs-* directory
	analysis, err := analyzeFirewallLogs(tmpDir, false)
	if err != nil {
		t.Fatalf("analyzeFirewallLogs failed: %v", err)
	}

	if analysis == nil {
		t.Fatal("Expected firewall analysis but got nil")
	}

	// Verify the analysis found our logs
	if analysis.TotalRequests != 3 {
		t.Errorf("TotalRequests: got %d, want 3", analysis.TotalRequests)
	}

	if analysis.AllowedRequests != 2 {
		t.Errorf("AllowedRequests: got %d, want 2", analysis.AllowedRequests)
	}

	if analysis.BlockedRequests != 1 {
		t.Errorf("BlockedRequests: got %d, want 1", analysis.BlockedRequests)
	}

	// Verify allowed domains
	expectedAllowed := map[string]bool{
		"api.enterprise.githubcopilot.com:443": true,
		"api.github.com:443":                   true,
	}
	for _, domain := range analysis.AllowedDomains {
		if !expectedAllowed[domain] {
			t.Errorf("Unexpected allowed domain: %s", domain)
		}
	}

	// Verify blocked domains
	if len(analysis.BlockedDomains) != 1 || analysis.BlockedDomains[0] != "blocked.example.com:443" {
		t.Errorf("BlockedDomains: got %v, want [blocked.example.com:443]", analysis.BlockedDomains)
	}
}

func TestParseFirewallLogInternalSquidErrorEntries(t *testing.T) {
	t.Parallel()
	// Create a temporary directory for the test
	tempDir := testutil.TempDir(t, "test-*")

	// Simulate internal Squid error entries interleaved with real traffic.
	// These internal entries (client IP ::1, domain "-", destIPPort "-:-") are internal
	// Squid connection errors (e.g., error:transaction-end-before-headers) and should be
	// filtered out entirely and not counted as blocked external requests.
	testLogContent := `1773003472.027 ::1:52010 - -:- 0.0 - 0 NONE_NONE:HIER_NONE error:transaction-end-before-headers "-"
1773003475.167 172.30.0.30:50232 api.anthropic.com:443 18.64.224.91:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.anthropic.com:443 "-"
1773003477.068 ::1:35712 - -:- 0.0 - 0 NONE_NONE:HIER_NONE error:transaction-end-before-headers "-"
1773003480.123 172.30.0.30:50235 api.anthropic.com:443 18.64.224.91:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.anthropic.com:443 "-"
1773003481.456 ::1:41200 - - 0.0 - 0 NONE_NONE:HIER_NONE error:transaction-end-before-headers "-"
`

	// Write test log file
	logPath := filepath.Join(tempDir, "firewall.log")
	err := os.WriteFile(logPath, []byte(testLogContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test firewall.log: %v", err)
	}

	// Test parsing
	analysis, err := parseFirewallLog(logPath, false)
	if err != nil {
		t.Fatalf("Failed to parse firewall log: %v", err)
	}

	// Internal Squid error entries should be filtered out entirely
	// Only the 2 real allowed requests should be counted
	if analysis.TotalRequests != 2 {
		t.Errorf("TotalRequests: got %d, want 2 (internal Squid entries should be excluded)", analysis.TotalRequests)
	}
	if analysis.AllowedRequests != 2 {
		t.Errorf("AllowedRequests: got %d, want 2", analysis.AllowedRequests)
	}
	if analysis.BlockedRequests != 0 {
		t.Errorf("BlockedRequests: got %d, want 0 (internal Squid entries should not be counted as blocked)", analysis.BlockedRequests)
	}

	// "-:-" should not appear in blocked domains
	for _, d := range analysis.BlockedDomains {
		if d == "-:-" {
			t.Error("BlockedDomains should not contain \"-:-\" (internal Squid error entries should be filtered out)")
		}
	}

	// The real traffic should still be tracked
	if stats, ok := analysis.RequestsByDomain["api.anthropic.com:443"]; !ok {
		t.Error("api.anthropic.com:443 should be in RequestsByDomain")
	} else if stats.Allowed != 2 {
		t.Errorf("api.anthropic.com:443 Allowed: got %d, want 2", stats.Allowed)
	}
}

func TestParseFirewallLogInternalSquidErrorEntriesDashDash(t *testing.T) {
	t.Parallel()
	// Create a temporary directory for the test
	tempDir := testutil.TempDir(t, "test-*")

	// Simulate internal Squid error entries where destIPPort is just "-" (not "-:-")
	// These should also be filtered out
	testLogContent := `1773003481.456 ::1:41200 - - 0.0 - 0 NONE_NONE:HIER_NONE error:transaction-end-before-headers "-"
1773003482.123 172.30.0.30:50235 blocked.example.com:443 10.0.0.1:443 1.1 CONNECT 403 NONE_NONE:HIER_NONE blocked.example.com:443 "-"
`

	// Write test log file
	logPath := filepath.Join(tempDir, "firewall.log")
	err := os.WriteFile(logPath, []byte(testLogContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test firewall.log: %v", err)
	}

	// Test parsing
	analysis, err := parseFirewallLog(logPath, false)
	if err != nil {
		t.Fatalf("Failed to parse firewall log: %v", err)
	}

	// Only the 1 real blocked request should be counted
	if analysis.TotalRequests != 1 {
		t.Errorf("TotalRequests: got %d, want 1 (internal Squid entry should be excluded)", analysis.TotalRequests)
	}
	if analysis.BlockedRequests != 1 {
		t.Errorf("BlockedRequests: got %d, want 1", analysis.BlockedRequests)
	}

	// The real blocked domain should appear, not internal error entries
	if len(analysis.BlockedDomains) != 1 || analysis.BlockedDomains[0] != "blocked.example.com:443" {
		t.Errorf("BlockedDomains: got %v, want [blocked.example.com:443]", analysis.BlockedDomains)
	}
}

func TestExtractFirewallFromAgentLog(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		logContent      string
		wantNil         bool
		wantBlocked     []string
		wantTotalReqs   int
		wantBlockedReqs int
	}{
		{
			name: "single blocked domain from Codex CLI warning",
			logContent: `[2026-01-01T10:00:00] thinking
[2026-01-01T10:00:01] [WARN] chatgpt.com is not in the allowed domains. To allow access, add --allow-domains chatgpt.com to your command.
[2026-01-01T10:00:01] agent exiting with code 1`,
			wantNil:         false,
			wantBlocked:     []string{"chatgpt.com"},
			wantTotalReqs:   1,
			wantBlockedReqs: 1,
		},
		{
			name: "multiple blocked domains",
			logContent: `[2026-01-01T10:00:00] thinking
add --allow-domains openai.com to your command
add --allow-domains anthropic.com to your command`,
			wantNil:         false,
			wantBlocked:     []string{"anthropic.com", "openai.com"},
			wantTotalReqs:   2,
			wantBlockedReqs: 2,
		},
		{
			name:            "comma-separated domains in single warning",
			logContent:      `add --allow-domains chatgpt.com,openai.com to access the API`,
			wantNil:         false,
			wantBlocked:     []string{"chatgpt.com", "openai.com"},
			wantTotalReqs:   2,
			wantBlockedReqs: 2,
		},
		{
			name:            "quoted comma-separated domains strips surrounding double quotes",
			logContent:      `[WARN] To fix domain issues: --allow-domains "*.githubusercontent.com,api.openai.com,chatgpt.com"`,
			wantNil:         false,
			wantBlocked:     []string{"*.githubusercontent.com", "api.openai.com", "chatgpt.com"},
			wantTotalReqs:   3,
			wantBlockedReqs: 3,
		},
		{
			name: "deduplicated repeated warnings for same domain",
			logContent: `add --allow-domains chatgpt.com to your command
add --allow-domains chatgpt.com to your command
add --allow-domains chatgpt.com to your command`,
			wantNil:         false,
			wantBlocked:     []string{"chatgpt.com"},
			wantTotalReqs:   1,
			wantBlockedReqs: 1,
		},
		{
			name: "no blocked domains in log",
			logContent: `[2026-01-01T10:00:00] thinking
[2026-01-01T10:00:01] tool github.list_pull_requests({"owner":"org","repo":"repo"})
[2026-01-01T10:00:02] tokens used: 1234`,
			wantNil: true,
		},
		{
			name:       "empty log",
			logContent: "",
			wantNil:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tempDir := testutil.TempDir(t, "agent-log-*")
			logPath := filepath.Join(tempDir, "agent-stdio.log")
			if err := os.WriteFile(logPath, []byte(tt.logContent), 0644); err != nil {
				t.Fatalf("Failed to write agent-stdio.log: %v", err)
			}

			result := extractFirewallFromAgentLog(tempDir, false)

			if tt.wantNil {
				if result != nil {
					t.Errorf("expected nil result, got %+v", result)
				}
				return
			}

			if result == nil {
				t.Fatal("expected non-nil result, got nil")
			}
			if result.TotalRequests != tt.wantTotalReqs {
				t.Errorf("TotalRequests: got %d, want %d", result.TotalRequests, tt.wantTotalReqs)
			}
			if result.BlockedRequests != tt.wantBlockedReqs {
				t.Errorf("BlockedRequests: got %d, want %d", result.BlockedRequests, tt.wantBlockedReqs)
			}
			if result.AllowedRequests != 0 {
				t.Errorf("AllowedRequests: got %d, want 0", result.AllowedRequests)
			}
			if len(result.GetBlockedDomains()) != len(tt.wantBlocked) {
				t.Errorf("BlockedDomains length: got %d, want %d (domains: %v)",
					len(result.GetBlockedDomains()), len(tt.wantBlocked), result.GetBlockedDomains())
			} else {
				for i, d := range tt.wantBlocked {
					if result.GetBlockedDomains()[i] != d {
						t.Errorf("BlockedDomains[%d]: got %q, want %q", i, result.GetBlockedDomains()[i], d)
					}
				}
			}
		})
	}
}

func TestExtractFirewallFromAgentLogNoFile(t *testing.T) {
	t.Parallel()
	tempDir := testutil.TempDir(t, "no-agent-log-*")
	// No agent-stdio.log created
	result := extractFirewallFromAgentLog(tempDir, false)
	if result != nil {
		t.Errorf("expected nil when agent-stdio.log is missing, got %+v", result)
	}
}

func TestFirewallAnalysisAddMetricsMergesDomains(t *testing.T) {
	t.Parallel()
	base := &FirewallAnalysis{
		AnalysisBase:     AnalysisBase{TotalRequests: 2, AllowedRequests: 1, BlockedRequests: 1},
		RequestsByDomain: map[string]DomainRequestStats{},
	}
	base.SetBlockedDomains([]string{"blocked-a.com"})
	base.SetAllowedDomains([]string{"allowed-a.com"})

	other := &FirewallAnalysis{
		AnalysisBase:     AnalysisBase{TotalRequests: 2, AllowedRequests: 1, BlockedRequests: 1},
		RequestsByDomain: map[string]DomainRequestStats{},
	}
	other.SetBlockedDomains([]string{"blocked-b.com"})
	other.SetAllowedDomains([]string{"allowed-b.com"})

	base.AddMetrics(other)

	if base.TotalRequests != 4 {
		t.Errorf("TotalRequests: got %d, want 4", base.TotalRequests)
	}
	if base.BlockedRequests != 2 {
		t.Errorf("BlockedRequests: got %d, want 2", base.BlockedRequests)
	}
	if base.AllowedRequests != 2 {
		t.Errorf("AllowedRequests: got %d, want 2", base.AllowedRequests)
	}

	blocked := base.GetBlockedDomains()
	if len(blocked) != 2 || blocked[0] != "blocked-a.com" || blocked[1] != "blocked-b.com" {
		t.Errorf("BlockedDomains: got %v, want [blocked-a.com, blocked-b.com]", blocked)
	}

	allowed := base.GetAllowedDomains()
	if len(allowed) != 2 || allowed[0] != "allowed-a.com" || allowed[1] != "allowed-b.com" {
		t.Errorf("AllowedDomains: got %v, want [allowed-a.com, allowed-b.com]", allowed)
	}
}

// TestAnalyzeFirewallLogsSandboxSquidSubdir verifies that analyzeFirewallLogs
// correctly finds access.log in the sandbox/firewall/logs/squid-logs/ subdirectory.
// AWF writes Squid logs to {proxy-logs-dir}/squid-logs/access.log, so after the
// agent artifact is downloaded and flattened the path is:
// {runDir}/sandbox/firewall/logs/squid-logs/access.log
func TestAnalyzeFirewallLogsSandboxSquidSubdir(t *testing.T) {
	t.Parallel()
	tmpDir := testutil.TempDir(t, "test-sandbox-squid-*")

	squidLogsDir := filepath.Join(tmpDir, "sandbox", "firewall", "logs", "squid-logs")
	if err := os.MkdirAll(squidLogsDir, 0755); err != nil {
		t.Fatalf("Failed to create squid-logs directory: %v", err)
	}

	logContent := `1761332530.474 172.30.0.20:35288 api.enterprise.githubcopilot.com:443 140.82.112.22:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.enterprise.githubcopilot.com:443 "-"
1761332531.123 172.30.0.20:35289 blocked.example.com:443 140.82.112.23:443 1.1 CONNECT 403 NONE_NONE:HIER_NONE blocked.example.com:443 "-"
1761332532.456 172.30.0.20:35290 api.github.com:443 140.82.112.5:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.github.com:443 "-"
`
	if err := os.WriteFile(filepath.Join(squidLogsDir, "access.log"), []byte(logContent), 0644); err != nil {
		t.Fatalf("Failed to write access.log: %v", err)
	}

	analysis, err := analyzeFirewallLogs(tmpDir, false)
	if err != nil {
		t.Fatalf("analyzeFirewallLogs failed: %v", err)
	}
	if analysis == nil {
		t.Fatal("Expected firewall analysis but got nil - access.log in squid-logs/ subdirectory was not found")
	}

	if analysis.TotalRequests != 3 {
		t.Errorf("TotalRequests: got %d, want 3", analysis.TotalRequests)
	}
	if analysis.AllowedRequests != 2 {
		t.Errorf("AllowedRequests: got %d, want 2", analysis.AllowedRequests)
	}
	if analysis.BlockedRequests != 1 {
		t.Errorf("BlockedRequests: got %d, want 1", analysis.BlockedRequests)
	}
	if len(analysis.BlockedDomains) != 1 || analysis.BlockedDomains[0] != "blocked.example.com:443" {
		t.Errorf("BlockedDomains: got %v, want [blocked.example.com:443]", analysis.BlockedDomains)
	}
}

// TestAnalyzeFirewallLogsSandboxFallbackToTopLevel verifies that when sandbox/firewall/logs/
// exists but has no squid-logs/ subdirectory, analyzeFirewallLogs falls back to looking for
// *.log files directly in sandbox/firewall/logs/ (backward compatibility for older AWF layout).
func TestAnalyzeFirewallLogsSandboxFallbackToTopLevel(t *testing.T) {
	t.Parallel()
	tmpDir := testutil.TempDir(t, "test-sandbox-fallback-*")

	sandboxLogsDir := filepath.Join(tmpDir, "sandbox", "firewall", "logs")
	if err := os.MkdirAll(sandboxLogsDir, 0755); err != nil {
		t.Fatalf("Failed to create sandbox/firewall/logs directory: %v", err)
	}

	logContent := `1761332530.474 172.30.0.20:35288 api.github.com:443 140.82.112.5:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.github.com:443 "-"
`
	if err := os.WriteFile(filepath.Join(sandboxLogsDir, "access.log"), []byte(logContent), 0644); err != nil {
		t.Fatalf("Failed to write access.log: %v", err)
	}

	analysis, err := analyzeFirewallLogs(tmpDir, false)
	if err != nil {
		t.Fatalf("analyzeFirewallLogs failed: %v", err)
	}
	if analysis == nil {
		t.Fatal("Expected firewall analysis but got nil")
	}
	if analysis.TotalRequests != 1 {
		t.Errorf("TotalRequests: got %d, want 1", analysis.TotalRequests)
	}
}

func TestAnalyzeFirewallLogsSandboxEmptySquidSubdirFallsBackToTopLevel(t *testing.T) {
	t.Parallel()
	tmpDir := testutil.TempDir(t, "test-sandbox-empty-squid-subdir-*")

	sandboxLogsDir := filepath.Join(tmpDir, "sandbox", "firewall", "logs")
	if err := os.MkdirAll(filepath.Join(sandboxLogsDir, "squid-logs"), 0755); err != nil {
		t.Fatalf("Failed to create sandbox/firewall/logs directories: %v", err)
	}

	logContent := `1761332530.474 172.30.0.20:35288 api.github.com:443 140.82.112.5:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.github.com:443 "-"
`
	if err := os.WriteFile(filepath.Join(sandboxLogsDir, "access.log"), []byte(logContent), 0644); err != nil {
		t.Fatalf("Failed to write access.log: %v", err)
	}

	analysis, err := analyzeFirewallLogs(tmpDir, false)
	if err != nil {
		t.Fatalf("analyzeFirewallLogs failed: %v", err)
	}
	if analysis == nil {
		t.Fatal("Expected firewall analysis but got nil")
	}
	if analysis.TotalRequests != 1 {
		t.Errorf("TotalRequests: got %d, want 1", analysis.TotalRequests)
	}
}

func TestFirewallAnalysisAddMetricsDeduplicatesDomains(t *testing.T) {
	t.Parallel()
	base := &FirewallAnalysis{
		AnalysisBase:     AnalysisBase{TotalRequests: 1, BlockedRequests: 1},
		RequestsByDomain: map[string]DomainRequestStats{},
	}
	base.SetBlockedDomains([]string{"chatgpt.com"})

	// Same domain added again (e.g. from two different log sources)
	other := &FirewallAnalysis{
		AnalysisBase:     AnalysisBase{TotalRequests: 1, BlockedRequests: 1},
		RequestsByDomain: map[string]DomainRequestStats{},
	}
	other.SetBlockedDomains([]string{"chatgpt.com"})

	base.AddMetrics(other)

	if len(base.GetBlockedDomains()) != 1 {
		t.Errorf("BlockedDomains should be deduplicated: got %v", base.GetBlockedDomains())
	}
}
