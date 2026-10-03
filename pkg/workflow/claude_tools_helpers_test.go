//go:build !integration

package workflow

import (
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
)

func TestHasBashWildcard(t *testing.T) {
	tests := []struct {
		name     string
		commands []any
		want     bool
	}{
		{name: "no wildcard", commands: []any{"jq", "sed"}, want: false},
		{name: "star wildcard", commands: []any{"*"}, want: true},
		{name: "colon star wildcard", commands: []any{":*"}, want: true},
		{name: "non-string values", commands: []any{1, true}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasBashWildcard(tt.commands)
			assert.Equal(t, tt.want, got, "hasBashWildcard() should match expected value")
		})
	}
}

func TestNormalizeSandboxWritablePattern(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		want     string
		wantOkay bool
	}{
		{name: "absolute directory path", input: "/tmp/cache", want: "//tmp/cache/**", wantOkay: true},
		{name: "absolute glob path", input: "/tmp/cache/*", want: "//tmp/cache/*", wantOkay: true},
		{name: "trim whitespace", input: "  /tmp/cache  ", want: "//tmp/cache/**", wantOkay: true},
		{name: "root directory", input: "/", want: "//**", wantOkay: true},
		{name: "already absolute permission", input: "//tmp/cache/**", want: "//tmp/cache/**", wantOkay: true},
		{name: "relative path rejected", input: "tmp/cache", want: "", wantOkay: false},
		{name: "empty path rejected", input: "  ", want: "", wantOkay: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotOkay := normalizeSandboxWritablePattern(tt.input)
			assert.Equal(t, tt.want, got, "pattern should match expected")
			assert.Equal(t, tt.wantOkay, gotOkay, "ok flag should match expected")
		})
	}
}

func TestGetOrCreateToolMapStoresCreatedMap(t *testing.T) {
	container := map[string]any{}

	created := getOrCreateToolMap(container, "claude")
	created["allowed"] = map[string]any{"Read": nil}

	stored, ok := container["claude"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, created, stored)
}

func TestAppendGitHubMCPTools(t *testing.T) {
	t.Run("uses wildcard alias", func(t *testing.T) {
		mcpConfig := map[string]any{"allowed": []any{"*"}}
		got := appendGitHubMCPTools(nil, "github", mcpConfig, mcpConfig)
		assert.Equal(t, []string{"mcp__github"}, got)
	})

	t.Run("expands specific allowed tools", func(t *testing.T) {
		mcpConfig := map[string]any{"allowed": []any{"issue_read", "issue_update"}}
		got := appendGitHubMCPTools(nil, "github", mcpConfig, mcpConfig)
		assert.Equal(t, []string{"mcp__github__issue_read", "mcp__github__issue_update"}, got)
	})

	t.Run("falls back to remote defaults", func(t *testing.T) {
		mcpConfig := map[string]any{"mode": "remote"}
		got := appendGitHubMCPTools(nil, "github", mcpConfig, mcpConfig)
		assert.Len(t, got, len(constants.DefaultGitHubToolsRemote))
		assert.Contains(t, got, "mcp__github__issue_read")
	})
}

func TestDedupeAllowedTools(t *testing.T) {
	got := dedupeAllowedTools([]string{"Read", "Bash", "Read", "Bash"})
	assert.Equal(t, []string{"Read", "Bash"}, got)
}
