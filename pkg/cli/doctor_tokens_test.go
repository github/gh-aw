//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoctorTokenASTDiscovery(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, content string
		want          []string
		wantErr       bool
	}{
		{"custom handler fallbacks", "safe-outputs:\n  add-comment:\n    github-token: ${{ secrets.MAINTENANCE || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}", []string{"GH_AW_GITHUB_TOKEN", "MAINTENANCE"}, false},
		{"head github token", "head-github-token: ${{ secrets.FORK_CREDENTIAL }}", []string{"FORK_CREDENTIAL"}, false},
		{"env custom token", "env:\n  GH_TOKEN: ${{ secrets['CUSTOM_CREDENTIAL'] }}", []string{"CUSTOM_CREDENTIAL"}, false},
		{"named token in header", "headers:\n  Authorization: Bearer ${{ secrets.CUSTOM_TOKEN }}", []string{"CUSTOM_TOKEN"}, false},
		{"case insensitive", "env:\n  GH_TOKEN: ${{ SECRETS['custom_credential'] }}", []string{"CUSTOM_CREDENTIAL"}, false},
		{"function references", "github-token: ${{ format('{0}', secrets.CUSTOM) }}", []string{"CUSTOM"}, false},
		{"quoted expression text", "github-token: ${{ 'secrets.NOT_A_TOKEN' }}", nil, false},
		{"YAML comments", "# github-token: ${{ secrets.NOT_A_TOKEN }}\nname: Example", nil, false},
		{"unrelated secrets", "env:\n  API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}", nil, false},
		{"Actions token only", "github-token: ${{ github.token || secrets.GITHUB_TOKEN }}", nil, false},
		{"dynamic reference", "github-token: ${{ secrets[inputs.name] }}", nil, false},
		{"invalid expression", "github-token: ${{ secrets.PRIVATE_TOKEN + }}", nil, true},
		{"implicit assignment", "safe-outputs:\n  assign-to-agent:\n    max: 3", []string{"GH_AW_AGENT_TOKEN", "GH_AW_GITHUB_TOKEN"}, false},
		{"assignment input is not a safe output", "safe-outputs:\n  jobs:\n    custom:\n      runs-on: ubuntu-latest\n      inputs:\n        assign-to-agent:\n          type: string\n      steps:\n        - run: echo ready", nil, false},
		{"explicit assignment", "safe-outputs:\n  assign-to-agent:\n    github-token: ${{ secrets.ASSIGNMENT }}", []string{"ASSIGNMENT"}, false},
		{"multiline configuration", "env:\n  GH_TOKEN: >-\n    ${{ secrets.FIRST ||\n        secrets.SECOND }}", []string{"FIRST", "SECOND"}, false},
		{"JSON stream", "{\"github-token\":\"${{ secrets.FIRST }}\"}\n{\"github-token\":\"${{ secrets.SECOND }}\"}", []string{"FIRST", "SECOND"}, false},
		{"environment input is not a job environment", "safe-outputs:\n  jobs:\n    custom:\n      runs-on: ubuntu-latest\n      inputs:\n        environment:\n          type: choice\n          options: [staging, production]\n      steps:\n        - env:\n            GH_TOKEN: ${{ secrets.CUSTOM_TOKEN }}", []string{"CUSTOM_TOKEN"}, false},
		{"imported environment variable is not a job environment", "environment:\n  description: unrelated configuration\n  github-token: ${{ secrets.CUSTOM_TOKEN }}", []string{"CUSTOM_TOKEN"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			names := map[string]struct{}{}
			err := scanDoctorTokenSection(tt.content, names, map[string]struct{}{})
			if tt.wantErr {
				require.Error(t, err)
				assert.NotContains(t, err.Error(), "PRIVATE_TOKEN")
				return
			}
			require.NoError(t, err)
			assert.Len(t, names, len(tt.want))
			for _, name := range tt.want {
				assert.Contains(t, names, name)
			}
		})
	}
}

func TestDoctorTokensDiscoverImportedConfiguration(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "main.md")
	require.NoError(t, os.WriteFile(file, []byte("---\non: workflow_dispatch\nimports:\n  - shared.md\n  - copilot-setup-steps.yml\nsafe-outputs:\n  add-comment:\n    github-token: ${{ secrets.LOCAL_TOKEN }}\n---\nPrompt\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "shared.md"), []byte("---\nenv:\n  GH_TOKEN: ${{ secrets.SHARED_CREDENTIAL }}\njobs:\n  prefilter:\n    runs-on: ubuntu-latest\n    environment: staging\n    steps:\n      - run: echo ready\nsafe-outputs:\n  approve-workflow-run:\n    github-token: ${{ secrets.APPROVAL }}\n---\nShared\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "copilot-setup-steps.yml"), []byte("name: Copilot Setup Steps\non: workflow_dispatch\njobs:\n  copilot-setup-steps:\n    runs-on: ubuntu-latest\n    steps:\n      - env:\n          GH_TOKEN: ${{ secrets.SETUP_TOKEN }}\n        run: echo setup\n"), 0600))
	names, environments, err := discoverDoctorTokens(file, parser.NewImportCache(root))
	require.NoError(t, err)
	assert.Equal(t, []string{"APPROVAL", "LOCAL_TOKEN", "SETUP_TOKEN", "SHARED_CREDENTIAL"}, names)
	assert.Equal(t, []string{"staging"}, environments)
}

func TestDoctorTokenDiscoveryKeepsPartialResults(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	broken := filepath.Join(root, "broken.md")
	valid := filepath.Join(root, "valid.md")
	require.NoError(t, os.WriteFile(broken, []byte("---\non: workflow_dispatch\ngithub-token: ${{ secrets.BROKEN + }}\n---\nBroken\n"), 0600))
	require.NoError(t, os.WriteFile(valid, []byte("---\non: workflow_dispatch\ngithub-token: ${{ secrets.VALID_TOKEN }}\n---\nValid\n"), 0600))

	checks, err := discoverDoctorTokenValidationsFromFiles(root, []string{broken, valid}, parser.NewImportCache(root))
	require.NoError(t, err)
	require.Len(t, checks, 2)
	assert.Equal(t, "broken.md", checks[0].Workflow)
	assert.Equal(t, "unknown", checks[0].Status)
	assert.Contains(t, checks[0].Message, "Token discovery is incomplete")
	assert.Equal(t, []string{"VALID_TOKEN"}, checks[1].Secrets)
}

func TestDoctorTokensRepositoryRegression(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	cache := parser.NewImportCache(root)
	cache.SetReadOnly(true)
	for _, tt := range []struct{ workflow, secret string }{
		{"pr-sous-chef", "AWI_MAINTENANCE_TOKEN"},
		{"issue-monster", "GH_AW_AGENT_TOKEN"},
	} {
		names, _, err := discoverDoctorTokens(filepath.Join(root, ".github/workflows", tt.workflow+".md"), cache)
		require.NoError(t, err)
		assert.Contains(t, names, tt.secret)
	}
}

func TestDoctorTokenLogEvidence(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ log, outcome string }{
		{"2026-10-31T16:45:12.5445378Z ##[error]Failed to add comment: Bad credentials - token-not-for-output", "unknown"},
		{"##[error]Failed to approve workflow run: Resource not accessible by integration", "unknown"},
		{"##[warning]Agent assignment credential validation failed: Bad credentials (HTTP 401)", "unknown"},
		{"✗ Message: Bad credentials (HTTP 401)", "unknown"},
		{"✗ Retry of message failed: Bad credentials", "unknown"},
		{"✗ Failed to submit PR review: Bad credentials", "unknown"},
		{"✗ Exception while submitting PR review: HTTP 401", "unknown"},
		{"Agent assignment failed for copilot on issue #42 due to Bad credentials", "unknown"},
		{`core.error("Failed to add comment: Bad credentials");`, "no_evidence"},
		{"OTLP export authorization failed: HTTP 401 Unauthorized", "no_evidence"},
		{"##[error]Failed to add comment: invalid issue number", "no_evidence"},
		{"Successfully added comment", "no_evidence"},
	} {
		result := classifyDoctorTokenLog(tt.log, DoctorAssignmentRunEvidence{Conclusion: "success"})
		assert.Equal(t, tt.outcome, result.Outcome)
		data, err := json.Marshal(result)
		require.NoError(t, err)
		assert.NotContains(t, string(data), "token-not-for-output")
	}
}

func TestDoctorTokenRunEvidenceAllJobs(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	miner := doctorAssignmentRunMiner{
		ctx: context.Background(), repo: "octo/repo", host: "https://github.com",
		now: now, allJobs: true, classify: classifyDoctorTokenLog,
	}
	miner.runGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		switch {
		case strings.Contains(args[3], "/jobs?"):
			return []byte(`{"total_count":2,"jobs":[{"id":11,"run_id":42,"run_attempt":1,"name":"agent","status":"completed","conclusion":"success"},{"id":12,"run_id":42,"run_attempt":1,"name":"safe_outputs","status":"completed","conclusion":"failure"}]}`), nil
		case strings.Contains(args[3], "/jobs/11/"):
			return []byte("No credential diagnostics"), nil
		case strings.Contains(args[3], "/jobs/12/"):
			return []byte("##[error]Failed to add comment: Bad credentials"), nil
		default:
			return nil, errors.New("unexpected request")
		}
	}
	result := miner.inspect(doctorAssignmentRunMetadata{ID: 42, Attempt: 1, CreatedAt: now, Status: "completed"}, now.Add(-time.Hour))
	assert.Equal(t, "unknown", result.Outcome)
	assert.Equal(t, 3, miner.requests)
}

func TestDoctorTokenReplacementGuidance(t *testing.T) {
	t.Parallel()
	for _, message := range []string{doctorTokenReplacementGuidance, doctorAssignmentRemedy("octo/repo", "CUSTOM")} {
		assert.Contains(t, message, "must elicit the user")
		assert.Contains(t, message, "secure Actions secrets UI")
		assert.Contains(t, message, "Never run gh auth token")
		assert.Contains(t, message, "gh auth login OAuth token")
	}
	require.Error(t, doctorTokenValidationError([]DoctorTokenValidation{{Status: "credential_error"}}))
	require.Error(t, doctorTokenValidationError([]DoctorTokenValidation{{Status: "unknown"}}))
	require.NoError(t, doctorTokenValidationError([]DoctorTokenValidation{{Status: "unverified"}}))
}

func TestDoctorTokenMetadataDoesNotValidateValues(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, status string
		coverageErr  error
		history      *DoctorAssignmentRunHistory
	}{
		{name: "present remains unverified", status: "unverified"},
		{name: "metadata denied", status: "unknown", coverageErr: errors.New("HTTP 403")},
		{name: "logs denied", status: "unknown", history: &DoctorAssignmentRunHistory{Error: "HTTP 403"}},
		{name: "partial jobs", status: "unknown", history: &DoctorAssignmentRunHistory{Runs: []DoctorAssignmentRunEvidence{{Outcome: "unknown"}}}},
		{name: "green run with failure", status: "credential_error", history: &DoctorAssignmentRunHistory{Runs: []DoctorAssignmentRunEvidence{{Conclusion: "success", Outcome: "credential_error"}}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			check := DoctorTokenValidation{Secrets: []string{"CUSTOM", "FALLBACK"}, RecentRuns: tt.history}
			diagnoseDoctorTokenValidation(&check, map[string]struct{}{"CUSTOM": {}}, tt.coverageErr)
			assert.Equal(t, tt.status, check.Status)
			assert.Equal(t, []string{"CUSTOM"}, check.Available)
			assert.Equal(t, []string{"FALLBACK"}, check.Missing)
			assert.Contains(t, check.Message, "must elicit the user")
		})
	}
}

func TestDoctorAuthIncludesTokenValidationJSON(t *testing.T) {
	runtime := setupRepositoryRuntime{
		checkAuth:             func(context.Context) error { return nil },
		checkAgentAssignments: func(context.Context, string, string) ([]DoctorAgentAssignmentCheck, error) { return nil, nil },
		checkTokens: func(_ context.Context, repo, dir string) ([]DoctorTokenValidation, error) {
			assert.Empty(t, repo)
			assert.Equal(t, ".", dir)
			check := DoctorTokenValidation{Workflow: "pr-sous-chef.md", Secrets: []string{"AWI_MAINTENANCE_TOKEN"}}
			diagnoseDoctorTokenValidation(&check, nil, nil)
			check.Status = "credential_error"
			return []DoctorTokenValidation{check}, nil
		},
	}
	output := captureSetupStdout(t, func() error {
		err := runSetupAuthWithRuntime(SetupAuthOptions{JSON: true, CheckAgentAssignment: true}, runtime)
		require.Error(t, err)
		return nil
	})
	var result SetupAuthResult
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	require.Len(t, result.TokenValidations, 1)
	assert.Equal(t, []string{"AWI_MAINTENANCE_TOKEN"}, result.TokenValidations[0].Secrets)
	assert.Contains(t, result.TokenValidations[0].Message, "gh auth login OAuth token")
}
