package cli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseManifestBootstrapAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		actionType string
		actionMap  map[string]any
		wantAction repositoryPackageBootstrapAction
		wantErrMsg string
	}{
		{
			name:       "require-owner-type happy path",
			actionType: "require-owner-type",
			actionMap: map[string]any{
				"owner": "repo",
				"value": "org",
			},
			wantAction: repositoryPackageBootstrapAction{
				Type:  "require-owner-type",
				Owner: "repo",
				Value: "org",
			},
		},
		{
			name:       "require-owner-type invalid owner",
			actionType: "require-owner-type",
			actionMap: map[string]any{
				"owner": "user",
				"value": "org",
			},
			wantErrMsg: "config[0].owner must be 'repo'",
		},
		{
			name:       "require-owner-type invalid value",
			actionType: "require-owner-type",
			actionMap: map[string]any{
				"value": "everyone",
			},
			wantErrMsg: "config[0].value must be one of: any, org, user",
		},
		{
			name:       "repo-variable happy path",
			actionType: "repo-variable",
			actionMap: map[string]any{
				"name":   "EXAMPLE",
				"prompt": "Enter a value",
			},
			wantAction: repositoryPackageBootstrapAction{
				Type:   "repo-variable",
				Name:   "EXAMPLE",
				Prompt: "Enter a value",
			},
		},
		{
			name:       "repo-variable missing name",
			actionType: "repo-variable",
			actionMap: map[string]any{
				"prompt": "Enter a value",
			},
			wantErrMsg: "config[0].name is required when type=repo-variable",
		},
		{
			name:       "repo-variable missing prompt",
			actionType: "repo-variable",
			actionMap: map[string]any{
				"name": "EXAMPLE",
			},
			wantErrMsg: "config[0].prompt is required when type=repo-variable",
		},
		{
			name:       "repo-secret happy path",
			actionType: "repo-secret",
			actionMap: map[string]any{
				"name":   "EXAMPLE_SECRET",
				"prompt": "Enter a secret",
			},
			wantAction: repositoryPackageBootstrapAction{
				Type:   "repo-secret",
				Name:   "EXAMPLE_SECRET",
				Prompt: "Enter a secret",
			},
		},
		{
			name:       "repo-secret missing name",
			actionType: "repo-secret",
			actionMap: map[string]any{
				"prompt": "Enter a secret",
			},
			wantErrMsg: "config[0].name is required when type=repo-secret",
		},
		{
			name:       "repo-secret missing prompt",
			actionType: "repo-secret",
			actionMap: map[string]any{
				"name": "EXAMPLE_SECRET",
			},
			wantErrMsg: "config[0].prompt is required when type=repo-secret",
		},
		{
			name:       "repo-label happy path",
			actionType: "repo-label",
			actionMap: map[string]any{
				"name":        "automation",
				"description": "Managed by automation",
				"color":       "1f6feb",
			},
			wantAction: repositoryPackageBootstrapAction{
				Type:        "repo-label",
				Name:        "automation",
				Description: "Managed by automation",
				Color:       "1f6feb",
			},
		},
		{
			name:       "repo-label missing name",
			actionType: "repo-label",
			actionMap: map[string]any{
				"description": "Managed by automation",
				"color":       "1f6feb",
			},
			wantErrMsg: "config[0].name is required when type=repo-label",
		},
		{
			name:       "repo-label missing description",
			actionType: "repo-label",
			actionMap: map[string]any{
				"name":  "automation",
				"color": "1f6feb",
			},
			wantErrMsg: "config[0].description is required when type=repo-label",
		},
		{
			name:       "repo-label missing color",
			actionType: "repo-label",
			actionMap: map[string]any{
				"name":        "automation",
				"description": "Managed by automation",
			},
			wantErrMsg: "config[0].color is required when type=repo-label",
		},
		{
			name:       "repo-label invalid color",
			actionType: "repo-label",
			actionMap: map[string]any{
				"name":        "automation",
				"description": "Managed by automation",
				"color":       "#1f6feb",
			},
			wantErrMsg: "config[0].color must be a 6-character hexadecimal color",
		},
		{
			name:       "repo-label name too long",
			actionType: "repo-label",
			actionMap: map[string]any{
				"name":        strings.Repeat("a", bootstrapLabelNameMaxLength+1),
				"description": "Managed by automation",
				"color":       "1f6feb",
			},
			wantErrMsg: "config[0].name must be at most 50 characters",
		},
		{
			name:       "repo-label description too long",
			actionType: "repo-label",
			actionMap: map[string]any{
				"name":        "automation",
				"description": strings.Repeat("a", bootstrapLabelDescriptionMaxLength+1),
				"color":       "1f6feb",
			},
			wantErrMsg: "config[0].description must be at most 100 characters",
		},
		{
			name:       "github-app defaults app name from name and mode",
			actionType: "github-app",
			actionMap: map[string]any{
				"name":               "my-app",
				"app-id-variable":    "APP_ID",
				"private-key-secret": "APP_PRIVATE_KEY",
				"homepage-url":       "https://example.com",
				"permissions":        map[string]any{"contents": "read"},
			},
			wantAction: repositoryPackageBootstrapAction{
				Type:             "github-app",
				Name:             "my-app",
				AppName:          "my-app",
				AppIDVariable:    "APP_ID",
				PrivateKeySecret: "APP_PRIVATE_KEY",
				HomepageURL:      "https://example.com",
				Mode:             "create-or-existing",
				Permissions:      map[string]string{"contents": "read"},
			},
		},
		{
			name:       "github-app existing-only sets mode to existing",
			actionType: "github-app",
			actionMap: map[string]any{
				"app-id-variable":    "APP_ID",
				"private-key-secret": "APP_PRIVATE_KEY",
				"existing-only":      true,
			},
			wantAction: repositoryPackageBootstrapAction{
				Type:             "github-app",
				AppIDVariable:    "APP_ID",
				PrivateKeySecret: "APP_PRIVATE_KEY",
				ExistingOnly:     true,
				Mode:             "existing",
			},
		},
		{
			name:       "github-app existing-only conflicting mode",
			actionType: "github-app",
			actionMap: map[string]any{
				"app-id-variable":    "APP_ID",
				"private-key-secret": "APP_PRIVATE_KEY",
				"existing-only":      true,
				"mode":               "create-or-existing",
			},
			wantErrMsg: "config[0].existing-only requires mode to be 'existing' or unset",
		},
		{
			name:       "github-app invalid mode",
			actionType: "github-app",
			actionMap: map[string]any{
				"app-id-variable":    "APP_ID",
				"private-key-secret": "APP_PRIVATE_KEY",
				"mode":               "bogus",
			},
			wantErrMsg: "config[0].mode must be one of: create-or-existing, existing",
		},
		{
			name:       "github-app invalid owner",
			actionType: "github-app",
			actionMap: map[string]any{
				"owner":              "user",
				"app-id-variable":    "APP_ID",
				"private-key-secret": "APP_PRIVATE_KEY",
			},
			wantErrMsg: "config[0].owner must be 'repo' when type=github-app",
		},
		{
			name:       "github-app missing app-id-variable",
			actionType: "github-app",
			actionMap: map[string]any{
				"private-key-secret": "APP_PRIVATE_KEY",
			},
			wantErrMsg: "config[0].app-id-variable is required when type=github-app",
		},
		{
			name:       "github-app missing private-key-secret",
			actionType: "github-app",
			actionMap: map[string]any{
				"app-id-variable": "APP_ID",
			},
			wantErrMsg: "config[0].private-key-secret is required when type=github-app",
		},
		{
			name:       "copilot-auth defaults",
			actionType: "copilot-auth",
			actionMap:  map[string]any{},
			wantAction: repositoryPackageBootstrapAction{
				Type:     "copilot-auth",
				Secret:   "COPILOT_GITHUB_TOKEN",
				Strategy: "prompt-if-actions-auth-unavailable",
			},
		},
		{
			name:       "copilot-auth invalid strategy",
			actionType: "copilot-auth",
			actionMap: map[string]any{
				"strategy": "always",
			},
			wantErrMsg: "config[0].strategy must be 'prompt-if-actions-auth-unavailable'",
		},
		{
			name:       "commit-and-push happy path",
			actionType: "commit-and-push",
			actionMap: map[string]any{
				"message": "Bootstrap repository changes",
			},
			wantAction: repositoryPackageBootstrapAction{
				Type:    "commit-and-push",
				Message: "Bootstrap repository changes",
			},
		},
		{
			name:       "commit-and-push missing message",
			actionType: "commit-and-push",
			actionMap:  map[string]any{},
			wantErrMsg: "config[0].message is required when type=commit-and-push",
		},
		{
			name:       "handoff happy path",
			actionType: "handoff",
			actionMap: map[string]any{
				"message": "Continue with repository-specific setup.",
			},
			wantAction: repositoryPackageBootstrapAction{
				Type:    "handoff",
				Message: "Continue with repository-specific setup.",
			},
		},
		{
			name:       "handoff missing message",
			actionType: "handoff",
			actionMap:  map[string]any{},
			wantErrMsg: "config[0].message is required when type=handoff",
		},
		{
			name:       "unsupported action type",
			actionType: "unknown-type",
			actionMap:  map[string]any{},
			wantErrMsg: `config[0].type "unknown-type" is not supported`,
		},
		{
			name:       "when field not supported",
			actionType: "commit-and-push",
			actionMap: map[string]any{
				"message": "msg",
				"when":    map[string]any{"variable": "x", "equals": "y"},
			},
			wantErrMsg: "config[0].when is not supported yet",
		},
		{
			name:       "invalid enum value",
			actionType: "commit-and-push",
			actionMap: map[string]any{
				"message": "msg",
				"enum":    "not-a-list",
			},
			wantErrMsg: "config[0].enum",
		},
		{
			name:       "invalid events value",
			actionType: "commit-and-push",
			actionMap: map[string]any{
				"message": "msg",
				"events":  123,
			},
			wantErrMsg: "config[0].events",
		},
		{
			name:       "invalid permissions value",
			actionType: "commit-and-push",
			actionMap: map[string]any{
				"message":     "msg",
				"permissions": "not-a-map",
			},
			wantErrMsg: "config[0].permissions",
		},
		{
			name:       "trims whitespace and collects all string fields with enum and events",
			actionType: "handoff",
			actionMap: map[string]any{
				"owner":              "  repo  ",
				"value":              "  org  ",
				"name":               "  EXAMPLE  ",
				"prompt":             "  Enter a value  ",
				"description":        "  desc  ",
				"default":            "  keepme  ",
				"secret":             "  SECRET  ",
				"strategy":           "  custom  ",
				"message":            "  hello  ",
				"mode":               "  existing  ",
				"app-id-variable":    "  APP_ID  ",
				"private-key-secret": "  KEY  ",
				"app-name":           "  App Name  ",
				"homepage-url":       "  https://x.example  ",
				"optional":           true,
				"existing-only":      false,
				"enum":               []any{"a", "b"},
				"events":             []any{"issues", "pull_request"},
			},
			wantAction: repositoryPackageBootstrapAction{
				Type:             "handoff",
				Owner:            "repo",
				Value:            "org",
				Name:             "EXAMPLE",
				Prompt:           "Enter a value",
				Description:      "desc",
				Default:          "  keepme  ",
				Secret:           "SECRET",
				Strategy:         "custom",
				Message:          "hello",
				Mode:             "existing",
				AppIDVariable:    "APP_ID",
				PrivateKeySecret: "KEY",
				AppName:          "App Name",
				HomepageURL:      "https://x.example",
				Optional:         true,
				ExistingOnly:     false,
				Enum:             []string{"a", "b"},
				Events:           []string{"issues", "pull_request"},
			},
		},
		{
			name:       "empty action map with unsupported default case",
			actionType: "",
			actionMap:  map[string]any{},
			wantErrMsg: `config[0].type "" is not supported`,
		},
		{
			name:       "wrong field type is rejected",
			actionType: "repo-variable",
			actionMap: map[string]any{
				"name":   "EXAMPLE",
				"prompt": 42,
			},
			wantErrMsg: "config[0].prompt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			action, err := parseManifestBootstrapAction(tt.actionType, tt.actionMap, "manifest.yaml", 0)

			if tt.wantErrMsg != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tt.wantErrMsg)
				assert.Equal(t, repositoryPackageBootstrapAction{}, action)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantAction, action)
		})
	}
}
