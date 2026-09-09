package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeProjectJSONAddedSettingsWin(t *testing.T) {
	existing := []byte(`{
		"utc": "-08:00",
		"help_command": true,
		"maintenance": {
			"runs_on": "self-hosted",
			"label_triggers": false
		},
		"action_pins": {
			"actions/checkout@v4": "internal/checkout@v4"
		}
	}`)
	added := []byte(`{
		"utc": "+01:00",
		"maintenance": {
			"label_triggers": true
		},
		"action_pins": {
			"actions/setup-go@v5": "internal/setup-go@v5"
		}
	}`)

	merged, err := mergeProjectJSON(existing, added)
	require.NoError(t, err)

	var settings map[string]any
	require.NoError(t, json.Unmarshal(merged, &settings))
	assert.Equal(t, "+01:00", settings["utc"])
	assert.Equal(t, true, settings["help_command"])
	assert.Equal(t, map[string]any{
		"runs_on":        "self-hosted",
		"label_triggers": true,
	}, settings["maintenance"])
	assert.Equal(t, map[string]any{
		"actions/checkout@v4": "internal/checkout@v4",
		"actions/setup-go@v5": "internal/setup-go@v5",
	}, settings["action_pins"])
}

func TestMergeProjectJSONRequiresObjects(t *testing.T) {
	_, err := mergeProjectJSON([]byte(`{}`), []byte(`[]`))
	require.ErrorContains(t, err, "added project file is not valid JSON")
}

func TestMergeProjectJSONPreservesLargeNumbers(t *testing.T) {
	merged, err := mergeProjectJSON(
		[]byte(`{"maintenance":{"action_failure_issue_expires":9007199254740993}}`),
		[]byte(`{"utc":"+01:00"}`),
	)
	require.NoError(t, err)
	assert.Contains(t, string(merged), "9007199254740993")
}

func TestMergeProjectFileWithTracking(t *testing.T) {
	gitRoot := t.TempDir()
	destFile := filepath.Join(gitRoot, workflow.RepoConfigFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(destFile), 0o755))
	require.NoError(t, os.WriteFile(destFile, []byte("{\"utc\":\"-08:00\",\"help_command\":true}\n"), 0o644))

	resolved := &ResolvedWorkflow{
		Spec: &WorkflowSpec{
			WorkflowPath:          "aw.json",
			DestinationPath:       workflow.RepoConfigFileName,
			IsPackageResourceFile: true,
		},
		Content:              []byte(`{"utc":"+01:00"}`),
		IsPackageProjectFile: true,
	}
	tracker := NewFileTracker()

	require.NoError(t, mergeProjectFileWithTracking(resolved, tracker, gitRoot))
	merged, err := os.ReadFile(destFile)
	require.NoError(t, err)
	assert.JSONEq(t, `{"utc":"+01:00","help_command":true}`, string(merged))
}

func TestMergeProjectFileWithTrackingDoesNotModifyCreatedFile(t *testing.T) {
	gitRoot := t.TempDir()
	destFile := filepath.Join(gitRoot, workflow.RepoConfigFileName)
	tracker := NewFileTracker()
	tracker.TrackCreated(destFile)
	require.NoError(t, os.MkdirAll(filepath.Dir(destFile), 0o755))
	require.NoError(t, os.WriteFile(destFile, []byte(`{"utc":"-08:00"}`), 0o644))

	resolved := &ResolvedWorkflow{
		Spec:    &WorkflowSpec{WorkflowPath: "aw.json"},
		Content: []byte(`{"utc":"+01:00"}`),
	}
	require.NoError(t, mergeProjectFileWithTracking(resolved, tracker, gitRoot))
	assert.Empty(t, tracker.ModifiedFiles)
}

func TestMergeProjectFileRejectsInvalidSettingsWithoutWriting(t *testing.T) {
	gitRoot := t.TempDir()
	destFile := filepath.Join(gitRoot, workflow.RepoConfigFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(destFile), 0o755))
	original := []byte("{\"utc\":\"-08:00\"}\n")
	require.NoError(t, os.WriteFile(destFile, original, 0o644))

	resolved := &ResolvedWorkflow{
		Spec:    &WorkflowSpec{WorkflowPath: "aw.json"},
		Content: []byte(`{"utc":"invalid"}`),
	}
	err := mergeProjectFileWithTracking(resolved, NewFileTracker(), gitRoot)
	require.Error(t, err)

	actual, readErr := os.ReadFile(destFile)
	require.NoError(t, readErr)
	assert.Equal(t, original, actual)
}

func TestMergeProjectFileRejectsDestinationSymlinkOutsideRoot(t *testing.T) {
	gitRoot := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.json")
	destFile := filepath.Join(gitRoot, workflow.RepoConfigFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(destFile), 0o755))
	require.NoError(t, os.Symlink(outside, destFile))

	resolved := &ResolvedWorkflow{
		Spec:    &WorkflowSpec{WorkflowPath: "aw.json"},
		Content: []byte(`{"utc":"+01:00"}`),
	}
	err := mergeProjectFileWithTracking(resolved, NewFileTracker(), gitRoot)
	require.Error(t, err)
	require.NoFileExists(t, outside)
}
