//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGHAWManifest(t *testing.T) {
	tests := []struct {
		name                string
		secretNames         []string
		actionRefs          []string
		resolutionFailures  []GHAWManifestResolutionFailure
		containers          []GHAWManifestContainer
		redirect            string
		skillSpecs          []string
		onField             any
		wantVersion         int
		wantSecrets         []string
		wantActionRepos     []string
		wantFailures        []GHAWManifestResolutionFailure
		wantContainerImages []string
		wantRedirect        string
		wantSkills          []string
		wantHasPR           bool
		wantHasPRTarget     bool
	}{
		{
			name:        "empty inputs",
			secretNames: nil,
			actionRefs:  nil,
			wantVersion: 1,
			wantSecrets: []string{},
		},
		{
			name:        "secrets prefix stripped to plain name",
			secretNames: []string{"GITHUB_TOKEN", "MY_SECRET"},
			wantVersion: 1,
			wantSecrets: []string{"GITHUB_TOKEN", "MY_SECRET"},
		},
		{
			name:        "secrets.NAME prefix is stripped on input",
			secretNames: []string{"secrets.GITHUB_TOKEN", "GITHUB_TOKEN"},
			wantVersion: 1,
			wantSecrets: []string{"GITHUB_TOKEN"},
		},
		{
			name:        "secrets are sorted and deduplicated",
			secretNames: []string{"Z_SECRET", "A_SECRET", "Z_SECRET"},
			wantVersion: 1,
			wantSecrets: []string{"A_SECRET", "Z_SECRET"},
		},
		{
			name: "action refs with SHA and comment",
			actionRefs: []string{
				"actions/checkout@abc1234def5678 # v4",
				"docker://alpine:3.14", // no @ separator; skipped
			},
			wantVersion:     1,
			wantSecrets:     []string{},
			wantActionRepos: []string{"actions/checkout"},
		},
		{
			name: "action refs without comment use sha as version",
			actionRefs: []string{
				"actions/checkout@v4",
			},
			wantVersion:     1,
			wantSecrets:     []string{},
			wantActionRepos: []string{"actions/checkout"},
		},
		{
			name: "duplicate action refs are deduplicated",
			actionRefs: []string{
				"actions/checkout@abc123 # v4",
				"actions/checkout@abc123 # v4",
			},
			wantVersion:     1,
			wantSecrets:     []string{},
			wantActionRepos: []string{"actions/checkout"},
		},
		{
			name: "resolution failures are normalized, deduplicated, and sorted",
			resolutionFailures: []GHAWManifestResolutionFailure{
				{Repo: "actions/setup-node", Ref: "v6", ErrorType: "dynamic_resolution_failed"},
				{Repo: "actions/setup-node", Ref: "v6", ErrorType: "dynamic_resolution_failed"},
				{Repo: "actions/setup-node", Ref: "v6", ErrorType: "pin_not_found"},
				{Repo: "actions/checkout", Ref: "v5", ErrorType: "pin_not_found"},
			},
			wantVersion: 1,
			wantSecrets: []string{},
			wantFailures: []GHAWManifestResolutionFailure{
				{Repo: "actions/checkout", Ref: "v5", ErrorType: "pin_not_found"},
				{Repo: "actions/setup-node", Ref: "v6", ErrorType: "dynamic_resolution_failed"},
				{Repo: "actions/setup-node", Ref: "v6", ErrorType: "pin_not_found"},
			},
		},
		{
			name: "containers are sorted and deduplicated",
			containers: []GHAWManifestContainer{
				{Image: "node:lts-alpine"},
				{Image: "alpine:3.14"},
				{Image: "node:lts-alpine"}, // duplicate
			},
			wantVersion:         1,
			wantSecrets:         []string{},
			wantContainerImages: []string{"alpine:3.14", "node:lts-alpine"},
		},
		{
			name: "container with digest retained",
			containers: []GHAWManifestContainer{
				{
					Image:       "node:lts-alpine",
					Digest:      "sha256:abc123",
					PinnedImage: "node:lts-alpine@sha256:abc123",
				},
			},
			wantVersion:         1,
			wantSecrets:         []string{},
			wantContainerImages: []string{"node:lts-alpine"},
		},
		{
			name:                "nil containers produces empty containers field",
			containers:          nil,
			redirect:            "",
			wantVersion:         1,
			wantSecrets:         []string{},
			wantContainerImages: []string{},
			wantRedirect:        "",
		},
		{
			name:         "redirect is included when configured",
			redirect:     "owner/repo/workflows/new.md@main",
			wantVersion:  1,
			wantSecrets:  []string{},
			wantRedirect: "owner/repo/workflows/new.md@main",
		},
		{
			name: "skills are sorted and deduplicated",
			skillSpecs: []string{
				"githubnext/skills/review/security@1f181b37d3fe5862ab590648f25a292e345b5de6",
				"githubnext/skills@1f181b37d3fe5862ab590648f25a292e345b5de6",
				"githubnext/skills@1f181b37d3fe5862ab590648f25a292e345b5de6", // duplicate
			},
			wantVersion: 1,
			wantSecrets: []string{},
			wantSkills: []string{
				"githubnext/skills/review/security@1f181b37d3fe5862ab590648f25a292e345b5de6",
				"githubnext/skills@1f181b37d3fe5862ab590648f25a292e345b5de6",
			},
		},
		{
			name:        "nil skills produces nil skills field",
			skillSpecs:  nil,
			wantVersion: 1,
			wantSecrets: []string{},
			wantSkills:  nil,
		},
		{
			name:            "detect pull_request from on string",
			onField:         "pull_request",
			wantVersion:     1,
			wantSecrets:     []string{},
			wantHasPR:       true,
			wantHasPRTarget: false,
		},
		{
			name:            "detect pull_request_target from on map",
			onField:         map[string]any{"pull_request_target": map[string]any{"types": []any{"opened"}}},
			wantVersion:     1,
			wantSecrets:     []string{},
			wantHasPRTarget: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewGHAWManifest(tt.secretNames, tt.actionRefs, tt.resolutionFailures, tt.containers, tt.redirect, tt.skillSpecs, nil, tt.onField)
			require.NotNil(t, m, "manifest should not be nil")
			assert.Equal(t, tt.wantVersion, m.Version, "manifest version")
			if tt.wantSecrets != nil {
				assert.Equal(t, tt.wantSecrets, m.Secrets, "manifest secrets")
			}
			if tt.wantActionRepos != nil {
				repos := make([]string, len(m.Actions))
				for i, a := range m.Actions {
					repos[i] = a.Repo
				}
				assert.Equal(t, tt.wantActionRepos, repos, "action repos")
			}
			if tt.wantContainerImages != nil {
				images := make([]string, len(m.Containers))
				for i, c := range m.Containers {
					images[i] = c.Image
				}
				assert.Equal(t, tt.wantContainerImages, images, "container images")
			}
			if tt.wantFailures != nil {
				assert.Equal(t, tt.wantFailures, m.ResolutionFailures, "resolution failures")
			}
			assert.Equal(t, tt.wantRedirect, m.Redirect, "manifest redirect")
			assert.Equal(t, tt.wantSkills, m.Skills, "manifest skills")
			assert.Equal(t, tt.wantHasPR, m.HasPullRequest, "manifest pull_request trigger")
			assert.Equal(t, tt.wantHasPRTarget, m.HasPullRequestTarget, "manifest pull_request_target trigger")
		})
	}
}

func TestCollectMemoryValidationScripts(t *testing.T) {
	data := &WorkflowData{
		RepoMemoryConfig: &RepoMemoryConfig{Memories: []RepoMemoryEntry{
			{ID: "repo", Validation: &MemoryValidationConfig{Script: "repo validation"}},
		}},
		CacheMemoryConfig: &CacheMemoryConfig{Caches: []CacheMemoryEntry{
			{ID: "cache", Validation: &MemoryValidationConfig{Script: "cache validation"}},
			{ID: "unvalidated"},
		}},
		DriveMemoryConfig: &DriveMemoryConfig{Drives: []DriveMemoryEntry{
			{ID: "drive", Validation: &MemoryValidationConfig{Script: "drive validation"}},
		}},
	}

	scripts := collectMemoryValidationScripts(data)

	require.Len(t, scripts, 3)
	assert.Equal(t, "cache-memory:cache", scripts[0].Memory)
	assert.Equal(t, "drive-memory:drive", scripts[1].Memory)
	assert.Equal(t, "repo-memory:repo", scripts[2].Memory)
	assert.Len(t, scripts[0].SHA256, 64)
	assert.Len(t, scripts[1].SHA256, 64)
	assert.Len(t, scripts[2].SHA256, 64)
	assert.NotEqual(t, scripts[0].SHA256, scripts[1].SHA256)
	assert.NotEqual(t, scripts[1].SHA256, scripts[2].SHA256)
}

func TestCollectMCPServersForManifest(t *testing.T) {
	data := &WorkflowData{
		Tools: map[string]any{
			"github": map[string]any{
				"allowed": []any{"list_issues", "get_issue"},
			},
			"my-api": map[string]any{
				"type":    "http",
				"url":     "https://api.example.test/mcp",
				"allowed": []any{"fetch_data", "list_items"},
			},
			"all-tools": map[string]any{
				"command": "npx example-mcp",
			},
			"dispatch_workflow": map[string]any{
				"type": "http",
				"url":  "https://example.test/mcp",
			},
			"cache-memory": true,
		},
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{},
			NoOp:         &NoOpConfig{},
			Scripts: map[string]*SafeScriptConfig{
				"triage-script": {},
			},
		},
		MCPScripts: &MCPScriptsConfig{Tools: map[string]*MCPScriptToolConfig{
			"lookup": {Name: "lookup"},
		}},
	}

	servers := collectMCPServersForManifest(data)

	assert.Equal(t, []GHAWManifestMCPServer{
		{Name: "all-tools", Tools: []string{"*"}},
		{Name: "github", Tools: []string{"get_issue", "list_issues"}},
		{Name: "mcpscripts", Tools: []string{"lookup"}},
		{Name: "my-api", Tools: []string{"fetch_data", "list_items"}},
		{Name: "safeoutputs", Tools: []string{"create_issue", "noop", "triage_script"}},
	}, servers)
}

func TestCollectMCPServersForManifestGitHubToolsets(t *testing.T) {
	servers := collectMCPServersForManifest(&WorkflowData{
		Tools: map[string]any{
			"github": map[string]any{
				"toolsets": []any{"repos", "issues"},
			},
		},
	})

	require.Len(t, servers, 1)
	assert.Equal(t, "github", servers[0].Name)
	assert.Contains(t, servers[0].Tools, "list_issues")
	assert.NotContains(t, servers[0].Tools, "actions_list")
	assert.NotContains(t, servers[0].Tools, "get_me")
}

func TestCollectMCPServersForManifestGitHubGHProxy(t *testing.T) {
	servers := collectMCPServersForManifest(&WorkflowData{
		Tools: map[string]any{
			"github": map[string]any{
				"mode": "gh-proxy",
			},
		},
	})

	assert.Empty(t, servers)
}

func TestStringsFromAnySlice(t *testing.T) {
	assert.Equal(t, []string{"first", "second"}, stringsFromAnySlice([]any{"first", "second"}))
	assert.Equal(t, []string{"first", "second"}, stringsFromAnySlice([]string{"first", "second"}))
	assert.Equal(t, []string{"tool"}, stringsFromAnySlice("tool"))
	assert.Equal(t, []string{"*"}, stringsFromAnySlice(""))
	assert.Equal(t, []string{"*"}, stringsFromAnySlice(42))
}

func TestGHAWManifestMCPServersAlwaysSerialized(t *testing.T) {
	json, err := (&GHAWManifest{
		Version:    1,
		Secrets:    []string{},
		Actions:    []GHAWManifestAction{},
		MCPServers: collectMCPServersForManifest(&WorkflowData{}),
	}).ToJSON()

	require.NoError(t, err)
	assert.Contains(t, json, `"mcp_servers":[]`)
}

func TestNewGHAWManifestContainerDigest(t *testing.T) {
	containers := []GHAWManifestContainer{
		{
			Image:       "node:lts-alpine",
			Digest:      "sha256:abc123",
			PinnedImage: "node:lts-alpine@sha256:abc123",
		},
		{
			Image: "alpine:3.14", // no digest
		},
	}
	m := NewGHAWManifest(nil, nil, nil, containers, "", nil, nil, nil)
	require.Len(t, m.Containers, 2, "should have two containers")

	// Sorted: alpine before node
	assert.Equal(t, "alpine:3.14", m.Containers[0].Image, "first container image")
	assert.Empty(t, m.Containers[0].Digest, "alpine digest should be empty")
	assert.Empty(t, m.Containers[0].PinnedImage, "alpine pinned_image should be empty")

	assert.Equal(t, "node:lts-alpine", m.Containers[1].Image, "second container image")
	assert.Equal(t, "sha256:abc123", m.Containers[1].Digest, "node digest")
	assert.Equal(t, "node:lts-alpine@sha256:abc123", m.Containers[1].PinnedImage, "node pinned_image")

	// JSON serialization: digest fields present only when non-empty (omitempty)
	jsonStr, err := m.ToJSON()
	require.NoError(t, err, "ToJSON should not fail")
	assert.Contains(t, jsonStr, `"containers"`, "containers key in JSON")
	assert.Contains(t, jsonStr, `"node:lts-alpine"`, "node image in JSON")
	assert.Contains(t, jsonStr, `"sha256:abc123"`, "node digest in JSON")
	assert.Contains(t, jsonStr, `"node:lts-alpine@sha256:abc123"`, "pinned_image in JSON")
	// alpine has no digest/pinned_image — omitempty must suppress them
	assert.NotContains(t, jsonStr, `"digest":""`, "empty digest must be omitted")
	assert.NotContains(t, jsonStr, `"pinned_image":""`, "empty pinned_image must be omitted")
}

func TestGHAWManifestToJSON(t *testing.T) {
	m := &GHAWManifest{
		Version: 1,
		Secrets: []string{"GITHUB_TOKEN", "MY_SECRET"},
		Actions: []GHAWManifestAction{
			{Repo: "actions/checkout", SHA: "abc123", Version: "v4"},
		},
		ResolutionFailures: []GHAWManifestResolutionFailure{
			{Repo: "actions/setup-node", Ref: "v6", ErrorType: "dynamic_resolution_failed"},
		},
		Redirect: "owner/repo/workflows/new.md@main",
	}

	json, err := m.ToJSON()
	require.NoError(t, err, "ToJSON should not fail")
	assert.Contains(t, json, `"version":1`, "version in JSON")
	assert.Contains(t, json, `"GITHUB_TOKEN"`, "GITHUB_TOKEN in JSON")
	assert.Contains(t, json, `"MY_SECRET"`, "MY_SECRET in JSON")
	assert.Contains(t, json, `"actions/checkout"`, "action repo in JSON")
	assert.Contains(t, json, `"abc123"`, "action SHA in JSON")
	assert.Contains(t, json, `"v4"`, "action version in JSON")
	assert.Contains(t, json, `"resolution_failures"`, "resolution failures in JSON")
	assert.Contains(t, json, `"dynamic_resolution_failed"`, "error type in JSON")
	assert.Contains(t, json, `"redirect":"owner/repo/workflows/new.md@main"`, "redirect in JSON")
}

func TestExtractGHAWManifestFromLockFile(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		wantNil      bool
		wantErr      bool
		wantVersion  int
		wantSecrets  []string
		wantRedirect string
	}{
		{
			name:    "no manifest line returns nil",
			content: "# gh-aw-metadata: {}\nsome: yaml",
			wantNil: true,
		},
		{
			name:        "manifest extracted successfully",
			content:     `# gh-aw-manifest: {"version":1,"secrets":["GITHUB_TOKEN"],"actions":[]}`,
			wantVersion: 1,
			wantSecrets: []string{"GITHUB_TOKEN"},
		},
		{
			name:        "manifest with leading spaces in comment",
			content:     `#  gh-aw-manifest: {"version":1,"secrets":[],"actions":[]}`,
			wantVersion: 1,
			wantSecrets: []string{},
		},
		{
			name:    "invalid JSON returns error",
			content: "# gh-aw-manifest: {invalid json}",
			wantErr: true,
		},
		{
			name: "manifest embedded in multi-line header",
			content: `# gh-aw-metadata: {"schema_version":"v3","frontmatter_hash":"abc"}
# gh-aw-manifest: {"version":1,"secrets":["FOO"],"actions":[]}
name: my-workflow`,
			wantVersion: 1,
			wantSecrets: []string{"FOO"},
		},
		{
			name:         "manifest with redirect field",
			content:      `# gh-aw-manifest: {"version":1,"secrets":[],"actions":[],"redirect":"owner/repo/workflows/new.md@main"}`,
			wantVersion:  1,
			wantSecrets:  []string{},
			wantRedirect: "owner/repo/workflows/new.md@main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := ExtractGHAWManifestFromLockFile(tt.content)
			if tt.wantErr {
				assert.Error(t, err, "expected error")
				return
			}
			require.NoError(t, err, "unexpected error")
			if tt.wantNil {
				assert.Nil(t, m, "expected nil manifest")
				return
			}
			require.NotNil(t, m, "manifest should not be nil")
			assert.Equal(t, tt.wantVersion, m.Version, "manifest version")
			assert.Equal(t, tt.wantSecrets, m.Secrets, "manifest secrets")
			assert.Equal(t, tt.wantRedirect, m.Redirect, "manifest redirect")
		})
	}
}

func TestNormalizeSecretName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"GITHUB_TOKEN", "GITHUB_TOKEN"},
		{"secrets.GITHUB_TOKEN", "GITHUB_TOKEN"},
		{"MY_SECRET", "MY_SECRET"},
		{"secrets.MY_SECRET", "MY_SECRET"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeSecretName(tt.input), "normalized secret name")
		})
	}
}

func TestParseActionRefs(t *testing.T) {
	tests := []struct {
		name    string
		refs    []string
		wantLen int
		check   func(t *testing.T, actions []GHAWManifestAction)
	}{
		{
			name:    "empty refs",
			refs:    nil,
			wantLen: 0,
		},
		{
			name: "ref with SHA and version comment",
			refs: []string{"actions/checkout@abc1234 # v4"},
			check: func(t *testing.T, actions []GHAWManifestAction) {
				require.Len(t, actions, 1, "expected 1 action")
				assert.Equal(t, "actions/checkout", actions[0].Repo, "repo")
				assert.Equal(t, "abc1234", actions[0].SHA, "sha")
				assert.Equal(t, "v4", actions[0].Version, "version")
			},
		},
		{
			name: "ref without comment uses sha as version",
			refs: []string{"actions/checkout@v4"},
			check: func(t *testing.T, actions []GHAWManifestAction) {
				require.Len(t, actions, 1, "expected 1 action")
				assert.Equal(t, "actions/checkout", actions[0].Repo, "repo")
				assert.Equal(t, "v4", actions[0].SHA, "sha")
				assert.Equal(t, "v4", actions[0].Version, "version (same as sha when no comment)")
			},
		},
		{
			name: "ref without @ is skipped",
			refs: []string{"actions/checkout"},
			check: func(t *testing.T, actions []GHAWManifestAction) {
				assert.Empty(t, actions, "action without @ should be skipped")
			},
		},
		{
			name: "duplicate refs deduplicated",
			refs: []string{
				"actions/checkout@abc123 # v4",
				"actions/checkout@abc123 # v4",
			},
			check: func(t *testing.T, actions []GHAWManifestAction) {
				assert.Len(t, actions, 1, "duplicates should be removed")
			},
		},
		{
			name: "actions sorted by repo then sha",
			refs: []string{
				"z-org/z-action@sha2",
				"a-org/a-action@sha1",
			},
			check: func(t *testing.T, actions []GHAWManifestAction) {
				require.Len(t, actions, 2, "expected 2 actions")
				assert.Equal(t, "a-org/a-action", actions[0].Repo, "first action repo")
				assert.Equal(t, "z-org/z-action", actions[1].Repo, "second action repo")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actions := parseActionRefs(tt.refs)
			if tt.wantLen > 0 {
				assert.Len(t, actions, tt.wantLen, "action count")
			}
			if tt.check != nil {
				tt.check(t, actions)
			}
		})
	}
}

func TestNewGHAWManifestPlugins(t *testing.T) {
	m := NewGHAWManifest(nil, nil, nil, nil, "", nil, []string{
		"octo-org/agent-plugin@" + strings.Repeat("a", 40),
		"octo-org/agent-plugin@" + strings.Repeat("a", 40), // duplicate, deduplicated
		"octo-org/other-plugin@" + strings.Repeat("b", 40),
	}, nil)
	require.NotNil(t, m, "manifest should not be nil")
	assert.Equal(t, []string{
		"octo-org/agent-plugin@" + strings.Repeat("a", 40),
		"octo-org/other-plugin@" + strings.Repeat("b", 40),
	}, m.Plugins, "manifest plugins should be deduplicated and sorted")
}

func TestNewGHAWManifestPluginsEmpty(t *testing.T) {
	m := NewGHAWManifest(nil, nil, nil, nil, "", nil, nil, nil)
	require.NotNil(t, m, "manifest should not be nil")
	assert.Nil(t, m.Plugins, "manifest plugins should be nil when no plugins are provided")
}
