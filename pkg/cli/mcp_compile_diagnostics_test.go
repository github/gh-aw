//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const compileDependencyCause = "shellcheck not available: binary not found in PATH and Docker is not running"
const compileDependencyRemediation = "install shellcheck or start Docker to enable run step linting"

func TestMCPCompileDiagnosticChild(t *testing.T) {
	scenario := os.Getenv("GH_AW_COMPILE_DIAGNOSTIC_CHILD")
	if scenario == "" {
		return
	}
	if scenario == "cancel" {
		time.Sleep(time.Minute)
		os.Exit(1)
	}
	log := logger.New("workflow:mcp_compile_test")
	if scenario == "unlisted" {
		log = logger.New("console:mcp_compile_test")
	}
	if scenario == "huge-line" {
		fmt.Fprint(os.Stderr, "✗ "+compileDependencyCause+" ")
		for range 12 << 10 {
			fmt.Fprint(os.Stderr, strings.Repeat("x", 1024))
		}
		fmt.Fprintln(os.Stderr, " "+compileDependencyRemediation)
	} else {
		for range 12 << 10 {
			log.Printf("%s", strings.Repeat("x", 1024))
		}
		fmt.Fprintf(os.Stderr, "\x1b[31m✗ %s\x1b[0m\n", compileDependencyCause)
		log.Print("debug interleaved before remediation")
		fmt.Fprintf(os.Stderr, "  %s\n", compileDependencyRemediation)
	}
	log.Print("trailing debug must not replace the dependency cause")
	fmt.Fprintln(os.Stderr, "✓ cleanup completed")
	if scenario == "whitespace" {
		fmt.Fprint(os.Stdout, " \n\t")
	}
	if scenario == "structured" {
		require.NoError(t, json.NewEncoder(os.Stdout).Encode(structuredCompileFailure()))
	}
	os.Exit(1)
}

func structuredCompileFailure() []ValidationResult {
	return []ValidationResult{{
		Scope: "batch", Workflow: "shellcheck", Valid: false,
		Errors:   []ValidationIssue{{Type: "shellcheck_error", Message: strings.Repeat("scanner findings\n", 4<<10)}},
		Warnings: []ValidationIssue{},
	}}
}

func TestCompileToolPreservesStructuredFailureAfterDebugFlood(t *testing.T) {
	t.Parallel()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	require.NoError(t, registerCompileTool(server, compileDiagnosticChild("structured"), ""))
	result, err := connectInMemory(t, server).CallTool(context.Background(), &mcp.CallToolParams{
		Name: "compile", Arguments: map[string]any{"workflows": []string{"windows"}, "dry_run": true},
	})
	require.NoError(t, err)
	output := extractTextResult(t, result)
	assert.Greater(t, len(output), maxMCPCompileFallbackBytes, "the fallback budget must not discard structured scanner findings")
	var results []ValidationResult
	require.NoError(t, json.Unmarshal([]byte(output), &results))
	assert.Equal(t, structuredCompileFailure(), results)
	assert.NotContains(t, output, "workflow:mcp_compile_test")
}

func compileDiagnosticChild(scenario string) execCmdFunc {
	return func(ctx context.Context, _ ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPCompileDiagnosticChild$")
		cmd.Env = append(os.Environ(), "GH_AW_COMPILE_DIAGNOSTIC_CHILD="+scenario, "DEBUG=workflow:mcp_compile_test,console:mcp_compile_test", "DEBUG_COLORS=0")
		return cmd
	}
}

func TestCompileToolBoundsHugeDebugStderr(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"debug", "whitespace", "huge-line", "unlisted"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
			require.NoError(t, registerCompileTool(server, compileDiagnosticChild(scenario), ""))
			session := connectInMemory(t, server)
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
				Name: "compile", Arguments: map[string]any{"workflows": []string{"windows", "archie"}, "dry_run": true},
			})
			require.NoError(t, err)
			require.False(t, result.IsError)
			output := extractTextResult(t, result)
			require.LessOrEqual(t, len(output), maxMCPCompileFallbackBytes)
			var results []ValidationResult
			require.NoError(t, json.Unmarshal([]byte(output), &results))
			require.Len(t, results, 2)
			for i, validation := range results {
				assert.Equal(t, []string{"windows.md", "archie.md"}[i], validation.Workflow)
				assert.False(t, validation.Valid)
				require.Len(t, validation.Errors, 1)
				assert.Equal(t, "config_error", validation.Errors[0].Type)
				message := validation.Errors[0].Message
				assert.True(t, strings.HasPrefix(message, "✗ "+compileDependencyCause), message)
				assert.Contains(t, message, compileDependencyRemediation)
				assert.LessOrEqual(t, len(message), maxMCPCompileDiagnosticBytes)
				assert.True(t, utf8.ValidString(message))
				assert.NotContains(t, message, "workflow:mcp_compile_test")
				assert.NotContains(t, message, "console:mcp_compile_test")
				assert.NotContains(t, message, "cleanup completed")
				assert.Contains(t, message, "omitted")
			}
		})
	}
}

func TestCompileToolBoundsSpawnFailure(t *testing.T) {
	t.Parallel()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	execCmd := func(ctx context.Context, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, filepath.Join(t.TempDir(), "missing-compiler"))
	}
	require.NoError(t, registerCompileTool(server, execCmd, ""))
	result, err := connectInMemory(t, server).CallTool(context.Background(), &mcp.CallToolParams{Name: "compile", Arguments: map[string]any{"workflows": []string{"test"}}})
	require.NoError(t, err)
	assert.Contains(t, extractTextResult(t, result), "missing-compiler")
}

func TestMCPCompileDiagnosticsStreaming(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, input, expected string
	}{
		{"plain", compileDependencyCause + "\n  " + compileDependencyRemediation + "\ncli:test noise +1ms\n", compileDependencyCause + "\n" + compileDependencyRemediation},
		{"ansi", "\x1b[31m✗ cause\x1b[0m\n  remediation\nℹ cleanup\n", "✗ cause\nremediation"},
		{"warning", "⚠ dependency unavailable\n✓ cleanup\n", "⚠ dependency unavailable"},
		{"empty", "", "exit status 1"},
		{"debug-only", "cli:test noise +1ms\n", "exit status 1"},
		{"multiple-errors", "✗ earlier error\n✗ terminal cause\n  remediation\n", "✗ terminal cause\nremediation"},
		{"interleaved-debug", "✗ cause\nworkflow:compile noisy debug +1ms\n  remediation\n", "✗ cause\nremediation"},
		{"unlisted-debug-only", "console:render Rendering table +1ms\ngitutil:gitutil Running git command +1ms\n", "exit status 1"},
		{"shellcheck-only", "shellcheck findings in a.lock.yml:\nscript: line 1: SC2086 quote variable\n\n", "exit status 1"},
		{"shellcheck-warning", "⚠ dependency unavailable\n⚠ shellcheck findings in a.lock.yml:\nscript: line 1: SC2086\n\n", "⚠ dependency unavailable"},
		{"shellcheck-after-error", "✗ cause\nshellcheck findings in a.lock.yml:\nscript: line 1: SC2086\n\n", "✗ cause"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			diagnostics := &mcpCompileDiagnostics{}
			for _, char := range []byte(tt.input) {
				n, err := diagnostics.Write([]byte{char})
				require.NoError(t, err)
				require.Equal(t, 1, n)
			}
			diagnostics.finishLine()
			message := diagnostics.failureMessage(errors.New("exit status 1"))
			assert.True(t, strings.HasPrefix(message, tt.expected), message)
			assert.LessOrEqual(t, len(message), maxMCPCompileDiagnosticBytes)
		})
	}
}

func TestMCPCompileDiagnosticsExcludeScannerFindingsFromFailure(t *testing.T) {
	t.Parallel()
	for _, header := range []string{"shellcheck findings in a.lock.yml:", "⚠ shellcheck findings in a.lock.yml:"} {
		diagnostics := &mcpCompileDiagnostics{}
		_, err := io.WriteString(diagnostics, "⚠ dependency unavailable\n"+header+"\nscript: line 1: SC2086\n\n")
		require.NoError(t, err)
		diagnostics.finishLine()
		assert.Empty(t, diagnostics.message)
		assert.Equal(t, "⚠ dependency unavailable", diagnostics.warning)
		assert.Equal(t, "⚠ dependency unavailable", diagnostics.failureMessage(errors.New("exit status 1")))
		assert.Contains(t, diagnostics.shellcheck.String(), header)
		assert.Contains(t, diagnostics.shellcheck.String(), "SC2086")
	}
}

func TestMCPCompileDiagnosticsLoggerFormat(t *testing.T) {
	t.Parallel()
	for _, namespace := range []string{"console:render", "gitutil:gitutil", "jsonutil:parse", "typeutil:types", "future-package:new_namespace", "bootstrap"} {
		for _, duration := range []string{"0ns", "5µs", "12ms", "1.5s", "2.0m", "3.0h"} {
			line := namespace + " debug message +" + duration
			assert.True(t, isCompileDebugLine(line), line)
			diagnostics := &mcpCompileDiagnostics{}
			_, err := io.WriteString(diagnostics, line+"\n")
			require.NoError(t, err)
			diagnostics.finishLine()
			assert.Empty(t, diagnostics.message)
			assert.Contains(t, diagnostics.failureMessage(errors.New("exit status 1")), "omitted")
		}
	}
	for _, line := range []string{
		"Error: dependency unavailable +1ms",
		"error: dependency unavailable +1ms",
		"workflow.md: error: bad config +1ms",
		"console:render real diagnostic without elapsed time",
		"gitutil:gitutil error with invalid duration +oops",
		"✗ shellcheck not available",
	} {
		assert.False(t, isCompileDebugLine(line), line)
	}
}

func TestMCPCompileDiagnosticsPreserveShellcheck(t *testing.T) {
	t.Parallel()
	const findings = "shellcheck findings in a.lock.yml\nscript: line 1: SC2086 quote variable\n\nshellcheck findings in b.lock.yml\nscript: line 2: SC1000 syntax error\n"
	diagnostics := &mcpCompileDiagnostics{}
	_, err := io.WriteString(diagnostics, "cli:test noise +1ms\n"+findings+"workflow:test trailing +1ms\n")
	require.NoError(t, err)
	diagnostics.finishLine()
	assert.Equal(t, extractShellcheckDiagnostics(findings), extractShellcheckDiagnostics(diagnostics.shellcheck.String()))
	assert.NotContains(t, diagnostics.shellcheck.String(), "cli:test")
}

func TestMCPCompileDiagnosticTextByteBoundaries(t *testing.T) {
	t.Parallel()
	for _, text := range []string{
		strings.Repeat("a", maxMCPCompileDiagnosticBytes),
		strings.Repeat("a", maxMCPCompileDiagnosticBytes/2-1) + "界" + "remediation",
		strings.Repeat("界", maxMCPCompileDiagnosticBytes/3),
	} {
		var diagnostic compileDiagnosticText
		for _, char := range []byte(text) {
			diagnostic.Write([]byte{char})
		}
		assert.Equal(t, text, diagnostic.String())
		assert.Equal(t, text, boundMCPCompileDiagnostic(text))
	}
	message := "cause\n" + strings.Repeat("界", maxMCPCompileDiagnosticBytes) + "\nremediation"
	bounded := boundMCPCompileDiagnostic(message)
	assert.True(t, utf8.ValidString(bounded))
	assert.True(t, strings.HasPrefix(bounded, "cause\n"))
	assert.True(t, strings.HasSuffix(bounded, "\nremediation"))
	assert.LessOrEqual(t, len(bounded), maxMCPCompileDiagnosticBytes)
	assert.Contains(t, bounded, "truncated")
	var streamed compileDiagnosticText
	for range 12 << 10 {
		streamed.Write([]byte(strings.Repeat("x", 1024)))
		assert.LessOrEqual(t, len(streamed.head)+len(streamed.tail), maxMCPCompileDiagnosticBytes)
	}
	assert.Contains(t, streamed.String(), "truncated")
}

func TestMCPCompileFallbackByteBudget(t *testing.T) {
	t.Parallel()
	for _, message := range []string{
		strings.Repeat("a", maxMCPCompileDiagnosticBytes),
		strings.Repeat("\x00\"\\<>&", maxMCPCompileDiagnosticBytes),
		strings.Repeat("界", maxMCPCompileDiagnosticBytes),
	} {
		workflows := make([]string, 334)
		for i := range workflows {
			workflows[i] = fmt.Sprintf("workflow-%d", i)
		}
		output, err := marshalMCPCompileErrorResults(workflows, "dependency cause\n"+message+"\nremediation")
		require.NoError(t, err)
		assert.LessOrEqual(t, len(output), maxMCPCompileFallbackBytes)
		var results []ValidationResult
		require.NoError(t, json.Unmarshal(output, &results))
		require.Len(t, results, 1)
		assert.Equal(t, "batch", results[0].Scope)
		assert.False(t, results[0].Valid)
		diagnostic := results[0].Errors[0].Message
		assert.True(t, strings.HasPrefix(diagnostic, "dependency cause"))
		assert.Contains(t, diagnostic, "remediation")
		assert.Contains(t, diagnostic, "334 workflows")
		assert.Contains(t, diagnostic, "per-workflow entries omitted")
		assert.LessOrEqual(t, len(diagnostic), maxMCPCompileDiagnosticBytes)
		assert.True(t, utf8.ValidString(diagnostic))
	}
}

func TestMCPCompileFallbackDiscoveryByteBudget(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	require.NoError(t, os.MkdirAll(".github/workflows", 0o755))
	emptyOutput, err := marshalMCPCompileErrorResults(nil, "dependency cause")
	require.NoError(t, err)
	var emptyResults []ValidationResult
	require.NoError(t, json.Unmarshal(emptyOutput, &emptyResults))
	require.Len(t, emptyResults, 1)
	assert.False(t, emptyResults[0].Valid)
	assert.Empty(t, emptyResults[0].Scope)
	for i := range 334 {
		require.NoError(t, os.WriteFile(fmt.Sprintf(".github/workflows/workflow-%d.md", i), []byte("---\non: workflow_dispatch\n---\n"), 0o600))
	}
	output, err := marshalMCPCompileErrorResults(nil, strings.Repeat("x", maxMCPCompileDiagnosticBytes))
	require.NoError(t, err)
	assert.LessOrEqual(t, len(output), maxMCPCompileFallbackBytes)
	assert.Contains(t, string(output), "334 workflows")
}

func TestMCPCompileOutputCancellationDuringExecution(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, _, err := runMCPCompileOutput(ctx, compileDiagnosticChild("cancel"))
	require.Error(t, err)
	require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
}

func TestMCPCompileOutputCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	executed := false
	output, diagnostics, err := runMCPCompileOutput(ctx, func(context.Context, ...string) *exec.Cmd {
		executed = true
		return exec.Command("unused")
	})
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, output)
	assert.NotNil(t, diagnostics)
	assert.False(t, executed)
}

func TestMCPCompileOutputRequestIsolation(t *testing.T) {
	t.Parallel()
	for i := range 8 {
		t.Run(fmt.Sprintf("request-%d", i), func(t *testing.T) {
			t.Parallel()
			marker := fmt.Sprintf("request-%d", i)
			stdout, diagnostics, err := runMCPCompileOutput(context.Background(), mockCommandWithOutput(`[]`, "✗ "+marker+"\n"))
			require.NoError(t, err)
			assert.Equal(t, `[]`, string(stdout))
			assert.Equal(t, "✗ "+marker, diagnostics.failureMessage(errors.New("unused")))
		})
	}
}
