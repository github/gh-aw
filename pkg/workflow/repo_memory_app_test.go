//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepoMemoryGitHubAppCompilation(t *testing.T) {
	for _, tc := range []struct {
		name        string
		config      string
		appIDs      []string
		defaultIDs  []string
		permissions string
	}{
		{
			name: "object",
			config: `    github-app:
      client-id: ${{ vars.MEMORY_APP_ID }}
      private-key: ${{ secrets.MEMORY_APP_KEY }}
`,
			appIDs: []string{"default"}, permissions: "read",
		},
		{
			name: "mixed array",
			config: `    - id: first
      github-app:
        client-id: ${{ vars.FIRST_APP_ID }}
        private-key: ${{ secrets.FIRST_APP_KEY }}
    - id: second
      target-repo: other/private-memory
      github-app:
        app-id: ${{ vars.SECOND_APP_ID }}
        private-key: ${{ secrets.SECOND_APP_KEY }}
    - id: ordinary
`,
			appIDs: []string{"first", "second"}, defaultIDs: []string{"ordinary"}, permissions: "write",
		},
		{
			name: "all app array",
			config: `    - id: first
      github-app:
        client-id: ${{ vars.FIRST_APP_ID }}
        private-key: ${{ secrets.FIRST_APP_KEY }}
    - id: second
      github-app:
        client-id: ${{ vars.SECOND_APP_ID }}
        private-key: ${{ secrets.SECOND_APP_KEY }}
`,
			appIDs: []string{"first", "second"}, permissions: "read",
		},
		{
			name: "no app",
			config: `    branch-name: memory/test
`,
			defaultIDs: []string{"default"}, permissions: "write",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "memory-app.md")
			source := "---\non:\n  workflow_dispatch:\n  admission: true\nengine: copilot\npermissions:\n  contents: read\ntools:\n  github: false\n  repo-memory:\n" + tc.config + "---\nMaintain memory.\n"
			require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
			c := NewCompiler(WithLint(true))
			c.gitRoot = dir
			c.SetReadOnly(true)
			c.SetSkipValidation(true)
			require.NoError(t, c.CompileWorkflow(path))
			content, err := os.ReadFile(filepath.Join(dir, "memory-app.lock.yml"))
			require.NoError(t, err)
			var compiled map[string]any
			require.NoError(t, yaml.Unmarshal(content, &compiled))
			jobs := compiled["jobs"].(map[string]any)
			job := jobs["push_repo_memory"].(map[string]any)
			assert.Equal(t, tc.permissions, job["permissions"].(map[string]any)["contents"])
			steps := job["steps"].([]any)
			byID := map[string]map[string]any{}
			positions := map[string]int{}
			mintCount := 0
			for i, raw := range steps {
				step := raw.(map[string]any)
				id, _ := step["id"].(string)
				byID[id] = step
				positions[id] = i
				if strings.HasPrefix(id, "repo-memory-app-token-") {
					mintCount++
				}
			}
			assert.Equal(t, len(tc.appIDs), mintCount)
			for _, id := range tc.appIDs {
				tokenID := "repo-memory-app-token-" + id
				mint := byID[tokenID]
				require.NotNil(t, mint)
				assert.Equal(t, strings.Fields(getActionPin("actions/create-github-app-token"))[0], mint["uses"])
				assert.Equal(t, "always()", mint["if"])
				with := mint["with"].(map[string]any)
				assert.Equal(t, "write", with["permission-contents"])
				assert.Equal(t, false, with["skip-token-revoke"])
				for key := range with {
					if strings.HasPrefix(key, "permission-") {
						assert.Equal(t, "permission-contents", key)
					}
				}
				if tc.name == "mixed array" && id == "second" {
					assert.Equal(t, "other", with["owner"])
					assert.Equal(t, "private-memory", with["repositories"])
					assert.Equal(t, "${{ vars.SECOND_APP_ID }}", with["client-id"])
				} else {
					assert.Equal(t, "${{ github.repository_owner }}", with["owner"])
					assert.Equal(t, "${{ github.event.repository.name }}", with["repositories"])
				}
				push := byID["push_repo_memory_"+id]
				token := "${{ steps." + tokenID + ".outputs.token }}"
				assert.Equal(t, token, push["env"].(map[string]any)["GH_TOKEN"])
				assert.Equal(t, token, push["with"].(map[string]any)["github-token"])
				assert.Less(t, positions[tokenID], positions["push_repo_memory_"+id])
			}
			for _, id := range tc.defaultIDs {
				push := byID["push_repo_memory_"+id]
				assert.Equal(t, "${{ github.token }}", push["env"].(map[string]any)["GH_TOKEN"])
				assert.NotContains(t, push["with"].(map[string]any), "github-token")
			}
		})
	}
}

func TestRepoMemoryGitHubAppParsing(t *testing.T) {
	for _, tc := range []struct {
		name string
		app  map[string]any
		fail bool
	}{
		{"missing-key", map[string]any{"client-id": "app"}, true},
		{"missing-id", map[string]any{"private-key": "key"}, true},
		{"empty-key", map[string]any{"client-id": "app", "private-key": "  "}, true},
		{"ignore-missing", map[string]any{"ignore-if-missing": true}, false},
		{"legacy", map[string]any{"app-id": "app", "private-key": "key"}, false},
		{"extra-permissions", map[string]any{"client-id": "app", "private-key": "key", "permissions": map[string]any{"issues": "write"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry, err := parseRepoMemoryEntry(map[string]any{"github-app": tc.app}, "test", "memory", false)
			if tc.fail {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "tools.repo-memory.github-app")
			} else {
				require.NoError(t, err)
				require.NotNil(t, entry.GitHubApp)
			}
		})
	}
}

func TestRepoMemoryGitHubAppClone(t *testing.T) {
	for _, target := range []string{"", "other/private-memory"} {
		t.Run(target, func(t *testing.T) {
			memory := newDefaultRepoMemoryEntry("test", "memory")
			memory.TargetRepo = target
			memory.GitHubApp = &GitHubAppConfig{AppID: "${{ vars.APP_ID }}", PrivateKey: "${{ secrets.APP_KEY }}"}
			var b strings.Builder
			NewCompiler().generateRepoMemorySteps(&b, &WorkflowData{RepoMemoryConfig: &RepoMemoryConfig{Memories: []RepoMemoryEntry{memory}}}, "agent")
			if target == "" {
				assert.NotContains(t, b.String(), "actions/create-github-app-token")
				assert.Contains(t, b.String(), "GH_TOKEN: ${{ github.token }}")
			} else {
				assert.Contains(t, b.String(), "permission-contents: read")
				assert.NotContains(t, b.String(), "permission-contents: write")
				assert.Contains(t, b.String(), "owner: other")
				assert.Contains(t, b.String(), "repositories: private-memory")
				assert.Contains(t, b.String(), "GH_TOKEN: ${{ steps.repo-memory-app-token-default.outputs.token }}")
			}
		})
	}
}

func TestRepoMemoryGitHubAppOverridesAndFallback(t *testing.T) {
	memory := newDefaultRepoMemoryEntry("test", "memory")
	memory.TargetRepo = "target/memory"
	memory.GitHubApp = &GitHubAppConfig{
		AppID: "${{ vars.APP_ID }}", PrivateKey: "${{ secrets.APP_KEY }}",
		Owner: "installation", Repositories: []string{"one", "two"}, IgnoreIfMissing: true,
	}
	c := NewCompiler()
	mint := strings.Join(c.buildRepoMemoryAppTokenMintStep(memory, PermissionWrite, pushRepoMemoryJobName), "")
	assert.Contains(t, mint, "owner: installation")
	assert.Contains(t, mint, "repositories: |-\n            one\n            two")
	assert.Contains(t, mint, "if:")
	assert.Contains(t, mint, "always() &&")
	assert.Contains(t, repoMemoryAppToken(memory), "outputs.token || github.token")
	job, err := c.buildPushRepoMemoryJob(&WorkflowData{RepoMemoryConfig: &RepoMemoryConfig{Memories: []RepoMemoryEntry{memory}}}, false)
	require.NoError(t, err)
	assert.Contains(t, job.Permissions, "contents: write")
}

func TestRepoMemoryGitHubAppExpressionTarget(t *testing.T) {
	memory := newDefaultRepoMemoryEntry("test", "memory")
	memory.TargetRepo = "${{ vars.MEMORY_REPO }}"
	memory.GitHubApp = &GitHubAppConfig{AppID: "${{ vars.APP_ID }}", PrivateKey: "${{ secrets.APP_KEY }}"}
	for _, level := range []PermissionLevel{PermissionRead, PermissionWrite} {
		mint := strings.Join(NewCompiler().buildRepoMemoryAppTokenMintStep(memory, level, pushRepoMemoryJobName), "")
		assert.Contains(t, mint, "GH_AW_TARGET_REPOSITORY: ${{ vars.MEMORY_REPO }}")
		assert.Contains(t, mint, "repositories: ${{ steps.repo-memory-app-target-default.outputs.repository }}")
		assert.Contains(t, mint, "owner: ${{ steps.repo-memory-app-target-default.outputs.owner }}")
		assert.NotContains(t, mint, "repositories: ${{ github.event.repository.name }}")
		assert.Contains(t, mint, `"$GH_AW_TARGET_REPOSITORY" =~`)
		if level == PermissionWrite {
			assert.Equal(t, 2, strings.Count(mint, "if: always()"))
		}
	}

}

func TestRepoMemoryGitHubAppStepIDsDoNotCollide(t *testing.T) {
	first := newDefaultRepoMemoryEntry("test", "memory")
	first.ID = "foo"
	first.TargetRepo = "${{ vars.MEMORY_REPO }}"
	first.GitHubApp = &GitHubAppConfig{AppID: "${{ vars.APP_ID }}", PrivateKey: "${{ secrets.APP_KEY }}"}
	second := first
	second.ID = "foo-target"
	job, err := NewCompiler().buildPushRepoMemoryJob(&WorkflowData{
		RepoMemoryConfig: &RepoMemoryConfig{Memories: []RepoMemoryEntry{first, second}},
	}, false)
	require.NoError(t, err)
	var parsed struct {
		Steps []struct{ ID string }
	}
	require.NoError(t, yaml.Unmarshal([]byte("steps:\n"+strings.Join(job.Steps, "")), &parsed))
	require.NotEmpty(t, parsed.Steps)
	seen := map[string]bool{}
	for _, step := range parsed.Steps {
		if step.ID != "" {
			assert.False(t, seen[step.ID], "duplicate step ID %s", step.ID)
			seen[step.ID] = true
		}
	}
}
