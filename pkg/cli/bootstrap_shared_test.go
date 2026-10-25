//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootstrapMCPMarkerSatisfied(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, content string
		want          bool
	}{
		{"mcpServers", `{"mcpServers":{"github-agentic-workflows":{"command":" gh ","args":["aw","mcp-server","--verbose"]}}}`, true},
		{"servers fallback", `{"servers":{"github-agentic-workflows":{"command":"gh","args":["aw","mcp-server"]}}}`, true},
		{"empty primary fallback", `{"mcpServers":{},"servers":{"github-agentic-workflows":{"command":"gh","args":["aw","mcp-server"]}}}`, true},
		{"nonempty primary wins", `{"mcpServers":{"other":{"command":"gh"}},"servers":{"github-agentic-workflows":{"command":"gh","args":["aw","mcp-server"]}}}`, false},
		{"different command", `{"mcpServers":{"github-agentic-workflows":{"command":"other","args":["aw","mcp-server"]}}}`, false},
		{"missing arguments", `{"mcpServers":{"github-agentic-workflows":{"command":"gh","args":["aw"]}}}`, false},
		{"different arguments", `{"mcpServers":{"github-agentic-workflows":{"command":"gh","args":["aw","compile"]}}}`, false},
		{"invalid JSON", `{`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "mcp.json")
			if err := os.WriteFile(path, []byte(tt.content), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := isBootstrapMCPMarkerSatisfied(path, bootstrapMCPConfigPath)
			if err != nil || got != tt.want {
				t.Fatalf("got (%v, %v), want (%v, nil)", got, err, tt.want)
			}
		})
	}
}

func TestBootstrapMCPMarkerReadError(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "missing.json")
	got, err := isBootstrapMCPMarkerSatisfied(path, bootstrapMCPConfigPath)
	if got || err == nil || !strings.Contains(err.Error(), "failed to inspect "+bootstrapMCPConfigPath) {
		t.Fatalf("unexpected missing-marker result: (%v, %v)", got, err)
	}
}
