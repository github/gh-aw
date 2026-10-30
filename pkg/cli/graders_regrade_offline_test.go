package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdditionalGradersSelection(t *testing.T) {
	workflowID := writeGraderRunWorkflow(t, "---\non: workflow_dispatch\ngraders:\n  custom-count:\n    script: return session.length\n  disabled:\n    enabled: false\n    script: return 1\n---\nTest.\n")
	definitions, err := loadAdditionalGraders([]string{workflowID + ":loops," + workflowID + ":custom-count", workflowID + ":loops"})
	require.NoError(t, err)
	require.Len(t, definitions, 2)
	assert.Equal(t, "custom-count", definitions[0].ID)
	assert.Equal(t, "loops", definitions[1].ID)
	assert.NotEmpty(t, definitions[0].Workflow)
	assert.NotEmpty(t, definitions[0].Selector)
	aliases, err := loadAdditionalGraders([]string{workflowID + ":loops", definitions[1].Workflow + ":loops"})
	require.NoError(t, err)
	assert.Len(t, aliases, 1)
	metadata, err := regradeDefinitionsMetadata(definitions)
	require.NoError(t, err)
	assert.Len(t, metadata[0].WorkflowDigest, 64)
	for _, selector := range []string{"", "loops", ":loops", workflowID + ":", workflowID + ":not-enabled", workflowID + ":disabled", workflowID + ":loops,"} {
		_, err := loadAdditionalGraders([]string{selector})
		require.Error(t, err, selector)
	}
}

func TestAdditionalGradersRejectsBashWithoutLoadingScript(t *testing.T) {
	workflowID := writeGraderRunWorkflow(t, "---\non: workflow_dispatch\ngraders:\n  operational-value:\n    run: ./does-not-exist.sh\n---\nTest.\n")
	_, err := loadAdditionalGraders([]string{workflowID + ":operational-value"})
	require.ErrorContains(t, err, "Bash")
}

func TestOfflineGradersCommandCollection(t *testing.T) {
	regradeTestRequireNode(t)
	workflowID := writeGraderRunWorkflow(t, "---\non: workflow_dispatch\ngraders:\n  extra:\n    script: return session.length\n---\nTest.\n")
	root := t.TempDir()
	second := regradeTestRun(t, root, 22, regradeTestEvent)
	first := regradeTestRun(t, root, 11, regradeTestEvent)
	const original = `{"version":2,"results":[{"id":"recorded","status":"pass","value":-4}]}`
	regradeTestFile(t, first.Run.LogsPath, "usage/graders/grader_results.json", original)
	regradeTestFile(t, root, "harbor/gh-aw-export.json", `{"format":"gh-aw-harbor"}`)
	regradeTestFile(t, root, "harbor/run-99/usage/aw_session.jsonl", "must not read")
	cmd := newGradersRunCommand()
	cmd.SetArgs([]string{workflowID, "loops", "--logs-dir", root, "--additional-grader", workflowID + ":extra"})
	var output bytes.Buffer
	cmd.SetOut(&output)
	require.NoError(t, cmd.Execute())
	var collection struct {
		Format  string
		Version int
		Runs    []RegradedRun
	}
	require.NoError(t, json.Unmarshal(output.Bytes(), &collection))
	assert.Equal(t, "gh-aw-regrading", collection.Format)
	assert.Equal(t, 1, collection.Version)
	require.Len(t, collection.Runs, 2)
	assert.Equal(t, first.Run.DatabaseID, collection.Runs[0].Run.DatabaseID)
	assert.Equal(t, second.Run.DatabaseID, collection.Runs[1].Run.DatabaseID)
	assert.JSONEq(t, original, string(collection.Runs[0].OriginalGraders))
	require.Len(t, collection.Runs[0].Results, 2)
	assert.Equal(t, "extra", collection.Runs[0].Definitions[0].ID)
	assert.Equal(t, "loops", collection.Runs[0].Definitions[1].ID)
	files, err := os.ReadDir(first.Run.LogsPath)
	require.NoError(t, err)
	assert.Len(t, files, 1, "offline grading must not write new files into run evidence")
}

func TestAdditionalGradersCollectionLoadDoesNotResolveRemoteImports(t *testing.T) {
	workflow := writeGraderRunWorkflow(t, "---\non: workflow_dispatch\nimports:\n  - owner/repo/shared.md@main\ngraders:\n  loops:\n    script: return session.length\n---\nTest.\n")
	_, err := loadAdditionalGraders([]string{workflow + ":loops"})
	require.ErrorContains(t, err, "offline imports require a cached pinned import")
}

func TestOfflineGradersNoArgumentsAndSummaryScores(t *testing.T) {
	workflowID := writeGraderRunWorkflow(t, "---\non: workflow_dispatch\n---\nTest.\n")
	root := t.TempDir()
	const original = `{"version":2,"results":[{"id":"recorded","status":"pass","value":0,"implementation":{"id":"historical","version":1}}],"total":1,"passed":1,"failed":0,"error_count":0,"unavailable_count":0}`
	summary := `{"runs":[{"run_id":42,"workflow_name":"old workflow","repository":"owner/repo","url":"https://github.com/owner/repo/actions/runs/42","run_attempt":"3","logs_path":"run-42","head_sha":"abc","status":"completed","conclusion":"failure","event":"schedule","branch":"main","actor":"tester","created_at":"2026-01-02T03:04:05Z","graders":` + original + `}]}`
	summaryPath := regradeTestFile(t, root, "summary.json", summary)
	cmd := newGradersRunCommand()
	cmd.SetArgs([]string{"--logs-dir", root, "--additional-grader", workflowID + ":loops"})
	var output bytes.Buffer
	cmd.SetOut(&output)
	require.NoError(t, cmd.Execute())
	var collection struct{ Runs []RegradedRun }
	require.NoError(t, json.Unmarshal(output.Bytes(), &collection))
	require.Len(t, collection.Runs, 1)
	run := collection.Runs[0]
	assert.Equal(t, "missing", run.SessionStatus)
	assert.Equal(t, "old workflow", run.Run.WorkflowName)
	assert.Equal(t, "abc", run.Run.HeadSha)
	assert.Equal(t, "completed", run.Run.Status)
	assert.Equal(t, "failure", run.Run.Conclusion)
	assert.Equal(t, "schedule", run.Run.Event)
	assert.Equal(t, "main", run.Run.HeadBranch)
	assert.Equal(t, "tester", run.Run.Actor)
	assert.False(t, run.Run.CreatedAt.IsZero())
	assert.Equal(t, 3, run.Run.Attempt)
	assert.Equal(t, "github.com/owner/repo/42/3", run.Identity)
	assert.JSONEq(t, original, string(run.OriginalGraders))
	require.Len(t, run.OriginalEvidence, 1)
	assert.Equal(t, summaryPath, run.OriginalEvidence[0].Path)
	assert.NotEmpty(t, run.OriginalEvidence[0].Field)
	var unavailable map[string]any
	require.NoError(t, json.Unmarshal(run.Results[0], &unavailable))
	assert.Equal(t, "unavailable", unavailable["status"])
	assert.NotEmpty(t, unavailable["error"])
	after, err := os.ReadFile(summaryPath)
	require.NoError(t, err)
	assert.Equal(t, summary, string(after))
}

func TestOfflineGradersSingleRunSummaryIgnoresCLIVersion(t *testing.T) {
	root := t.TempDir()
	regradeTestFile(t, root, "run_summary.json", `{"cli_version":"historical-version","run_id":33,"run":{"databaseId":33,"workflowName":"old workflow","repository":"owner/repo","attempt":4}}`)
	runs, err := loadOfflineGradingRuns(root)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, int64(33), runs[0].Run.DatabaseID)
	assert.Equal(t, 4, runs[0].Run.Attempt)
	assert.Equal(t, root, runs[0].Run.LogsPath)
}

func TestOfflineGradersRejectsHarborAncestorsAndSymlinks(t *testing.T) {
	for _, test := range []struct{ name, marker, content string }{
		{"job", "gh-aw-export.json", `{"format":"gh-aw-harbor"}`},
		{"unknown export", "gh-aw-export.json", `{"format":"other"}`},
		{"trial", "gh-aw/metadata.json", `{"format":"gh-aw-harbor-trial"}`},
		{"metadata", "metadata.json", `{"format":"gh-aw-harbor-trial"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			run := regradeTestRun(t, root, 1, regradeTestEvent)
			regradeTestFile(t, root, test.marker, test.content)
			_, err := loadOfflineGradingRuns(run.Run.LogsPath)
			require.ErrorContains(t, err, "Harbor")
			_, err = regradeProcessedRuns(t.Context(), root, []ProcessedRun{run}, nil)
			require.ErrorContains(t, err, "Harbor")
		})
	}
	t.Run("symlink root", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(root, "actual"), 0o700))
		require.NoError(t, os.Symlink(filepath.Join(root, "actual"), filepath.Join(root, "link")))
		_, err := loadOfflineGradingRuns(filepath.Join(root, "link"))
		require.ErrorContains(t, err, "symlink")
	})
	t.Run("symlink run directory", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(root, "actual"), 0o700))
		require.NoError(t, os.Symlink(filepath.Join(root, "actual"), filepath.Join(root, "run-77")))
		_, err := loadOfflineGradingRuns(root)
		require.ErrorContains(t, err, "symbolic link")
	})
	t.Run("harbor backup directory", func(t *testing.T) {
		root := t.TempDir()
		run := regradeTestRun(t, root, 77, "")
		regradeTestFile(t, root, ".harbor-backup-interrupted/gh-aw-export.json", `{"format":"gh-aw-harbor"}`)
		runs, err := loadOfflineGradingRuns(root)
		require.NoError(t, err)
		require.Len(t, runs, 1)
		assert.Equal(t, run.Run.DatabaseID, runs[0].Run.DatabaseID)
	})
	t.Run("symlink session parent", func(t *testing.T) {
		root := t.TempDir()
		run := regradeTestRun(t, root, 1, "")
		regradeTestFile(t, root, "external/aw_session.jsonl", regradeTestEvent)
		require.NoError(t, os.Symlink(filepath.Join(root, "external"), filepath.Join(run.Run.LogsPath, "usage")))
		results, err := regradeProcessedRuns(t.Context(), root, []ProcessedRun{run}, nil)
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, "invalid", results[0].SessionStatus)
		assert.Contains(t, results[0].SessionError, "link")
	})
}

func TestOfflineGradersSelectionAndIdentityErrors(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"workflow"},
		{"--logs-dir", "unused"},
		{"workflow", "--logs-dir", "unused"},
		{"workflow", "loops", "123", "--logs-dir", "unused"},
		{"--additional-grader", "workflow:loops"},
		{"workflow", "loops", "--additional-grader", "workflow:loops"},
	} {
		cmd := newGradersRunCommand()
		cmd.SetArgs(args)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		require.Error(t, cmd.Execute(), "%v", args)
	}
	t.Run("empty directory", func(t *testing.T) {
		_, err := loadOfflineGradingRuns(t.TempDir())
		require.ErrorContains(t, err, "no downloaded workflow runs")
	})
	t.Run("invalid summary", func(t *testing.T) {
		root := t.TempDir()
		regradeTestFile(t, root, "summary.json", "{broken")
		_, err := loadOfflineGradingRuns(root)
		require.ErrorContains(t, err, "invalid logs collection")
	})
	t.Run("path traversal", func(t *testing.T) {
		root := t.TempDir()
		regradeTestFile(t, root, "summary.json", `{"runs":[{"run_id":1,"logs_path":"../run-1"}]}`)
		_, err := loadOfflineGradingRuns(root)
		require.ErrorContains(t, err, "escapes")
	})
	t.Run("mismatched run metadata", func(t *testing.T) {
		root := t.TempDir()
		regradeTestFile(t, root, "run-1/run_summary.json", `{"run_id":2}`)
		_, err := loadOfflineGradingRuns(root)
		require.ErrorContains(t, err, "does not match")
	})
	t.Run("conflicting duplicate identity", func(t *testing.T) {
		root := t.TempDir()
		first := regradeTestRun(t, root, 1, "")
		second := first
		second.Run.LogsPath = filepath.Join(root, "elsewhere")
		_, err := regradeProcessedRuns(t.Context(), root, []ProcessedRun{first, second}, nil)
		require.ErrorContains(t, err, "conflicting evidence")
	})
}

func TestOfflineGradersRelocatedSummary(t *testing.T) {
	root := t.TempDir()
	regradeTestFile(t, root, "run-7/usage/aw_session.jsonl", regradeTestEvent)
	regradeTestFile(t, root, "summary.json", `{"runs":[{"run_id":7,"logs_path":"/nonexistent/old-root/run-7","repository":"owner/repo"}]}`)
	runs, err := loadOfflineGradingRuns(root)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, filepath.Join(root, "run-7"), runs[0].Run.LogsPath)
}

func TestOfflineGradersRebasesNestedSummaryPaths(t *testing.T) {
	root := t.TempDir()
	source := regradeEpisodeFixture(t, root, 2, "collection-a")
	recordedRoot := filepath.Join(t.TempDir(), "old-logs")
	recorded := filepath.Join(recordedRoot, "episodes", "github.com", "owner", "repo", "run-77", "attempt-2", "collection-a")
	summary := map[string]any{
		"logs_location": recordedRoot,
		"runs": []map[string]any{{
			"run_id": 77, "repository": "owner/repo", "url": "https://github.com/owner/repo/actions/runs/77",
			"run_attempt": "2", "logs_path": recorded,
		}},
	}
	data, err := json.Marshal(summary)
	require.NoError(t, err)
	regradeTestFile(t, root, "summary.json", string(data))
	runs, err := loadOfflineGradingRuns(root)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, source, runs[0].Run.LogsPath)
}

func TestOfflineGradersOriginalScoreSummaryRunCountMustMatch(t *testing.T) {
	root := t.TempDir()
	regradeTestFile(t, root, "summary.json", `{"runs":[{"graders":{"results":[]}}]}`)
	_, err := offlineGradingOriginals(root, nil)
	require.ErrorContains(t, err, "cannot safely correlate")
}

func TestOfflineGradersRejectsRawOnlyURLIdentityConflict(t *testing.T) {
	root := t.TempDir()
	regradeTestFile(t, root, "run.json", `{"id":77,"html_url":"https://github.com/owner/repo/actions/runs/88","repository":{"full_name":"owner/repo"}}`)
	_, err := loadOfflineGradingRuns(root)
	require.ErrorContains(t, err, "URL conflicts with recorded run ID")
}

func TestOfflineGradersRejectsSummaryOuterIDAndRunURLConflict(t *testing.T) {
	root := t.TempDir()
	regradeTestFile(t, root, "run_summary.json", `{"run_id":77,"run":{"url":"https://github.com/owner/repo/actions/runs/88"}}`)
	_, err := loadOfflineGradingRuns(root)
	require.ErrorContains(t, err, "URL conflicts with recorded run ID")
}

func TestAdditionalGradersRejectsCrossWorkflowIDCollisions(t *testing.T) {
	first := writeGraderRunWorkflow(t, "---\non: workflow_dispatch\n---\nFirst.\n")
	root, err := os.Getwd()
	require.NoError(t, err)
	regradeTestFile(t, root, ".github/workflows/other.md", "---\non: workflow_dispatch\n---\nOther.\n")
	_, err = loadAdditionalGraders([]string{first + ":loops", "other:loops"})
	require.ErrorContains(t, err, "use distinct grader IDs")
	definitions, err := loadAdditionalGraders([]string{first + ":loops", first + ":loops"})
	require.NoError(t, err)
	assert.Len(t, definitions, 1)
}

func TestOfflineGradersRawActionsMetadataIdentity(t *testing.T) {
	root := t.TempDir()
	const raw = `{"id":77,"run_number":8,"run_attempt":4,"html_url":"https://ghe.example/owner/repo/actions/runs/77","repository":{"full_name":"owner/repo"},"status":"completed","conclusion":"failure","name":"recorded","path":".github/workflows/recorded.yml","head_sha":"original-sha","head_branch":"main","event":"push","actor":{"login":"tester"},"created_at":"2026-01-02T03:04:05Z"}`
	path := regradeTestFile(t, root, "run.json", raw)
	runs, err := loadOfflineGradingRuns(root)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "ghe.example/owner/repo/77/4", harborRunIdentity(runs[0]))
	assert.Equal(t, "completed", runs[0].Run.Status)
	assert.Equal(t, "failure", runs[0].Run.Conclusion)
	assert.Equal(t, "original-sha", runs[0].Run.HeadSha)
	assert.Equal(t, "tester", runs[0].Run.Actor)
	assert.Equal(t, 8, runs[0].Run.Number)
	assert.False(t, runs[0].Run.CreatedAt.IsZero())
	results, err := regradeProcessedRuns(t.Context(), root, runs, nil)
	require.NoError(t, err)
	assert.Empty(t, results[0].IdentityMissing)
	assert.Equal(t, "missing", results[0].SessionStatus)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, raw, string(after)) //nolint:testifylint // Exact evidence bytes, not semantic JSON.
}

func TestOfflineGradersUnknownIdentityIsExplicit(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "run-9"), 0o700))
	runs, err := loadOfflineGradingRuns(root)
	require.NoError(t, err)
	results, err := regradeProcessedRuns(t.Context(), root, runs, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, []string{"host", "repository", "attempt"}, results[0].IdentityMissing)
	assert.Zero(t, results[0].Run.Attempt, "the Harbor key default must not fabricate a recorded attempt")
	assert.Empty(t, results[0].Run.Repository)
}

func TestOfflineGradersRejectsNativeHarborBundles(t *testing.T) {
	for _, test := range []struct {
		name, config, result string
		harbor               bool
	}{
		{"trial", `{"task":{"path":"task"},"agent":{"name":"recorded"}}`, `{"trial_name":"trial","task_name":"task","verifier_result":null}`, true},
		{"job", `{"agents":[],"tasks":[]}`, `{"stats":{},"n_total_trials":1}`, true},
		{"unrelated", `{"model":"test"}`, `{"value":1}`, false},
		{"malformed", `{broken`, `{"value":1}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			run := regradeTestRun(t, root, 1, regradeTestEvent)
			regradeTestFile(t, root, "config.json", test.config)
			regradeTestFile(t, root, "result.json", test.result)
			_, err := loadOfflineGradingRuns(run.Run.LogsPath)
			if test.harbor {
				require.ErrorContains(t, err, "native Harbor")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
