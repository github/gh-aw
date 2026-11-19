//go:build !integration

package cli

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBuildEpisodeDataDoesNotReconstructUnrecordedCalls(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	makeRun := func(id int64, event string, offset time.Duration) RunData {
		return RunData{
			RunID: id, Event: event, Repository: "acme/repo",
			Ref: "refs/heads/main", SHA: "same-sha", Actor: "octocat", RunAttempt: "1",
			CreatedAt: start.Add(offset), AIC: new(float64(id)),
		}
	}
	runs := []RunData{
		makeRun(100, "push", 0),
		makeRun(200, "workflow_call", time.Minute),
		makeRun(300, "push", 24*time.Hour),
		makeRun(400, "workflow_call", 24*time.Hour+time.Minute),
	}
	episodes, edges := buildEpisodeData(runs, nil)
	assert.Empty(t, episodes)
	assert.Empty(t, edges)

	episodes, edges = buildEpisodeData([]RunData{runs[1], runs[3]}, nil)
	assert.Empty(t, episodes, "matching worker metadata is not canonical identity")
	assert.Empty(t, edges)
}

func TestBuildEpisodeDataConflictingBridgeIsOrderIndependent(t *testing.T) {
	t.Parallel()
	a := RunData{RunID: 100, AwContext: &AwContext{EpisodeID: "episode-a"}}
	bridge := RunData{RunID: 150, AwContext: &AwContext{RunID: "100"}, AIC: new(2.0)}
	b := RunData{RunID: 200, AwContext: &AwContext{RunID: "150", EpisodeID: "episode-b"}}
	expectedEpisodes, expectedEdges := buildEpisodeData([]RunData{a, bridge, b}, nil)
	for _, runs := range [][]RunData{
		{a, b, bridge}, {bridge, a, b}, {bridge, b, a}, {b, a, bridge}, {b, bridge, a},
	} {
		original := slices.Clone(runs)
		episodes, edges := buildEpisodeData(runs, nil)
		assert.Equal(t, expectedEpisodes, episodes)
		assert.Equal(t, expectedEdges, edges)
		assert.Equal(t, original, runs, "input ordering must remain unchanged")
	}
}
