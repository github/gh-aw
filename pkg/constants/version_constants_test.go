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
		{"Claude Code", DefaultClaudeCodeVersion, "2.1.325"},
		{"Copilot CLI", DefaultCopilotVersion, "1.0.103"},
		{"Copilot SDK", DefaultCopilotSDKVersion, "1.0.16"},
		{"Codex", DefaultCodexVersion, "0.171.0"},
		{"Gemini CLI", DefaultGeminiVersion, "0.64.0"},
		{"Agy CLI", DefaultAgyVersion, "1.3.6"},
		{"Pi CLI", DefaultPiVersion, "1.6.0"},
		{"GitHub MCP Server", DefaultGitHubMCPServerVersion, "v2.2.0"},
		{"Firewall", DefaultFirewallVersion, "v0.29.20"},
		{"Cost-only model routing", AWFCostOnlyModelRoutingMinVersion, "v0.29.17"},
		{"OpenAI WIF", AWFOpenAIWIFMinVersion, "v0.29.11"},
		{"Router", DefaultRouterVersion, "0.1.3"},
		{"Router image release", AWFRouterReleaseImageMinVersion, "v0.28.62"},
		{"MCP Gateway", DefaultMCPGatewayVersion, "v0.5.14"},
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

func TestDefaultCopilotChecksumsSHA256(t *testing.T) {
	t.Parallel()

	const expected = "26cd3e4b32cde498e4961fb4d48e7b85d7859fc69af1a645a19201fdd577bdb2"
	if DefaultCopilotChecksumsSHA256 != expected {
		t.Fatalf("DefaultCopilotChecksumsSHA256 = %q, want the reviewed v1.0.103 manifest digest %q", DefaultCopilotChecksumsSHA256, expected)
	}
}

func TestDefaultPlaywrightCLIVersionOutsideCooldownWindow(t *testing.T) {
	t.Parallel()
	const (
		expectedVersion    Version = "0.1.25"
		publishedAtRFC3339         = "2026-11-18T18:55:19Z"
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
