//go:build !integration

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/atif"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func assertATIFFile(t *testing.T, path string) {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	var trajectory atif.Trajectory
	require.NoError(t, json.Unmarshal(content, &trajectory))
	require.NoError(t, trajectory.Validate())
	require.Equal(t, atif.SchemaVersion, trajectory.SchemaVersion)
	require.True(t, bytes.HasSuffix(content, []byte("\n")))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Zero(t, info.Mode().Perm()&0o077)
}

func TestSessionsDownloadATIF(t *testing.T) {
	for _, source := range []string{"123", "https://github.example.com/owner/repo/actions/runs/123"} {
		t.Run(source, func(t *testing.T) {
			installSessionTestGH(t, map[string]map[string]string{"usage": {"aw_session.jsonl": sessionTestHeader + sessionTestAgentEvent}})
			output, err := executeSessionTestCommand(t, source, "--repo", "owner/repo", "--atif")
			require.NoError(t, err)
			var trajectory atif.Trajectory
			require.NoError(t, json.Unmarshal([]byte(output), &trajectory))
			require.NoError(t, trajectory.Validate())
			require.Equal(t, "Session download works.\n", trajectory.Steps[0].Message)
			require.NotContains(t, output, `"schema_version": "jsonl"`)
			path := filepath.Join(t.TempDir(), "session.atif")
			output, err = executeSessionTestCommand(t, source, "--repo", "owner/repo", "--atif", "-o", path)
			require.NoError(t, err)
			require.Empty(t, output)
			assertATIFFile(t, path)
		})
	}
}

func TestSessionsDownloadATIFErrorsPreserveDestination(t *testing.T) {
	installSessionTestGH(t, map[string]map[string]string{"usage": {"aw_session.jsonl": sessionTestHeader}})
	path := filepath.Join(t.TempDir(), "session.atif")
	require.NoError(t, os.WriteFile(path, []byte("existing"), 0o600))
	output, err := executeSessionTestCommand(t, "123", "--atif", "-o", path)
	require.ErrorContains(t, err, "at least one step")
	require.Empty(t, output)
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "existing", string(content))
	_, err = executeSessionTestCommand(t, "123", "--atif", "--format", "markdown")
	require.ErrorContains(t, err, "cannot be combined")
}

func TestExportRunATIFPreservesSession(t *testing.T) {
	for _, relative := range []string{"aw_session.jsonl", "usage/aw_session.jsonl"} {
		t.Run(relative, func(t *testing.T) {
			root := t.TempDir()
			content := sessionTestHeader + sessionTestAgentEvent
			writeSessionTestFile(t, root, relative, content)
			run := &parser.GitHubURLComponents{Number: 123, Owner: "owner", Repo: "repo", Host: "github.com"}
			for range 2 {
				require.NoError(t, exportRunATIF(t.Context(), run, root, false))
			}
			original, err := os.ReadFile(filepath.Join(root, relative))
			require.NoError(t, err)
			require.Equal(t, content, string(original))
			assertATIFFile(t, filepath.Join(root, strings.TrimSuffix(relative, ".jsonl")+".atif"))
		})
	}
}

func TestExportRunATIFDownloadsMissingSession(t *testing.T) {
	content := sessionTestHeader + sessionTestAgentEvent
	calls := installSessionTestGH(t, map[string]map[string]string{"usage": {"aw_session.jsonl": content}})
	root := t.TempDir()
	run := &parser.GitHubURLComponents{Number: 123, Owner: "owner", Repo: "repo", Host: "github.example.com"}
	require.NoError(t, exportRunATIF(t.Context(), run, root, false))
	require.NoError(t, exportRunATIF(t.Context(), run, root, false))
	original, err := os.ReadFile(filepath.Join(root, "usage", "aw_session.jsonl"))
	require.NoError(t, err)
	require.Equal(t, content, string(original))
	assertATIFFile(t, filepath.Join(root, "usage", "aw_session.atif"))
	commands, err := os.ReadFile(calls)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(commands), "run download"))
	require.Contains(t, string(commands), "--hostname github.example.com")
}

func TestExportRunATIFErrors(t *testing.T) {
	run := &parser.GitHubURLComponents{Number: 123}
	t.Run("bad input", func(t *testing.T) {
		root := t.TempDir()
		writeSessionTestFile(t, root, "aw_session.jsonl", "not-json\n")
		require.Error(t, exportRunATIF(t.Context(), run, root, false))
		require.NoFileExists(t, filepath.Join(root, "aw_session.atif"))
	})
	t.Run("write error", func(t *testing.T) {
		root := t.TempDir()
		writeSessionTestFile(t, root, "aw_session.jsonl", sessionTestHeader+sessionTestAgentEvent)
		require.NoError(t, os.Mkdir(filepath.Join(root, "aw_session.atif"), 0o700))
		require.ErrorContains(t, exportRunATIF(t.Context(), run, root, false), "write ATIF")
		require.FileExists(t, filepath.Join(root, "aw_session.jsonl"))
	})
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		require.ErrorIs(t, exportRunATIF(ctx, run, t.TempDir(), false), context.Canceled)
	})
	t.Run("download denied", func(t *testing.T) {
		installSessionTestGH(t, map[string]map[string]string{"usage": {"aw_session.jsonl": sessionTestHeader + sessionTestAgentEvent}})
		t.Setenv("SESSION_TEST_DOWNLOAD_ERROR", "usage")
		require.ErrorContains(t, exportRunATIF(t.Context(), run, t.TempDir(), false), "artifact download denied")
	})
}

func TestATIFFlagPropagation(t *testing.T) {
	cmd := NewLogsCommand()
	cmd.PersistentFlags().Bool("verbose", false, "")
	require.NoError(t, cmd.ParseFlags([]string{"--atif"}))
	opts, err := loadCommonLogsOptions(cmd)
	require.NoError(t, err)
	require.True(t, opts.ATIF)
	stdin, err := loadStdinLogsOptions(cmd)
	require.NoError(t, err)
	require.True(t, stdin.ATIF)
	audit := NewAuditCommand()
	audit.PersistentFlags().Bool("verbose", false, "")
	require.NoError(t, audit.ParseFlags([]string{"--atif"}))
	auditOpts, err := getAuditCommandOptions(audit)
	require.NoError(t, err)
	require.True(t, auditOpts.atif)
	cfg, err := newAuditRunConfig(123, AuditOptions{ATIF: true, Hostname: "github.com"})
	require.NoError(t, err)
	require.True(t, cfg.atif)
	for _, command := range []*cobra.Command{NewLogsCommand(), NewAuditCommand(), NewAuditDiffSubcommand()} {
		require.NotNil(t, command.Flags().Lookup("atif"))
	}
}

func TestExportRunATIFRejectsUnsafeInputFiles(t *testing.T) {
	for _, kind := range []string{"directory", "symlink", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "aw_session.jsonl")
			switch kind {
			case "directory":
				require.NoError(t, os.Mkdir(path, 0o700))
			case "symlink":
				target := filepath.Join(t.TempDir(), "session.jsonl")
				require.NoError(t, os.WriteFile(target, []byte(sessionTestHeader+sessionTestAgentEvent), 0o600))
				require.NoError(t, os.Symlink(target, path))
			case "oversized":
				file, err := os.Create(path)
				require.NoError(t, err)
				require.NoError(t, file.Truncate(atif.MaxSessionBytes+1))
				require.NoError(t, file.Close())
			}
			_, err := readATIFSessionFile(path)
			require.Error(t, err)
		})
	}
}

func TestLogsATIFExportError(t *testing.T) {
	root := t.TempDir()
	runDir := filepath.Join(root, "run-123")
	writeSessionTestFile(t, runDir, "usage/aw_session.jsonl", "invalid\n")
	var exportErr error
	collector := newOrderedLogsRunCollector(t.Context(), 0, 1, processWorkflowRunBatchOptions{
		outputDir: root, count: 1, atif: true, atifError: &exportErr,
	})
	collector.recordResult(0, DownloadResult{CachedRun: &RunData{RunID: 123, LogsPath: runDir}})
	require.ErrorContains(t, exportErr, "ATIF export for run 123")
	require.NoFileExists(t, filepath.Join(runDir, "usage", "aw_session.atif"))
}

func TestLogsATIFCachedRun(t *testing.T) {
	root := t.TempDir()
	runDir := filepath.Join(root, "run-123")
	content := sessionTestHeader + sessionTestAgentEvent
	writeSessionTestFile(t, runDir, "usage/aw_session.jsonl", content)
	for _, enabled := range []bool{false, true} {
		var exportErr error
		opts := processWorkflowRunBatchOptions{
			outputDir: root, count: 1, atif: enabled, atifError: &exportErr,
		}
		collector := newOrderedLogsRunCollector(t.Context(), 0, 1, opts)
		collector.recordResult(0, DownloadResult{CachedRun: &RunData{RunID: 123, LogsPath: runDir}})
		require.NoError(t, exportErr)
		if enabled {
			assertATIFFile(t, filepath.Join(runDir, "usage", "aw_session.atif"))
		} else {
			require.NoFileExists(t, filepath.Join(runDir, "usage", "aw_session.atif"))
		}
		require.Equal(t, 1, collector.acceptedCount)
	}
}

func TestLogsATIFStdinCachedRuns(t *testing.T) {
	root := t.TempDir()
	var script strings.Builder
	script.WriteString("#!/bin/sh\ncase \"$*\" in\n")
	for _, id := range []int64{123, 124} {
		fmt.Fprintf(&script, "  *\"/runs/%d --jq\"*) cat <<'EOF'\n", id)
		fmt.Fprintf(&script, `{"databaseId":%d,"htmlUrl":"https://github.com/owner/repo/actions/runs/%d","status":"completed","conclusion":"success","attempt":1,"updatedAt":"2026-01-01T00:01:00Z","repository":"owner/repo","workflowPath":".github/workflows/test.lock.yml"}`+"\nEOF\n    ;;\n", id, id)
		writeSessionTestFile(t, resolveAuditOutputDir(root, id), "usage/aw_session.jsonl", sessionTestHeader+sessionTestAgentEvent)
	}
	script.WriteString("  *) exit 1 ;;\nesac\n")
	binDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "gh"), []byte(script.String()), 0o700))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	writeATIFCachedRun(t, root, 123)
	runDir := resolveAuditOutputDir(root, 123)
	writeSessionTestFile(t, runDir, runAPIResponseFileName, `{"id":123,"status":"completed","conclusion":"success","path":".github/workflows/test.lock.yml","repository":{"full_name":"owner/repo"}}`)
	require.NoError(t, markArtifactDownloaded(runDir, constants.UsageArtifactName.String()))
	cachePath := filepath.Join(root, "cached.jsonl")
	writeSessionTestFile(t, root, "cached.jsonl", fmt.Sprintf(`{"schema_version":%d,"kind":"run","run":{"run_id":124,"workflow_path":".github/workflows/test.lock.yml","status":"completed","conclusion":"success","run_attempt":"1","updated_at":"2026-01-01T00:01:00Z","repository":"owner/repo"}}`+"\n", cachedLogsJSONLSchemaVersion))
	_, _ = captureOutput(t, func() error {
		err := DownloadWorkflowLogsFromStdin(t.Context(), StdinLogsOptions{
			RunURLs: []string{"123", "124"}, RepoOverride: "owner/repo", OutputDir: root,
			CachedJSONL: cachePath, ArtifactSets: []string{"usage"}, ATIF: true,
		})
		require.NoError(t, err)
		return err
	})
	for _, id := range []int64{123, 124} {
		assertATIFFile(t, filepath.Join(resolveAuditOutputDir(root, id), "usage", "aw_session.atif"))
	}
}

func writeATIFCachedRun(t *testing.T, root string, runID int64) {
	t.Helper()
	runDir := resolveAuditOutputDir(root, runID)
	summary := RunSummary{
		CLIVersion: GetVersion(), RunID: runID,
		RunAnalysis: RunAnalysis{Run: WorkflowRun{
			DatabaseID: runID, Status: "completed", Conclusion: "success",
			WorkflowPath: ".github/workflows/test.lock.yml", LogsPath: runDir,
		}},
	}
	content, err := json.Marshal(summary)
	require.NoError(t, err)
	writeSessionTestFile(t, runDir, runSummaryFileName, string(content))
	writeSessionTestFile(t, runDir, "usage/aw_session.jsonl", sessionTestHeader+sessionTestAgentEvent)
}

func TestAuditATIFCachedRuns(t *testing.T) {
	for _, mode := range []string{"single", "diff", "group"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			writeATIFCachedRun(t, root, 123)
			writeATIFCachedRun(t, root, 124)
			_, _ = captureOutput(t, func() error {
				switch mode {
				case "single":
					_, err := AuditWorkflowRun(t.Context(), 123, AuditOptions{
						OutputDir: root, Hostname: "github.com", ATIF: true,
						NoBaseline: true, JSONOutput: true, ArtifactSets: []string{"usage"},
					})
					require.NoError(t, err)
				case "diff":
					require.NoError(t, RunAuditDiff(t.Context(), 123, []int64{124}, AuditOptions{
						OutputDir: root, Hostname: "github.com", ATIF: true, JSONOutput: true,
					}))
				case "group":
					require.NoError(t, runAuditGrouped(t.Context(), []string{
						"https://github.com/owner/repo/actions/runs/123", "124",
					}, auditCommandOptions{outputDir: root, atif: true, artifacts: []string{"usage"}, jsonOutput: true}))
				}
				return nil
			})
			assertATIFFile(t, filepath.Join(root, "run-123", "usage", "aw_session.atif"))
			if mode != "single" {
				assertATIFFile(t, filepath.Join(root, "run-124", "usage", "aw_session.atif"))
			}
		})
	}
}
