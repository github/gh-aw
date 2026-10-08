//go:build !integration

package workflow

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDryRunMutationLogging(t *testing.T) {
	if os.Getenv("GH_AW_TEST_DRY_RUN_LOGGING") == "1" {
		credits := "25"
		data := &WorkflowData{
			On: "on: push\n", Roles: []string{"all"}, Bots: []string{"trusted-bot"},
			AIReaction: "eyes", LockForAgent: true, LabelCommandRemoveLabel: true,
			OTLPEndpoint: "private-endpoint-canary",
			Env:          "env:\n  OTEL_SERVICE_NAME: private-env-canary\n  KEEP: unchanged\n",
			EnvSources:   map[string]string{"OTEL_SERVICE_NAME": "private-source-canary"},
			RawFrontmatter: map[string]any{
				"observability":        map[string]any{"endpoint": "private-config-canary"},
				"max-daily-ai-credits": 25,
			},
			MaxDailyAICredits: &credits, MaxDailyAICreditsExplicit: true,
			EngineConfig:      &EngineConfig{},
			Tools:             map[string]any{"github": true, "work-queue": true},
			SafeOutputs:       &SafeOutputsConfig{NoOp: &NoOpConfig{}},
			CacheMemoryConfig: &CacheMemoryConfig{Caches: []CacheMemoryEntry{{ID: "test"}}},
			DriveMemoryConfig: &DriveMemoryConfig{Drives: []DriveMemoryEntry{{ID: "test"}}},
		}
		compiler := NewCompiler()
		compiler.SetDryRun(true)
		prepared, err := compiler.prepareDryRunWorkflowData(data)
		require.NoError(t, err)
		repeated, err := compiler.prepareDryRunWorkflowData(prepared)
		require.NoError(t, err)
		assert.Equal(t, prepared, repeated)
		require.NoError(t, compiler.jobManager.AddJob(&Job{Name: "push_custom", If: "true"}))
		compiler.disableDryRunPushJobs()
		compiler.disableDryRunPushJobs()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDryRunMutationLogging$")
	command.Env = append(os.Environ(), "DEBUG=workflow:compiler_development", "DEBUG_COLORS=0", "GH_AW_TEST_DRY_RUN_LOGGING=1")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	for _, path := range []string{
		"DryRun", "Roles", "Bots", "AIReaction", "LockForAgent", "LabelCommandRemoveLabel",
		"OTLPEndpoint", "RawFrontmatter.observability", "RawFrontmatter.max-daily-ai-credits",
		"MaxDailyAICredits", "EngineConfig.MaxAICredits", "Tools.github", "Tools.work-queue",
		"SafeOutputs.Staged", "SafeOutputs.NoOp.ReportAsIssue", "EnvSources.OTEL_SERVICE_NAME",
		"CacheMemoryConfig.Caches[0].RestoreOnly", "DriveMemoryConfig.Drives[0].RestoreOnly",
		"OTEL_SERVICE_NAME", "RawFrontmatter.on", "push_custom",
	} {
		assert.Contains(t, string(output), path)
	}
	for _, secret := range []string{"private-endpoint-canary", "private-env-canary", "private-source-canary", "private-config-canary"} {
		assert.NotContains(t, string(output), secret)
	}
	for _, mutation := range []string{`Dry-run mutation: "Roles"`, `removed env key "OTEL_SERVICE_NAME"`, `disable job "push_custom"`} {
		assert.Equal(t, 1, strings.Count(string(output), mutation), "repeated preparation must not duplicate mutation logs")
	}
}
