//go:build !integration

package constants

import (
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
		{"Copilot CLI", DefaultCopilotVersion, "1.0.87"},
		{"Codex", DefaultCodexVersion, "0.154.0"},
		{"Pi CLI", DefaultPiVersion, "0.87.0"},
		{"GitHub MCP Server", DefaultGitHubMCPServerVersion, "v1.12.2"},
		{"MCP Gateway", DefaultMCPGatewayVersion, "v0.4.25"},
		{"Threat Detect", DefaultThreatDetectVersion, "v0.5.2"},
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

	expectedDigests := map[string]string{
		"threat-detect-linux-amd64":  "b4ecda6a8f1ee09913c40b58e5e9d3337d2173618d41b1bfdef9207e4e7959b9",
		"threat-detect-linux-arm64":  "f6260a0f9ad72bcb67c7af19c4ce262ca34e2c3d5ccbf912832a8bd277200904",
		"threat-detect-darwin-x64":   "7ed0a68ffbdd927eb2e25f862864602af289ad9cfc85f1385d83d4d52f11251c",
		"threat-detect-darwin-arm64": "0d4f41134a0839a496ca34f5fbbce44ba89ca0be6d681f960e7c06070b8b04c6",
	}
	if len(DefaultThreatDetectSHA256) != len(expectedDigests) {
		t.Fatalf("DefaultThreatDetectSHA256 has %d entries, want the complete %d-asset release matrix", len(DefaultThreatDetectSHA256), len(expectedDigests))
	}
	for asset, want := range expectedDigests {
		got, ok := DefaultThreatDetectSHA256[asset]
		if !ok {
			t.Errorf("DefaultThreatDetectSHA256 is missing %q", asset)
			continue
		}
		if got != want {
			t.Errorf("DefaultThreatDetectSHA256[%q] = %q, want the reviewed v0.5.2 digest %q; update the version and reviewed digest table together", asset, got, want)
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
