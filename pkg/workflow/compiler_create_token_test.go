//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateTokenJobBindings(t *testing.T) {
	for _, jobName := range []string{"activation", "agent", "safe_outputs", "conclusion"} {
		t.Run(jobName, func(t *testing.T) {
			for _, permissions := range []string{"", "permissions: {}", "permissions: read-all", "permissions:\n      contents: read\n      id-token: none", "permissions:\n      contents: read\n      id-token: write"} {
				t.Run(permissions, func(t *testing.T) {
					compiler := NewCompiler()
					compiler.jobManager = NewJobManager()
					job := &Job{Name: jobName, Permissions: permissions, Steps: []string{
						"      - run: echo before\n        env:\n          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}\n",
						"      - id: 'create-token'\n        uses: actions/create-github-app-token@v2\n",
						"      - name: Start MCP Gateway\n        id: start-mcp-gateway\n        run: echo after\n",
						"        env:\n",
						"          GH_AW_GITHUB_TOKEN: ${{ secrets.GH_AW_GITHUB_TOKEN }}\n",
						"          GH_AW_GITHUB_MCP_SERVER_TOKEN: ${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN }}\n",
						"          GITHUB_MCP_SERVER_TOKEN: ${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}\n",
						"          GH_TOKEN: ${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}\n",
						"          GITHUB_TOKEN: ${{ secrets.CUSTOM_TOKEN }}\n",
						"          COPILOT_GITHUB_TOKEN: ${{ secrets.COPILOT_GITHUB_TOKEN }}\n",
						"      - name: Build checkout manifest for safe-outputs handlers\n        uses: actions/github-script@v8\n        env:\n          GH_TOKEN: ${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}\n",
						"      - name: Redact secrets in logs\n        id: redact_secrets\n",
						"        env:\n",
						"          GH_AW_SECRET_NAMES: 'GH_AW_GITHUB_TOKEN'\n",
						"          SECRET_GH_AW_GITHUB_TOKEN: ${{ secrets.GH_AW_GITHUB_TOKEN }}\n",
						"      - run: echo explicit\n        env:\n          GH_TOKEN: ${{ steps.other.outputs.token || secrets.GITHUB_TOKEN }}\n",
						"      - run: echo authored\n        env:\n          GH_TOKEN: ${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}\n",
						"      - run: |\n          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}\n",
					}}
					fragmentCount := len(job.Steps)
					lastFragment := job.Steps[fragmentCount-1]
					require.NoError(t, compiler.jobManager.AddJob(job))
					if err := compiler.wireCreateTokenJobs(nil); err != nil {
						t.Fatalf("%v\n%s", err, strings.Join(job.Steps, ""))
					}
					content := strings.Join(job.Steps, "")
					assert.Len(t, job.Steps, fragmentCount)
					assert.Equal(t, lastFragment, job.Steps[fragmentCount-1], "later step fragments must remain intact")
					assert.Contains(t, content, "GH_AW_GITHUB_TOKEN: ${{ steps.create-token.outputs.token || secrets.GH_AW_GITHUB_TOKEN }}")
					assert.Contains(t, content, "GH_AW_GITHUB_MCP_SERVER_TOKEN: ${{ steps.create-token.outputs.token || secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN }}")
					assert.Contains(t, content, "GITHUB_MCP_SERVER_TOKEN: ${{ steps.create-token.outputs.token || secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}")
					assert.Equal(t, 2, strings.Count(content, "GH_TOKEN: ${{ steps.create-token.outputs.token || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}"))
					assert.Contains(t, content, "GH_AW_SECRET_NAMES: 'GH_AW_GITHUB_TOKEN,GH_AW_GITHUB_TOKEN_MINTED'")
					assert.Contains(t, content, "SECRET_GH_AW_GITHUB_TOKEN: ${{ secrets.GH_AW_GITHUB_TOKEN }}")
					assert.Contains(t, content, "SECRET_GH_AW_GITHUB_TOKEN_MINTED: ${{ steps.create-token.outputs.token }}")
					assert.Contains(t, content, "GITHUB_TOKEN: ${{ secrets.CUSTOM_TOKEN }}")
					assert.Contains(t, content, "COPILOT_GITHUB_TOKEN: ${{ secrets.COPILOT_GITHUB_TOKEN }}")
					assert.Contains(t, content, "GH_TOKEN: ${{ steps.other.outputs.token || secrets.GITHUB_TOKEN }}")
					assert.Contains(t, content, "GH_TOKEN: ${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}")
					assert.Contains(t, job.Steps[0], "GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}")
					assert.Contains(t, job.Steps[1], "uses: actions/create-github-app-token@v2")
					assert.Contains(t, content, "run: |\n          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}")
					if strings.Contains(permissions, "id-token: none") {
						assert.NotContains(t, job.Permissions, "id-token: write")
					} else {
						assert.Contains(t, job.Permissions, "id-token: write")
					}
					if strings.Contains(permissions, "contents: read") || permissions == "permissions: read-all" {
						assert.Contains(t, job.Permissions, "contents: read")
					}
					require.NoError(t, compiler.wireCreateTokenJobs(nil))
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
	require.NoError(t, compiler.wireCreateTokenJobs(nil))
	assert.Equal(t, before, strings.Join(job.Steps, ""))
	assert.Equal(t, "permissions: {}", job.Permissions)
}

func TestCreateTokenIDRequiresTrustedAction(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	job := &Job{Name: "agent", Steps: []string{`      - id: create-token
        run: echo "token=example" >> "$GITHUB_OUTPUT"
      - name: Start MCP Gateway
        id: start-mcp-gateway
        env:
          GITHUB_MCP_SERVER_TOKEN: ${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}
`}}
	require.NoError(t, compiler.jobManager.AddJob(job))
	before := strings.Join(job.Steps, "")
	require.NoError(t, compiler.wireCreateTokenJobs(nil))
	assert.Equal(t, before, strings.Join(job.Steps, ""))
	assert.Empty(t, job.Permissions)
}

func TestCreateTokenOIDCOptOuts(t *testing.T) {
	none := string(PermissionNone)
	for _, tt := range []struct {
		name string
		data *WorkflowData
		job  *Job
	}{
		{name: "safe outputs", data: &WorkflowData{SafeOutputs: &SafeOutputsConfig{IDToken: &none}}, job: &Job{Name: "agent"}},
		{name: "workflow permissions", data: &WorkflowData{Permissions: "permissions:\n  id-token: none"}, job: &Job{Name: "agent"}},
		{name: "job permissions", data: &WorkflowData{}, job: &Job{Name: "agent", Permissions: "permissions:\n  id-token: none"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.jobManager = NewJobManager()
			tt.job.Steps = []string{`      - id: create-token
        uses: actions/create-github-app-token@v2
`}
			require.NoError(t, compiler.jobManager.AddJob(tt.job))
			require.NoError(t, compiler.wireCreateTokenJobs(tt.data))
			assert.NotContains(t, tt.job.Permissions, "id-token: write")
		})
	}
}

func TestCreateTokenPreservesExplicitAndJobScopedBindings(t *testing.T) {
	tests := []struct {
		name           string
		jobName        string
		value          string
		rawFrontmatter map[string]any
		engineConfig   *EngineConfig
	}{
		{
			name:    "job token only",
			jobName: "agent",
			value:   "${{ secrets.GITHUB_TOKEN }}",
		},
		{
			name:    "explicit safe output token",
			jobName: "safe_outputs",
			value:   "${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}",
			rawFrontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-pull-request": map[string]any{
						"github-token": "${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}",
					},
				},
			},
		},
		{
			name:         "engine environment override",
			jobName:      "agent",
			value:        "${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}",
			engineConfig: &EngineConfig{Env: map[string]string{"GITHUB_MCP_SERVER_TOKEN": "${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.jobManager = NewJobManager()
			job := &Job{Name: tt.jobName, Steps: []string{fmt.Sprintf(`      - id: create-token
        uses: actions/create-github-app-token@v2
      - name: Start MCP Gateway
        id: start-mcp-gateway
        env:
          GITHUB_MCP_SERVER_TOKEN: %s
`, tt.value)}}
			require.NoError(t, compiler.jobManager.AddJob(job))
			require.NoError(t, compiler.wireCreateTokenJobs(&WorkflowData{RawFrontmatter: tt.rawFrontmatter, EngineConfig: tt.engineConfig}))
			assert.Contains(t, strings.Join(job.Steps, ""), "GITHUB_MCP_SERVER_TOKEN: "+tt.value)
			assert.NotContains(t, strings.Join(job.Steps, ""), "steps.create-token.outputs.token")
		})
	}
}

func TestCreateTokenRedactionFallbackCollision(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	job := &Job{Name: "agent", Steps: []string{`      - id: create-token
        uses: actions/create-github-app-token@v2
      - name: Redact secrets in logs
        id: redact_secrets
        env:
          GH_AW_SECRET_NAMES: 'GH_AW_GITHUB_TOKEN,GH_AW_GITHUB_TOKEN_MINTED'
          SECRET_GH_AW_GITHUB_TOKEN: ${{ secrets.GH_AW_GITHUB_TOKEN }}
          SECRET_GH_AW_GITHUB_TOKEN_MINTED_1: ${{ secrets.OTHER_TOKEN }}
`}}
	require.NoError(t, compiler.jobManager.AddJob(job))
	require.NoError(t, compiler.wireCreateTokenJobs(nil))
	content := strings.Join(job.Steps, "")
	assert.Contains(t, content, "GH_AW_SECRET_NAMES: 'GH_AW_GITHUB_TOKEN,GH_AW_GITHUB_TOKEN_MINTED,GH_AW_GITHUB_TOKEN_MINTED_2'")
	assert.Contains(t, content, "SECRET_GH_AW_GITHUB_TOKEN_MINTED_1: ${{ secrets.OTHER_TOKEN }}")
	assert.Contains(t, content, "SECRET_GH_AW_GITHUB_TOKEN_MINTED_2: ${{ steps.create-token.outputs.token }}")
	require.NoError(t, compiler.wireCreateTokenJobs(nil))
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
    uses: actions/create-github-app-token@v3
    with:
      app-id: ${{ vars.APP_ID }}
      private-key: ${{ secrets.APP_PRIVATE_KEY }}
      owner: ${{ github.repository_owner }}
      repositories: ${{ github.event.repository.name }}
      permission-contents: read
safe-outputs:
  add-comment:
jobs:
  activation:
    pre-steps:
      - id: create-token
        uses: actions/create-github-app-token@v3
        with:
          app-id: ${{ vars.APP_ID }}
          private-key: ${{ secrets.APP_PRIVATE_KEY }}
          owner: ${{ github.repository_owner }}
          repositories: ${{ github.event.repository.name }}
          permission-contents: read
  safe_outputs:
    pre-steps:
      - id: create-token
        uses: actions/create-github-app-token@v3
        with:
          app-id: ${{ vars.APP_ID }}
          private-key: ${{ secrets.APP_PRIVATE_KEY }}
          owner: ${{ github.repository_owner }}
          repositories: ${{ github.event.repository.name }}
          permission-contents: read
---

# Minted token compilation
`)
	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowFile))
	lockPath := stringutil.MarkdownToLockFile(workflowFile)
	content, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "\npermissions: {}\n", "compiled workflows do not leave inherited token scopes")
	for _, name := range []string{"activation", "agent", "safe_outputs"} {
		job := extractJobSection(string(content), name)
		require.NotEmpty(t, job)
		assert.Contains(t, job, "id: create-token")
		assert.Contains(t, job, "id-token: write")
		if name == "activation" {
			assert.Contains(t, job, "GH_AW_GITHUB_TOKEN: ${{ secrets.GH_AW_GITHUB_TOKEN }}")
		}
		if name == "agent" {
			assert.Contains(t, job, "steps.create-token.outputs.token || secrets.GH_AW_GITHUB_TOKEN")
		}
	}
	agent := extractJobSection(string(content), "agent")
	assert.Contains(t, agent, "GH_TOKEN: ${{ steps.create-token.outputs.token || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}")
	assert.Contains(t, agent, "GITHUB_MCP_SERVER_TOKEN: ${{ steps.create-token.outputs.token || secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}")
	assert.Contains(t, agent, "SECRET_GH_AW_GITHUB_TOKEN_MINTED: ${{ steps.create-token.outputs.token }}")
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
