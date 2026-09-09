//go:build !integration

package cli

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/timeutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTokenUsageFile(t *testing.T) {
	t.Run("valid single entry", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "token-usage")
		filePath := filepath.Join(tmpDir, "token-usage.jsonl")

		content := `{"timestamp":"2026-04-01T17:56:38.042Z","request_id":"abc-123","provider":"anthropic","model":"claude-sonnet-4-6","path":"/v1/messages","status":200,"streaming":true,"input_tokens":100,"output_tokens":200,"cache_read_tokens":5000,"cache_write_tokens":3000,"duration_ms":2500,"response_bytes":1500}`
		require.NoError(t, os.WriteFile(filePath, []byte(content+"\n"), 0o644), "should write test file")

		summary, err := parseTokenUsageFile(filePath)
		require.NoError(t, err, "should parse without error")
		require.NotNil(t, summary, "should return non-nil summary")

		assert.Equal(t, 100, summary.TotalInputTokens, "input tokens")
		assert.Equal(t, 200, summary.TotalOutputTokens, "output tokens")
		assert.Equal(t, 5000, summary.TotalCacheReadTokens, "cache read tokens")
		assert.Equal(t, 3000, summary.TotalCacheWriteTokens, "cache write tokens")
		assert.Equal(t, 1, summary.TotalRequests, "total requests")
		assert.Equal(t, 2500, summary.TotalDurationMs, "total duration ms")
		assert.Equal(t, 1500, summary.TotalResponseBytes, "total response bytes")

		// Check by-model breakdown
		require.Contains(t, summary.ByModel, "claude-sonnet-4-6", "should have model entry")
		model := summary.ByModel["claude-sonnet-4-6"]
		assert.Equal(t, "anthropic", model.Provider, "model provider")
		assert.Equal(t, 100, model.InputTokens, "model input tokens")
		assert.Equal(t, 200, model.OutputTokens, "model output tokens")
	})

	t.Run("multiple entries with multiple models", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "token-usage")
		filePath := filepath.Join(tmpDir, "token-usage.jsonl")

		content := `{"timestamp":"2026-04-01T17:56:38.042Z","request_id":"1","provider":"anthropic","model":"claude-sonnet-4-6","path":"/v1/messages","status":200,"streaming":true,"input_tokens":3,"output_tokens":414,"cache_read_tokens":14044,"cache_write_tokens":26035,"duration_ms":6383,"response_bytes":2843}
{"timestamp":"2026-04-01T17:57:00.000Z","request_id":"2","provider":"anthropic","model":"claude-sonnet-4-6","path":"/v1/messages","status":200,"streaming":true,"input_tokens":3,"output_tokens":450,"cache_read_tokens":40984,"cache_write_tokens":0,"duration_ms":4000,"response_bytes":3000}
{"timestamp":"2026-04-01T17:58:00.000Z","request_id":"3","provider":"anthropic","model":"claude-haiku-4-5","path":"/v1/messages","status":200,"streaming":false,"input_tokens":769,"output_tokens":86,"cache_read_tokens":0,"cache_write_tokens":0,"duration_ms":700,"response_bytes":500}`
		require.NoError(t, os.WriteFile(filePath, []byte(content+"\n"), 0o644), "should write test file")

		summary, err := parseTokenUsageFile(filePath)
		require.NoError(t, err, "should parse without error")
		require.NotNil(t, summary, "should return non-nil summary")

		assert.Equal(t, 775, summary.TotalInputTokens, "total input tokens")
		assert.Equal(t, 950, summary.TotalOutputTokens, "total output tokens")
		assert.Equal(t, 55028, summary.TotalCacheReadTokens, "total cache read tokens")
		assert.Equal(t, 26035, summary.TotalCacheWriteTokens, "total cache write tokens")
		assert.Equal(t, 3, summary.TotalRequests, "total requests")
		assert.Equal(t, 11083, summary.TotalDurationMs, "total duration ms")

		// Check by-model
		require.Len(t, summary.ByModel, 2, "should have 2 models")
		assert.Equal(t, 2, summary.ByModel["claude-sonnet-4-6"].Requests, "sonnet requests")
		assert.Equal(t, 1, summary.ByModel["claude-haiku-4-5"].Requests, "haiku requests")

		assert.InDelta(t, 0.0, summary.CacheEfficiency, 0.001, "cache efficiency is not computed from raw token counts")
	})

	t.Run("prefers exact AWF-reported credits", func(t *testing.T) {
		fixturePath := filepath.Join("..", "..", "actions", "setup", "js", "fixtures", "awf-v0.28.7-aic-token-usage.jsonl")

		summary, err := parseTokenUsageFile(fixturePath)
		require.NoError(t, err)
		require.NotNil(t, summary)

		assert.Equal(t, 5, summary.TotalRequests)
		assert.InDelta(t, 1.03602, summary.TotalAIC, 1e-9)
		require.Contains(t, summary.ByModel, "gpt-4o-mini-2024-07-18")
		assert.InDelta(t, 1.03602, summary.ByModel["gpt-4o-mini-2024-07-18"].AIC, 1e-9)
		assert.Empty(t, summary.Warnings)
	})

	t.Run("retains legacy repricing when AWF-reported fields are absent", func(t *testing.T) {
		fixture, err := os.ReadFile(filepath.Join("..", "..", "actions", "setup", "js", "fixtures", "awf-v0.28.7-aic-token-usage.jsonl"))
		require.NoError(t, err)
		legacyLines := make([]string, 0, 5)
		for line := range strings.SplitSeq(strings.TrimSpace(string(fixture)), "\n") {
			var record map[string]any
			require.NoError(t, json.Unmarshal([]byte(line), &record))
			delete(record, "ai_credits_this_response")
			delete(record, "ai_credits_total")
			encoded, marshalErr := json.Marshal(record)
			require.NoError(t, marshalErr)
			legacyLines = append(legacyLines, string(encoded))
		}
		filePath := filepath.Join(testutil.TempDir(t, "token-usage-legacy-aic"), "token-usage.jsonl")
		require.NoError(t, os.WriteFile(filePath, []byte(strings.Join(legacyLines, "\n")+"\n"), 0o644))

		summary, err := parseTokenUsageFile(filePath)
		require.NoError(t, err)
		require.NotNil(t, summary)
		assert.InDelta(t, 0.44538, summary.TotalAIC, 1e-9)
	})

	t.Run("falls back and warns for malformed AWF-reported credits", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "token-usage-malformed-aic")
		filePath := filepath.Join(tmpDir, "token-usage.jsonl")
		content := `{"provider":"copilot","model":"gpt-4o-mini-2024-07-18","input_tokens":19288,"output_tokens":35,"cache_read_tokens":0,"cache_write_tokens":0,"ai_credits_this_response":"0.29142","ai_credits_total":-1}`
		require.NoError(t, os.WriteFile(filePath, []byte(content+"\n"), 0o644))

		summary, err := parseTokenUsageFile(filePath)
		require.NoError(t, err)
		require.NotNil(t, summary)

		assert.InDelta(t, 0.29142, summary.TotalAIC, 1e-9)
		require.Len(t, summary.Warnings, 1)
		assert.Contains(t, summary.Warnings[0], "fallback accounting")
	})

	t.Run("distinguishes zero AWF-reported credits from absent fields", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "token-usage-zero-aic")
		filePath := filepath.Join(tmpDir, "token-usage.jsonl")
		content := `{"provider":"copilot","model":"gpt-4o-mini-2024-07-18","input_tokens":19288,"output_tokens":35,"cache_read_tokens":0,"cache_write_tokens":0,"ai_credits_this_response":0,"ai_credits_total":0}`
		require.NoError(t, os.WriteFile(filePath, []byte(content+"\n"), 0o644))

		summary, err := parseTokenUsageFile(filePath)
		require.NoError(t, err)
		require.NotNil(t, summary)

		assert.Zero(t, summary.TotalAIC)
		assert.Zero(t, summary.ByModel["gpt-4o-mini-2024-07-18"].AIC)
		assert.Empty(t, summary.Warnings)
	})

	t.Run("treats null AWF fields as malformed rather than zero", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "token-usage-null-aic")
		filePath := filepath.Join(tmpDir, "token-usage.jsonl")
		content := `{"provider":"copilot","model":"gpt-4o-mini-2024-07-18","input_tokens":19288,"output_tokens":35,"cache_read_tokens":0,"cache_write_tokens":0,"ai_credits_this_response":null,"ai_credits_total":null}`
		require.NoError(t, os.WriteFile(filePath, []byte(content+"\n"), 0o644))

		summary, err := parseTokenUsageFile(filePath)
		require.NoError(t, err)
		require.NotNil(t, summary)

		assert.InDelta(t, 0.29142, summary.TotalAIC, 1e-9)
		require.Len(t, summary.Warnings, 1)
		assert.Contains(t, summary.Warnings[0], "fallback accounting")
	})

	t.Run("treats null cache semantics as absent", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "token-usage-null-cache-semantics")
		filePath := filepath.Join(tmpDir, "token-usage.jsonl")
		content := `{"provider":"copilot","model":"gpt-4o-mini-2024-07-18","input_tokens":1000,"output_tokens":100,"cache_read_tokens":400,"cache_write_tokens":100,"input_tokens_include_cache":null}`
		require.NoError(t, os.WriteFile(filePath, []byte(content+"\n"), 0o644))

		summary, err := parseTokenUsageFile(filePath)
		require.NoError(t, err)
		require.NotNil(t, summary)

		assert.InDelta(t, 0.0195, summary.TotalAIC, 1e-9)
		assert.Empty(t, summary.Warnings)
	})

	t.Run("uses legacy provider semantics and warns for invalid cache semantics", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "token-usage-invalid-cache-semantics")
		filePath := filepath.Join(tmpDir, "token-usage.jsonl")
		content := `{"provider":"copilot","model":"gpt-4o-mini-2024-07-18","input_tokens":1000,"output_tokens":100,"cache_read_tokens":400,"cache_write_tokens":100,"input_tokens_include_cache":"invalid"}`
		require.NoError(t, os.WriteFile(filePath, []byte(content+"\n"), 0o644))

		summary, err := parseTokenUsageFile(filePath)
		require.NoError(t, err)
		require.NotNil(t, summary)

		assert.InDelta(t, 0.0195, summary.TotalAIC, 1e-9)
		require.Len(t, summary.Warnings, 1)
		assert.Contains(t, summary.Warnings[0], "invalid input_tokens_include_cache")
	})

	t.Run("does not warn for invalid cache semantics when reported credits are valid", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "token-usage-valid-aic-invalid-cache-semantics")
		filePath := filepath.Join(tmpDir, "token-usage.jsonl")
		content := `{"provider":"copilot","model":"gpt-4o-mini-2024-07-18","input_tokens":1000,"output_tokens":100,"cache_read_tokens":400,"cache_write_tokens":100,"input_tokens_include_cache":"invalid","ai_credits_this_response":0.123,"ai_credits_total":0.123}`
		require.NoError(t, os.WriteFile(filePath, []byte(content+"\n"), 0o644))

		summary, err := parseTokenUsageFile(filePath)
		require.NoError(t, err)
		require.NotNil(t, summary)

		assert.InDelta(t, 0.123, summary.TotalAIC, 1e-9)
		assert.Empty(t, summary.Warnings)
	})

	for _, testCase := range []struct {
		name               string
		inputIncludesCache bool
		expectedAIC        float64
	}{
		{name: "inclusive cache fields", inputIncludesCache: true, expectedAIC: 0.018},
		{name: "additive cache fields", inputIncludesCache: false, expectedAIC: 0.0255},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "token-usage-explicit-cache-semantics")
			filePath := filepath.Join(tmpDir, "token-usage.jsonl")
			content := fmt.Sprintf(
				`{"provider":"copilot","model":"gpt-4o-mini-2024-07-18","input_tokens":1000,"output_tokens":100,"cache_read_tokens":400,"cache_write_tokens":100,"input_tokens_include_cache":%t}`,
				testCase.inputIncludesCache,
			)
			require.NoError(t, os.WriteFile(filePath, []byte(content+"\n"), 0o644))

			summary, err := parseTokenUsageFile(filePath)
			require.NoError(t, err)
			require.NotNil(t, summary)

			assert.InDelta(t, testCase.expectedAIC, summary.TotalAIC, 1e-9)
			assert.Empty(t, summary.Warnings)
		})
	}

	t.Run("continues from the last valid reported total after malformed data", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "token-usage-aic-continuation")
		filePath := filepath.Join(tmpDir, "token-usage.jsonl")
		content := `{"request_id":"one","provider":"copilot","model":"gpt-4o-mini-2024-07-18","input_tokens":10,"output_tokens":1,"ai_credits_this_response":0.2,"ai_credits_total":0.2}
{"request_id":"two","provider":"copilot","model":"gpt-4o-mini-2024-07-18","input_tokens":10,"output_tokens":1,"ai_credits_this_response":0.3,"ai_credits_total":"invalid"}`
		require.NoError(t, os.WriteFile(filePath, []byte(content+"\n"), 0o644))

		summary, err := parseTokenUsageFile(filePath)
		require.NoError(t, err)
		require.NotNil(t, summary)

		assert.InDelta(t, 0.5, summary.TotalAIC, 1e-9)
		assert.InDelta(t, 0.5, summary.ByModel["gpt-4o-mini-2024-07-18"].AIC, 1e-9)
		require.Len(t, summary.Warnings, 1)
	})

	t.Run("aggregates reported credits by model while preserving the run total", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "token-usage-multi-model-aic")
		filePath := filepath.Join(tmpDir, "token-usage.jsonl")
		content := `{"request_id":"one","provider":"copilot","model":"gpt-4o-mini","input_tokens":10,"output_tokens":1,"ai_credits_this_response":0.2,"ai_credits_total":0.2}
{"request_id":"two","provider":"copilot","model":"claude-sonnet-4-6","input_tokens":20,"output_tokens":2,"ai_credits_this_response":0.8,"ai_credits_total":1}`
		require.NoError(t, os.WriteFile(filePath, []byte(content+"\n"), 0o644))

		summary, err := parseTokenUsageFile(filePath)
		require.NoError(t, err)
		require.NotNil(t, summary)

		assert.InDelta(t, 0.2, summary.ByModel["gpt-4o-mini"].AIC, 1e-9)
		assert.InDelta(t, 0.8, summary.ByModel["claude-sonnet-4-6"].AIC, 1e-9)
		assert.InDelta(t, 1, summary.TotalAIC, 1e-9)
	})

	t.Run("uses the chronologically last valid reported total", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "token-usage-chronological-aic")
		filePath := filepath.Join(tmpDir, "token-usage.jsonl")
		content := `{"timestamp":"2026-08-28T09:00:02Z","request_id":"third","provider":"copilot","model":"gpt-4o-mini","input_tokens":1,"output_tokens":1,"ai_credits_this_response":1,"ai_credits_total":3}
{"timestamp":"2026-08-28T09:00:00Z","request_id":"first","provider":"copilot","model":"gpt-4o-mini","input_tokens":1,"output_tokens":1,"ai_credits_this_response":1,"ai_credits_total":1}
{"timestamp":"2026-08-28T09:00:01Z","request_id":"second","provider":"copilot","model":"gpt-4o-mini","input_tokens":1,"output_tokens":1,"ai_credits_this_response":1,"ai_credits_total":2}`
		require.NoError(t, os.WriteFile(filePath, []byte(content+"\n"), 0o644))

		summary, err := parseTokenUsageFile(filePath)
		require.NoError(t, err)
		require.NotNil(t, summary)

		assert.InDelta(t, 3, summary.TotalAIC, 1e-9)
		assert.Empty(t, summary.Warnings)
	})

	t.Run("warns when cumulative and per-request reported credits diverge", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "token-usage-divergent-aic")
		filePath := filepath.Join(tmpDir, "token-usage.jsonl")
		content := `{"request_id":"one","provider":"copilot","model":"gpt-4o-mini","input_tokens":1,"output_tokens":1,"ai_credits_this_response":0.2,"ai_credits_total":0.2}
{"request_id":"two","provider":"copilot","model":"gpt-4o-mini","input_tokens":1,"output_tokens":1,"ai_credits_this_response":0.8,"ai_credits_total":0.9}`
		require.NoError(t, os.WriteFile(filePath, []byte(content+"\n"), 0o644))

		summary, err := parseTokenUsageFile(filePath)
		require.NoError(t, err)
		require.NotNil(t, summary)

		assert.InDelta(t, 0.9, summary.TotalAIC, 1e-9)
		require.Len(t, summary.Warnings, 1)
		assert.Contains(t, summary.Warnings[0], "differs from the sum")
	})

	t.Run("deduplicates mirrored requests before aggregating AWF-reported credits", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "token-usage-duplicate-aic")
		filePath := filepath.Join(tmpDir, "token-usage.jsonl")
		record := `{"request_id":"same-request","provider":"copilot","model":"gpt-4o-mini-2024-07-18","input_tokens":10,"output_tokens":1,"ai_credits_this_response":0.2,"ai_credits_total":0.2}`
		require.NoError(t, os.WriteFile(filePath, []byte(record+"\n"+record+"\n"), 0o644))

		summary, err := parseTokenUsageFile(filePath)
		require.NoError(t, err)
		require.NotNil(t, summary)

		assert.Equal(t, 1, summary.TotalRequests)
		assert.InDelta(t, 0.2, summary.TotalAIC, 1e-9)
		require.Len(t, summary.Warnings, 1)
		assert.Contains(t, summary.Warnings[0], "duplicate token usage")
	})

	t.Run("extracts ambient context from first chronological invocation", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "token-usage")
		filePath := filepath.Join(tmpDir, "token-usage.jsonl")

		content := `{"timestamp":"2026-04-01T17:58:00.000Z","request_id":"2","provider":"anthropic","model":"claude-sonnet-4-6","path":"/v1/messages","status":200,"streaming":true,"input_tokens":12,"output_tokens":10,"cache_read_tokens":99,"cache_write_tokens":0,"duration_ms":4000,"response_bytes":3000}
{"timestamp":"2026-04-01T17:56:00.000Z","request_id":"1","provider":"anthropic","model":"claude-sonnet-4-6","path":"/v1/messages","status":200,"streaming":true,"input_tokens":7,"output_tokens":5,"cache_read_tokens":3,"cache_write_tokens":0,"duration_ms":1000,"response_bytes":500}`
		require.NoError(t, os.WriteFile(filePath, []byte(content+"\n"), 0o644), "should write test file")

		summary, err := parseTokenUsageFile(filePath)
		require.NoError(t, err, "should parse without error")
		require.NotNil(t, summary, "should return non-nil summary")
		require.NotNil(t, summary.AmbientContext, "ambient context should be present")
		assert.Equal(t, 7, summary.AmbientContext.InputTokens, "ambient input tokens should come from first invocation")
		assert.Equal(t, 3, summary.AmbientContext.CachedTokens, "ambient cached tokens should come from first invocation")
		assert.Equal(t, 0, summary.AmbientContext.EffectiveTokens, "ambient effective tokens are no longer computed")
	})

	t.Run("ambient context defaults cached tokens to zero when absent", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "token-usage")
		filePath := filepath.Join(tmpDir, "token-usage.jsonl")

		content := `{"timestamp":"2026-04-01T17:56:00.000Z","request_id":"1","provider":"anthropic","model":"claude-sonnet-4-6","path":"/v1/messages","status":200,"streaming":true,"input_tokens":11,"output_tokens":5,"duration_ms":1000,"response_bytes":500}`
		require.NoError(t, os.WriteFile(filePath, []byte(content+"\n"), 0o644), "should write test file")

		summary, err := parseTokenUsageFile(filePath)
		require.NoError(t, err, "should parse without error")
		require.NotNil(t, summary, "should return non-nil summary")
		require.NotNil(t, summary.AmbientContext, "ambient context should be present")
		assert.Equal(t, 11, summary.AmbientContext.InputTokens, "ambient input tokens should match")
		assert.Equal(t, 0, summary.AmbientContext.CachedTokens, "missing cached tokens should default to zero")
		assert.Equal(t, 0, summary.AmbientContext.EffectiveTokens, "ambient effective tokens are no longer computed")
	})

	t.Run("empty file returns nil", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "token-usage")
		filePath := filepath.Join(tmpDir, "token-usage.jsonl")
		require.NoError(t, os.WriteFile(filePath, []byte(""), 0o644))

		summary, err := parseTokenUsageFile(filePath)
		require.NoError(t, err, "should not error on empty file")
		assert.Nil(t, summary, "should return nil for empty file")
	})

	t.Run("file with only blank lines returns nil", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "token-usage")
		filePath := filepath.Join(tmpDir, "token-usage.jsonl")
		require.NoError(t, os.WriteFile(filePath, []byte("\n\n\n"), 0o644))

		summary, err := parseTokenUsageFile(filePath)
		require.NoError(t, err, "should not error on blank-only file")
		assert.Nil(t, summary, "should return nil for blank-only file")
	})

	t.Run("skips invalid JSON lines", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "token-usage")
		filePath := filepath.Join(tmpDir, "token-usage.jsonl")

		content := `not json
{"timestamp":"2026-04-01T17:56:38.042Z","request_id":"1","provider":"anthropic","model":"claude-sonnet-4-6","path":"/v1/messages","status":200,"streaming":true,"input_tokens":100,"output_tokens":200,"cache_read_tokens":0,"cache_write_tokens":0,"duration_ms":1000,"response_bytes":500}
also not json`
		require.NoError(t, os.WriteFile(filePath, []byte(content+"\n"), 0o644))

		summary, err := parseTokenUsageFile(filePath)
		require.NoError(t, err, "should not error on mixed content")
		require.NotNil(t, summary, "should return summary from valid lines")
		assert.Equal(t, 1, summary.TotalRequests, "should count only valid entries")
		assert.Equal(t, 100, summary.TotalInputTokens, "input tokens from valid entry")
	})

	t.Run("file not found returns error", func(t *testing.T) {
		_, err := parseTokenUsageFile("/nonexistent/path/token-usage.jsonl")
		assert.Error(t, err, "should error on missing file")
	})

	t.Run("entry with empty model uses unknown", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "token-usage")
		filePath := filepath.Join(tmpDir, "token-usage.jsonl")

		content := `{"timestamp":"2026-04-01T17:56:38.042Z","request_id":"1","provider":"anthropic","model":"","path":"/v1/messages","status":200,"streaming":true,"input_tokens":50,"output_tokens":25,"cache_read_tokens":0,"cache_write_tokens":0,"duration_ms":500,"response_bytes":200}`
		require.NoError(t, os.WriteFile(filePath, []byte(content+"\n"), 0o644))

		summary, err := parseTokenUsageFile(filePath)
		require.NoError(t, err, "should parse without error")
		require.NotNil(t, summary, "should return non-nil summary")
		require.Contains(t, summary.ByModel, "unknown", "should use 'unknown' for empty model")
	})
}

func TestFindTokenUsageFile(t *testing.T) {
	t.Run("finds in sandbox/firewall/logs path", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "find-token-usage")
		logsDir := filepath.Join(tmpDir, "sandbox", "firewall", "logs", "api-proxy-logs")
		require.NoError(t, os.MkdirAll(logsDir, 0o755))
		tokenFile := filepath.Join(logsDir, "token-usage.jsonl")
		require.NoError(t, os.WriteFile(tokenFile, []byte(`{"input_tokens":1}`+"\n"), 0o644))

		result := findTokenUsageFile(tmpDir)
		assert.Equal(t, tokenFile, result, "should find file in primary path")
	})

	t.Run("finds in sandbox/firewall/audit path (AWF v0.27.7+)", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "find-token-usage")
		auditDir := filepath.Join(tmpDir, "sandbox", "firewall", "audit", "api-proxy-logs")
		require.NoError(t, os.MkdirAll(auditDir, 0o755))
		tokenFile := filepath.Join(auditDir, "token-usage.jsonl")
		require.NoError(t, os.WriteFile(tokenFile, []byte(`{"input_tokens":1}`+"\n"), 0o644))

		result := findTokenUsageFile(tmpDir)
		assert.Equal(t, tokenFile, result, "should find file in AWF audit path")
	})

	t.Run("prefers sandbox/firewall/logs over sandbox/firewall/audit when both exist", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "find-token-usage")
		logsDir := filepath.Join(tmpDir, "sandbox", "firewall", "logs", "api-proxy-logs")
		auditDir := filepath.Join(tmpDir, "sandbox", "firewall", "audit", "api-proxy-logs")
		require.NoError(t, os.MkdirAll(logsDir, 0o755))
		require.NoError(t, os.MkdirAll(auditDir, 0o755))
		logsFile := filepath.Join(logsDir, "token-usage.jsonl")
		auditFile := filepath.Join(auditDir, "token-usage.jsonl")
		require.NoError(t, os.WriteFile(logsFile, []byte(`{"input_tokens":1}`+"\n"), 0o644))
		require.NoError(t, os.WriteFile(auditFile, []byte(`{"input_tokens":2}`+"\n"), 0o644))

		result := findTokenUsageFile(tmpDir)
		assert.Equal(t, logsFile, result, "should prefer primary logs path over AWF audit path")
	})

	t.Run("finds in firewall-audit-logs directory", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "find-token-usage")
		logsDir := filepath.Join(tmpDir, "firewall-audit-logs", "api-proxy-logs")
		require.NoError(t, os.MkdirAll(logsDir, 0o755))
		tokenFile := filepath.Join(logsDir, "token-usage.jsonl")
		require.NoError(t, os.WriteFile(tokenFile, []byte(`{"input_tokens":1}`+"\n"), 0o644))

		result := findTokenUsageFile(tmpDir)
		assert.Equal(t, tokenFile, result, "should find file in firewall-audit-logs")
	})

	t.Run("finds usage artifact token_usage.jsonl", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "find-token-usage")
		usageDir := filepath.Join(tmpDir, "usage", "agent")
		require.NoError(t, os.MkdirAll(usageDir, 0o755))
		tokenFile := filepath.Join(usageDir, "token_usage.jsonl")
		require.NoError(t, os.WriteFile(tokenFile, []byte(`{"input_tokens":1}`+"\n"), 0o644))

		result := findTokenUsageFile(tmpDir)
		assert.Equal(t, tokenFile, result, "should prefer usage artifact token usage file")
	})

	t.Run("returns empty string when not found", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "find-token-usage")
		result := findTokenUsageFile(tmpDir)
		assert.Empty(t, result, "should return empty string when file not found")
	})
}

func TestAnalyzeTokenUsageAICOnly(t *testing.T) {
	t.Run("sums agent and detection usage artifact jsonl files", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "analyze-token-usage-aic-only")
		usageDir := filepath.Join(tmpDir, "usage")
		require.NoError(t, os.MkdirAll(filepath.Join(usageDir, "agent"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(usageDir, "detection"), 0o755))

		require.NoError(t, os.WriteFile(
			filepath.Join(usageDir, "agent_usage.jsonl"),
			[]byte(`{"ai_credits":1.25}`+"\n"),
			0o644,
		))
		require.NoError(t, os.WriteFile(
			filepath.Join(usageDir, "detection_usage.jsonl"),
			[]byte(`{"usage":{"ai_credits":2.5}}`+"\n"),
			0o644,
		))
		require.NoError(t, os.WriteFile(
			filepath.Join(usageDir, "aw-info.jsonl"),
			[]byte(`{"note":"ignored"}`+"\n"),
			0o644,
		))

		summary, err := analyzeTokenUsageAICOnly(tmpDir, false)
		require.NoError(t, err)
		require.NotNil(t, summary)
		assert.InDelta(t, 3.75, summary.TotalAIC, 1e-9)
	})

	t.Run("falls back to agent_usage.json when token_usage.jsonl is empty", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "analyze-token-usage-aic-only-agent-usage")
		usageDir := filepath.Join(tmpDir, "usage")
		agentSubDir := filepath.Join(usageDir, "agent")
		require.NoError(t, os.MkdirAll(agentSubDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(agentSubDir, "token_usage.jsonl"), []byte(""), 0o644))
		require.NoError(t, os.WriteFile(
			filepath.Join(usageDir, "agent_usage.json"),
			[]byte(`{"input_tokens":5463,"output_tokens":17080,"cache_read_tokens":1440173,"cache_write_tokens":64504,"ambient_context":8424,"ai_credits":94.653,"primary_model":"claude-sonnet-4.6"}`),
			0o644,
		))

		summary, err := analyzeTokenUsageAICOnly(tmpDir, false)
		require.NoError(t, err)
		require.NotNil(t, summary)
		assert.InDelta(t, 94.653, summary.TotalAIC, 1e-6)
	})

	t.Run("falls back to agent_usage.json when token usage has no priced AIC", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "analyze-token-usage-aic-only-unpriced")
		usageDir := filepath.Join(tmpDir, "usage")
		agentSubDir := filepath.Join(usageDir, "agent")
		require.NoError(t, os.MkdirAll(agentSubDir, 0o755))
		require.NoError(t, os.WriteFile(
			filepath.Join(agentSubDir, "token_usage.jsonl"),
			[]byte(`{"event":"token_usage","provider":"unknown","model":"unpriced","input_tokens":10,"output_tokens":5}`+"\n"),
			0o644,
		))
		require.NoError(t, os.WriteFile(
			filepath.Join(usageDir, "agent_usage.json"),
			[]byte(`{"ai_credits":2.5,"primary_model":"unpriced"}`),
			0o644,
		))

		summary, err := analyzeTokenUsageAICOnly(tmpDir, false)
		require.NoError(t, err)
		require.NotNil(t, summary)
		assert.InDelta(t, 2.5, summary.TotalAIC, 1e-9)
	})

	t.Run("preserves AWF-reported totals from token usage artifacts", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "analyze-token-usage-aic-only-awf")
		usageDir := filepath.Join(tmpDir, "usage", "agent")
		require.NoError(t, os.MkdirAll(usageDir, 0o755))
		fixture, err := os.ReadFile(filepath.Join("..", "..", "actions", "setup", "js", "fixtures", "awf-v0.28.7-aic-token-usage.jsonl"))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(usageDir, "token_usage.jsonl"), fixture, 0o644))

		summary, err := analyzeTokenUsageAICOnly(tmpDir, false)
		require.NoError(t, err)
		require.NotNil(t, summary)
		assert.InDelta(t, 1.03602, summary.TotalAIC, 1e-9)
	})

	t.Run("propagates token usage fallback warnings", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "analyze-token-usage-aic-only-warnings")
		usageDir := filepath.Join(tmpDir, "usage", "agent")
		require.NoError(t, os.MkdirAll(usageDir, 0o755))
		content := `{"_schema":"token-usage/v0.28.7","event":"token_usage","provider":"copilot","model":"gpt-4o-mini-2024-07-18","input_tokens":19288,"output_tokens":35,"ai_credits_this_response":null,"ai_credits_total":null}`
		require.NoError(t, os.WriteFile(filepath.Join(usageDir, "token_usage.jsonl"), []byte(content+"\n"), 0o644))

		summary, err := analyzeTokenUsageAICOnly(tmpDir, false)
		require.NoError(t, err)
		require.NotNil(t, summary)
		require.Len(t, summary.Warnings, 1)
		assert.Contains(t, summary.Warnings[0], "fallback accounting")
	})

	t.Run("preserves zero AWF-reported totals instead of falling back to agent usage", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "analyze-token-usage-aic-only-zero-awf")
		usageDir := filepath.Join(tmpDir, "usage", "agent")
		require.NoError(t, os.MkdirAll(usageDir, 0o755))
		require.NoError(t, os.WriteFile(
			filepath.Join(usageDir, "token_usage.jsonl"),
			[]byte(`{"_schema":"token-usage/v0.28.7","event":"token_usage","request_id":"zero","provider":"copilot","model":"gpt-4o-mini-2024-07-18","input_tokens":19288,"output_tokens":35,"ai_credits_this_response":0,"ai_credits_total":0}`+"\n"),
			0o644,
		))
		require.NoError(t, os.WriteFile(
			filepath.Join(tmpDir, "usage", "agent_usage.json"),
			[]byte(`{"input_tokens":19288,"output_tokens":35,"ai_credits":2.5,"primary_model":"gpt-4o-mini-2024-07-18"}`),
			0o644,
		))

		summary, err := analyzeTokenUsageAICOnly(tmpDir, false)
		require.NoError(t, err)
		require.NotNil(t, summary)
		assert.Zero(t, summary.TotalAIC)
	})

	t.Run("deduplicates mirrored AWF artifacts while summing distinct legacy usage", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "analyze-token-usage-aic-only-mirrored-awf-and-legacy")
		agentDir := filepath.Join(tmpDir, "usage", "agent")
		detectionDir := filepath.Join(tmpDir, "usage", "detection")
		require.NoError(t, os.MkdirAll(agentDir, 0o755))
		require.NoError(t, os.MkdirAll(detectionDir, 0o755))
		awfRecord := `{"_schema":"token-usage/v0.28.7","event":"token_usage","request_id":"shared-awf","provider":"copilot","model":"gpt-4o-mini-2024-07-18","input_tokens":10,"output_tokens":1,"ai_credits_this_response":0.2,"ai_credits_total":0.2}`
		require.NoError(t, os.WriteFile(filepath.Join(agentDir, "token_usage.jsonl"), []byte(awfRecord+"\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(detectionDir, "mirrored-token-usage.jsonl"), []byte(awfRecord+"\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "usage", "detection_usage.jsonl"), []byte(`{"ai_credits":0.75}`+"\n"), 0o644))

		summary, err := analyzeTokenUsageAICOnly(tmpDir, false)
		require.NoError(t, err)
		require.NotNil(t, summary)
		assert.InDelta(t, 0.95, summary.TotalAIC, 1e-9)
		require.Len(t, summary.Warnings, 1)
		assert.Contains(t, summary.Warnings[0], "duplicate token usage")
	})
}

func TestExtractUsageRecord(t *testing.T) {
	t.Run("returns nested usage record", func(t *testing.T) {
		record := extractUsageRecord(map[string]any{"ai_credits": 1.5})
		require.NotNil(t, record)
		assert.InDelta(t, 1.5, record["ai_credits"].(float64), 1e-9)
	})

	t.Run("returns nil for non-map input", func(t *testing.T) {
		assert.Nil(t, extractUsageRecord("not-a-map"))
		assert.Nil(t, extractUsageRecord(nil))
	})
}

func TestIsFinite(t *testing.T) {
	assert.True(t, isFinite(1.25))
	assert.True(t, isFinite(0))
	assert.False(t, isFinite(math.NaN()))
	assert.False(t, isFinite(math.Inf(1)))
	assert.False(t, isFinite(math.Inf(-1)))
}

func TestParseAgentUsageFilePreservesZeroAIC(t *testing.T) {
	tmpDir := testutil.TempDir(t, "agent-usage-zero-aic")
	filePath := filepath.Join(tmpDir, "agent_usage.json")
	require.NoError(t, os.WriteFile(
		filePath,
		[]byte(`{"input_tokens":19288,"output_tokens":35,"ai_credits":0,"primary_model":"gpt-4o-mini-2024-07-18"}`),
		0o644,
	))

	summary, err := parseAgentUsageFile(filePath)
	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.True(t, summary.AICFound)
	assert.Zero(t, summary.TotalAIC)
}

func TestTokenUsageSummaryMethods(t *testing.T) {
	t.Run("TotalTokens", func(t *testing.T) {
		summary := &TokenUsageSummary{
			TotalInputTokens:      100,
			TotalOutputTokens:     200,
			TotalCacheReadTokens:  5000,
			TotalCacheWriteTokens: 3000,
		}
		assert.Equal(t, 8300, summary.TotalTokens(), "total tokens should be sum of all types")
	})

	t.Run("AvgDurationMs", func(t *testing.T) {
		summary := &TokenUsageSummary{
			TotalDurationMs: 10000,
			TotalRequests:   4,
		}
		assert.Equal(t, 2500, summary.AvgDurationMs(), "avg duration should be total/requests")
	})

	t.Run("AvgDurationMs with zero requests", func(t *testing.T) {
		summary := &TokenUsageSummary{
			TotalDurationMs: 10000,
			TotalRequests:   0,
		}
		assert.Equal(t, 0, summary.AvgDurationMs(), "avg duration should be 0 for zero requests")
	})

	t.Run("ModelRows sorted by total tokens", func(t *testing.T) {
		summary := &TokenUsageSummary{
			ByModel: map[string]*ModelTokenUsage{
				"small-model": {
					Provider:         "provider-a",
					TokenCoreMetrics: TokenCoreMetrics{InputTokens: 10},
					Requests:         1,
					DurationMs:       100,
				},
				"large-model": {
					Provider: "provider-b",
					TokenCoreMetrics: TokenCoreMetrics{
						InputTokens:      100,
						OutputTokens:     200,
						CacheReadTokens:  5000,
						CacheWriteTokens: 3000,
						ReasoningTokens:  30,
						EffectiveTokens:  8330,
					},
					Requests:   5,
					DurationMs: 5000,
				},
			},
		}

		rows := summary.ModelRows()
		require.Len(t, rows, 2, "should have 2 model rows")
		assert.Equal(t, "large-model", rows[0].Model, "first row should be model with most tokens")
		assert.Equal(t, "small-model", rows[1].Model, "second row should be model with fewer tokens")
		assert.Equal(t, "1.0s", rows[0].AvgDuration, "avg duration for large model")
		assert.Equal(t, 30, summary.ByModel["large-model"].ReasoningTokens, "reasoning tokens should remain tracked in model core metrics")
		assert.Equal(t, 8330, summary.ByModel["large-model"].EffectiveTokens, "effective tokens should remain tracked in model core metrics")

		encoded, err := json.Marshal(rows[0])
		require.NoError(t, err)
		assert.NotContains(t, string(encoded), "reasoning_tokens", "row JSON should preserve legacy shape")
		assert.NotContains(t, string(encoded), "effective_tokens", "row JSON should preserve legacy shape")

		rendered := console.RenderStruct(rows)
		assert.Contains(t, rendered, "Input", "row table should keep quartet columns")
		assert.Contains(t, rendered, "Output", "row table should keep quartet columns")
		assert.Contains(t, rendered, "Cache Read", "row table should keep quartet columns")
		assert.Contains(t, rendered, "Cache Write", "row table should keep quartet columns")
		assert.NotContains(t, rendered, "ReasoningTokens", "row table should preserve legacy columns")
		assert.NotContains(t, rendered, "EffectiveTokens", "row table should preserve legacy columns")
	})
}

func TestFormatDurationMs(t *testing.T) {
	tests := []struct {
		ms       int
		expected string
	}{
		{0, "0ms"},
		{500, "500ms"},
		{999, "999ms"},
		{1000, "1.0s"},
		{1500, "1.5s"},
		{6383, "6.4s"},
		{59999, "1.0m"},
		{60000, "1.0m"},
		{90000, "1.5m"},
		{119999, "2.0m"},
		{125000, "2.1m"},
		{3599999, "1.0h"},
		{3600000, "1.0h"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, timeutil.FormatDurationMs(tt.ms), "FormatDurationMs(%d)", tt.ms)
		})
	}
}

func TestAnalyzeTokenUsage(t *testing.T) {
	t.Run("returns nil when no file found", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "analyze-token-usage")
		summary, err := analyzeTokenUsage(tmpDir, false)
		require.NoError(t, err, "should not error when file not found")
		assert.Nil(t, summary, "should return nil when no file found")
	})

	t.Run("parses file from sandbox path", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "analyze-token-usage")
		logsDir := filepath.Join(tmpDir, "sandbox", "firewall", "logs", "api-proxy-logs")
		require.NoError(t, os.MkdirAll(logsDir, 0o755))
		tokenFile := filepath.Join(logsDir, "token-usage.jsonl")
		content := `{"timestamp":"2026-04-01T17:56:38.042Z","request_id":"1","provider":"anthropic","model":"claude-sonnet-4-6","path":"/v1/messages","status":200,"streaming":true,"input_tokens":100,"output_tokens":200,"cache_read_tokens":5000,"cache_write_tokens":3000,"duration_ms":2500,"response_bytes":1500}`
		require.NoError(t, os.WriteFile(tokenFile, []byte(content+"\n"), 0o644))

		summary, err := analyzeTokenUsage(tmpDir, false)
		require.NoError(t, err, "should parse without error")
		require.NotNil(t, summary, "should return summary")
		assert.Equal(t, 1, summary.TotalRequests, "should have 1 request")
		assert.Equal(t, 100, summary.TotalInputTokens, "should have correct input tokens")
		assert.InDelta(t, 1.575, summary.TotalAIC, 1e-9, "should compute AI Credits from model pricing")
	})

	t.Run("counts steering events from api-proxy events log", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "analyze-token-steering-events")
		logsDir := filepath.Join(tmpDir, "sandbox", "firewall", "logs", "api-proxy-logs")
		require.NoError(t, os.MkdirAll(logsDir, 0o755))
		tokenFile := filepath.Join(logsDir, "token-usage.jsonl")
		tokenContent := `{"timestamp":"2026-04-01T17:56:38.042Z","request_id":"1","provider":"anthropic","model":"claude-sonnet-4-6","path":"/v1/messages","status":200,"streaming":true,"input_tokens":100,"output_tokens":200,"cache_read_tokens":5000,"cache_write_tokens":3000,"duration_ms":2500,"response_bytes":1500}`
		require.NoError(t, os.WriteFile(tokenFile, []byte(tokenContent+"\n"), 0o644))

		eventsFile := filepath.Join(logsDir, "events.jsonl")
		eventsContent := strings.Join([]string{
			`{"event":"token_steering","message":"[AWF TOKEN WARNING] You have used 80% of your effective token budget. Begin planning to wrap up your current work."}`,
			`{"type":"token_steering","message":"[AWF TOKEN WARNING] You have used 90% of your effective token budget. Complete your current task and prepare final output."}`,
			`{"event_name":"timeout_steering","message":"[AWF TIME WARNING] You have used 80% of your allotted run time. Begin planning to wrap up your current work."}`,
			`{"eventName":"timeout_steering","message":"[AWF TIME WARNING] You have used 90% of your allotted run time. Complete your current task and prepare final output."}`,
			`{"event":"request.forwarded"}`,
			`{"event":"token_steering","message":"warn 95%"}`,
			`{"event":"budget_steering","message":"[AWF TOKEN WARNING] non-spec event name"}`,
		}, "\n")
		require.NoError(t, os.WriteFile(eventsFile, []byte(eventsContent+"\n"), 0o644))

		summary, err := analyzeTokenUsage(tmpDir, false)
		require.NoError(t, err)
		require.NotNil(t, summary)
		assert.Equal(t, 4, summary.TotalSteeringEvents, "should count spec-compliant steering events from api-proxy events.jsonl")
	})

	t.Run("counts steering events from legacy firewall-audit-logs events file", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "analyze-token-steering-events-legacy")
		logsDir := filepath.Join(tmpDir, "firewall-audit-logs", "api-proxy-logs")
		require.NoError(t, os.MkdirAll(logsDir, 0o755))
		tokenFile := filepath.Join(logsDir, "token-usage.jsonl")
		tokenContent := `{"timestamp":"2026-04-01T17:56:38.042Z","request_id":"1","provider":"anthropic","model":"claude-sonnet-4-6","path":"/v1/messages","status":200,"streaming":true,"input_tokens":100,"output_tokens":200,"cache_read_tokens":5000,"cache_write_tokens":3000,"duration_ms":2500,"response_bytes":1500}`
		require.NoError(t, os.WriteFile(tokenFile, []byte(tokenContent+"\n"), 0o644))

		eventsFile := filepath.Join(logsDir, "events.jsonl")
		require.NoError(t, os.WriteFile(eventsFile, []byte(`{"event":"token_steering","message":"[AWF TOKEN WARNING] You have used 95% of your effective token budget. Finalize and submit your work now."}`+"\n"), 0o644))

		summary, err := analyzeTokenUsage(tmpDir, false)
		require.NoError(t, err)
		require.NotNil(t, summary)
		assert.Equal(t, 1, summary.TotalSteeringEvents, "should count steering events from legacy events.jsonl")
	})

	t.Run("falls back to agent_usage.json when token-usage.jsonl is missing", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "analyze-agent-usage")
		agentUsageFile := filepath.Join(tmpDir, "agent_usage.json")
		content := `{"provider":"anthropic","model":"claude-sonnet-4-6","input_tokens":5944,"output_tokens":8698,"cache_read_tokens":1170605,"cache_write_tokens":86049,"effective_tokens":243846}`
		require.NoError(t, os.WriteFile(agentUsageFile, []byte(content), 0o644))

		summary, err := analyzeTokenUsage(tmpDir, false)
		require.NoError(t, err, "should parse agent_usage.json without error")
		require.NotNil(t, summary, "should return summary from agent_usage.json")
		assert.Equal(t, 5944, summary.TotalInputTokens, "input tokens should match agent usage")
		assert.Equal(t, 8698, summary.TotalOutputTokens, "output tokens should match agent usage")
		assert.Equal(t, 0, summary.TotalEffectiveTokens, "effective tokens are no longer computed")
		assert.Greater(t, summary.TotalAIC, 0.0, "AI Credits should be recomputed from raw usage")
		assert.Equal(t, 1, summary.TotalRequests, "agent usage fallback should synthesize one request")
	})

	t.Run("does not recompute ET from raw usage", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "analyze-agent-usage-recompute")
		agentUsageFile := filepath.Join(tmpDir, "agent_usage.json")
		content := `{"model":"unknown","input_tokens":10,"output_tokens":5,"cache_read_tokens":0,"cache_write_tokens":0,"effective_tokens":9999}`
		require.NoError(t, os.WriteFile(agentUsageFile, []byte(content), 0o644))

		summary, err := analyzeTokenUsage(tmpDir, false)
		require.NoError(t, err, "should parse agent_usage.json without error")
		require.NotNil(t, summary, "should return summary from agent_usage.json")
		assert.Equal(t, 0, summary.TotalEffectiveTokens, "ET should not be recomputed")
		require.Contains(t, summary.ByModel, "unknown")
		assert.Equal(t, 0, summary.ByModel["unknown"].EffectiveTokens, "per-model ET should remain unset")
	})

	t.Run("unknown model keeps ET unset", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "analyze-agent-usage-unknown-model")
		awInfoFile := filepath.Join(tmpDir, "aw_info.json")
		awInfoContent := `{"token_weights":{"multipliers":{"known-model":5}}}`
		require.NoError(t, os.WriteFile(awInfoFile, []byte(awInfoContent), 0o644))

		agentUsageFile := filepath.Join(tmpDir, "agent_usage.json")
		agentUsageContent := `{"model":"mystery-model","input_tokens":10,"output_tokens":5,"cache_read_tokens":0,"cache_write_tokens":0}`
		require.NoError(t, os.WriteFile(agentUsageFile, []byte(agentUsageContent), 0o644))

		summary, err := analyzeTokenUsage(tmpDir, false)
		require.NoError(t, err, "should parse agent_usage.json with unknown model")
		require.NotNil(t, summary, "should return summary from agent_usage.json")
		assert.Equal(t, 0, summary.TotalEffectiveTokens, "effective tokens should remain unset")
		require.Contains(t, summary.ByModel, "mystery-model")
		assert.Equal(t, 0, summary.ByModel["mystery-model"].EffectiveTokens, "per-model ET should remain unset")
	})

	t.Run("custom weights do not affect ET because ET is disabled", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "analyze-agent-usage-custom-weights")
		awInfoFile := filepath.Join(tmpDir, "aw_info.json")
		awInfoContent := `{"token_weights":{"multipliers":{"unknown":2}}}`
		require.NoError(t, os.WriteFile(awInfoFile, []byte(awInfoContent), 0o644))

		agentUsageFile := filepath.Join(tmpDir, "agent_usage.json")
		agentUsageContent := `{"input_tokens":10,"output_tokens":5,"cache_read_tokens":0,"cache_write_tokens":0}`
		require.NoError(t, os.WriteFile(agentUsageFile, []byte(agentUsageContent), 0o644))

		summary, err := analyzeTokenUsage(tmpDir, false)
		require.NoError(t, err, "should parse agent_usage.json with custom weights")
		require.NotNil(t, summary, "should return summary from agent_usage.json")
		assert.Equal(t, 0, summary.TotalEffectiveTokens, "effective tokens should remain unset")
		require.Contains(t, summary.ByModel, "unknown", "unknown model bucket should be present")
		assert.Equal(t, 0, summary.ByModel["unknown"].EffectiveTokens, "per-model effective tokens should remain unset")
	})

	t.Run("records requested sub-agent models and mismatch when token logs do not show requested model", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "analyze-subagent-model-mismatch")
		logsDir := filepath.Join(tmpDir, "sandbox", "firewall", "logs", "api-proxy-logs")
		require.NoError(t, os.MkdirAll(logsDir, 0o755))
		tokenFile := filepath.Join(logsDir, "token-usage.jsonl")
		tokenContent := `{"timestamp":"2026-04-01T17:56:38.042Z","request_id":"1","provider":"anthropic","model":"claude-sonnet-4-6","path":"/v1/messages","status":200,"streaming":true,"input_tokens":100,"output_tokens":200,"cache_read_tokens":0,"cache_write_tokens":0,"duration_ms":2500,"response_bytes":1500}`
		require.NoError(t, os.WriteFile(tokenFile, []byte(tokenContent+"\n"), 0o644))

		agentLogContent := `● Agent-alpha(claude-haiku-4.5) Get model name
● Agent-beta(claude-haiku-4.5) Get model name
● Agent-gamma(claude-haiku-4.5) Get model name`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "agent-stdio.log"), []byte(agentLogContent), 0o644))

		summary, err := analyzeTokenUsage(tmpDir, false)
		require.NoError(t, err)
		require.NotNil(t, summary)
		require.Len(t, summary.SubagentModelRequests, 3)
		require.Len(t, summary.SubagentModelActuals, 1)
		assert.Equal(t, 3, summary.MismatchCount)
		assert.Equal(t, "claude-sonnet-4-6", summary.SubagentModelActuals[0].Model)
		require.Contains(t, summary.Warnings, subagentStdioWarning)

		for _, req := range summary.SubagentModelRequests {
			assert.Equal(t, "claude-haiku-4.5", req.RequestedModel)
			assert.Equal(t, 1, req.InvocationCount)
			assert.Equal(t, "claude-sonnet-4-6", req.EffectiveModel)
			assert.Equal(t, modelMismatchReasonModelNotObserved, req.ReasonCode)
		}
	})

	t.Run("records token-usage-missing reason when sub-agent model request is present but no model actuals exist", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "analyze-subagent-model-token-missing")
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "agent_usage.json"), []byte(`{}`), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "agent-stdio.log"), []byte(`● Agent-alpha(claude-haiku-4.5) Get model name`), 0o644))

		summary, err := analyzeTokenUsage(tmpDir, false)
		require.NoError(t, err)
		require.NotNil(t, summary)
		require.Len(t, summary.SubagentModelRequests, 1)
		assert.Empty(t, summary.SubagentModelActuals)
		assert.Equal(t, 1, summary.MismatchCount)
		assert.Equal(t, modelMismatchReasonTokenUsageMissing, summary.SubagentModelRequests[0].ReasonCode)
		assert.Empty(t, summary.SubagentModelRequests[0].EffectiveModel)
		require.Contains(t, summary.Warnings, subagentStdioWarning)
	})

	t.Run("falls back to agent_usage.json in usage subdir when token_usage.jsonl is empty", func(t *testing.T) {
		// Reproduces the usage-only-mode scenario where the usage artifact has an empty
		// placeholder token_usage.jsonl but agent_usage.json is now also copied there.
		tmpDir := testutil.TempDir(t, "analyze-usage-subdir-fallback")
		// Create the usage artifact directory as gh aw logs would lay it out.
		usageDir := filepath.Join(tmpDir, "usage")
		agentSubDir := filepath.Join(usageDir, "agent")
		require.NoError(t, os.MkdirAll(agentSubDir, 0o755))
		// Empty placeholder written by the conclusion job fallback line.
		require.NoError(t, os.WriteFile(filepath.Join(agentSubDir, "token_usage.jsonl"), []byte(""), 0o644))
		// agent_usage.json now copied to usage/ by buildUsageArtifactUploadSteps.
		agentUsageContent := `{"input_tokens":5463,"output_tokens":17080,"cache_read_tokens":1440173,"cache_write_tokens":64504,"ambient_context":8424,"ai_credits":94.653,"primary_model":"claude-sonnet-4.6"}`
		require.NoError(t, os.WriteFile(filepath.Join(usageDir, "agent_usage.json"), []byte(agentUsageContent), 0o644))

		summary, err := analyzeTokenUsage(tmpDir, false)
		require.NoError(t, err, "should not error when token_usage.jsonl is empty but agent_usage.json present")
		require.NotNil(t, summary, "should fall back to agent_usage.json")
		assert.Equal(t, 5463, summary.TotalInputTokens, "input tokens should come from agent_usage.json")
		assert.Equal(t, 17080, summary.TotalOutputTokens, "output tokens should come from agent_usage.json")
		// ai_credits from the file should be used directly.
		assert.InDelta(t, 94.653, summary.TotalAIC, 1e-6, "AIC should be taken from the pre-computed ai_credits field")
		require.Contains(t, summary.ByModel, "claude-sonnet-4.6", "primary_model should be used for ByModel attribution")
		assert.InDelta(t, 94.653, summary.ByModel["claude-sonnet-4.6"].AIC, 1e-6, "per-model AIC should match pre-computed ai_credits")
		require.NotNil(t, summary.AmbientContext, "ambient context should be captured from agent_usage.json")
		assert.Equal(t, 8424, summary.AmbientContext.InputTokens, "ambient context should use the dedicated ambient_context field")
	})

	t.Run("captures alias-based sub-agent model requests used by workflow subagents", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "analyze-subagent-model-alias")
		logsDir := filepath.Join(tmpDir, "sandbox", "firewall", "logs", "api-proxy-logs")
		require.NoError(t, os.MkdirAll(logsDir, 0o755))
		tokenFile := filepath.Join(logsDir, "token-usage.jsonl")
		tokenContent := `{"timestamp":"2026-04-01T17:56:38.042Z","request_id":"1","provider":"openai","model":"gpt-5-mini","path":"/v1/messages","status":200,"streaming":true,"input_tokens":100,"output_tokens":200,"cache_read_tokens":0,"cache_write_tokens":0,"duration_ms":2500,"response_bytes":1500}`
		require.NoError(t, os.WriteFile(tokenFile, []byte(tokenContent+"\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "agent-stdio.log"), []byte(`● workflow-characterizer(small) Classify`), 0o644))

		summary, err := analyzeTokenUsage(tmpDir, false)
		require.NoError(t, err)
		require.NotNil(t, summary)
		require.Len(t, summary.SubagentModelRequests, 1)
		assert.Equal(t, "small", summary.SubagentModelRequests[0].RequestedModel)
		assert.Equal(t, "gpt-5-mini", summary.SubagentModelRequests[0].EffectiveModel)
		assert.Equal(t, modelMismatchReasonModelNotObserved, summary.SubagentModelRequests[0].ReasonCode)
		assert.Equal(t, 1, summary.MismatchCount)
		require.Contains(t, summary.Warnings, subagentStdioWarning)
	})
}

func TestCorrelateToolCallsWithTokenDelta(t *testing.T) {
	t.Run("does not assign deltas from token usage", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "token-delta")
		filePath := filepath.Join(tmpDir, "token-usage.jsonl")
		// Two API calls; tool call happens between them.
		// ET for first entry (model "unknown", default weights, m=1):
		//   1.0*1000 + 4.0*50 = 1200
		// ET for second entry:
		//   1.0*1500 + 4.0*80 = 1820
		// Expected delta = 1820 - 1200 = 620
		content := `{"timestamp":"2026-05-19T21:10:00.000Z","model":"unknown","provider":"test","input_tokens":1000,"output_tokens":50,"cache_read_tokens":0,"cache_write_tokens":0}
{"timestamp":"2026-05-19T21:10:10.000Z","model":"unknown","provider":"test","input_tokens":1500,"output_tokens":80,"cache_read_tokens":0,"cache_write_tokens":0}`
		require.NoError(t, os.WriteFile(filePath, []byte(content+"\n"), 0o644))

		toolCalls := []MCPToolCall{
			{
				Timestamp:  "2026-05-19T21:10:05.000Z",
				ServerName: "test-server",
				ToolName:   "test-tool",
			},
		}
		result := correlateToolCallsWithTokenDelta(toolCalls, filePath)
		require.Len(t, result, 1)
		assert.Equal(t, 0, result[0].EffectiveTokenDelta, "effective-token deltas are no longer computed")
	})

	t.Run("leaves delta zero when tool call has no preceding API call", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "token-delta-no-prev")
		filePath := filepath.Join(tmpDir, "token-usage.jsonl")
		content := `{"timestamp":"2026-05-19T21:10:10.000Z","model":"unknown","provider":"test","input_tokens":1000,"output_tokens":50,"cache_read_tokens":0,"cache_write_tokens":0}`
		require.NoError(t, os.WriteFile(filePath, []byte(content+"\n"), 0o644))

		toolCalls := []MCPToolCall{
			{
				Timestamp:  "2026-05-19T21:10:05.000Z", // before the only API call
				ServerName: "test-server",
				ToolName:   "test-tool",
			},
		}
		result := correlateToolCallsWithTokenDelta(toolCalls, filePath)
		require.Len(t, result, 1)
		assert.Equal(t, 0, result[0].EffectiveTokenDelta, "no delta when no preceding API call")
	})

	t.Run("leaves delta zero when tool call has no following API call", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "token-delta-no-next")
		filePath := filepath.Join(tmpDir, "token-usage.jsonl")
		content := `{"timestamp":"2026-05-19T21:10:00.000Z","model":"unknown","provider":"test","input_tokens":1000,"output_tokens":50,"cache_read_tokens":0,"cache_write_tokens":0}`
		require.NoError(t, os.WriteFile(filePath, []byte(content+"\n"), 0o644))

		toolCalls := []MCPToolCall{
			{
				Timestamp:  "2026-05-19T21:10:05.000Z", // after the only API call
				ServerName: "test-server",
				ToolName:   "test-tool",
			},
		}
		result := correlateToolCallsWithTokenDelta(toolCalls, filePath)
		require.Len(t, result, 1)
		assert.Equal(t, 0, result[0].EffectiveTokenDelta, "no delta when no following API call")
	})

	t.Run("handles empty token usage file path", func(t *testing.T) {
		toolCalls := []MCPToolCall{{Timestamp: "2026-05-19T21:10:05.000Z", ToolName: "t"}}
		result := correlateToolCallsWithTokenDelta(toolCalls, "")
		require.Len(t, result, 1)
		assert.Equal(t, 0, result[0].EffectiveTokenDelta, "no delta with empty file path")
	})

	t.Run("keeps deltas zero for multiple sequential tool calls", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "token-delta-multi")
		filePath := filepath.Join(tmpDir, "token-usage.jsonl")
		// Three API calls, two tool calls between consecutive pairs.
		content := `{"timestamp":"2026-05-19T21:10:00.000Z","model":"unknown","provider":"test","input_tokens":1000,"output_tokens":50,"cache_read_tokens":0,"cache_write_tokens":0}
{"timestamp":"2026-05-19T21:10:10.000Z","model":"unknown","provider":"test","input_tokens":1500,"output_tokens":80,"cache_read_tokens":0,"cache_write_tokens":0}
{"timestamp":"2026-05-19T21:10:20.000Z","model":"unknown","provider":"test","input_tokens":2000,"output_tokens":100,"cache_read_tokens":0,"cache_write_tokens":0}`
		require.NoError(t, os.WriteFile(filePath, []byte(content+"\n"), 0o644))
		// ET[0] = 1000 + 4*50 = 1200
		// ET[1] = 1500 + 4*80 = 1820  → delta1 = 620
		// ET[2] = 2000 + 4*100 = 2400 → delta2 = 580

		toolCalls := []MCPToolCall{
			{Timestamp: "2026-05-19T21:10:05.000Z", ServerName: "s", ToolName: "tool-a"},
			{Timestamp: "2026-05-19T21:10:15.000Z", ServerName: "s", ToolName: "tool-b"},
		}
		result := correlateToolCallsWithTokenDelta(toolCalls, filePath)
		require.Len(t, result, 2)
		assert.Equal(t, 0, result[0].EffectiveTokenDelta, "delta for tool-a")
		assert.Equal(t, 0, result[1].EffectiveTokenDelta, "delta for tool-b")
	})
}

func TestCacheEfficiency(t *testing.T) {
	t.Run("remains zero to avoid transforming raw token counts", func(t *testing.T) {
		tmpDir := testutil.TempDir(t, "cache-eff")
		filePath := filepath.Join(tmpDir, "token-usage.jsonl")
		content := `{"provider":"anthropic","model":"sonnet","input_tokens":100,"output_tokens":50,"cache_read_tokens":9900,"cache_write_tokens":0,"duration_ms":100}`
		require.NoError(t, os.WriteFile(filePath, []byte(content+"\n"), 0o644))

		summary, err := parseTokenUsageFile(filePath)
		require.NoError(t, err)
		require.NotNil(t, summary)
		assert.InDelta(t, 0.0, summary.CacheEfficiency, 0.001, "cache efficiency should remain unset")
	})
}

func TestModelTokenUsageReasoningTokensJSONRoundTrip(t *testing.T) {
	original := ModelTokenUsage{
		Provider: "anthropic",
		TokenCoreMetrics: TokenCoreMetrics{
			InputTokens:     10,
			OutputTokens:    20,
			ReasoningTokens: 30,
		},
	}

	raw, err := json.Marshal(original)
	require.NoError(t, err)
	var encoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &encoded))
	assert.EqualValues(t, 30, encoded["reasoning_tokens"], "reasoning tokens should be persisted for ET recomputation")

	var decoded ModelTokenUsage
	require.NoError(t, json.Unmarshal(raw, &decoded))
	assert.Equal(t, 30, decoded.ReasoningTokens, "reasoning tokens should survive JSON round-trip")
}
