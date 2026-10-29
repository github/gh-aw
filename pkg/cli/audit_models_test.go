//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditModelsCommand(t *testing.T) {
	t.Parallel()
	cmd := NewAuditCommand()
	require.NotNil(t, cmd.Flags().Lookup("models"))
	opts, err := getAuditCommandOptions(cmd)
	require.NoError(t, err)
	assert.False(t, opts.models)
	require.NoError(t, cmd.Flags().Set("models", "true"))
	opts, err = getAuditCommandOptions(cmd)
	require.NoError(t, err)
	assert.True(t, opts.models)

	for _, args := range [][]string{
		{"123", "456"},
		{"https://github.com/owner/repo/actions/runs/123/job/456"},
		{"https://github.com/owner/repo/actions/runs/123/job/456#step:7:1"},
	} {
		require.ErrorContains(t, runAuditCommand(cmd, args), "--models")
	}
	require.NoError(t, cmd.Flags().Set("group", "true"))
	require.ErrorContains(t, runAuditCommand(cmd, []string{"123"}), "--models")
}

func TestAuditModelsConfig(t *testing.T) {
	t.Parallel()
	for _, artifacts := range [][]string{nil, {"usage"}, {"all"}} {
		cfg, err := newAuditRunConfig(42, AuditOptions{
			Models: true, Hostname: "github.com", ArtifactSets: artifacts,
		})
		require.NoError(t, err)
		assert.True(t, cfg.models)
		assert.True(t, cfg.auditOptions().Models)
		for _, name := range []string{"agent", "usage", "info"} {
			assert.True(t, artifactMatchesFilter(name, cfg.artifactFilter), "required artifact %s", name)
		}
		if len(artifacts) == 0 {
			assert.Empty(t, cfg.artifactFilter, "default remains all artifacts")
		}
	}
	for _, opts := range []AuditOptions{{Models: true, Group: true}, {Models: true, JobID: 1}} {
		_, err := newAuditRunConfig(42, opts)
		require.ErrorContains(t, err, "--models")
	}
}

func TestBuildAuditModelCatalog(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir()
	writeModelAvailabilityEvidence(t, runDir, "sandbox/firewall/awf-reflect.json", `{
		"models_fetch_complete":true,
		"secret":"must-not-appear",
		"endpoints":[
			{"provider":"copilot","configured":true,"url":"https://must-not-appear",
			 "models":["claude-sonnet-5",{"id":"claude-opus-5.5","secret":"must-not-appear"},{"name":"gpt-5"}],
			 "routing_models":[{"model_id":"claude-opus-5.5","supported_endpoints":["/v1/messages","/responses","/responses","untrusted\nendpoint"]}]},
			{"provider":"openai","configured":false,"models":[]}
		]}`)
	report := buildAuditModelCatalog(runDir)
	assert.True(t, report.ModelsFetchComplete)
	assert.Empty(t, report.Warnings)
	require.Len(t, report.Endpoints, 2)
	assert.True(t, report.Endpoints[0].Configured)
	assert.Equal(t, "copilot", report.Endpoints[0].Provider)
	assert.Equal(t, []AuditCatalogModel{
		{ID: "claude-sonnet-5"},
		{ID: "claude-opus-5.5", SupportedEndpoints: []string{"/responses", "/v1/messages"}},
		{ID: "gpt-5"},
	}, report.Endpoints[0].Models)
	assert.False(t, report.Endpoints[1].Configured)
	assert.Empty(t, report.Endpoints[1].Models)
	assert.Empty(t, report.Endpoints[1].Warning)
	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "must-not-appear")
	assert.NotContains(t, string(encoded), "untrusted")
}

func TestAuditModelCatalogUnavailable(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name       string
		reflection string
		warning    string
	}{
		{"missing", "", "unavailable or unsafe"},
		{"invalid", "{", "invalid"},
		{"incomplete", `{"endpoints":[{"provider":"copilot","configured":true,"models":["sonnet"]}]}`, "did not complete"},
		{"no endpoints", `{"models_fetch_complete":true}`, "No provider endpoints"},
		{"bad provider", `{"models_fetch_complete":true,"endpoints":[{"provider":"bad\nprovider"}]}`, "invalid provider"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runDir := t.TempDir()
			if tt.reflection != "" {
				writeModelAvailabilityEvidence(t, runDir, "sandbox/firewall/awf-reflect.json", tt.reflection)
			}
			report := buildAuditModelCatalog(runDir)
			assert.Contains(t, report.Warnings[0], tt.warning)
		})
	}
	for _, tt := range []struct {
		models  string
		warning string
	}{
		{"null", "No model inventory"},
		{`["bad\nmodel", "sonnet"]`, "Invalid model entries"},
	} {
		runDir := t.TempDir()
		writeModelAvailabilityEvidence(t, runDir, "sandbox/firewall/awf-reflect.json",
			`{"models_fetch_complete":true,"endpoints":[{"provider":"copilot","configured":true,"models":`+tt.models+`}]}`)
		report := buildAuditModelCatalog(runDir)
		require.Len(t, report.Endpoints, 1)
		assert.Contains(t, report.Endpoints[0].Warning, tt.warning)
	}
	runDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(runDir, "sandbox"), 0o700))
	require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(runDir, "sandbox", "firewall")))
	assert.Contains(t, buildAuditModelCatalog(runDir).Warnings[0], "unsafe")
}

func TestRenderAuditModelCatalog(t *testing.T) {
	runDir := t.TempDir()
	writeModelAvailabilityEvidence(t, runDir, "sandbox/firewall/awf-reflect.json",
		`{"models_fetch_complete":true,"endpoints":[{"provider":"copilot","configured":true,"models":["claude-opus-5.5"]}]}`)
	run := WorkflowRun{DatabaseID: 42, Status: "completed", Conclusion: "success", LogsPath: runDir}
	require.NoError(t, writeAuditData(runDir, AuditData{
		CacheSource: auditCacheSourceFull, Overview: buildAuditOverview(run, nil),
		EngineConfig: &AuditEngineConfig{RequestedModel: "opus", Model: "claude-opus-5.5"},
	}))
	for _, models := range []bool{true, false} {
		stdout, _ := captureOutput(t, func() error {
			return renderAuditReport(context.Background(), ProcessedRun{Run: run}, LogMetrics{}, nil, AuditOptions{
				OutputDir: runDir, JSONOutput: true, Models: models,
			})
		})
		var data AuditData
		require.NoError(t, json.Unmarshal([]byte(stdout), &data))
		if models {
			require.NotNil(t, data.ModelCatalog)
			assert.True(t, data.ModelCatalog.ModelsFetchComplete)
			assert.Equal(t, "claude-opus-5.5", data.ModelCatalog.Endpoints[0].Models[0].ID)
			output := testutil.CaptureStderr(t, func() {
				renderConsoleModelCatalog(data.ModelCatalog, data.EngineConfig)
			})
			assert.Contains(t, output, "requested_model=opus effective_model=claude-opus-5.5")
			assert.Contains(t, output, "provider=copilot configured=true models=1")
		} else {
			assert.NotContains(t, stdout, `"model_catalog"`)
		}
	}
	cached, ok := loadCachedAuditData(runDir, run, auditCacheSourceFull)
	require.True(t, ok)
	assert.Nil(t, cached.ModelCatalog, "model details must not leak into default cached output")
}
