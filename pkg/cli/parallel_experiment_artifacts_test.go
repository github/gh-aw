package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/require"
)

func TestParallelExperimentArtifactsRemainSeparate(t *testing.T) {
	dir := t.TempDir()
	names := []string{"parallel-experiment-1-entry-1-agent", "parallel-experiment-2-entry-2-agent"}
	for _, name := range names {
		artifact := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Join(artifact, "evidence"), 0700))
		require.NoError(t, os.WriteFile(filepath.Join(artifact, "parallel.json"), []byte(`{"configured":{"experiment":"style","variant":"`+name+`"},"usage":{"aic":null}}`), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(artifact, "evidence", "agent-stdio.log"), []byte("experimental"), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(artifact, "evidence", "agent_usage.json"), []byte(`{"ai_credits":42}`), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(artifact, "evidence", "token-usage.jsonl"), []byte("{}"), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(artifact, "evidence", "events.jsonl"), []byte("{}"), 0600))
	}
	require.True(t, artifactMatchesFilter(names[0], ResolveArtifactFilter([]string{"experiment"})))
	require.False(t, artifactMatchesFilter(names[0], ResolveArtifactFilter([]string{"agent"})))
	require.NoError(t, flattenSingleFileArtifacts(dir, false))
	require.NoError(t, flattenUnifiedArtifact(dir, false))
	require.Empty(t, findArtifactDir(dir, "agent", "agent-artifacts"))
	require.Empty(t, findAgentUsageFile(dir))
	require.Empty(t, findTokenUsageFile(dir))
	require.Empty(t, findEventsJSONLFile(dir))
	require.Empty(t, findAgentStdioLogPath(dir))
	_, found := findAgentLogFile(dir, workflow.NewCopilotEngine())
	require.False(t, found)
	data := extractExperimentData(dir)
	require.NotNil(t, data)
	require.Len(t, data.Parallel, 2)
	require.Empty(t, data.Assignments)
	require.False(t, experimentMatchesFilter(data, "style", names[0]))
	require.Equal(t, names[0], data.Parallel[0].Artifact)
	var manifest map[string]any
	require.NoError(t, json.Unmarshal(data.Parallel[0].Manifest, &manifest))
	require.Nil(t, manifest["usage"].(map[string]any)["aic"])
	require.FileExists(t, filepath.Join(dir, names[1], "parallel.json"))
}

func TestParallelExperimentCacheRequiresEnumeration(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "parallel-experiment-1-entry-1-agent"), 0700))
	require.Equal(t, []string{parallelExperimentArtifactPrefix}, findMissingFilterEntries([]string{parallelExperimentArtifactPrefix}, dir))
	require.False(t, artifactMatchesFilter("parallel-experiment-1-entry-1-agent", []string{"agent"}))
	require.True(t, artifactMatchesFilter("parallel-experiment-1-entry-1-agent", []string{parallelExperimentArtifactPrefix}))
	usable, err := cachedAuditArtifactsUsable(auditRunConfig{outputDir: dir, artifactFilter: ResolveArtifactFilter([]string{"experiment"})})
	require.NoError(t, err)
	require.False(t, usable)
	require.NoError(t, markArtifactDownloaded(dir, string(ArtifactSetAll)))
	usable, err = cachedAuditArtifactsUsable(auditRunConfig{outputDir: dir, artifactFilter: ResolveArtifactFilter([]string{"experiment"})})
	require.NoError(t, err)
	require.False(t, usable)
	name := "parallel-experiment-1-entry-1-agent"
	require.Equal(t, []string{name}, missingParallelDownloadNames([]string{name}, dir))
	require.NoError(t, markArtifactDownloaded(dir, name))
	require.Empty(t, missingParallelDownloadNames([]string{name}, dir))
}

func TestParallelExperimentPartialArtifact(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "parallel-experiment-1-entry-1-agent"), 0700))
	data := extractExperimentData(dir)
	require.NotNil(t, data)
	require.Len(t, data.Parallel, 1)
	require.Equal(t, "parallel manifest unavailable", data.Parallel[0].Error)
}
