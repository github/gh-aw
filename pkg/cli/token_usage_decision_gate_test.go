//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const decisionGateUsageReceipt = `{
  "version":1,"decision":"run","status":"evaluated","provider":"openai",
  "resolved_model":"openai/gpt-6-luna","usage_complete":true,
  "usage":{"input_tokens":1000,"output_tokens":100,"total_tokens":1100},
  "questions":[{"index":0,"decision":"run","status":"evaluated","provider":"openai",
    "resolved_model":"openai/gpt-6-luna","duration_ms":13,"evaluated":true,
    "usage":{"input_tokens":1000,"output_tokens":100,"total_tokens":1100}}]
}`

func writeDecisionGateUsageReceipt(t *testing.T, runDir string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "decision-gate.json"), []byte(decisionGateUsageReceipt), 0600))
}

func TestAnalyzeTokenUsageIncludesDecisionGateActivationCost(t *testing.T) {
	runDir := t.TempDir()
	writeDecisionGateUsageReceipt(t, runDir)
	usageDir := filepath.Join(runDir, "usage", "agent")
	require.NoError(t, os.MkdirAll(usageDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(usageDir, "token_usage.jsonl"), []byte(
		`{"provider":"openai","model":"gpt-6-luna","input_tokens":2000,"output_tokens":200}`+"\n",
	), 0600))

	summary, err := analyzeTokenUsage(runDir, false)
	require.NoError(t, err)
	require.NotNil(t, summary)
	require.Contains(t, summary.ByModel, "gpt-6-luna")
	require.InDelta(t, 3000, summary.TotalInputTokens, 0)
	require.InDelta(t, 300, summary.TotalOutputTokens, 0)
	require.Equal(t, 2, summary.TotalRequests)
	require.InDelta(t, 0.045, summary.TotalAIC, 1e-9)
	require.InDelta(t, summary.TotalAIC, summary.ByModel["gpt-6-luna"].AIC, 1e-9)

	aicOnly, err := analyzeTokenUsageAICOnly(runDir, false)
	require.NoError(t, err)
	require.NotNil(t, aicOnly)
	require.InDelta(t, summary.TotalAIC, aicOnly.TotalAIC, 1e-9)
}

func TestAnalyzeEpisodeUsageIncludesDecisionGateActivationCost(t *testing.T) {
	runDir := t.TempDir()
	writeDecisionGateUsageReceipt(t, runDir)

	summary, err := analyzeEpisodeUsage([]string{runDir}, false, false)
	require.NoError(t, err)
	require.NotNil(t, summary)
	require.Equal(t, 1, summary.TotalRequests)
	require.InDelta(t, 1000, summary.TotalInputTokens, 0)
	require.InDelta(t, 100, summary.TotalOutputTokens, 0)
	require.InDelta(t, 0.015, summary.TotalAIC, 1e-9)
}
