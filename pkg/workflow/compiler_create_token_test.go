//go:build !integration

package workflow

import (
	"os"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateTokenJobBindings(t *testing.T) {
	for _, jobName := range []string{"activation", "agent", "safe_outputs", "conclusion", "custom"} {
		t.Run(jobName, func(t *testing.T) {
			for _, permissions := range []string{"", "permissions: {}", "permissions: read-all", "permissions:\n      contents: read\n      id-token: none", "permissions:\n      contents: read\n      id-token: write"} {
				t.Run(permissions, func(t *testing.T) {
					compiler := NewCompiler()
					compiler.jobManager = NewJobManager()
					job := &Job{Name: jobName, Permissions: permissions, Steps: []string{
						"      - run: echo before\n        env:\n          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}\n",
						"      - id: 'create-token'\n        run: echo mint\n        env:\n          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}\n",
						"      - run: echo after\n",
						"        env:\n",
						"          GH_AW_GITHUB_TOKEN: ${{ secrets.GH_AW_GITHUB_TOKEN }}\n",
						"          GH_AW_GITHUB_MCP_SERVER_TOKEN: ${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN }}\n",
						"          GH_TOKEN: ${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}\n",
						"          GITHUB_TOKEN: ${{ secrets.CUSTOM_TOKEN }}\n",
						"          COPILOT_GITHUB_TOKEN: ${{ secrets.COPILOT_GITHUB_TOKEN }}\n",
						"          GH_AW_SECRET_NAMES: 'GH_AW_GITHUB_TOKEN'\n",
						"          SECRET_GH_AW_GITHUB_TOKEN: ${{ secrets.GH_AW_GITHUB_TOKEN }}\n",
						"      - run: echo explicit\n        env:\n          GH_TOKEN: ${{ steps.other.outputs.token || secrets.GITHUB_TOKEN }}\n",
						"      - run: |\n          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}\n",
					}}
					require.NoError(t, compiler.jobManager.AddJob(job))
					require.NoError(t, compiler.wireCreateTokenJobs())
					content := strings.Join(job.Steps, "")
					assert.Contains(t, content, "GH_AW_GITHUB_TOKEN: ${{ steps.create-token.outputs.token || secrets.GH_AW_GITHUB_TOKEN }}")
					assert.Contains(t, content, "GH_AW_GITHUB_MCP_SERVER_TOKEN: ${{ steps.create-token.outputs.token || secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN }}")
					assert.Contains(t, content, "GH_TOKEN: ${{ steps.create-token.outputs.token || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}")
					assert.Contains(t, content, "SECRET_GH_AW_GITHUB_TOKEN: ${{ steps.create-token.outputs.token || secrets.GH_AW_GITHUB_TOKEN }}")
					assert.Contains(t, content, "GH_AW_SECRET_NAMES: 'GH_AW_GITHUB_TOKEN,GH_AW_GITHUB_TOKEN_FALLBACK'")
					assert.Contains(t, content, "SECRET_GH_AW_GITHUB_TOKEN_FALLBACK: ${{ secrets.GH_AW_GITHUB_TOKEN }}")
					assert.Contains(t, content, "GITHUB_TOKEN: ${{ secrets.CUSTOM_TOKEN }}")
					assert.Contains(t, content, "COPILOT_GITHUB_TOKEN: ${{ secrets.COPILOT_GITHUB_TOKEN }}")
					assert.Contains(t, content, "GH_TOKEN: ${{ steps.other.outputs.token || secrets.GITHUB_TOKEN }}")
					assert.Contains(t, job.Steps[0], "GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}")
					assert.Contains(t, job.Steps[1], "GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}")
					assert.Contains(t, content, "run: |\n          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}")
					assert.Contains(t, job.Permissions, "id-token: write")
					if strings.Contains(permissions, "contents: read") || permissions == "permissions: read-all" {
						assert.Contains(t, job.Permissions, "contents: read")
					}
					require.NoError(t, compiler.wireCreateTokenJobs())
					assert.Equal(t, content, strings.Join(job.Steps, ""), "wiring must be idempotent")
				})
			}
		})
	}
}

func TestCreateTokenNonMintingJobUnchanged(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	job := &Job{Name: "plain", Permissions: "permissions: {}", Steps: []string{`      - run: |
          id: create-token
          env:
            GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        env:
          GH_TOKEN: ${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}
      - id: create-token-other
        run: echo not a mint
`}}
	require.NoError(t, compiler.jobManager.AddJob(job))
	before := strings.Join(job.Steps, "")
	require.NoError(t, compiler.wireCreateTokenJobs())
	assert.Equal(t, before, strings.Join(job.Steps, ""))
	assert.Equal(t, "permissions: {}", job.Permissions)
}

func TestCreateTokenRedactionFallbackCollision(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	job := &Job{Name: "agent", Steps: []string{`      - id: create-token
        run: echo mint
      - run: echo redact
        env:
          GH_AW_SECRET_NAMES: 'GH_AW_GITHUB_TOKEN,GH_AW_GITHUB_TOKEN_FALLBACK'
          SECRET_GH_AW_GITHUB_TOKEN: ${{ secrets.GH_AW_GITHUB_TOKEN }}
          SECRET_GH_AW_GITHUB_TOKEN_FALLBACK: ${{ secrets.OTHER_TOKEN }}
`}}
	require.NoError(t, compiler.jobManager.AddJob(job))
	require.NoError(t, compiler.wireCreateTokenJobs())
	content := strings.Join(job.Steps, "")
	assert.Contains(t, content, "GH_AW_SECRET_NAMES: 'GH_AW_GITHUB_TOKEN,GH_AW_GITHUB_TOKEN_FALLBACK,GH_AW_GITHUB_TOKEN_FALLBACK_'")
	assert.Contains(t, content, "SECRET_GH_AW_GITHUB_TOKEN_FALLBACK: ${{ secrets.OTHER_TOKEN }}")
	assert.Contains(t, content, "SECRET_GH_AW_GITHUB_TOKEN_FALLBACK_: ${{ secrets.GH_AW_GITHUB_TOKEN }}")
	require.NoError(t, compiler.wireCreateTokenJobs())
	assert.Equal(t, content, strings.Join(job.Steps, ""))
}

func TestCreateTokenBareCompile(t *testing.T) {
	workflowFile := writeStepTokenWorkflow(t, "create-token", `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
strict: false
checkout:
  - repository: owner/other
    path: ./other
    github-token: ${{ secrets.CROSS_REPO_PAT }}
pre-steps:
  - id: create-token
    run: echo "token=example" >> "$GITHUB_OUTPUT"
safe-outputs:
  add-comment:
jobs:
  activation:
    pre-steps:
      - id: create-token
        run: echo "token=example" >> "$GITHUB_OUTPUT"
  safe_outputs:
    pre-steps:
      - id: create-token
        run: echo "token=example" >> "$GITHUB_OUTPUT"
---

# Minted token compilation
`)
	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowFile))
	lockPath := stringutil.MarkdownToLockFile(workflowFile)
	content, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	for _, name := range []string{"activation", "agent", "safe_outputs"} {
		job := extractJobSection(string(content), name)
		require.NotEmpty(t, job)
		assert.Contains(t, job, "id: create-token")
		assert.Contains(t, job, "id-token: write")
		if name != "safe_outputs" {
			assert.Contains(t, job, "steps.create-token.outputs.token || secrets.GH_AW_GITHUB_TOKEN")
		}
	}
	agent := extractJobSection(string(content), "agent")
	assert.Contains(t, agent, "GH_TOKEN: ${{ steps.create-token.outputs.token || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}")
	assert.Contains(t, agent, "GH_AW_CHECKOUT_TOKEN_0: ${{ secrets.CROSS_REPO_PAT }}")
	conclusion := extractJobSection(string(content), "conclusion")
	require.NotEmpty(t, conclusion)
	assert.NotContains(t, conclusion, "steps.create-token.")
	assert.NotContains(t, conclusion, "id-token: write")
	require.NoError(t, compiler.CompileWorkflow(workflowFile))
	recompiled, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	assert.Equal(t, string(content), string(recompiled), "bare compilation should not drift")
}
