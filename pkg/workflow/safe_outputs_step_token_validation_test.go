//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const stsMintStep = `octo-sts/action@v1.1.1`

func writeStepTokenWorkflow(t *testing.T, name string, content string) string {
	t.Helper()
	tmpDir := testutil.TempDir(t, "step-token-"+name)
	workflowFile := filepath.Join(tmpDir, name+".md")
	require.NoError(t, os.WriteFile(workflowFile, []byte(content), 0644))
	return workflowFile
}

// User-provided token-minting actions must not execute in conclusion.
func TestSameJobStepTokenMintedInConclusionRejected(t *testing.T) {
	workflowFile := writeStepTokenWorkflow(t, "minted", `---
on:
  workflow_dispatch:
permissions:
  contents: read
  id-token: write
engine: claude
strict: false
pre-steps:
  - name: Mint token (agent)
    id: octosts
    uses: `+stsMintStep+`
safe-outputs:
  push-to-pull-request-branch:
  github-token: ${{ steps.octosts.outputs.token || secrets.CUSTOM_PAT || secrets.GITHUB_TOKEN }}
jobs:
  safe_outputs:
    pre-steps:
      - name: Mint token (safe outputs)
        id: octosts
        uses: `+stsMintStep+`
  conclusion:
    pre-steps:
      - name: Mint token (conclusion)
        id: octosts
        uses: `+stsMintStep+`
---

# Same-job token minting
`)

	compiler := NewCompiler()
	require.ErrorContains(t, compiler.CompileWorkflow(workflowFile), "jobs.conclusion cannot run user-provided steps")
}

// TestSameJobStepTokenMissingInConsumingJobFails verifies that a token reference which is
// never minted in a consuming job is reported at compile time instead of producing a lock
// file with an unresolvable step reference.
func TestSameJobStepTokenMissingInConsumingJobFails(t *testing.T) {
	workflowFile := writeStepTokenWorkflow(t, "missing", `---
on:
  workflow_dispatch:
permissions:
  contents: read
  id-token: write
engine: claude
strict: false
pre-steps:
  - name: Mint token (agent)
    id: octosts
    uses: `+stsMintStep+`
safe-outputs:
  push-to-pull-request-branch:
  github-token: ${{ steps.octosts.outputs.token || secrets.GITHUB_TOKEN }}
---

# Missing same-job token minting
`)

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(workflowFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conclusion accepts compiler-generated steps only")
	assert.Contains(t, err.Error(), "safe-outputs.github-app")
}

func TestSameJobStepTokenMissingInSafeOutputMentionResolutionFails(t *testing.T) {
	workflowFile := writeStepTokenWorkflow(t, "missing-agent", `---
on:
  workflow_dispatch:
permissions:
  contents: read
  id-token: write
engine: claude
strict: false
safe-outputs:
  add-comment:
  mentions:
    allowed-teams: [my-org/my-team]
    github-token: ${{ steps.octosts.outputs.token || secrets.GITHUB_TOKEN }}
---

# Missing safe-output mention-resolution token
`)

	err := NewCompiler().CompileWorkflow(workflowFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `job "safe_outputs" has no step with id "octosts"`)
	assert.Contains(t, err.Error(), "safe-outputs.mentions.github-token")
	assert.Contains(t, err.Error(), "pre-steps:")
}

func TestSameJobStepTokenPerOutputMintedInSafeOutputs(t *testing.T) {
	for _, tt := range []struct {
		name      string
		mintToken bool
	}{
		{name: "missing"},
		{name: "minted", mintToken: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			content := `---
on:
  workflow_dispatch:
permissions:
  contents: read
engine: claude
strict: false
safe-outputs:
  add-comment:
    max: 1
    target: "*"
    github-token: ${{ steps.output_mint.outputs.token }}
`
			if tt.mintToken {
				content += `jobs:
  safe_outputs:
    pre-steps:
      - name: Mint safe-output token
        id: output_mint
        uses: ` + stsMintStep + "\n"
			}
			content += "---\n\n# Job-local safe-output token\n"
			workflowFile := writeStepTokenWorkflow(t, "per-output", content)
			err := NewCompiler().CompileWorkflow(workflowFile)
			if !tt.mintToken {
				require.ErrorContains(t, err, `job "safe_outputs" has no step with id "output_mint"`)
				return
			}
			require.NoError(t, err)
			lockContent, err := os.ReadFile(filepath.Join(filepath.Dir(workflowFile), "per-output.lock.yml"))
			require.NoError(t, err)
			lockYAML := string(lockContent)
			assert.NotContains(t, extractJobSection(lockYAML, "agent"), "steps.output_mint.outputs.token")
			assert.NotContains(t, extractJobSection(lockYAML, "conclusion"), "steps.output_mint.outputs.token")
			safeOutputs := extractJobSection(lockYAML, "safe_outputs")
			assert.Contains(t, safeOutputs, `\"github-token\":\"${{ steps.output_mint.outputs.token }}\"`)
			declarationIndex := jobStepIDDeclarationIndex(safeOutputs, "output_mint")
			consumptionIndex := jobStepOutputConsumptionIndex(safeOutputs, "output_mint")
			require.GreaterOrEqual(t, declarationIndex, 0)
			require.GreaterOrEqual(t, consumptionIndex, 0)
			assert.Less(t, declarationIndex, consumptionIndex)
		})
	}
}

func TestSameJobStepTokenMentionsOnlyMintedInAgentCompiles(t *testing.T) {
	workflowFile := writeStepTokenWorkflow(t, "mentions-only", `---
on:
  workflow_dispatch:
permissions:
  contents: read
  id-token: write
engine: claude
strict: false
safe-outputs:
  add-comment:
  mentions:
    allowed-teams: [my-org/my-team]
    github-token: ${{ steps.mention_mint.outputs.token }}
jobs:
  safe_outputs:
    pre-steps:
      - name: Mint mention token
        id: mention_mint
        uses: `+stsMintStep+`
---

# Mention-only same-job token
`)

	require.NoError(t, NewCompiler().CompileWorkflow(workflowFile))
	lockContent, err := os.ReadFile(filepath.Join(filepath.Dir(workflowFile), "mentions-only.lock.yml"))
	require.NoError(t, err)
	lockYAML := string(lockContent)
	agent := extractJobSection(lockYAML, "agent")
	assert.NotContains(t, agent, "steps.mention_mint.outputs.token")
	safeOutputs := extractJobSection(lockYAML, "safe_outputs")
	assert.Contains(t, safeOutputs, "GH_AW_MENTIONS_GITHUB_TOKEN: ${{ steps.mention_mint.outputs.token }}")
	assert.Less(t, jobStepIDDeclarationIndex(safeOutputs, "mention_mint"), jobStepOutputConsumptionIndex(safeOutputs, "mention_mint"))
	assert.NotContains(t, extractJobSection(lockYAML, "conclusion"), "steps.mention_mint.outputs.token")
}

// TestSameJobStepTokenMintedAfterConsumerFails verifies that safe-outputs.steps, which run
// after the safe_outputs checkout and git credential steps, are reported as too late for a
// token consumed by those steps.
func TestSameJobStepTokenMintedAfterConsumerFails(t *testing.T) {
	compiler := NewCompiler()
	require.NoError(t, compiler.jobManager.AddJob(&Job{
		Name: "safe_outputs",
		Steps: []string{
			"      - name: Consume token\n        env:\n          GH_TOKEN: ${{ steps.octosts.outputs.token }}\n        run: echo consuming\n",
			"      - name: Mint token\n        id: octosts\n        uses: " + stsMintStep + "\n",
		},
	}))
	err := compiler.validateSafeOutputStepTokenReferences(&WorkflowData{SafeOutputs: &SafeOutputsConfig{GitHubToken: "${{ steps.octosts.outputs.token }}"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "after the first step that consumes the token")
	assert.Contains(t, err.Error(), "safe_outputs")
}

func TestCollectSafeOutputStepTokenIDs(t *testing.T) {
	config := &SafeOutputsConfig{
		GitHubToken: "${{ steps.global_mint.outputs.token || secrets.GITHUB_TOKEN }}",
		Mentions: &MentionsConfig{
			GitHubToken: "${{ steps.mention_mint.outputs.token }}",
		},
		CreateIssues: &CreateIssuesConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{
				GitHubToken: "${{ steps.issue_mint.outputs.token }}",
			},
		},
		AddComments: &AddCommentsConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{
				GitHubToken: "${{ secrets.CUSTOM_PAT }}",
			},
		},
	}

	ids := collectSafeOutputStepTokenIDs(config)
	assert.Len(t, ids, 3)
	assert.Contains(t, ids, "mention_mint")
	assert.Contains(t, ids, "global_mint")
	assert.Contains(t, ids, "issue_mint")

	assert.Empty(t, collectSafeOutputStepTokenIDs(nil))
}

func TestJobStepIDDeclarationAndConsumptionIndexes(t *testing.T) {
	jobYAML := "      - name: Mint\n        id: octosts\n        uses: octo-sts/action@v1\n" +
		"      - name: Use\n        with:\n          github-token: ${{ steps.octosts.outputs.token }}\n"

	declareIdx := jobStepIDDeclarationIndex(jobYAML, "octosts")
	consumeIdx := jobStepOutputConsumptionIndex(jobYAML, "octosts")
	assert.GreaterOrEqual(t, declareIdx, 0)
	assert.Greater(t, consumeIdx, declareIdx)

	assert.Equal(t, -1, jobStepIDDeclarationIndex(jobYAML, "other"))
	assert.Equal(t, -1, jobStepOutputConsumptionIndex(jobYAML, "other"))

	// List-item form of the id declaration is recognized.
	assert.GreaterOrEqual(t, jobStepIDDeclarationIndex("      - id: octosts\n", "octosts"), 0)

	// References in free-form script text are not treated as token consumption.
	assert.Equal(t, -1, jobStepOutputConsumptionIndex("          echo '${{ steps.octosts.outputs.token }}'\n", "octosts"))
}
