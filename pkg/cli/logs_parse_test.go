//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow"
)

// SKIPPED: Scripts now use require() pattern and are loaded at runtime from external files
func TestParseAgentLog(t *testing.T) {
	t.Skip("Test skipped - agent log parser scripts now use require() pattern and are loaded at runtime from external files")
}

// SKIPPED: Scripts now use require() pattern and are loaded at runtime from external files
func TestParseAgentLogWithAgentOutputDir(t *testing.T) {
	t.Skip("Test skipped - agent log parser scripts now use require() pattern and are loaded at runtime from external files")
}

func TestParseAgentLogNoAgentOutput(t *testing.T) {
	// Create a temporary directory without agent logs
	tempDir := testutil.TempDir(t, "test-*")

	// Get the Claude engine
	registry := workflow.GetGlobalEngineRegistry()
	engine, err := registry.GetEngine("claude")
	if err != nil {
		t.Fatalf("Failed to get Claude engine: %v", err)
	}

	// Run the parser - should not fail, just skip
	err = parseAgentLog(tempDir, engine, true)
	if err != nil {
		t.Fatalf("parseAgentLog should not fail when agent logs are missing: %v", err)
	}

	// Check that log.md was NOT created
	logMdPath := filepath.Join(tempDir, "log.md")
	if _, err := os.Stat(logMdPath); !os.IsNotExist(err) {
		t.Fatalf("log.md should not be created when agent logs are missing")
	}
}

func TestParseAgentLogNoEngine(t *testing.T) {
	// Create a temporary directory with agent-stdio.log
	tempDir := testutil.TempDir(t, "test-*")

	agentStdioPath := filepath.Join(tempDir, "agent-stdio.log")
	if err := os.WriteFile(agentStdioPath, []byte("[]"), 0644); err != nil {
		t.Fatalf("Failed to create mock agent-stdio.log: %v", err)
	}

	// Run the parser with nil engine - should skip gracefully
	err := parseAgentLog(tempDir, nil, true)
	if err != nil {
		t.Fatalf("parseAgentLog should not fail with nil engine: %v", err)
	}

	// Check that log.md was NOT created
	logMdPath := filepath.Join(tempDir, "log.md")
	if _, err := os.Stat(logMdPath); !os.IsNotExist(err) {
		t.Fatalf("log.md should not be created when engine is nil")
	}
}
