//go:build !integration

package cli

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/stringutil"
)

func TestFormatAge(t *testing.T) {
	tests := []struct {
		name     string
		days     int
		expected string
	}{
		{"zero days", 0, "today"},
		{"one day", 1, "1 day"},
		{"multiple days", 15, "15 days"},
		{"one month", 30, "1 months"},
		{"multiple months", 90, "3 months"},
		{"one year", 365, "1 years"},
		{"multiple years", 730, "2 years"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create duration from days
			d := daysToHours(tt.days)
			result := formatAge(d)
			if result != tt.expected {
				t.Errorf("formatAge(%v days) = %v, want %v", tt.days, result, tt.expected)
			}
		})
	}
}

func TestGetUpdateStatus(t *testing.T) {
	tests := []struct {
		name     string
		dep      OutdatedDependency
		contains string
	}{
		{
			name: "non-v0 dependency",
			dep: OutdatedDependency{
				Module:  "github.com/example/pkg",
				Current: "v1.0.0",
				Latest:  "v1.1.0",
				IsV0:    false,
			},
			contains: "Update available",
		},
		{
			name: "v0.x dependency",
			dep: OutdatedDependency{
				Module:  "github.com/example/pkg",
				Current: "v0.9.0",
				Latest:  "v0.10.0",
				IsV0:    true,
			},
			contains: "⚠️ v0.x",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getUpdateStatus(tt.dep)
			if !strings.Contains(result, tt.contains) {
				t.Errorf("getUpdateStatus() = %v, should contain %v", result, tt.contains)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"shorter than max", "hello", 10, "hello"},
		{"equal to max", "hello", 5, "hello"},
		{"longer than max", "hello world", 8, "hello..."},
		{"empty string", "", 5, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stringutil.Truncate(tt.input, tt.maxLen)
			if result != tt.want {
				t.Errorf("stringutil.Truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.want)
			}
		})
	}
}

func TestParseGoMod(t *testing.T) {
	goModContent := `module github.com/example/test

go 1.25.0

require (
	github.com/spf13/cobra v1.7.0
	github.com/stretchr/testify v1.8.4
	golang.org/x/crypto v0.0.0-20230711153332-06a737ee72cb // indirect
)
`
	// Create temp file
	tmpFile := createTempFile(t, goModContent)
	defer removeTempFile(t, tmpFile)

	deps, err := parseGoMod(tmpFile)
	if err != nil {
		t.Fatalf("parseGoMod() error = %v", err)
	}

	// Should parse direct dependencies only (not indirect)
	if len(deps) != 2 {
		t.Errorf("parseGoMod() found %d dependencies, want 2", len(deps))
	}

	// Check first dependency
	if deps[0].Path != "github.com/spf13/cobra" {
		t.Errorf("deps[0].Path = %v, want github.com/spf13/cobra", deps[0].Path)
	}
	if deps[0].Version != "v1.7.0" {
		t.Errorf("deps[0].Version = %v, want v1.7.0", deps[0].Version)
	}
}

func TestParseGoModWithIndirect(t *testing.T) {
	goModContent := `module github.com/example/test

go 1.25.0

require (
	github.com/spf13/cobra v1.7.0
	github.com/stretchr/testify v1.8.4
	golang.org/x/crypto v0.0.0-20230711153332-06a737ee72cb // indirect
)
`
	// Create temp file
	tmpFile := createTempFile(t, goModContent)
	defer removeTempFile(t, tmpFile)

	deps, err := parseGoModWithIndirect(tmpFile)
	if err != nil {
		t.Fatalf("parseGoModWithIndirect() error = %v", err)
	}

	// Should parse all dependencies including indirect
	if len(deps) != 3 {
		t.Errorf("parseGoModWithIndirect() found %d dependencies, want 3", len(deps))
	}

	// Check indirect flag
	indirectCount := 0
	for _, dep := range deps {
		if dep.Indirect {
			indirectCount++
		}
	}
	if indirectCount != 1 {
		t.Errorf("parseGoModWithIndirect() found %d indirect dependencies, want 1", indirectCount)
	}
}

func TestGetSeverityIcon(t *testing.T) {
	tests := []struct {
		severity string
		expected string
	}{
		{"critical", "🔴"},
		{"high", "🟠"},
		{"medium", "🟡"},
		{"low", "🟢"},
		{"unknown", "⚠️"},
	}

	for _, tt := range tests {
		t.Run(tt.severity, func(t *testing.T) {
			result := getSeverityIcon(tt.severity)
			if result != tt.expected {
				t.Errorf("getSeverityIcon(%v) = %v, want %v", tt.severity, result, tt.expected)
			}
		})
	}
}

func TestSeverityWeight(t *testing.T) {
	// Test that critical > high > medium > low
	if severityWeight("critical") <= severityWeight("high") {
		t.Error("critical should have higher weight than high")
	}
	if severityWeight("high") <= severityWeight("medium") {
		t.Error("high should have higher weight than medium")
	}
	if severityWeight("medium") <= severityWeight("low") {
		t.Error("medium should have higher weight than low")
	}
}

func TestPluralize(t *testing.T) {
	tests := []struct {
		word     string
		count    int
		expected string
	}{
		{"advisory", 1, "advisory"},
		{"advisory", 2, "advisories"},
		{"dependency", 1, "dependency"},
		{"dependency", 5, "dependencies"},
	}

	for _, tt := range tests {
		t.Run(tt.word, func(t *testing.T) {
			result := pluralize(tt.word, tt.count)
			if result != tt.expected {
				t.Errorf("pluralize(%v, %d) = %v, want %v", tt.word, tt.count, result, tt.expected)
			}
		})
	}
}

// Helper functions for testing

func daysToHours(days int) time.Duration {
	return time.Duration(days) * 24 * time.Hour
}

func createTempFile(t *testing.T, content string) string {
	t.Helper()
	tmpFile, err := os.CreateTemp("", "go.mod")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}
	return tmpFile.Name()
}

func removeTempFile(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Logf("Warning: Failed to remove temp file %s: %v", path, err)
	}
}
