//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateSafeOutputsMergePullRequestLabelValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  *SafeOutputsConfig
		wantErr string
	}{
		{
			name: "empty required-labels entry fails",
			config: &SafeOutputsConfig{
				MergePullRequest: &MergePullRequestConfig{
					RequiredLabels: []string{"safe-to-merge", "   "},
				},
			},
			wantErr: "safe-outputs.merge-pull-request.required-labels[1] cannot be empty",
		},
		{
			name: "non-empty labels pass",
			config: &SafeOutputsConfig{
				MergePullRequest: &MergePullRequestConfig{
					RequiredLabels: []string{"safe-to-merge", "automerge"},
				},
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSafeOutputsMergePullRequest(tt.config)
			if tt.wantErr == "" {
				assert.NoError(t, err, "expected merge-pull-request label validation to pass")
				return
			}
			require.Error(t, err, "expected merge-pull-request label validation to fail")
			require.ErrorContains(t, err, tt.wantErr, "expected validation error to include field-specific message")
			require.ErrorContains(t, err, "Example:", "expected validation error to include an example")
		})
	}
}

func TestValidateSafeOutputsMergePullRequestAllowedBranchesValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  *SafeOutputsConfig
		wantErr string
	}{
		{
			name: "invalid pattern at index 0 fails",
			config: &SafeOutputsConfig{
				MergePullRequest: &MergePullRequestConfig{
					AllowedBranches: []string{"feature branch"},
				},
			},
			wantErr: `invalid glob pattern "feature branch" in safe-outputs.merge-pull-request.allowed-branches[0]`,
		},
		{
			name: "invalid pattern at index 1 reports index 1",
			config: &SafeOutputsConfig{
				MergePullRequest: &MergePullRequestConfig{
					AllowedBranches: []string{"main", "feature branch"},
				},
			},
			wantErr: `invalid glob pattern "feature branch" in safe-outputs.merge-pull-request.allowed-branches[1]`,
		},
		{
			name: "valid patterns pass",
			config: &SafeOutputsConfig{
				MergePullRequest: &MergePullRequestConfig{
					AllowedBranches: []string{"main", "feature/*"},
				},
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSafeOutputsMergePullRequest(tt.config)
			if tt.wantErr == "" {
				assert.NoError(t, err, "expected merge-pull-request allowed-branches validation to pass")
				return
			}

			require.Error(t, err, "expected merge-pull-request allowed-branches validation to fail")
			require.ErrorContains(t, err, tt.wantErr, "expected field-specific allowed-branches error")
			require.ErrorContains(t, err, "Example:", "expected validation error to include an example")
		})
	}
}

func TestMergePullRequestExplicitPolicies(t *testing.T) {
	compiler := NewCompiler()
	cfg := compiler.parseMergePullRequestConfig(map[string]any{
		"merge-pull-request": map[string]any{
			"required-checks":     []any{"ci/test", "ci/lint"},
			"required-approvers":  []any{"maintainer"},
			"required-check-apps": map[string]any{"ci/test": 123},
		},
	})
	require.NotNil(t, cfg)
	assert.Equal(t, []string{"ci/test", "ci/lint"}, cfg.RequiredChecks)
	assert.Equal(t, []string{"maintainer"}, cfg.RequiredApprovers)
	assert.Equal(t, map[string]int64{"ci/test": 123}, cfg.RequiredCheckApps)
	config := &SafeOutputsConfig{MergePullRequest: cfg}
	require.NoError(t, validateSafeOutputsMergePullRequest(config))
	require.NoError(t, validateMergePullRequestPolicies(config, "Test Workflow"))
	handler := pullRequestHandlerRegistry["merge_pull_request"](config)
	assert.Equal(t, []string{"ci/test", "ci/lint"}, handler["required_checks"])
	assert.Equal(t, []string{"maintainer"}, handler["required_approvers"])
	assert.Equal(t, map[string]int64{"ci/test": 123}, handler["required_check_apps"])
	checkPermission, ok := ComputePermissionsForSafeOutputs(config).Get(PermissionChecks)
	require.True(t, ok)
	assert.Equal(t, PermissionRead, checkPermission)

	for _, field := range []string{"required-checks", "required-approvers"} {
		t.Run(field, func(t *testing.T) {
			invalid := compiler.parseMergePullRequestConfig(map[string]any{
				"merge-pull-request": map[string]any{field: []any{"   "}},
			})
			err := validateMergePullRequestPolicies(&SafeOutputsConfig{MergePullRequest: invalid}, "Test Workflow")
			require.ErrorContains(t, err, field+"[0] cannot be empty")
		})
	}

	cfg.RequiredApprovers = []string{"@maintainer"}
	require.ErrorContains(t, validateMergePullRequestPolicies(config, "Test Workflow"), "invalid GitHub username")
}

func TestMergePullRequestCheckProducers(t *testing.T) {
	for _, tt := range []struct {
		name    string
		apps    map[string]int64
		wantErr string
	}{
		{"expected producer", map[string]int64{"ci": 123}, ""},
		{"large app ID", map[string]int64{"ci": 9007199254740991}, ""},
		{"unknown check", map[string]int64{"other": 123}, "not listed in required-checks"},
		{"invalid app", map[string]int64{"ci": 0}, "positive GitHub App ID"},
		{"unsafe app ID", map[string]int64{"ci": 9007199254740992}, "positive GitHub App ID"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			config := &SafeOutputsConfig{MergePullRequest: &MergePullRequestConfig{RequiredChecks: []string{"ci"}, RequiredCheckApps: tt.apps}}
			err := validateMergePullRequestPolicies(config, "Test Workflow")
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}

func TestMergePullRequestCheckAppParsing(t *testing.T) {
	for _, value := range []any{123, int64(9007199254740991), uint64(9007199254740991), float64(9007199254740991), 1.5, "123", uint64(9007199254740992)} {
		cfg := NewCompiler().parseMergePullRequestConfig(map[string]any{
			"merge-pull-request": map[string]any{
				"required-checks":     []any{"ci"},
				"required-check-apps": map[string]any{"ci": value},
			},
		})
		err := validateMergePullRequestPolicies(&SafeOutputsConfig{MergePullRequest: cfg}, "Workflow")
		switch value {
		case 1.5, "123", uint64(9007199254740992):
			require.ErrorContains(t, err, "positive GitHub App ID")
		case 123:
			require.NoError(t, err)
			assert.Equal(t, int64(123), cfg.RequiredCheckApps["ci"])
		default:
			require.NoError(t, err)
			assert.Equal(t, int64(9007199254740991), cfg.RequiredCheckApps["ci"])
		}
	}
}

func TestMergePullRequestRejectsAgentControlledChecks(t *testing.T) {
	tests := []struct {
		name         string
		workflowName string
		checkName    string
		requiredName string
		wantErr      bool
	}{
		{"explicit overlap", "Workflow", "ci/test", "ci/test", true},
		{"default workflow name overlap", "Workflow", "", "Workflow (Result)", true},
		{"explicit workflow name deduplicated", "Workflow", "Workflow", "Workflow (Result)", true},
		{"fallback name overlap", "", "", "Agent Check", true},
		{"independent check", "Workflow", "agent-result", "ci/test", false},
		{"exact names only", "Workflow", "ci/test", "CI/Test", false},
		{"workflow check distinct from result", "Workflow", "", "Workflow", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &SafeOutputsConfig{
				MergePullRequest: &MergePullRequestConfig{RequiredChecks: []string{tt.requiredName}},
				CreateCheckRun:   &CreateCheckRunConfig{Name: tt.checkName},
			}
			err := validateMergePullRequestPolicies(config, tt.workflowName)
			if tt.wantErr {
				require.ErrorContains(t, err, "agent-controlled create-check-run name")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
