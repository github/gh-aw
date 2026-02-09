//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccessLogParsing(t *testing.T) {
	// Create a temporary directory for the test
	tempDir := testutil.TempDir(t, "test-*")

	// Create test access.log content
	testLogContent := `1701234567.123    180 192.168.1.100 TCP_MISS/200 1234 GET http://example.com/api/data - HIER_DIRECT/93.184.216.34 text/html
1701234568.456    250 192.168.1.100 TCP_DENIED/403 0 CONNECT github.com:443 - HIER_NONE/- -
1701234569.789    120 192.168.1.100 TCP_HIT/200 5678 GET http://api.github.com/repos - HIER_DIRECT/140.82.112.6 application/json
1701234570.012    0 192.168.1.100 TCP_DENIED/403 0 GET http://malicious.site/evil - HIER_NONE/- -`

	// Write test log file
	accessLogPath := filepath.Join(tempDir, "access.log")
	err := os.WriteFile(accessLogPath, []byte(testLogContent), 0644)
	require.NoError(t, err, "should create test access log file")

	// Test parsing
	analysis, err := parseSquidAccessLog(accessLogPath, false)
	require.NoError(t, err, "should parse valid squid access log")
	require.NotNil(t, analysis, "should return analysis result")

	// Verify results
	assert.Equal(t, 4, analysis.TotalRequests, "should count all log entries")
	assert.Equal(t, 2, analysis.AllowedCount, "should count allowed requests")
	assert.Equal(t, 2, analysis.BlockedCount, "should count blocked requests")

	// Check allowed domains
	expectedAllowed := []string{"api.github.com", "example.com"}
	assert.Len(t, analysis.AllowedDomains, len(expectedAllowed), "should extract correct number of allowed domains")
}

func TestMultipleAccessLogAnalysis(t *testing.T) {
	// Create a temporary directory for the test
	tempDir := testutil.TempDir(t, "test-*")
	accessLogsDir := filepath.Join(tempDir, "access.log")
	err := os.MkdirAll(accessLogsDir, 0755)
	require.NoError(t, err, "should create access.log directory")

	// Create test access log content for multiple MCP servers
	fetchLogContent := `1701234567.123    180 192.168.1.100 TCP_MISS/200 1234 GET http://example.com/api/data - HIER_DIRECT/93.184.216.34 text/html
1701234568.456    250 192.168.1.100 TCP_HIT/200 5678 GET http://api.github.com/repos - HIER_DIRECT/140.82.112.6 application/json`

	browserLogContent := `1701234569.789    120 192.168.1.100 TCP_DENIED/403 0 CONNECT github.com:443 - HIER_NONE/- -
1701234570.012    0 192.168.1.100 TCP_DENIED/403 0 GET http://malicious.site/evil - HIER_NONE/- -`

	// Write separate log files for different MCP servers
	fetchLogPath := filepath.Join(accessLogsDir, "access-fetch.log")
	err = os.WriteFile(fetchLogPath, []byte(fetchLogContent), 0644)
	require.NoError(t, err, "should create test access-fetch.log")

	browserLogPath := filepath.Join(accessLogsDir, "access-browser.log")
	err = os.WriteFile(browserLogPath, []byte(browserLogContent), 0644)
	require.NoError(t, err, "should create test access-browser.log")

	// Test analysis of multiple access logs
	analysis, err := analyzeMultipleAccessLogs(accessLogsDir, false)
	require.NoError(t, err, "should analyze multiple access logs")
	require.NotNil(t, analysis, "should return analysis result")

	// Verify aggregated results
	assert.Equal(t, 4, analysis.TotalRequests, "should count all requests from multiple logs")
	assert.Equal(t, 2, analysis.AllowedCount, "should count allowed requests")
	assert.Equal(t, 2, analysis.BlockedCount, "should count blocked requests")

	// Check allowed domains
	expectedAllowed := []string{"api.github.com", "example.com"}
	assert.Len(t, analysis.AllowedDomains, len(expectedAllowed), "should extract correct number of allowed domains")

	// Check blocked domains
	expectedDenied := []string{"github.com", "malicious.site"}
	assert.Len(t, analysis.BlockedDomains, len(expectedDenied), "should extract correct number of blocked domains")
}

func TestAnalyzeAccessLogsDirectory(t *testing.T) {
	// Create a temporary directory structure
	tempDir := testutil.TempDir(t, "test-*")

	t.Run("multiple access logs in subdirectory", func(t *testing.T) {
		// Test case 1: Multiple access logs in access-logs subdirectory
		accessLogsDir := filepath.Join(tempDir, "run1", "access.log")
		err := os.MkdirAll(accessLogsDir, 0755)
		require.NoError(t, err, "should create access.log directory")

		fetchLogContent := `1701234567.123    180 192.168.1.100 TCP_MISS/200 1234 GET http://example.com/api/data - HIER_DIRECT/93.184.216.34 text/html`
		fetchLogPath := filepath.Join(accessLogsDir, "access-fetch.log")
		err = os.WriteFile(fetchLogPath, []byte(fetchLogContent), 0644)
		require.NoError(t, err, "should create test access-fetch.log")

		analysis, err := analyzeAccessLogs(filepath.Join(tempDir, "run1"), false)
		require.NoError(t, err, "should analyze access logs")
		require.NotNil(t, analysis, "should return analysis for valid logs")
		assert.Equal(t, 1, analysis.TotalRequests, "should count request from log file")
	})

	t.Run("no access logs - returns nil", func(t *testing.T) {
		// Test case 2: No access logs
		run2Dir := filepath.Join(tempDir, "run2")
		err := os.MkdirAll(run2Dir, 0755)
		require.NoError(t, err, "should create run2 directory")

		analysis, err := analyzeAccessLogs(run2Dir, false)
		require.NoError(t, err, "should not error when no logs present")
		assert.Nil(t, analysis, "should return nil when no logs found")
	})

	t.Run("access logs in sandbox/firewall/logs/ (new path)", func(t *testing.T) {
		// Test case 3: Access logs in sandbox/firewall/logs/ directory after artifact download
		sandboxLogsDir := filepath.Join(tempDir, "run3", "sandbox", "firewall", "logs")
		err := os.MkdirAll(sandboxLogsDir, 0755)
		require.NoError(t, err, "should create sandbox/firewall/logs directory")

		fetchLogContent := `1701234567.123    180 192.168.1.100 TCP_MISS/200 1234 GET http://example.com/api/data - HIER_DIRECT/93.184.216.34 text/html
1701234568.456    250 192.168.1.100 TCP_HIT/200 5678 GET http://api.github.com/repos - HIER_DIRECT/140.82.112.6 application/json`
		fetchLogPath := filepath.Join(sandboxLogsDir, "access-1.log")
		err = os.WriteFile(fetchLogPath, []byte(fetchLogContent), 0644)
		require.NoError(t, err, "should create test access log in sandbox path")

		analysis, err := analyzeAccessLogs(filepath.Join(tempDir, "run3"), false)
		require.NoError(t, err, "should analyze access logs from sandbox path")
		require.NotNil(t, analysis, "should return analysis for logs in sandbox path")
		assert.Equal(t, 2, analysis.TotalRequests, "should count requests from sandbox path")
	})
}

func TestExtractDomainFromURL(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"http://example.com/path", "example.com"},
		{"https://api.github.com/repos", "api.github.com"},
		{"github.com:443", "github.com"},
		{"malicious.site", "malicious.site"},
		{"http://sub.domain.com:8080/path", "sub.domain.com"},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			result := stringutil.ExtractDomainFromURL(tt.url)
			assert.Equal(t, tt.expected, result, "should extract correct domain from URL")
		})
	}
}

func TestParseSquidLogLine(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		expected  *AccessLogEntry
		shouldErr bool
	}{
		{
			name: "valid squid log line",
			line: "1701234567.123 180 192.168.1.100 TCP_MISS/200 1234 GET http://example.com/api - HIER_DIRECT/93.184.216.34 text/html",
			expected: &AccessLogEntry{
				Timestamp: "1701234567.123",
				Duration:  "180",
				ClientIP:  "192.168.1.100",
				Status:    "TCP_MISS/200",
				Size:      "1234",
				Method:    "GET",
				URL:       "http://example.com/api",
				User:      "-",
				Hierarchy: "HIER_DIRECT/93.184.216.34",
				Type:      "text/html",
			},
			shouldErr: false,
		},
		{
			name: "valid denied request",
			line: "1701234568.456 250 192.168.1.100 TCP_DENIED/403 0 CONNECT github.com:443 - HIER_NONE/- -",
			expected: &AccessLogEntry{
				Timestamp: "1701234568.456",
				Duration:  "250",
				ClientIP:  "192.168.1.100",
				Status:    "TCP_DENIED/403",
				Size:      "0",
				Method:    "CONNECT",
				URL:       "github.com:443",
				User:      "-",
				Hierarchy: "HIER_NONE/-",
				Type:      "-",
			},
			shouldErr: false,
		},
		{
			name:      "insufficient fields - should error",
			line:      "1701234567.123 180 192.168.1.100",
			shouldErr: true,
		},
		{
			name:      "empty line",
			line:      "",
			shouldErr: true,
		},
		{
			name:      "exactly 9 fields - should error",
			line:      "1701234567.123 180 192.168.1.100 TCP_MISS/200 1234 GET http://example.com/api - HIER_DIRECT/93.184.216.34",
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseSquidLogLine(tt.line)

			if tt.shouldErr {
				require.Error(t, err, "should return error for invalid line")
				assert.Nil(t, result, "should not return entry on error")
			} else {
				require.NoError(t, err, "should parse valid log line")
				require.NotNil(t, result, "should return parsed entry")
				assert.Equal(t, tt.expected.Timestamp, result.Timestamp, "timestamp should match")
				assert.Equal(t, tt.expected.Duration, result.Duration, "duration should match")
				assert.Equal(t, tt.expected.ClientIP, result.ClientIP, "client IP should match")
				assert.Equal(t, tt.expected.Status, result.Status, "status should match")
				assert.Equal(t, tt.expected.Size, result.Size, "size should match")
				assert.Equal(t, tt.expected.Method, result.Method, "method should match")
				assert.Equal(t, tt.expected.URL, result.URL, "URL should match")
				assert.Equal(t, tt.expected.User, result.User, "user should match")
				assert.Equal(t, tt.expected.Hierarchy, result.Hierarchy, "hierarchy should match")
				assert.Equal(t, tt.expected.Type, result.Type, "type should match")
			}
		})
	}
}

func TestAddMetrics(t *testing.T) {
	tests := []struct {
		name     string
		base     *DomainAnalysis
		toAdd    LogAnalysis
		expected *DomainAnalysis
	}{
		{
			name: "add valid domain analysis",
			base: &DomainAnalysis{
				TotalRequests: 10,
				AllowedCount:  8,
				BlockedCount:  2,
			},
			toAdd: &DomainAnalysis{
				TotalRequests: 5,
				AllowedCount:  4,
				BlockedCount:  1,
			},
			expected: &DomainAnalysis{
				TotalRequests: 15,
				AllowedCount:  12,
				BlockedCount:  3,
			},
		},
		{
			name: "add zero values",
			base: &DomainAnalysis{
				TotalRequests: 10,
				AllowedCount:  8,
				BlockedCount:  2,
			},
			toAdd: &DomainAnalysis{
				TotalRequests: 0,
				AllowedCount:  0,
				BlockedCount:  0,
			},
			expected: &DomainAnalysis{
				TotalRequests: 10,
				AllowedCount:  8,
				BlockedCount:  2,
			},
		},
		{
			name: "add to empty base",
			base: &DomainAnalysis{
				TotalRequests: 0,
				AllowedCount:  0,
				BlockedCount:  0,
			},
			toAdd: &DomainAnalysis{
				TotalRequests: 5,
				AllowedCount:  3,
				BlockedCount:  2,
			},
			expected: &DomainAnalysis{
				TotalRequests: 5,
				AllowedCount:  3,
				BlockedCount:  2,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.base.AddMetrics(tt.toAdd)
			assert.Equal(t, tt.expected.TotalRequests, tt.base.TotalRequests, "total requests should match")
			assert.Equal(t, tt.expected.AllowedCount, tt.base.AllowedCount, "allowed count should match")
			assert.Equal(t, tt.expected.BlockedCount, tt.base.BlockedCount, "blocked count should match")
		})
	}
}
