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
		{"Claude Code", DefaultClaudeCodeVersion, "2.1.247"},
		{"Codex", DefaultCodexVersion, "0.150.1"},
		{"GitHub MCP Server", DefaultGitHubMCPServerVersion, "v1.11.0"},
		{"MCP Gateway", DefaultMCPGatewayVersion, "v0.4.20"},
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

func TestDefaultPlaywrightCLIVersionOutsideCooldownWindow(t *testing.T) {
	t.Parallel()
	const (
		expectedVersion    Version = "0.1.18"
		publishedAtRFC3339         = "2026-08-06T00:00:00Z"
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
