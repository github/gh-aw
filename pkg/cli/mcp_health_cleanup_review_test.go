//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestHealthCheckpointCleanupOnCompletionAndShutdown(t *testing.T) {
	var paths []string
	calls := 0
	execCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		calls++
		for _, arg := range args {
			if path, ok := strings.CutPrefix(arg, "--checkpoint="); ok {
				paths = append(paths, filepath.Dir(path))
				require.NoError(t, os.WriteFile(path+".interrupted", []byte("private"), 0o600))
			}
		}
		output := `{"schema_version":1,"coverage":{"partial":false}}`
		if calls == 2 || calls == 3 {
			output = `{"schema_version":1,"coverage":{"partial":true},"next_action":{"type":"resume"}}`
		}
		return mockCommandWithOutput(output, "")(ctx, args...)
	}
	store := &healthMCPCheckpoints{}
	t.Cleanup(func() { _ = store.close() })
	server := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)
	registerHealthTool(server, execCmd, store)
	first := connectInMemory(t, server)
	second := connectInMemory(t, server)
	call := func(client *mcp.ClientSession, args map[string]any) *mcp.CallToolResult {
		result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "health", Arguments: args})
		require.NoError(t, err)
		return result
	}
	call(first, nil)
	_, err := os.Stat(paths[0])
	require.ErrorIs(t, err, os.ErrNotExist)

	partial := call(first, nil)
	data, err := json.Marshal(partial.StructuredContent)
	require.NoError(t, err)
	var report healthFleetReport
	require.NoError(t, json.Unmarshal(data, &report))
	require.NotNil(t, report.NextAction)
	token := report.NextAction.Checkpoint
	require.NotEmpty(t, token)
	_, err = second.CallTool(t.Context(), &mcp.CallToolParams{Name: "health", Arguments: map[string]any{"resume": token}})
	require.Error(t, err)
	call(second, nil)
	require.DirExists(t, paths[1])
	require.DirExists(t, paths[2])

	require.NoError(t, store.close())
	for _, path := range paths {
		_, err := os.Stat(path)
		require.ErrorIs(t, err, os.ErrNotExist)
	}
	_, err = first.CallTool(t.Context(), &mcp.CallToolParams{Name: "health", Arguments: map[string]any{"resume": token}})
	require.Error(t, err)
	require.NoError(t, store.close())
}

func TestHealthCheckpointShutdownWaitsForActiveAndExpiry(t *testing.T) {
	store := &healthMCPCheckpoints{}
	session := &mcp.ServerSession{}
	checkpoint, token, err := store.open(session, "")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(checkpoint.dir, "health.json.tmp"), []byte("private"), 0o600))

	checkpoint.expiry.Stop()
	started := make(chan struct{})
	expired := make(chan struct{})
	checkpoint.expiry = time.AfterFunc(0, func() {
		close(started)
		defer close(expired)
		checkpoint.mu.Lock()
		defer checkpoint.mu.Unlock()
		_ = store.remove(token, checkpoint)
	})
	<-started
	done := make(chan error, 1)
	go func() { done <- store.close() }()
	require.Eventually(t, func() bool {
		store.mu.Lock()
		defer store.mu.Unlock()
		return store.closed
	}, time.Second, time.Millisecond)
	select {
	case <-done:
		t.Fatal("shutdown returned while checkpoint was in use")
	default:
	}
	_, _, err = store.open(session, "")
	require.ErrorIs(t, err, os.ErrClosed)
	checkpoint.mu.Unlock()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("shutdown did not finish")
	}
	select {
	case <-expired:
	case <-time.After(time.Second):
		t.Fatal("expiry callback did not finish")
	}
	_, err = os.Stat(checkpoint.dir)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestHealthCheckpointStoresAreIsolated(t *testing.T) {
	first, second := &healthMCPCheckpoints{}, &healthMCPCheckpoints{}
	t.Cleanup(func() {
		_ = first.close()
		_ = second.close()
	})
	session := &mcp.ServerSession{}
	checkpoint, token, err := first.open(session, "")
	require.NoError(t, err)
	checkpoint.mu.Unlock()
	_, _, err = second.open(session, token)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NoError(t, second.close())
	require.DirExists(t, checkpoint.dir)
	resumed, _, err := first.open(session, token)
	require.NoError(t, err)
	resumed.mu.Unlock()
	require.NoError(t, first.close())
	_, err = os.Stat(checkpoint.dir)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestHealthCheckpointServerUsesOwnedStore(t *testing.T) {
	script := filepath.Join(t.TempDir(), "health-command")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s' '{\"schema_version\":1,\"coverage\":{\"partial\":true},\"next_action\":{\"type\":\"resume\"}}'\n"), 0o700))
	store := &healthMCPCheckpoints{}
	t.Cleanup(func() { _ = store.close() })
	server := createMCPServer(script, "", false, false, "", nil, mcpServerCaches{healthCheckpoints: store})
	client := connectInMemory(t, server)
	result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "health"})
	require.NoError(t, err)
	data, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	var report healthFleetReport
	require.NoError(t, json.Unmarshal(data, &report))
	require.NotNil(t, report.NextAction)
	store.mu.Lock()
	checkpoint := store.items[report.NextAction.Checkpoint]
	store.mu.Unlock()
	require.NotNil(t, checkpoint)
	require.DirExists(t, checkpoint.dir)
	require.NoError(t, store.close())
	_, err = os.Stat(checkpoint.dir)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestHealthCheckpointCleanupOnFailedAndResumedRequests(t *testing.T) {
	var paths []string
	calls := 0
	execCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		calls++
		for _, arg := range args {
			if path, ok := strings.CutPrefix(arg, "--checkpoint="); ok {
				paths = append(paths, filepath.Dir(path))
				require.NoError(t, os.WriteFile(path+".tmp", []byte("private"), 0o600))
			}
		}
		switch calls {
		case 1, 3:
			return exec.CommandContext(ctx, "sh", "-c", "exit 1")
		case 2:
			return mockCommandWithOutput(`{"schema_version":1,"coverage":{"partial":true},"next_action":{"type":"resume"}}`, "")(ctx, args...)
		default:
			return mockCommandWithOutput(`{"schema_version":1,"coverage":{"partial":false}}`, "")(ctx, args...)
		}
	}
	store := &healthMCPCheckpoints{}
	t.Cleanup(func() { _ = store.close() })
	server := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)
	registerHealthTool(server, execCmd, store)
	client := connectInMemory(t, server)
	_, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "health"})
	require.Error(t, err)
	_, err = os.Stat(paths[0])
	require.ErrorIs(t, err, os.ErrNotExist)
	result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "health"})
	require.NoError(t, err)
	data, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	var report healthFleetReport
	require.NoError(t, json.Unmarshal(data, &report))
	require.NotNil(t, report.NextAction)
	args := map[string]any{"resume": report.NextAction.Checkpoint}
	_, err = client.CallTool(t.Context(), &mcp.CallToolParams{Name: "health", Arguments: args})
	require.Error(t, err)
	require.DirExists(t, paths[1])
	_, err = client.CallTool(t.Context(), &mcp.CallToolParams{Name: "health", Arguments: args})
	require.NoError(t, err)
	require.Equal(t, paths[1], paths[2])
	require.Equal(t, paths[1], paths[3])
	_, err = os.Stat(paths[1])
	require.ErrorIs(t, err, os.ErrNotExist)
}
