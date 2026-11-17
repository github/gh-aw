//go:build !integration

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeEpisodeAccountingFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for path, content := range files {
		path = filepath.Join(dir, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
}

func runEpisodeAccountingReport(t *testing.T, files map[string]string) (EpisodeOutcomesData, string) {
	t.Helper()
	dir := t.TempDir()
	writeEpisodeAccountingFiles(t, dir, files)
	logs := episodeLogs(1)
	logs.Runs[0].LogsPath = dir
	data := LogsData{Runs: logs.Runs, Episodes: []EpisodeData{logs.Episodes[0].EpisodeData}}
	reports := []OutcomesData{{RunID: 1, Items: []OutcomeReport{episodeObservation(1, "item-1", OutcomeStatusAccepted).OutcomeReport}}}
	require.NoError(t, attachLogsOutcomes(&data, reports))
	require.NotNil(t, data.EpisodeOutcomes)
	require.Len(t, data.EpisodeOutcomes.Episodes, 1)
	var diagnostics []string
	for _, qualification := range data.EpisodeOutcomes.Episodes[0].Qualifications {
		if strings.HasPrefix(qualification, "Run 1: ") {
			diagnostics = append(diagnostics, qualification)
		}
	}
	return *data.EpisodeOutcomes, strings.Join(diagnostics, "\n")
}

func TestEpisodeAccountingPhaseSources(t *testing.T) {
	t.Parallel()
	agent := episodeAccountingAWFRecord("agent-1", 3, 3) + "\n" + episodeAccountingAWFRecord("agent-2", 5, 8)
	detection := episodeAccountingAWFRecord("detection-1", 1, 1) + "\n" + episodeAccountingAWFRecord("detection-2", 1, 2)
	for _, tc := range []struct {
		name    string
		files   map[string]string
		cost    float64
		warning string
	}{
		{
			name: "zero detection preserves agent JSON fallback",
			files: map[string]string{
				"usage/agent/token_usage.jsonl": "",
				"usage/agent_usage.json":        `{"ai_credits":8}`,
				"usage/detection_usage.jsonl":   `{"provider":"unknown","ai_credits":0}`,
			},
			cost: 8,
		},
		{
			name: "positive detection preserves agent JSON fallback",
			files: map[string]string{
				"usage/agent/token_usage.jsonl": "",
				"usage/agent_usage.json":        `{"ai_credits":8}`,
				"usage/detection_usage.jsonl":   `{"ai_credits":2}`,
			},
			cost: 10,
		},
		{
			name: "zero detection does not hide missing agent accounting",
			files: map[string]string{
				"usage/agent/token_usage.jsonl": "",
				"usage/detection_usage.jsonl":   `{"provider":"unknown","ai_credits":0}`,
			},
			warning: "agent usage: no usable accounting",
		},
		{
			name: "positive detection does not hide missing agent accounting",
			files: map[string]string{
				"usage/agent/token_usage.jsonl": "",
				"usage/detection_usage.jsonl":   `{"ai_credits":2}`,
			},
			cost: 2, warning: "agent usage: no usable accounting",
		},
		{
			name: "unpriced agent uses its own fallback",
			files: map[string]string{
				"usage/agent/token_usage.jsonl": `{"event":"token_usage","provider":"unknown","input_tokens":10}`,
				"usage/agent_usage.json":        `{"ai_credits":8}`,
				"usage/detection_usage.jsonl":   `{"ai_credits":2}`,
			},
			cost: 10, warning: "token_usage.jsonl line 1: unpriced usage",
		},
		{
			name: "malformed agent fallback emits a single diagnostic",
			files: map[string]string{
				"usage/agent/token_usage.jsonl": "invalid",
				"usage/agent_usage.json":        `{"ai_credits":8}`,
				"usage/detection_usage.jsonl":   `{"ai_credits":2}`,
			},
			cost: 10, warning: "token_usage.jsonl line 1: malformed usage record",
		},
		{
			name: "genuine agent zero remains authoritative",
			files: map[string]string{
				"usage/agent/token_usage.jsonl": episodeAccountingAWFRecord("agent-zero", 0, 0),
				"usage/agent_usage.json":        `{"ai_credits":8}`,
				"usage/detection_usage.jsonl":   `{"ai_credits":2}`,
			},
			cost: 2,
		},
		{
			name: "independent AWF cumulative counters",
			files: map[string]string{
				"usage/agent/token_usage.jsonl":     agent,
				"usage/detection/token_usage.jsonl": detection,
			},
			cost: 10,
		},
		{
			name: "workflow call discovers every accounting stream",
			files: map[string]string{
				"abc123-usage/agent/token_usage.jsonl":     agent,
				"abc123-usage/detection/token_usage.jsonl": detection,
			},
			cost: 10,
		},
		{
			name: "workflow call selects agent JSON fallback separately",
			files: map[string]string{
				"abc123-usage/agent/token_usage.jsonl": "",
				"abc123-usage/agent_usage.json":        `{"ai_credits":8}`,
				"abc123-usage/detection_usage.jsonl":   `{"ai_credits":0}`,
			},
			cost: 8,
		},
		{
			name: "prefixed malformed detection qualifies agent cost once",
			files: map[string]string{
				"abc123-usage/agent/token_usage.jsonl": agent,
				"abc123-usage/detection_usage.jsonl":   "invalid",
			},
			cost: 8, warning: "detection_usage.jsonl line 1: malformed usage record",
		},
		{
			name: "mirrored requests do not hide independent detection cost",
			files: map[string]string{
				"usage/agent/token_usage.jsonl":              agent,
				"usage/detection/mirrored-token-usage.jsonl": agent,
				"usage/detection_usage.jsonl":                `{"ai_credits":2}`,
			},
			cost: 10, warning: "2 duplicate token usage record(s)",
		},
		{
			name: "partly mirrored stream excludes shared cumulative prefix",
			files: map[string]string{
				"usage/agent/token_usage.jsonl":     agent,
				"usage/detection/token_usage.jsonl": agent + "\n" + episodeAccountingAWFRecord("new-request", 2, 10),
			},
			cost: 10, warning: "2 duplicate token usage record(s)",
		},
		{
			name: "mirrored suffix excludes cumulative prefix omitted from file",
			files: map[string]string{
				"usage/agent/token_usage.jsonl": agent,
				"usage/detection/token_usage.jsonl": episodeAccountingAWFRecord("agent-2", 5, 8) + "\n" +
					episodeAccountingAWFRecord("new-request", 2, 10),
			},
			cost: 10, warning: "1 duplicate token usage record(s)",
		},
		{
			name: "failed agent fallback preserves detection cost",
			files: map[string]string{
				"usage/agent_usage.json":      "{",
				"usage/detection_usage.jsonl": `{"ai_credits":2}`,
			},
			cost: 2, warning: "Cannot inspect agent usage",
		},
		{
			name: "skipped detection placeholder is not missing execution",
			files: map[string]string{
				"usage/agent_usage.jsonl":           `{"ai_credits":8}`,
				"usage/detection/token_usage.jsonl": "",
			},
			cost: 8,
		},
		{
			name: "executed detection without accounting is qualified",
			files: map[string]string{
				"usage/agent_usage.jsonl":           `{"ai_credits":8}`,
				"usage/detection/token_usage.jsonl": "",
				"usage/detection/execution.json":    `{"exit_code":0,"adoption":"success"}`,
			},
			cost: 8, warning: "detection usage: no usable accounting",
		},
		{
			name: "executed agent without a stream is qualified",
			files: map[string]string{
				"usage/agent/execution.json":  `{"exit_code":0,"adoption":"success"}`,
				"usage/detection_usage.jsonl": `{"ai_credits":2}`,
			},
			cost: 2, warning: "agent usage: no usable accounting",
		},
		{
			name: "evals counter cannot replace or suppress agent fallback",
			files: map[string]string{
				"usage/agent_usage.json":        `{"ai_credits":8}`,
				"usage/evals/token_usage.jsonl": detection,
			},
			cost: 10,
		},
		{
			name: "non-usage metadata is not a missing agent stream",
			files: map[string]string{
				"usage/aw-info.jsonl":         `{"workflow":"test"}`,
				"usage/detection_usage.jsonl": `{"ai_credits":2}`,
			},
			cost: 2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			report, diagnostics := runEpisodeAccountingReport(t, tc.files)
			require.Len(t, report.Episodes, 1)
			episode := report.Episodes[0]
			require.NotNil(t, episode.ObservedAIC)
			assert.InDelta(t, tc.cost, *episode.ObservedAIC, 1e-9)
			if tc.warning == "" {
				assert.Empty(t, diagnostics)
				assert.Equal(t, "available", episode.CostCoverage)
				require.NotNil(t, episode.CostPerAcceptedOutcome)
				assert.InDelta(t, tc.cost, *episode.CostPerAcceptedOutcome, 1e-9)
			} else {
				assert.Equal(t, "partial", episode.CostCoverage)
				assert.Nil(t, episode.CostPerAcceptedOutcome)
				assert.Equal(t, 1, strings.Count(diagnostics, tc.warning), "one diagnostic per accounting issue")
				count := 0
				for _, qualification := range episode.Qualifications {
					if strings.Contains(qualification, tc.warning) {
						count++
					}
				}
				assert.Equal(t, 1, count, "a shared warning must not be appended by a second scanner")
			}
		})
	}
}

func TestEpisodeAccountingAggregatesCurrentAttemptArtifacts(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir()
	collectionDir := filepath.Join(runDir, "collection")
	require.NoError(t, os.MkdirAll(collectionDir, 0o700))
	rootInfo, err := json.Marshal(AwInfo{RunID: NumericID(42), RunAttempt: "2", Repository: "org/repo"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(collectionDir, "aw_info.json"), rootInfo, 0o600))
	writeArtifact := func(name, attempt, usage string) {
		t.Helper()
		dir := filepath.Join(runDir, name)
		info, err := json.Marshal(AwInfo{RunID: NumericID(42), RunAttempt: attempt, Repository: "org/repo"})
		require.NoError(t, err)
		writeEpisodeAccountingFiles(t, dir, map[string]string{
			"aw_info.json":            string(info),
			"agent/token_usage.jsonl": usage,
		})
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".episode-complete"), []byte("v1\n"), 0o600))
	}
	writeArtifact("20-usage", "2", episodeAccountingAWFRecord("shared-request", 0.25, 0.25))
	writeArtifact("21-usage", "2", episodeAccountingAWFRecord("shared-request", 0.25, 0.25)+"\n"+
		episodeAccountingAWFRecord("independent-request", 0.75, 1.0))
	writeArtifact("22-usage", "1", episodeAccountingAWFRecord("old-request", 9, 9))
	inventory, err := json.Marshal([]string{"../20-usage", "../21-usage"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(collectionDir, ".episode-artifacts.json"), inventory, 0o600))
	usage, err := analyzeTokenUsageAICOnly(collectionDir, false)
	require.NoError(t, err)
	require.NotNil(t, usage)
	assert.InDelta(t, 1.0, usage.TotalAIC, 1e-9)
}

func TestEpisodeAccountingPrefixedAgentDiscovery(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeEpisodeAccountingFiles(t, dir, map[string]string{
		"abc123-usage/agent/token_usage.jsonl":                   episodeAccountingAWFRecord("agent", 8, 8),
		"abc123-usage/detection/token_usage.jsonl":               episodeAccountingAWFRecord("detection", 2, 2),
		"abc123-usage/agent_usage.json":                          `{"ai_credits":8}`,
		"aaa-mirror/agent_usage.json":                            `{"ai_credits":99}`,
		"sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl": episodeAccountingAWFRecord("stale-proxy", 99, 99),
	})
	assert.Equal(t, filepath.Join(dir, "abc123-usage", "agent", "token_usage.jsonl"), findTokenUsageFile(dir))
	assert.Equal(t, filepath.Join(dir, "abc123-usage", "agent_usage.json"), findAgentUsageFile(dir))
}

func TestEpisodeAccountingLegacyFieldPresence(t *testing.T) {
	t.Parallel()
	for _, record := range []string{
		`{"aiCredits":"invalid"}`,
		`{"aic":null}`,
		`{"ai_credits":null}`,
		`{"aiCredits":null}`,
		`{"usage":{"aiCredits":"invalid"}}`,
		`{"usage":{"aic":null}}`,
		`{"usage":{"ai_credits":null}}`,
		`{"inputTokens":10,"model":"unpriced"}`,
		`{"usage":{"cacheReadTokens":10,"model":"unpriced"}}`,
	} {
		t.Run(record, func(t *testing.T) {
			t.Parallel()
			report, diagnostics := runEpisodeAccountingReport(t, map[string]string{
				"abc123-usage/agent_usage.jsonl":        `{"aic":8}`,
				"abc123-usage/detection/requests.jsonl": record,
			})
			episode := report.Episodes[0]
			require.NotNil(t, episode.ObservedAIC)
			assert.InDelta(t, 8, *episode.ObservedAIC, 1e-9)
			assert.Equal(t, "partial", episode.CostCoverage)
			assert.Nil(t, episode.CostPerAcceptedOutcome)
			assert.Equal(t, 1, strings.Count(diagnostics, "requests.jsonl line 1: unpriced usage"))
		})
	}
}

func TestEpisodeAccountingLegacyZeroAliases(t *testing.T) {
	t.Parallel()
	for _, record := range []string{
		`{"aiCredits":0}`, `{"aic":0}`, `{"ai_credits":0}`,
		`{"aiCredits":"0"}`, `{"usage":{"aic":"0"}}`,
	} {
		t.Run(record, func(t *testing.T) {
			t.Parallel()
			report, diagnostics := runEpisodeAccountingReport(t, map[string]string{
				"usage/agent_usage.jsonl":            `{"aic":8}`,
				"usage/detection/additional.jsonl":   record,
				"usage/detection/another-file.jsonl": "\n",
			})
			episode := report.Episodes[0]
			assert.Empty(t, diagnostics)
			assert.Equal(t, "available", episode.CostCoverage)
			require.NotNil(t, episode.ObservedAIC)
			assert.InDelta(t, 8, *episode.ObservedAIC, 1e-9)
		})
	}
}
