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
			result := isRequestAllowed(tt.decision, tt.status)
			if result != tt.expected {
				t.Errorf("isRequestAllowed(%q, %q) = %v, want %v", tt.decision, tt.status, result, tt.expected)
			}
		})
	}
}

func TestParseFirewallLog(t *testing.T) {
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

func TestAnalyzeMultipleFirewallLogs(t *testing.T) {
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
			result := workflow.SanitizeWorkflowName(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeWorkflowName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestAnalyzeFirewallLogsWithWorkflowSuffix(t *testing.T) {
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
