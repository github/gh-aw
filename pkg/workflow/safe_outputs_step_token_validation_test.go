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

// TestSameJobStepTokenMintedInEveryConsumingJobCompiles verifies that a same-job
// steps.<id>.outputs.token expression compiles when the minting step is injected into
// the agent job (top-level pre-steps) and into the safe_outputs and conclusion jobs
// (jobs.<job>.pre-steps).
func TestSameJobStepTokenMintedInEveryConsumingJobCompiles(t *testing.T) {
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
	require.NoError(t, compiler.CompileWorkflow(workflowFile))

	lockContent, err := os.ReadFile(filepath.Join(filepath.Dir(workflowFile), "minted.lock.yml"))
	require.NoError(t, err)
	lockYAML := string(lockContent)

	for _, jobName := range []string{"safe_outputs", "conclusion"} {
		section := extractJobSection(lockYAML, jobName)
		require.NotEmpty(t, section, "expected %s job section", jobName)
		assert.Contains(t, section, "id: octosts")
		assert.Contains(t, section, "${{ steps.octosts.outputs.token || secrets.CUSTOM_PAT || secrets.GITHUB_TOKEN }}")
	}
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
	assert.Contains(t, err.Error(), `has no step with id "octosts"`)
	assert.Contains(t, err.Error(), "pre-steps:")
}

// TestSameJobStepTokenMintedAfterConsumerFails verifies that safe-outputs.steps, which run
// after the safe_outputs checkout and git credential steps, are reported as too late for a
// token consumed by those steps.
func TestSameJobStepTokenMintedAfterConsumerFails(t *testing.T) {
	workflowFile := writeStepTokenWorkflow(t, "late", `---
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
  steps:
    - name: Mint token (too late)
      id: octosts
      uses: `+stsMintStep+`
  github-token: ${{ steps.octosts.outputs.token || secrets.GITHUB_TOKEN }}
jobs:
  conclusion:
    pre-steps:
      - name: Mint token (conclusion)
        id: octosts
        uses: `+stsMintStep+`
---

# Late same-job token minting
`)

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(workflowFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "after the first step that consumes the token")
	assert.Contains(t, err.Error(), "safe_outputs")
}

func TestCollectSafeOutputStepTokenIDs(t *testing.T) {
	config := &SafeOutputsConfig{
		GitHubToken: "${{ steps.global_mint.outputs.token || secrets.GITHUB_TOKEN }}",
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
	assert.Len(t, ids, 2)
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
