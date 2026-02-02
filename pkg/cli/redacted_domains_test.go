//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/console"
)

func TestParseRedactedDomainsLog(t *testing.T) {
	tests := []struct {
		name            string
		logContent      string
		expectedTotal   int
		expectedDomains []string
	}{
		{
			name:            "empty file",
			logContent:      "",
			expectedTotal:   0,
			expectedDomains: nil,
		},
		{
			name:            "single domain",
			logContent:      "evil.example.com\n",
			expectedTotal:   1,
			expectedDomains: []string{"evil.example.com"},
		},
		{
			name:            "multiple domains",
			logContent:      "evil.example.com\nmalicious.site.org\nphishing.domain.net\n",
			expectedTotal:   3,
			expectedDomains: []string{"evil.example.com", "malicious.site.org", "phishing.domain.net"},
		},
		{
			name:            "duplicate domains are deduplicated",
			logContent:      "evil.example.com\nevil.example.com\nother.com\n",
			expectedTotal:   2,
			expectedDomains: []string{"evil.example.com", "other.com"},
		},
		{
			name:            "domains are sorted",
			logContent:      "zebra.com\nalpha.com\nmiddle.com\n",
			expectedTotal:   3,
			expectedDomains: []string{"alpha.com", "middle.com", "zebra.com"},
		},
		{
			name:            "blank lines are ignored",
			logContent:      "first.com\n\nsecond.com\n\n\nthird.com\n",
			expectedTotal:   3,
			expectedDomains: []string{"first.com", "second.com", "third.com"},
		},
		{
			name:            "comment lines are ignored",
			logContent:      "# This is a comment\nreal.domain.com\n# Another comment\nreal2.domain.com\n",
			expectedTotal:   2,
			expectedDomains: []string{"real.domain.com", "real2.domain.com"},
		},
		{
			name:            "whitespace is trimmed",
			logContent:      "  spaced.com  \n\ttabbed.com\t\n  \n",
			expectedTotal:   2,
			expectedDomains: []string{"spaced.com", "tabbed.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary file with the test content
			tmpDir := t.TempDir()
			logPath := filepath.Join(tmpDir, "redacted-urls.log")
			if err := os.WriteFile(logPath, []byte(tt.logContent), 0644); err != nil {
				t.Fatalf("failed to create test log file: %v", err)
			}

			analysis, err := parseRedactedDomainsLog(logPath, false)
			if err != nil {
				t.Fatalf("parseRedactedDomainsLog failed: %v", err)
			}

			if analysis.TotalDomains != tt.expectedTotal {
				t.Errorf("TotalDomains = %d, want %d", analysis.TotalDomains, tt.expectedTotal)
			}

			if len(analysis.Domains) != len(tt.expectedDomains) {
				t.Errorf("Domains length = %d, want %d", len(analysis.Domains), len(tt.expectedDomains))
			} else {
				for i, domain := range analysis.Domains {
					if domain != tt.expectedDomains[i] {
						t.Errorf("Domains[%d] = %q, want %q", i, domain, tt.expectedDomains[i])
					}
				}
			}
		})
	}
}

func TestParseRedactedDomainsLog_FileNotFound(t *testing.T) {
	_, err := parseRedactedDomainsLog("/nonexistent/path/redacted-urls.log", false)
	if err == nil {
		t.Error("expected error for non-existent file, got nil")
	}
}

func TestAnalyzeRedactedDomains_DirectPath(t *testing.T) {
	tmpDir := t.TempDir()
	logContent := "example.com\ntest.org\n"
	logPath := filepath.Join(tmpDir, "redacted-urls.log")
	if err := os.WriteFile(logPath, []byte(logContent), 0644); err != nil {
		t.Fatalf("failed to create test log file: %v", err)
	}

	analysis, err := analyzeRedactedDomains(tmpDir, false)
	if err != nil {
		t.Fatalf("analyzeRedactedDomains failed: %v", err)
	}

	if analysis == nil {
		t.Fatal("expected analysis result, got nil")
	}

	if analysis.TotalDomains != 2 {
		t.Errorf("TotalDomains = %d, want 2", analysis.TotalDomains)
	}
}

func TestAnalyzeRedactedDomains_AgentOutputsPath(t *testing.T) {
	tmpDir := t.TempDir()
	agentOutputsDir := filepath.Join(tmpDir, "agent_outputs")
	if err := os.MkdirAll(agentOutputsDir, 0755); err != nil {
		t.Fatalf("failed to create agent_outputs directory: %v", err)
	}

	logContent := "example.com\n"
	logPath := filepath.Join(agentOutputsDir, "redacted-urls.log")
	if err := os.WriteFile(logPath, []byte(logContent), 0644); err != nil {
		t.Fatalf("failed to create test log file: %v", err)
	}

	analysis, err := analyzeRedactedDomains(tmpDir, false)
	if err != nil {
		t.Fatalf("analyzeRedactedDomains failed: %v", err)
	}

	if analysis == nil {
		t.Fatal("expected analysis result, got nil")
	}

	if analysis.TotalDomains != 1 {
		t.Errorf("TotalDomains = %d, want 1", analysis.TotalDomains)
	}
}

func TestAnalyzeRedactedDomains_FullArtifactPath(t *testing.T) {
	tmpDir := t.TempDir()
	fullPath := filepath.Join(tmpDir, "agent_outputs", "tmp", "gh-aw")
	if err := os.MkdirAll(fullPath, 0755); err != nil {
		t.Fatalf("failed to create full path directory: %v", err)
	}

	logContent := "domain1.com\ndomain2.org\ndomain3.net\n"
	logPath := filepath.Join(fullPath, "redacted-urls.log")
	if err := os.WriteFile(logPath, []byte(logContent), 0644); err != nil {
		t.Fatalf("failed to create test log file: %v", err)
	}

	analysis, err := analyzeRedactedDomains(tmpDir, false)
	if err != nil {
		t.Fatalf("analyzeRedactedDomains failed: %v", err)
	}

	if analysis == nil {
		t.Fatal("expected analysis result, got nil")
	}

	if analysis.TotalDomains != 3 {
		t.Errorf("TotalDomains = %d, want 3", analysis.TotalDomains)
	}
}

func TestAnalyzeRedactedDomains_NoLogFile(t *testing.T) {
	tmpDir := t.TempDir()

	analysis, err := analyzeRedactedDomains(tmpDir, false)
	if err != nil {
		t.Fatalf("analyzeRedactedDomains failed: %v", err)
	}

	if analysis != nil {
		t.Error("expected nil analysis for missing log file, got non-nil")
	}
}

func TestAnalyzeRedactedDomains_RecursiveSearch(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a nested directory structure
	nestedDir := filepath.Join(tmpDir, "some", "nested", "path")
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatalf("failed to create nested directory: %v", err)
	}

	logContent := "found-via-recursive.com\n"
	logPath := filepath.Join(nestedDir, "redacted-urls.log")
	if err := os.WriteFile(logPath, []byte(logContent), 0644); err != nil {
		t.Fatalf("failed to create test log file: %v", err)
	}

	analysis, err := analyzeRedactedDomains(tmpDir, false)
	if err != nil {
		t.Fatalf("analyzeRedactedDomains failed: %v", err)
	}

	if analysis == nil {
		t.Fatal("expected analysis result, got nil")
	}

	if analysis.TotalDomains != 1 {
		t.Errorf("TotalDomains = %d, want 1", analysis.TotalDomains)
	}

	if len(analysis.Domains) == 0 || analysis.Domains[0] != "found-via-recursive.com" {
		t.Errorf("expected domain 'found-via-recursive.com', got %v", analysis.Domains)
	}
}

func TestRedactedDomainsAnalysis_ConsoleRendering(t *testing.T) {
	// Test that the Domains field is rendered in console output
	analysis := &RedactedDomainsAnalysis{
		TotalDomains: 3,
		Domains:      []string{"evil.com", "malicious.org", "phishing.net"},
	}

	// Import the console package to render the struct
	// We use RenderStruct to verify the domains are included
	output := console.RenderStruct(analysis)

	// Verify the output contains the domains
	expectedDomains := []string{"evil.com", "malicious.org", "phishing.net"}
	for _, domain := range expectedDomains {
		if !strings.Contains(output, domain) {
			t.Errorf("expected output to contain domain %q, got:\n%s", domain, output)
		}
	}

	// Verify the total count is shown
	if !strings.Contains(output, "Total Domains") {
		t.Errorf("expected output to contain 'Total Domains', got:\n%s", output)
	}
}

func TestRedactedDomainsLogSummary_ConsoleRendering(t *testing.T) {
	// Test that the Domains field in RedactedDomainsLogSummary is rendered in console output
	summary := &RedactedDomainsLogSummary{
		TotalDomains: 2,
		Domains:      []string{"blocked.com", "denied.org"},
	}

	output := console.RenderStruct(summary)

	// Verify the output contains the domains
	expectedDomains := []string{"blocked.com", "denied.org"}
	for _, domain := range expectedDomains {
		if !strings.Contains(output, domain) {
			t.Errorf("expected output to contain domain %q, got:\n%s", domain, output)
		}
	}
}

func TestRedactedDomainsAnalysis_EmptyDomains(t *testing.T) {
	// Test that empty domains list is handled correctly (omitempty)
	analysis := &RedactedDomainsAnalysis{
		TotalDomains: 0,
		Domains:      []string{},
	}

	output := console.RenderStruct(analysis)

	// Verify the output does not contain "Redacted Domains" section header for empty list
	// The omitempty tag should hide this when the slice is empty
	if strings.Contains(output, "Redacted Domains") {
		t.Errorf("expected empty domains list to be omitted, but found 'Redacted Domains' in output:\n%s", output)
	}
}
