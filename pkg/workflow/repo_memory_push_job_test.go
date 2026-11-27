//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPushRepoMemoryOutputsExcludesLedgerCompaction(t *testing.T) {
	outputs := buildPushRepoMemoryOutputs([]RepoMemoryEntry{
		{ID: "default", Ledger: &RepoMemoryLedgerConfig{Compaction: &RepoMemoryLedgerCompactionConfig{MinSegments: 2, MaxSegments: 4}}},
	})

	assert.NotContains(t, outputs, "ledger_compaction")
	assert.Equal(t, "${{ steps.push_repo_memory_default.outputs.save_status }}", outputs["save_status_default"])
	assert.Equal(t, "${{ steps.push_repo_memory_default.outcome }}", outputs["save_outcome_default"])
	assert.Equal(t, "${{ steps.push_repo_memory_default.outputs.save_branch }}", outputs["save_branch_default"])
	assert.Equal(t, "${{ steps.push_repo_memory_default.outputs.save_commit }}", outputs["save_commit_default"])
}

// TestRepoMemoryWikiPushAllowedRepos tests that wiki mode sets REPO_MEMORY_ALLOWED_REPOS
// in the push step so the push script accepts the .wiki repo as a valid target.
func TestRepoMemoryWikiPushAllowedRepos(t *testing.T) {
	config := &RepoMemoryConfig{
		Memories: []RepoMemoryEntry{
			{
				ID:         "default",
				BranchName: "master",
				Wiki:       true,
			},
		},
	}

	data := &WorkflowData{
		RepoMemoryConfig: config,
	}

	compiler := NewCompiler()
	pushJob, err := compiler.buildPushRepoMemoryJob(data, false)
	require.NoError(t, err, "Should build push job without error")
	require.NotNil(t, pushJob, "Should produce a push job")

	pushJobOutput := strings.Join(pushJob.Steps, "\n")
	assert.Contains(t, pushJobOutput, "REPO_MEMORY_ALLOWED_REPOS: ${{ github.repository }}.wiki",
		"Wiki push step should pre-populate allowed repos with the wiki repo")
}

// TestRepoMemoryNonWikiPushNoAllowedRepos tests that non-wiki mode does NOT set REPO_MEMORY_ALLOWED_REPOS
func TestRepoMemoryNonWikiPushNoAllowedRepos(t *testing.T) {
	config := &RepoMemoryConfig{
		Memories: []RepoMemoryEntry{
			{
				ID:         "default",
				BranchName: "memory/default",
			},
		},
	}

	data := &WorkflowData{
		RepoMemoryConfig: config,
	}

	compiler := NewCompiler()
	pushJob, err := compiler.buildPushRepoMemoryJob(data, false)
	require.NoError(t, err, "Should build push job without error")
	require.NotNil(t, pushJob, "Should produce a push job")

	pushJobOutput := strings.Join(pushJob.Steps, "\n")
	assert.NotContains(t, pushJobOutput, "REPO_MEMORY_ALLOWED_REPOS",
		"Non-wiki push step should not set REPO_MEMORY_ALLOWED_REPOS")
}

// TestRepoMemoryPushReceivesAgentStartingCommit verifies that the agent job records the
// commit each memory clone started from and passes it to the matching push step, so the
// push can replay only this run's changes onto rows other runs saved in the meantime.
func TestRepoMemoryPushReceivesAgentStartingCommit(t *testing.T) {
	data := &WorkflowData{
		RepoMemoryConfig: &RepoMemoryConfig{
			Memories: []RepoMemoryEntry{
				{ID: "default", BranchName: "memory/my-workflow"},
				{ID: "notes", BranchName: "memory/notes"},
			},
		},
	}
	compiler := NewCompiler()

	var agentSteps, preActivationSteps strings.Builder
	compiler.generateRepoMemorySteps(&agentSteps, data, "agent")
	compiler.generateRepoMemorySteps(&preActivationSteps, data, "pre_activation")
	outputs := compiler.buildMainJobOutputs(data)

	for _, memory := range data.RepoMemoryConfig.Memories {
		assert.Contains(t, agentSteps.String(), "        id: clone_repo_memory_"+memory.ID+"\n", "Agent clone step should be addressable")
		assert.Equal(t, "${{ steps.clone_repo_memory_"+memory.ID+".outputs.head }}", outputs["repo_memory_base_commit_"+memory.ID], "Agent job should expose the cloned commit")
		step := compiler.buildSinglePushRepoMemoryStep(data, memory, true, false)
		assert.Contains(t, step, "REPO_MEMORY_BASE_COMMIT: ${{ needs.agent.outputs.repo_memory_base_commit_"+memory.ID+" }}", "Push step should receive the cloned commit")
	}
	assert.NotContains(t, preActivationSteps.String(), "id: clone_repo_memory_", "Only the agent job's clone records the starting commit")
}

func TestRepoMemoryDailyAICWriterFilterExemption(t *testing.T) {
	for _, tc := range []struct {
		name      string
		backend   string
		credits   *string
		memoryIDs []string
		writerID  string
	}{
		{name: "ordinary memory", memoryIDs: []string{"default"}},
		{name: "artifact backend", credits: strPtr("10000"), memoryIDs: []string{"default"}},
		{name: "default selected", backend: maxDailyAICBackendRepoMemory, memoryIDs: []string{"other", "default"}, writerID: "default"},
		{name: "first selected", backend: maxDailyAICBackendRepoMemory, memoryIDs: []string{"first", "other"}, writerID: "first"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := &WorkflowData{
				MaxDailyAICBackend: tc.backend,
				MaxDailyAICredits:  tc.credits,
				RepoMemoryConfig:   &RepoMemoryConfig{},
			}
			for _, id := range tc.memoryIDs {
				data.RepoMemoryConfig.Memories = append(data.RepoMemoryConfig.Memories, RepoMemoryEntry{ID: id})
			}
			compiler := NewCompiler()
			for _, memory := range data.RepoMemoryConfig.Memories {
				step := compiler.buildSinglePushRepoMemoryStep(data, memory, true, false)
				if memory.ID == tc.writerID {
					assert.Contains(t, step, "GH_AW_DAILY_AIC_REPO_MEMORY_WRITER: ${{ steps.daily-aic-repo-memory-reset.outcome == 'success' }}",
						"Only a successfully sanitized daily AIC writer may bypass persistence filters")
				} else {
					assert.NotContains(t, step, "GH_AW_DAILY_AIC_REPO_MEMORY_WRITER",
						"Other memories must retain their configured persistence filters")
				}
			}
		})
	}
}

// TestPushRepoMemoryJobHasNoConcurrencyGroup verifies that the push_repo_memory job does not
// declare a concurrency group. GitHub Actions only queues one pending job per group, so a
// shared group silently cancels memory writes when many runs finish at once; the push script
// converges with a compare-and-swap retry loop instead.
func TestPushRepoMemoryJobHasNoConcurrencyGroup(t *testing.T) {
	data := &WorkflowData{
		RepoMemoryConfig: &RepoMemoryConfig{
			Memories: []RepoMemoryEntry{
				{ID: "default", BranchName: "memory/my-workflow"},
				{ID: "remote", BranchName: "memory/shared", TargetRepo: "other-org/other-repo"},
			},
		},
	}

	compiler := NewCompiler()
	pushJob, err := compiler.buildPushRepoMemoryJob(data, false)
	require.NoError(t, err, "Should build push job without error")
	require.NotNil(t, pushJob, "Should produce a push job")

	assert.Empty(t, pushJob.Concurrency, "push_repo_memory job should not set a concurrency group")
	assert.Equal(t, pushRepoMemoryTimeoutMinutes, pushJob.TimeoutMinutes, "push_repo_memory job should expose an explicit retry ceiling")
}

// TestPushRepoMemoryJobConditions verifies disabled detection only gates cancellation, while
// enabled detection also gates the agent and detection results.
func TestPushRepoMemoryJobConditions(t *testing.T) {
	data := &WorkflowData{
		RepoMemoryConfig: &RepoMemoryConfig{
			Memories: []RepoMemoryEntry{
				{ID: "default", BranchName: "memory/my-workflow"},
			},
		},
	}

	compiler := NewCompiler()

	t.Run("without threat detection", func(t *testing.T) {
		pushJob, err := compiler.buildPushRepoMemoryJob(data, false)
		require.NoError(t, err, "Should build push job without error")
		require.NotNil(t, pushJob, "Should produce a push job")

		assert.Equal(t,
			"always() && (!cancelled())",
			pushJob.If,
			"Condition should run unless the workflow is cancelled",
		)
	})

	t.Run("with threat detection", func(t *testing.T) {
		pushJob, err := compiler.buildPushRepoMemoryJob(data, true)
		require.NoError(t, err, "Should build push job without error")
		require.NotNil(t, pushJob, "Should produce a push job")

		assert.Contains(t, pushJob.If, "always()",
			"Condition should contain always()")
		assert.Contains(t, pushJob.If, "!cancelled()",
			"Condition should contain !cancelled() to prevent running after cancellation")
		assert.Contains(t, pushJob.If, "needs.agent.result != 'skipped'",
			"Condition should run for completed agent jobs, including failures")
		assert.Contains(t, pushJob.If, "needs.detection.result",
			"Condition should still check detection result when threat detection is enabled")
	})
}

// TestRepoMemoryFormatJSONPushStepEnvVar tests that FORMAT_JSON env var is emitted in push steps
func TestRepoMemoryFormatJSONPushStepEnvVar(t *testing.T) {
	t.Run("format-json=true emits FORMAT_JSON env var", func(t *testing.T) {
		config := &RepoMemoryConfig{
			Memories: []RepoMemoryEntry{
				{
					ID:           "default",
					BranchName:   "memory/default",
					MaxFileSize:  102400,
					MaxFileCount: 100,
					MaxPatchSize: 10240,
					CreateOrphan: true,
					FormatJSON:   true,
				},
			},
		}
		data := &WorkflowData{RepoMemoryConfig: config}

		compiler := NewCompiler()
		pushJob, err := compiler.buildPushRepoMemoryJob(data, false)
		require.NoError(t, err)
		require.NotNil(t, pushJob)

		pushJobOutput := strings.Join(pushJob.Steps, "\n")
		assert.Contains(t, pushJobOutput, "FORMAT_JSON: 'true'",
			"Push step should include FORMAT_JSON env var when format-json is true")
	})

	t.Run("format-json=false omits FORMAT_JSON env var", func(t *testing.T) {
		config := &RepoMemoryConfig{
			Memories: []RepoMemoryEntry{
				{
					ID:           "default",
					BranchName:   "memory/default",
					MaxFileSize:  102400,
					MaxFileCount: 100,
					MaxPatchSize: 10240,
					CreateOrphan: true,
					FormatJSON:   false,
				},
			},
		}
		data := &WorkflowData{RepoMemoryConfig: config}

		compiler := NewCompiler()
		pushJob, err := compiler.buildPushRepoMemoryJob(data, false)
		require.NoError(t, err)
		require.NotNil(t, pushJob)

		pushJobOutput := strings.Join(pushJob.Steps, "\n")
		assert.NotContains(t, pushJobOutput, "FORMAT_JSON",
			"Push step should NOT include FORMAT_JSON env var when format-json is false")
	})
}
