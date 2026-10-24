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
		{"Claude Code", DefaultClaudeCodeVersion, "2.1.310"},
		{"Copilot CLI", DefaultCopilotVersion, "1.0.101"},
		{"Copilot SDK", DefaultCopilotSDKVersion, "1.0.16"},
		{"Codex", DefaultCodexVersion, "0.164.0"},
		{"Gemini CLI", DefaultGeminiVersion, "0.64.0"},
		{"Agy CLI", DefaultAgyVersion, "1.3.6"},
		{"Pi CLI", DefaultPiVersion, "1.3.0"},
		{"GitHub MCP Server", DefaultGitHubMCPServerVersion, "v2.0.3"},
		{"Firewall", DefaultFirewallVersion, "v0.28.70"},
		{"Router", DefaultRouterVersion, "0.1.3"},
		{"Router image release", AWFRouterReleaseImageMinVersion, "v0.28.62"},
		{"MCP Gateway", DefaultMCPGatewayVersion, "v0.4.37"},
		{"Threat Detect", DefaultThreatDetectVersion, "v0.5.3"},
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

	if DefaultThreatDetectVersion != "v0.5.3" {
		t.Fatalf("DefaultThreatDetectVersion = %q, want v0.5.3; update the version and reviewed digest table together", DefaultThreatDetectVersion)
	}

	expectedDigests := map[string]string{
		"threat-detect-linux-amd64":  "7cfab3bd02717968a0584ddd3790cff22550059e36d6f656c4f35e9d27851009",
		"threat-detect-linux-arm64":  "6f355949af98e7b5527a958ac11431bd7867db45c0191b94c3a205c662181cc6",
		"threat-detect-darwin-x64":   "e0b606fb321a8b3d67f57c4bca7aac7e02976c0cf30c675032a7599b7225ff93",
		"threat-detect-darwin-arm64": "2600ba84538d50e91926268b2b7c59ae9443e1ae6f709673cacb05e167ddb0e9",
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
			t.Errorf("DefaultThreatDetectSHA256[%q] = %q, want the reviewed v0.5.3 digest %q; update the version and reviewed digest table together", asset, got, want)
		}
	}
}

func TestDefaultPlaywrightCLIVersionOutsideCooldownWindow(t *testing.T) {
	t.Parallel()
	const (
		expectedVersion    Version = "0.1.22"
		publishedAtRFC3339         = "2026-09-28T23:24:31.286Z"
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
