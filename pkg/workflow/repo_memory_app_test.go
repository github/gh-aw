//go:build !integration

package workflow

import (
	"os"
	"os/exec"
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
		fallback    bool
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
			name: "bounded expression",
			config: `    target-repo: ${{ vars.MEMORY_REPO }}
    github-app:
      client-id: ${{ vars.MEMORY_APP_ID }}
      private-key: ${{ secrets.MEMORY_APP_KEY }}
      owner: other
      repositories: [private-memory]
`,
			appIDs: []string{"default"}, permissions: "read",
		},
		{
			name: "missing optional credentials",
			config: `    github-app:
      ignore-if-missing: true
`,
			appIDs: []string{"default"}, permissions: "write", fallback: true,
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
			source := "---\non:\n  workflow_dispatch:\n  admission: true\nengine: copilot\nenvironment: memory\npermissions:\n  contents: read\ntools:\n  github: false\n  repo-memory:\n" + tc.config + "---\nMaintain memory.\n"
			require.NoError(t, os.WriteFile(path, []byte(source), 0o600))
			c := NewCompiler(WithLint(true))
			c.gitRoot = dir
			c.SetReadOnly(true)
			require.NoError(t, c.CompileWorkflow(path))
			content, err := os.ReadFile(filepath.Join(dir, "memory-app.lock.yml"))
			require.NoError(t, err)
			var compiled map[string]any
			require.NoError(t, yaml.Unmarshal(content, &compiled))
			jobs := compiled["jobs"].(map[string]any)
			job := jobs["push_repo_memory"].(map[string]any)
			assert.Equal(t, tc.permissions, job["permissions"].(map[string]any)["contents"])
			if len(tc.appIDs) > 0 {
				assert.Equal(t, "memory", job["environment"])
			} else {
				assert.NotContains(t, job, "environment")
			}
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
				if tc.fallback || tc.name == "bounded expression" {
					assert.Contains(t, mint["if"], "always() &&")
				} else {
					assert.Equal(t, "always()", mint["if"])
				}
				with := mint["with"].(map[string]any)
				if tc.fallback {
					assert.Equal(t, "${{ '' }}", with["client-id"])
					assert.Equal(t, "${{ '' }}", with["private-key"])
				}
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
					assert.Equal(t, "other/private-memory", byID["push_repo_memory_"+id]["env"].(map[string]any)["REPO_MEMORY_ALLOWED_REPOS"])
				} else if tc.name == "bounded expression" {
					assert.Equal(t, "${{ steps.repo-memory-app-target-default.outputs.owner }}", with["owner"])
					assert.Equal(t, "${{ steps.repo-memory-app-target-default.outputs.repository }}", with["repositories"])
				} else {
					assert.Equal(t, "${{ github.repository_owner }}", with["owner"])
					assert.Equal(t, "${{ github.event.repository.name }}", with["repositories"])
				}
				push := byID["push_repo_memory_"+id]
				token := "${{ steps." + tokenID + ".outputs.token }}"
				if tc.fallback {
					token = "${{ steps." + tokenID + ".outputs.token || github.token }}"
				}
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

func TestRepoMemoryGitHubAppTargetValidation(t *testing.T) {
	for _, target := range []string{"repository", "/repository", "owner/", "owner/repo/extra", "owner/repo name"} {
		for _, field := range []string{"github-app", "github-app-read", "github-app-write"} {
			t.Run(target+"/"+field, func(t *testing.T) {
				_, err := parseRepoMemoryEntry(map[string]any{
					"target-repo": target,
					field:         map[string]any{"client-id": "${{ vars.APP_ID }}", "private-key": "${{ secrets.APP_KEY }}"},
				}, "test", "memory", false)
				require.ErrorContains(t, err, "tools.repo-memory.target-repo must use owner/repo")
			})
		}
	}
	for _, target := range []string{"", "owner/repo", "${{ vars.MEMORY_REPO }}", "${{ github.repository }}"} {
		t.Run(target, func(t *testing.T) {
			_, err := parseRepoMemoryEntry(map[string]any{
				"target-repo": target,
				"github-app":  map[string]any{"client-id": "${{ vars.APP_ID }}", "private-key": "${{ secrets.APP_KEY }}"},
			}, "test", "memory", false)
			require.NoError(t, err)
		})
	}
}

func TestRepoMemoryGitHubAppDailyLedger(t *testing.T) {
	for _, target := range []string{"", "other/private-memory", "${{ vars.MEMORY_REPO }}"} {
		for _, tc := range []struct {
			name                                  string
			separate, readFallback, writeFallback bool
		}{
			{name: "shared"},
			{name: "shared fallback", readFallback: true, writeFallback: true},
			{name: "separate", separate: true},
			{name: "read fallback", separate: true, readFallback: true},
			{name: "write fallback", separate: true, writeFallback: true},
		} {
			t.Run(target+"/"+tc.name, func(t *testing.T) {
				memory := newDefaultRepoMemoryEntry("test", "memory")
				memory.TargetRepo = target
				memory.GitHubApp = &GitHubAppConfig{
					AppID: "${{ vars.APP_ID }}", PrivateKey: "${{ secrets.APP_KEY }}", IgnoreIfMissing: tc.writeFallback,
				}
				if tc.separate {
					memory.GitHubApp = nil
					memory.GitHubAppRead = &GitHubAppConfig{
						AppID: "${{ vars.READ_APP_ID }}", PrivateKey: "${{ secrets.READ_APP_KEY }}", IgnoreIfMissing: tc.readFallback,
					}
					memory.GitHubAppWrite = &GitHubAppConfig{
						AppID: "${{ vars.WRITE_APP_ID }}", PrivateKey: "${{ secrets.WRITE_APP_KEY }}", IgnoreIfMissing: tc.writeFallback,
					}
				}
				data := &WorkflowData{
					RepoMemoryConfig:   &RepoMemoryConfig{Memories: []RepoMemoryEntry{memory}},
					MaxDailyAICBackend: maxDailyAICBackendRepoMemory,
				}
				c := NewCompiler()
				activation := strings.Join(c.buildActivationDailyAICGuardrailStep(data), "")
				job, err := c.buildPushRepoMemoryJob(data, false)
				require.NoError(t, err)
				push := strings.Join(job.Steps, "")
				readID := repoMemoryAppTokenStepID(memory, PermissionRead)
				writeID := repoMemoryAppTokenStepID(memory, PermissionWrite)
				for _, steps := range []string{activation, push} {
					assert.Contains(t, steps, "GH_TOKEN: "+repoMemoryAppToken(memory, PermissionRead))
					assert.Contains(t, steps, "permission-contents: read")
					if tc.separate {
						assert.Contains(t, steps, "client-id: ${{ vars.READ_APP_ID }}")
					}
				}
				if tc.separate {
					assert.NotContains(t, activation, "WRITE_APP")
					assert.Contains(t, push, "client-id: ${{ vars.WRITE_APP_ID }}")
				}
				assert.Less(t, strings.Index(activation, "id: "+readID), strings.Index(activation, "id: daily-aic-repo-memory-clone"))
				assert.Less(t, strings.Index(push, "id: "+readID), strings.Index(push, "Clone daily AIC repo-memory ledger for hydration"))
				assert.Less(t, strings.Index(push, "Clone daily AIC repo-memory ledger for hydration"), strings.Index(push, "id: "+writeID))
				permission := "read"
				if tc.writeFallback {
					permission = "write"
				}
				assert.Contains(t, job.Permissions, "contents: "+permission)
				var parsed struct {
					Steps []struct {
						ID   string         `yaml:"id"`
						If   string         `yaml:"if"`
						With map[string]any `yaml:"with"`
					} `yaml:"steps"`
				}
				require.NoError(t, yaml.Unmarshal([]byte("steps:\n"+push), &parsed))
				seen := map[string]bool{}
				for _, step := range parsed.Steps {
					if step.ID == "" {
						continue
					}
					assert.False(t, seen[step.ID], "duplicate step ID %s", step.ID)
					seen[step.ID] = true
					if step.ID == readID {
						assert.Contains(t, step.If, "always()")
						assert.Equal(t, "read", step.With["permission-contents"])
					}
				}
			})
		}
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
		for _, field := range []string{"github-app", "github-app-read", "github-app-write"} {
			t.Run(tc.name+"/"+field, func(t *testing.T) {
				entry, err := parseRepoMemoryEntry(map[string]any{field: tc.app}, "test", "memory", false)
				if tc.fail {
					require.Error(t, err)
					assert.Contains(t, err.Error(), "tools.repo-memory."+field)
				} else {
					require.NoError(t, err)
					assert.True(t, entry.githubApp(PermissionRead) != nil || entry.githubApp(PermissionWrite) != nil)
				}
			})
		}
	}
}

func TestRepoMemoryGitHubAppReadWriteCompilation(t *testing.T) {
	for _, tc := range []struct {
		name, shared, read, write   string
		readFallback, writeFallback bool
		emptyRead, emptyWrite       bool
	}{
		{name: "separate", read: "READ", write: "WRITE"},
		{name: "read override", shared: "SHARED", read: "READ"},
		{name: "write override", shared: "SHARED", write: "WRITE"},
		{name: "read only", read: "READ"},
		{name: "write only", write: "WRITE"},
		{name: "read fallback overrides shared", shared: "SHARED", read: "READ", readFallback: true},
		{name: "write fallback overrides shared", shared: "SHARED", write: "WRITE", writeFallback: true},
		{name: "missing read credentials", shared: "SHARED", read: "READ", readFallback: true, emptyRead: true},
		{name: "missing write credentials", shared: "SHARED", write: "WRITE", writeFallback: true, emptyWrite: true},
	} {
		for _, target := range []string{"", "other/memory", "${{ vars.MEMORY_REPO }}"} {
			for _, array := range []bool{false, true} {
				t.Run(tc.name+"/"+target+map[bool]string{false: "/object", true: "/array"}[array], func(t *testing.T) {
					config := map[string]any{"target-repo": target}
					for field, id := range map[string]string{"github-app": tc.shared, "github-app-read": tc.read, "github-app-write": tc.write} {
						if id != "" {
							config[field] = map[string]any{
								"client-id": "${{ vars." + id + "_APP_ID }}", "private-key": "${{ secrets." + id + "_APP_KEY }}",
								"owner": "other", "repositories": []string{"memory", strings.ToLower(id) + "-only"},
								"ignore-if-missing": field == "github-app-read" && tc.readFallback || field == "github-app-write" && tc.writeFallback,
							}
							if field == "github-app-read" && tc.emptyRead || field == "github-app-write" && tc.emptyWrite {
								config[field] = map[string]any{"ignore-if-missing": true}
							}
						}
					}
					var memory any = config
					if array {
						config["id"] = "default"
						memory = []any{config}
					}
					source, err := yaml.Marshal(map[string]any{
						"on": map[string]any{"workflow_dispatch": nil, "admission": true}, "engine": "copilot",
						"environment": "memory", "permissions": map[string]any{"contents": "read"},
						"tools": map[string]any{"github": false, "repo-memory": memory},
					})
					require.NoError(t, err)
					dir := t.TempDir()
					path := filepath.Join(dir, "memory-app.md")
					require.NoError(t, os.WriteFile(path, []byte("---\n"+string(source)+"---\nMaintain memory.\n"), 0o600))
					c := NewCompiler(WithLint(true))
					c.gitRoot = dir
					c.SetReadOnly(true)
					require.NoError(t, c.CompileWorkflow(path))
					content, err := os.ReadFile(filepath.Join(dir, "memory-app.lock.yml"))
					require.NoError(t, err)
					var compiled map[string]any
					require.NoError(t, yaml.Unmarshal(content, &compiled))
					jobs := compiled["jobs"].(map[string]any)
					for _, level := range []PermissionLevel{PermissionRead, PermissionWrite} {
						jobName, appID, fallback := "agent", tc.read, tc.readFallback
						if level == PermissionWrite {
							jobName, appID, fallback = "push_repo_memory", tc.write, tc.writeFallback
						}
						if appID == "" {
							appID, fallback = tc.shared, false
						}
						job := jobs[jobName].(map[string]any)
						byID := map[string]map[string]any{}
						for _, raw := range job["steps"].([]any) {
							step := raw.(map[string]any)
							id, _ := step["id"].(string)
							byID[id] = step
						}
						entry := RepoMemoryEntry{ID: "default"}
						tokenID := repoMemoryAppTokenStepID(entry, level)
						if appID == "" || level == PermissionRead && target == "" && tc.read == "" {
							assert.NotContains(t, byID, tokenID)
						} else {
							mint := byID[tokenID]
							require.NotNil(t, mint)
							with := mint["with"].(map[string]any)
							empty := level == PermissionRead && tc.emptyRead || level == PermissionWrite && tc.emptyWrite
							if empty {
								assert.Equal(t, "${{ '' }}", with["client-id"])
								assert.Equal(t, "${{ '' }}", with["private-key"])
							} else {
								assert.Equal(t, "${{ vars."+appID+"_APP_ID }}", with["client-id"])
								assert.Equal(t, "${{ secrets."+appID+"_APP_KEY }}", with["private-key"])
							}
							assert.Equal(t, string(level), with["permission-contents"])
							if fallback {
								assert.Contains(t, mint["if"], "!= ''")
							}
							if strings.Contains(target, "${{") {
								resolver := byID[repoMemoryAppTargetStepID(entry, level)]
								require.NotNil(t, resolver)
								allowed := "${{ github.repository }} other/memory other/" + strings.ToLower(appID) + "-only"
								if empty {
									allowed = "${{ github.repository }}"
								}
								assert.Equal(t, allowed, resolver["env"].(map[string]any)["GH_AW_ALLOWED_REPOSITORIES"])
							}
						}
						if level == PermissionWrite {
							permission, token := "read", "${{ steps."+tokenID+".outputs.token }}"
							if appID == "" {
								permission, token = "write", "${{ github.token }}"
							} else if fallback {
								permission, token = "write", "${{ steps."+tokenID+".outputs.token || github.token }}"
							}
							assert.Equal(t, permission, job["permissions"].(map[string]any)["contents"])
							push := byID["push_repo_memory_default"]
							assert.Equal(t, token, push["env"].(map[string]any)["GH_TOKEN"])
							if appID != "" {
								assert.Equal(t, token, push["with"].(map[string]any)["github-token"])
							}
							assert.Equal(t, "memory", job["environment"])
						} else {
							for _, raw := range job["steps"].([]any) {
								step := raw.(map[string]any)
								if _, ok := step["run"].(string); ok && strings.Contains(step["run"].(string), "clone_repo_memory_branch.sh") {
									token := "${{ github.token }}"
									if appID != "" && (target != "" || tc.read != "") {
										token = "${{ steps." + tokenID + ".outputs.token }}"
										if fallback {
											token = "${{ steps." + tokenID + ".outputs.token || github.token }}"
										}
									}
									assert.Equal(t, token, step["env"].(map[string]any)["GH_TOKEN"])
								}
							}
						}
					}
				})
			}
		}
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
				assert.Contains(t, b.String(), "GH_TOKEN: ${{ steps.repo-memory-app-read-token-default.outputs.token }}")
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
	assert.Contains(t, repoMemoryAppToken(memory, PermissionWrite), "outputs.token || github.token")
	job, err := c.buildPushRepoMemoryJob(&WorkflowData{RepoMemoryConfig: &RepoMemoryConfig{Memories: []RepoMemoryEntry{memory}}}, false)
	require.NoError(t, err)
	assert.Contains(t, job.Permissions, "contents: write")
}

func TestRepoMemoryGitHubAppExpressionTarget(t *testing.T) {
	memory := newDefaultRepoMemoryEntry("test", "memory")
	memory.TargetRepo = "${{ vars.MEMORY_REPO }}"
	memory.GitHubApp = &GitHubAppConfig{AppID: "${{ vars.APP_ID }}", PrivateKey: "${{ secrets.APP_KEY }}"}
	for _, level := range []PermissionLevel{PermissionRead, PermissionWrite} {
		targetID := "repo-memory-app-target-default"
		if level == PermissionRead {
			targetID = "repo-memory-app-read-target-default"
		}
		mint := strings.Join(NewCompiler().buildRepoMemoryAppTokenMintStep(memory, level, pushRepoMemoryJobName), "")
		assert.Contains(t, mint, "GH_AW_TARGET_REPOSITORY: ${{ vars.MEMORY_REPO }}")
		assert.Contains(t, mint, "repositories: ${{ steps."+targetID+".outputs.repository }}")
		assert.Contains(t, mint, "owner: ${{ steps."+targetID+".outputs.owner }}")
		assert.NotContains(t, mint, "repositories: ${{ github.event.repository.name }}")
		assert.Contains(t, mint, `"$GH_AW_TARGET_REPOSITORY" =~`)
		if level == PermissionWrite {
			assert.Equal(t, 2, strings.Count(mint, "always()"))
		}
	}

}

func TestRepoMemoryGitHubAppDynamicTargetAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name, target, owner string
		repositories        []string
		allowed             bool
	}{
		{name: "current repository", target: "current/repo", allowed: true},
		{name: "unapproved repository", target: "other/memory"},
		{name: "approved repository", target: "other/memory", owner: "other", repositories: []string{"memory"}, allowed: true},
		{name: "wrong owner", target: "attacker/memory", owner: "other", repositories: []string{"memory"}},
		{name: "wrong repository", target: "other/unapproved", owner: "other", repositories: []string{"memory"}},
		{name: "current owner", target: "current/memory", repositories: []string{"memory"}, allowed: true},
		{name: "case insensitive", target: "OTHER/Memory", owner: "other", repositories: []string{"memory"}, allowed: true},
		{name: "malformed", target: "other/memory/extra", owner: "other", repositories: []string{"memory"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			memory := newDefaultRepoMemoryEntry("test", "memory")
			memory.TargetRepo = "${{ inputs.memory_repo }}"
			memory.GitHubApp = &GitHubAppConfig{
				AppID: "${{ vars.APP_ID }}", PrivateKey: "${{ secrets.APP_KEY }}", IgnoreIfMissing: true,
				Owner: tc.owner, Repositories: tc.repositories,
			}
			mint := strings.Join(NewCompiler().buildRepoMemoryAppTokenMintStep(memory, PermissionWrite, pushRepoMemoryJobName), "")
			var parsed struct {
				Steps []struct {
					Run string            `yaml:"run"`
					Env map[string]string `yaml:"env"`
					If  string            `yaml:"if"`
				} `yaml:"steps"`
			}
			require.NoError(t, yaml.Unmarshal([]byte("steps:\n"+mint), &parsed))
			require.Len(t, parsed.Steps, 2)
			resolver := parsed.Steps[0]
			allowlist := strings.NewReplacer("${{ github.repository }}", "current/repo", "${{ github.repository_owner }}", "current").Replace(resolver.Env["GH_AW_ALLOWED_REPOSITORIES"])
			outputPath := filepath.Join(t.TempDir(), "output")
			command := exec.Command("bash", "-c", resolver.Run)
			command.Env = append(os.Environ(), "GH_AW_TARGET_REPOSITORY="+tc.target, "GH_AW_ALLOWED_REPOSITORIES="+allowlist, "GITHUB_OUTPUT="+outputPath)
			output, err := command.CombinedOutput()
			if tc.allowed {
				require.NoError(t, err, "%s", output)
				resolved, err := os.ReadFile(outputPath)
				require.NoError(t, err)
				assert.Contains(t, string(resolved), "owner=")
			} else {
				require.Error(t, err)
				_, err = os.Stat(outputPath)
				require.True(t, os.IsNotExist(err), "failed authorization must not emit token scope outputs")
			}
			assert.Contains(t, parsed.Steps[1].If, "steps.repo-memory-app-target-default.outcome == 'success'")
			assert.NotContains(t, mint, "repositories: |-")
			push := NewCompiler().buildSinglePushRepoMemoryStep(&WorkflowData{}, memory, false, false)
			assert.Contains(t, push, "always() && steps.repo-memory-app-target-default.outcome == 'success'")
			assert.Contains(t, push, "TARGET_REPO: ${{ steps.repo-memory-app-target-default.outputs.owner }}/${{ steps.repo-memory-app-target-default.outputs.repository }}")
		})
	}
}

func TestRepoMemoryGitHubAppDynamicAllowlistValidation(t *testing.T) {
	for _, app := range []map[string]any{
		{"owner": "${{ inputs.owner }}", "repositories": []any{"memory"}},
		{"repositories": []any{"*"}},
		{"repositories": []any{"${{ inputs.repository }}"}},
		{"repositories": []any{"other/memory"}},
	} {
		app["client-id"] = "${{ vars.APP_ID }}"
		app["private-key"] = "${{ secrets.APP_KEY }}"
		for _, field := range []string{"github-app", "github-app-read", "github-app-write"} {
			_, err := parseRepoMemoryEntry(map[string]any{"target-repo": "${{ inputs.memory_repo }}", field: app}, "test", "memory", false)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "expression target-repo authorization")
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
