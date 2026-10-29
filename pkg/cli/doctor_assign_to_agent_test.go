//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoctorAssignmentCredential(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		token     string
		secrets   map[string]struct{}
		lookupErr error
		status    string
		secret    string
	}{
		{name: "dedicated PAT", token: "${{ secrets.GH_AW_AGENT_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}", secrets: map[string]struct{}{"GH_AW_AGENT_TOKEN": {}, "GH_AW_GITHUB_TOKEN": {}}, status: "configured", secret: "GH_AW_AGENT_TOKEN"},
		{name: "fallback PAT", token: "${{ secrets.GH_AW_AGENT_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}", secrets: map[string]struct{}{"GH_AW_GITHUB_TOKEN": {}}, status: "configured", secret: "GH_AW_GITHUB_TOKEN"},
		{name: "custom PAT", token: "${{ secrets.CUSTOM_PAT }}", secrets: map[string]struct{}{"CUSTOM_PAT": {}}, status: "configured", secret: "CUSTOM_PAT"},
		{name: "bracket and case", token: "${{ (secrets['custom_pat'] || secrets.GITHUB_TOKEN) }}", secrets: map[string]struct{}{"CUSTOM_PAT": {}}, status: "configured", secret: "CUSTOM_PAT"},
		{name: "explicit missing does not use magic secret", token: "${{ secrets.CUSTOM_PAT }}", secrets: map[string]struct{}{"GH_AW_AGENT_TOKEN": {}}, status: "missing"},
		{name: "automatic token", token: "${{ secrets.GITHUB_TOKEN }}", secrets: map[string]struct{}{"GITHUB_TOKEN": {}}, status: "missing"},
		{name: "github context token", token: "${{ github.token }}", status: "missing"},
		{name: "unreachable fallback", token: "${{ secrets.GITHUB_TOKEN || secrets.CUSTOM_PAT }}", secrets: map[string]struct{}{"CUSTOM_PAT": {}}, status: "missing"},
		{name: "missing PAT", token: "${{ secrets.CUSTOM_PAT }}", status: "missing"},
		{name: "permission denied", token: "${{ secrets.CUSTOM_PAT }}", lookupErr: errors.New("HTTP 403"), status: "unknown"},
		{name: "known repository secret despite unavailable org API", token: "${{ secrets.CUSTOM_PAT }}", secrets: map[string]struct{}{"CUSTOM_PAT": {}}, lookupErr: errors.New("HTTP 403"), status: "configured", secret: "CUSTOM_PAT"},
		{name: "unknown higher priority inherited secret", token: "${{ secrets.FIRST || secrets.SECOND }}", secrets: map[string]struct{}{"SECOND": {}}, lookupErr: errors.New("HTTP 403"), status: "unknown"},
		{name: "dynamic secret", token: "${{ secrets[inputs.token] }}", status: "unknown"},
		{name: "step credential", token: "${{ steps.app.outputs.token }}", status: "unknown"},
		{name: "conditional expression", token: "${{ inputs.use_pat && secrets.CUSTOM_PAT }}", secrets: map[string]struct{}{"CUSTOM_PAT": {}}, status: "unknown"},
		{name: "literal credential", token: "never-echo-this-value", status: "unknown"},
		{name: "literal in expression", token: "${{ 'never-echo-this-value' }}", status: "unknown"},
		{name: "concatenation", token: "${{ secrets.CUSTOM_PAT }}suffix", secrets: map[string]struct{}{"CUSTOM_PAT": {}}, status: "unknown"},
		{name: "multiple expressions", token: "${{ secrets.FIRST }}${{ secrets.SECOND }}", status: "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			status, secret, message := doctorAssignmentCredential(tc.token, tc.secrets, tc.lookupErr)
			assert.Equal(t, tc.status, status)
			assert.Equal(t, tc.secret, secret)
			assert.NotContains(t, message, "never-echo-this-value")
			if tc.status == "configured" {
				assert.Contains(t, message, "cannot be verified")
			}
		})
	}
}

func TestDiscoverDoctorAssignmentWorkflows(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		files   map[string]string
		want    int
		token   string
		wantErr string
	}{
		{name: "no workflow directory"},
		{name: "unrelated workflow", files: map[string]string{"normal.md": "---\non: push\nsafe-outputs:\n  add-comment:\n---\nPrompt"}, want: 0},
		{name: "null shorthand", files: map[string]string{"assign.md": "---\non: issues\nsafe-outputs:\n  assign-to-agent:\n---\nPrompt"}, want: 1},
		{name: "boolean shorthand", files: map[string]string{"assign.md": "---\non: issues\nsafe-outputs:\n  assign-to-agent: true\n---\nPrompt"}, want: 1},
		{name: "custom token", files: map[string]string{"assign.md": "---\non: issues\nsafe-outputs:\n  assign-to-agent:\n    github-token: ${{ secrets.CUSTOM_PAT }}\n---\nPrompt"}, want: 1, token: "${{ secrets.CUSTOM_PAT }}"},
		{name: "unused shared fragment", files: map[string]string{"shared.md": "---\nsafe-outputs:\n  assign-to-agent:\n---\nPrompt"}, want: 0},
		{name: "imported assignment", files: map[string]string{"assign.md": "---\non: issues\nimports:\n  - shared/assign.md\n---\nPrompt", "shared/assign.md": "---\nsafe-outputs:\n  assign-to-agent:\n    github-token: ${{ secrets.IMPORTED_PAT }}\n---\nPrompt"}, want: 1, token: "${{ secrets.IMPORTED_PAT }}"},
		{name: "local override", files: map[string]string{"assign.md": "---\non: issues\nimports:\n  - shared/assign.md\nsafe-outputs:\n  assign-to-agent:\n    github-token: ${{ secrets.LOCAL_PAT }}\n---\nPrompt", "shared/assign.md": "---\nsafe-outputs:\n  assign-to-agent:\n    github-token: ${{ secrets.IMPORTED_PAT }}\n---\nPrompt"}, want: 1, token: "${{ secrets.LOCAL_PAT }}"},
		{name: "README ignored", files: map[string]string{"README.md": "---\non: issues\nsafe-outputs:\n  assign-to-agent:\n---\nPrompt"}, want: 0},
		{name: "invalid YAML", files: map[string]string{"broken.md": "---\non: [\n---\nPrompt"}, wantErr: "failed to parse"},
		{name: "missing import", files: map[string]string{"assign.md": "---\non: issues\nimports:\n  - shared/missing.md\n---\nPrompt"}, wantErr: "failed to resolve imports"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			for name, content := range tc.files {
				path := filepath.Join(root, ".github", "workflows", name)
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
				require.NoError(t, os.WriteFile(path, []byte(content), 0600))
			}
			checks, err := discoverDoctorAssignmentWorkflows(root)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Len(t, checks, tc.want)
			if tc.want > 0 {
				assert.Equal(t, ".github/workflows/assign.md", checks[0].path)
				assert.Equal(t, tc.token, checks[0].config.AssignToAgent.GitHubToken)
			}
		})
	}
}

func TestDiagnoseDoctorAssignments(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		app        bool
		handlerApp bool
		secrets    map[string]struct{}
		status     string
	}{
		{name: "missing PAT", status: "missing"},
		{name: "global App alone is insufficient", app: true, status: "missing"},
		{name: "global App with dedicated PAT", app: true, secrets: map[string]struct{}{"GH_AW_AGENT_TOKEN": {}}, status: "configured"},
		{name: "handler App overrides PAT", handlerApp: true, secrets: map[string]struct{}{"GH_AW_AGENT_TOKEN": {}}, status: "missing"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			config := &workflow.SafeOutputsConfig{AssignToAgent: &workflow.AssignToAgentConfig{}}
			if tc.app {
				config.GitHubApp = &workflow.GitHubAppConfig{}
			}
			if tc.handlerApp {
				config.AssignToAgent.GitHubApp = &workflow.GitHubAppConfig{}
			}
			calls := 0
			checks := diagnoseDoctorAssignments(context.Background(), "octo/repo", "https://ghe.example.com", []doctorAssignmentWorkflow{{path: "assign.md", config: config}, {path: "other.md", config: config}},
				func(_ context.Context, repo, host string) (map[string]struct{}, error) {
					calls++
					assert.Equal(t, "octo/repo", repo)
					assert.Equal(t, "https://ghe.example.com", host)
					return tc.secrets, nil
				})
			assert.Equal(t, 1, calls, "secret metadata is fetched once for all workflows")
			require.Len(t, checks, 2)
			assert.Equal(t, tc.status, checks[0].Status)
			assert.Equal(t, "https://ghe.example.com/octo/repo/settings/secrets/actions/new", checks[0].SecretsURL)
			patURL, err := url.Parse(checks[0].PATURL)
			require.NoError(t, err)
			assert.Equal(t, "ghe.example.com", patURL.Host)
			assert.Equal(t, "/settings/personal-access-tokens/new", patURL.Path)
			assert.Equal(t, "octo", patURL.Query().Get("target_name"))
			assert.Equal(t, "GH_AW_AGENT_TOKEN", patURL.Query().Get("name"))
			for _, permission := range []string{"actions", "contents", "issues", "pull_requests"} {
				assert.Equal(t, "write", patURL.Query().Get(permission))
			}
			if tc.status == "missing" {
				assert.Contains(t, checks[0].Message, "GitHub App installation tokens")
				assert.Error(t, doctorAgentAssignmentError(checks))
			} else {
				assert.NoError(t, doctorAgentAssignmentError(checks))
			}
		})
	}
}

func TestListDoctorAssignmentSecrets(t *testing.T) {
	for _, tc := range []struct {
		name         string
		repoResponse string
		orgResponse  string
		orgFailure   bool
		wantErr      string
	}{
		{name: "repository and inherited secrets", repoResponse: `[{"secrets":[{"name":"REPO_PAT"}]},{"secrets":[{"name":"SECOND_PAT"}]}]`, orgResponse: `[{"secrets":[{"name":"ORG_PAT"}]}]`},
		{name: "empty", repoResponse: `[{"secrets":[]}]`, orgResponse: `[{"secrets":[]}]`},
		{name: "permission denied preserves repository names", repoResponse: `[{"secrets":[{"name":"REPO_PAT"}]}]`, orgFailure: true, wantErr: "cannot inspect Actions organization-secrets"},
		{name: "malformed response", repoResponse: `not JSON`, wantErr: "failed to parse"},
		{name: "missing secrets array", repoResponse: `[{}]`, wantErr: "missing the secrets array"},
		{name: "empty pages", repoResponse: `[]`, wantErr: "no pages"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			script := `#!/bin/sh
case "$*" in
  *'--hostname ghe.example.com'*'--paginate --slurp repos/octo/repo/actions/organization-secrets?per_page=100'*)
    if [ "$DOCTOR_ORG_FAILURE" = "true" ]; then exit 1; fi
    printf '%s' "$DOCTOR_ORG_RESPONSE" ;;
  *'--hostname ghe.example.com'*'--paginate --slurp repos/octo/repo/actions/secrets?per_page=100'*)
    printf '%s' "$DOCTOR_REPO_RESPONSE" ;;
  *) echo "unexpected gh arguments" >&2; exit 2 ;;
esac
`
			require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0755))
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("DOCTOR_REPO_RESPONSE", tc.repoResponse)
			t.Setenv("DOCTOR_ORG_RESPONSE", tc.orgResponse)
			t.Setenv("DOCTOR_ORG_FAILURE", "false")
			if tc.orgFailure {
				t.Setenv("DOCTOR_ORG_FAILURE", "true")
			}
			secrets, err := listDoctorAssignmentSecrets(context.Background(), "octo/repo", "https://ghe.example.com")
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			if strings.Contains(tc.repoResponse, "REPO_PAT") {
				assert.Contains(t, secrets, "REPO_PAT")
			}
			if tc.name == "repository and inherited secrets" {
				assert.Contains(t, secrets, "SECOND_PAT")
				assert.Contains(t, secrets, "ORG_PAT")
			}
		})
	}
}

func TestDoctorAssignmentsJSONAndExitStatus(t *testing.T) {
	for _, status := range []string{"configured", "missing", "unknown"} {
		t.Run(status, func(t *testing.T) {
			var runErr error
			output := captureSetupStdout(t, func() error {
				runErr = runSetupAuthWithRuntime(SetupAuthOptions{JSON: true, CheckAgentAssignment: true}, setupRepositoryRuntime{
					checkAuth: func(context.Context) error { return nil },
					checkAgentAssignments: func(_ context.Context, repo, dir string) ([]DoctorAgentAssignmentCheck, error) {
						assert.Empty(t, repo)
						assert.Equal(t, ".", dir)
						return []DoctorAgentAssignmentCheck{{Workflow: "assign.md", Status: status, PATURL: "https://github.com/settings/personal-access-tokens/new"}}, nil
					},
				})
				return nil
			})
			var result SetupAuthResult
			require.NoError(t, json.Unmarshal([]byte(output), &result))
			require.Len(t, result.AgentAssignments, 1)
			assert.Equal(t, status, result.AgentAssignments[0].Status)
			assert.True(t, result.Authenticated)
			if status == "configured" {
				assert.NoError(t, runErr)
			} else {
				assert.Error(t, runErr)
			}
		})
	}
}

func TestDoctorAssignmentsHumanOutput(t *testing.T) {
	stdout, stderr := captureOutput(t, func() error {
		err := printDoctorAgentAssignments([]DoctorAgentAssignmentCheck{{
			Workflow: "assign.md", Status: "missing", Message: "PAT missing",
			PATURL:     "https://github.com/settings/personal-access-tokens/new",
			SecretsURL: "https://github.com/octo/repo/settings/secrets/actions/new",
		}})
		assert.Error(t, err)
		return nil
	})
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "assign.md")
	assert.Contains(t, stderr, "Create PAT: https://github.com/settings/personal-access-tokens/new")
	assert.Contains(t, stderr, "Add repository secret: https://github.com/octo/repo/settings/secrets/actions/new")
}

func TestCheckDoctorAssignmentsOutsideCheckout(t *testing.T) {
	t.Parallel()
	checks, err := checkDoctorAgentAssignments(context.Background(), "", t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, checks)
}

func TestCheckDoctorAssignmentsCheckout(t *testing.T) {
	for _, tc := range []struct {
		name   string
		remote string
		host   string
	}{
		{name: "HTTPS", remote: "https://github.com/octo/repo.git", host: "github.com"},
		{name: "SSH", remote: "git@github.com:octo/repo.git", host: "github.com"},
		{name: "enterprise HTTPS", remote: "https://ghe.example.com/octo/repo.git", host: "ghe.example.com"},
		{name: "enterprise SSH", remote: "ssh://git@ghe.example.com/octo/repo.git", host: "ghe.example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, initTestGitRepo(root))
			require.NoError(t, addOriginRemoteToTestRepo(root, tc.remote))
			dir := filepath.Join(root, ".github", "workflows")
			require.NoError(t, os.MkdirAll(dir, 0755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "assign.md"), []byte("---\non: issues\nsafe-outputs:\n  assign-to-agent:\n---\nPrompt"), 0600))
			bin := t.TempDir()
			script := `#!/bin/sh
printf '%s\n' "$*" >> "$DOCTOR_CALLS"
case "$*" in
  *'/runs?'*) printf '%s' '{"workflow_runs":[]}' ;;
  *) printf '%s' '[{"secrets":[]}]' ;;
esac
`
			require.NoError(t, os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0755))
			callsFile := filepath.Join(bin, "calls")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("DOCTOR_CALLS", callsFile)
			for _, name := range []string{"GH_HOST", "GITHUB_HOST", "GITHUB_ENTERPRISE_HOST", "GITHUB_SERVER_URL", "GH_AW_WORKFLOWS_DIR"} {
				t.Setenv(name, "")
			}
			// A subdirectory still diagnoses the root checkout, not the current directory.
			checks, err := checkDoctorAgentAssignments(context.Background(), "", dir)
			require.NoError(t, err)
			require.Len(t, checks, 1)
			assert.Equal(t, "missing", checks[0].Status)
			assert.Equal(t, "https://"+tc.host+"/octo/repo/settings/secrets/actions/new", checks[0].SecretsURL)
			calls, err := os.ReadFile(callsFile)
			require.NoError(t, err)
			assert.Contains(t, string(calls), "--hostname "+tc.host)
			assert.Contains(t, string(calls), "repos/octo/repo/actions/secrets?per_page=100")
			assert.Contains(t, string(calls), "repos/octo/repo/actions/organization-secrets?per_page=100")
			assert.Contains(t, string(calls), "actions/workflows/assign.lock.yml/runs?")
			assert.Len(t, strings.Split(strings.TrimSpace(string(calls)), "\n"), 3)
		})
	}
}

func TestDoctorAssignmentsRepositoryJSON(t *testing.T) {
	root := initBootstrapGitRepo(t)
	var runErr error
	output := captureSetupStdout(t, func() error {
		runErr = runSetupRepositoryCheckWithRuntime(SetupRepositoryCheckOptions{
			Repo: "octo/repo", Dir: root, RequireOwnerType: "any", JSON: true, CheckAgentAssignment: true,
		}, setupRepositoryRuntime{
			checkAuth:          func(context.Context) error { return nil },
			ownerType:          func(context.Context, string) (string, error) { return "User", nil },
			repoExists:         func(context.Context, string) (bool, error) { return true, nil },
			dirOriginRepo:      func(string) (string, error) { return "octo/repo", nil },
			checkCleanWorktree: func(bool) error { return nil },
			checkAgentAssignments: func(_ context.Context, repo, dir string) ([]DoctorAgentAssignmentCheck, error) {
				assert.Equal(t, "octo/repo", repo)
				assert.Equal(t, root, dir)
				return []DoctorAgentAssignmentCheck{{Workflow: "assign.md", Status: "missing"}}, nil
			},
		})
		return nil
	})
	require.Error(t, runErr)
	var result SetupRepositoryCheckResult
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.Equal(t, "octo/repo", result.Repository)
	assert.True(t, result.CheckoutAttached)
	require.Len(t, result.AgentAssignments, 1)
	assert.Equal(t, "missing", result.AgentAssignments[0].Status)
}

func TestDoctorAssignmentsCustomSecretRemedy(t *testing.T) {
	t.Parallel()
	config := &workflow.SafeOutputsConfig{AssignToAgent: &workflow.AssignToAgentConfig{
		BaseSafeOutputConfig: workflow.BaseSafeOutputConfig{GitHubToken: "${{ secrets.CUSTOM_PAT }}"},
	}}
	checks := diagnoseDoctorAssignments(context.Background(), "octo/repo", "https://github.com", []doctorAssignmentWorkflow{{path: "assign.md", config: config}},
		func(context.Context, string, string) (map[string]struct{}, error) { return nil, nil })
	require.Len(t, checks, 1)
	assert.Equal(t, "CUSTOM_PAT", checks[0].SuggestedSecret)
	assert.Contains(t, checks[0].Message, "Store it as the Actions secret CUSTOM_PAT")
	patURL, err := url.Parse(checks[0].PATURL)
	require.NoError(t, err)
	assert.Equal(t, "CUSTOM_PAT", patURL.Query().Get("name"))
}
