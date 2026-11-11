//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppTokenRepositoryAdvisoriesOverride(t *testing.T) {
	for _, tt := range []struct {
		name     string
		inferred map[PermissionScope]PermissionLevel
		override string
		want     map[string]string
	}{
		{
			name:     "none removes inferred write and preserves other scopes",
			inferred: map[PermissionScope]PermissionLevel{PermissionRepositoryAdvisories: PermissionWrite, PermissionIssues: PermissionWrite},
			override: "none",
			want:     map[string]string{"permission-issues": "write"},
		},
		{
			name:     "normalized none removes inferred read",
			inferred: map[PermissionScope]PermissionLevel{PermissionRepositoryAdvisories: PermissionRead, PermissionContents: PermissionRead},
			override: " None ",
			want:     map[string]string{"permission-contents": "read"},
		},
		{
			name:     "none alone retains fail closed fallback",
			inferred: map[PermissionScope]PermissionLevel{PermissionRepositoryAdvisories: PermissionWrite},
			override: "none",
			want:     map[string]string{"permission-contents": "none"},
		},
		{
			name:     "read overrides inferred write",
			inferred: map[PermissionScope]PermissionLevel{PermissionRepositoryAdvisories: PermissionWrite},
			override: "read",
			want:     map[string]string{"permission-repository-advisories": "read"},
		},
		{
			name:     "write adds explicit permission",
			inferred: map[PermissionScope]PermissionLevel{PermissionContents: PermissionRead},
			override: "write",
			want:     map[string]string{"permission-contents": "read", "permission-repository-advisories": "write"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			app := &GitHubAppConfig{Permissions: map[string]string{"repository-advisories": tt.override}}
			assert.Equal(t, tt.want, appTokenPermissionFields(app, NewPermissionsFromMap(tt.inferred)))
		})
	}
}

func TestAppTokenRepositoryAdvisoriesInputContract(t *testing.T) {
	// Offline replay of runner Handler.AddInputsToEnvironment and the pinned
	// actions/create-github-app-token getPermissionsFromInputs contract:
	// https://github.com/actions/create-github-app-token/blob/64ab729a6294000f5aa4a4bc0b73d1f32bc45510/lib/get-permissions-from-inputs.js
	for _, level := range []PermissionLevel{PermissionRead, PermissionWrite, PermissionNone} {
		t.Run(string(level), func(t *testing.T) {
			app := &GitHubAppConfig{
				AppID: "test-client", PrivateKey: "test-key",
				Permissions: map[string]string{"repository-advisories": string(level)},
			}
			permissions := NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
				PermissionRepositoryAdvisories: PermissionWrite,
				PermissionContents:             PermissionRead,
			})
			step := strings.Join(NewCompiler().buildGitHubAppTokenMintStep(app, permissions, ""), "")
			var parsed struct {
				Steps []struct {
					With map[string]string `yaml:"with"`
				} `yaml:"steps"`
			}
			require.NoError(t, yaml.Unmarshal([]byte("steps:\n"+step), &parsed))
			require.Len(t, parsed.Steps, 1)

			installationPermissions := map[string]string{}
			for input, value := range parsed.Steps[0].With {
				envKey := "INPUT_" + strings.ToUpper(strings.ReplaceAll(input, " ", "_"))
				if !strings.HasPrefix(envKey, "INPUT_PERMISSION-") || value == "" {
					continue
				}
				scope := strings.ToLower(strings.TrimPrefix(envKey, "INPUT_PERMISSION-"))
				installationPermissions[strings.ReplaceAll(scope, "-", "_")] = value
			}
			want := map[string]string{"contents": "read"}
			if level != PermissionNone {
				want["repository_advisories"] = string(level)
			}
			assert.Equal(t, want, installationPermissions)
		})
	}
}
