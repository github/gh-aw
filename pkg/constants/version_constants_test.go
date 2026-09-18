//go:build !integration

package constants

import (
	"regexp"
	"testing"
	"time"
)

func TestDefaultCLIMCPVersions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  Version
		want Version
	}{
		{"Claude Code", DefaultClaudeCodeVersion, "2.1.273"},
		{"Copilot CLI", DefaultCopilotVersion, "1.0.85"},
		{"Codex", DefaultCodexVersion, "0.154.0"},
		{"GitHub MCP Server", DefaultGitHubMCPServerVersion, "v1.12.1"},
		{"MCP Gateway", DefaultMCPGatewayVersion, "v0.4.25"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.got != tt.want {
				t.Fatalf("%s default version = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestDefaultThreatDetectReleasePins(t *testing.T) {
	t.Parallel()

	if DefaultThreatDetectVersion != "v0.5.2" {
		t.Fatalf("DefaultThreatDetectVersion = %q, want v0.5.2; update the version and reviewed digest table together", DefaultThreatDetectVersion)
	}

	expectedAssets := []string{
		"threat-detect-linux-amd64",
		"threat-detect-linux-arm64",
		"threat-detect-darwin-x64",
		"threat-detect-darwin-arm64",
	}
	if len(DefaultThreatDetectSHA256) != len(expectedAssets) {
		t.Fatalf("DefaultThreatDetectSHA256 has %d entries, want the complete %d-asset release matrix", len(DefaultThreatDetectSHA256), len(expectedAssets))
	}
	digestPattern := regexp.MustCompile(`^[0-9a-f]{64}$`)
	for _, asset := range expectedAssets {
		digest, ok := DefaultThreatDetectSHA256[asset]
		if !ok {
			t.Errorf("DefaultThreatDetectSHA256 is missing %q", asset)
		} else if !digestPattern.MatchString(digest) {
			t.Errorf("DefaultThreatDetectSHA256[%q] is not a lowercase SHA-256 digest", asset)
		}
	}
}

func TestDefaultPlaywrightCLIVersionOutsideCooldownWindow(t *testing.T) {
	t.Parallel()
	const (
		expectedVersion    Version = "0.1.19"
		publishedAtRFC3339         = "2026-09-01T16:19:56.878Z"
		minReleaseAge              = 72 * time.Hour
	)

	if DefaultPlaywrightCLIVersion != expectedVersion {
		t.Fatalf("DefaultPlaywrightCLIVersion = %q, want %q; update this test metadata when changing the pinned default", DefaultPlaywrightCLIVersion, expectedVersion)
	}

	publishedAt, err := time.Parse(time.RFC3339Nano, publishedAtRFC3339)
	if err != nil {
		t.Fatalf("parse publishedAtRFC3339: %v", err)
	}

	age := time.Since(publishedAt)
	if age < minReleaseAge {
		t.Fatalf("@playwright/cli@%s is only %s old, but Playwright CLI installs enforce a %s npm release-age cooldown", DefaultPlaywrightCLIVersion, age.Round(time.Second), minReleaseAge)
	}
}
