//go:build !integration

package workflow

import (
	"testing"
)

func TestAddCommentsConfigTargetRepo(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name           string
		configMap      map[string]any
		expectedTarget string
		expectedRepo   string
		shouldBeNil    bool
	}{
		{
			name: "basic target-repo configuration",
			configMap: map[string]any{
				"add-comment": map[string]any{
					"max":         5,
					"target":      "*",
					"target-repo": "github/customer-feedback",
				},
			},
			expectedTarget: "*",
			expectedRepo:   "github/customer-feedback",
			shouldBeNil:    false,
		},
		{
			name: "target-repo with wildcard should be rejected",
			configMap: map[string]any{
				"add-comment": map[string]any{
					"max":         5,
					"target":      "123",
					"target-repo": "*",
				},
			},
			shouldBeNil: true, // Configuration should be nil due to validation
		},
		{
			name: "target-repo without target field",
			configMap: map[string]any{
				"add-comment": map[string]any{
					"max":         1,
					"target-repo": "owner/repo",
				},
			},
			expectedTarget: "",
			expectedRepo:   "owner/repo",
			shouldBeNil:    false,
		},
		{
			name: "no target-repo field",
			configMap: map[string]any{
				"add-comment": map[string]any{
					"max":    2,
					"target": "triggering",
				},
			},
			expectedTarget: "triggering",
			expectedRepo:   "",
			shouldBeNil:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := compiler.parseCommentsConfig(tt.configMap)

			if tt.shouldBeNil {
				if config != nil {
					t.Errorf("Expected config to be nil for invalid target-repo, but got %+v", config)
				}
				return
			}

			if config == nil {
				t.Fatal("Expected valid config, but got nil")
			}

			if config.Target != tt.expectedTarget {
				t.Errorf("Expected Target = %q, got %q", tt.expectedTarget, config.Target)
			}

			if config.TargetRepoSlug != tt.expectedRepo {
				t.Errorf("Expected TargetRepoSlug = %q, got %q", tt.expectedRepo, config.TargetRepoSlug)
			}
		})
	}
}

func TestAddCommentsConfigHideOlderComments(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name                      string
		configMap                 map[string]any
		expectedHideOlderComments bool
	}{
		{
			name: "hide-older-comments enabled",
			configMap: map[string]any{
				"add-comment": map[string]any{
					"max":                 1,
					"hide-older-comments": true,
				},
			},
			expectedHideOlderComments: true,
		},
		{
			name: "hide-older-comments disabled",
			configMap: map[string]any{
				"add-comment": map[string]any{
					"max":                 1,
					"hide-older-comments": false,
				},
			},
			expectedHideOlderComments: false,
		},
		{
			name: "hide-older-comments not specified (default false)",
			configMap: map[string]any{
				"add-comment": map[string]any{
					"max": 1,
				},
			},
			expectedHideOlderComments: false,
		},
		{
			name: "hide-older-comments with other fields",
			configMap: map[string]any{
				"add-comment": map[string]any{
					"max":                 3,
					"target":              "*",
					"target-repo":         "owner/repo",
					"hide-older-comments": true,
				},
			},
			expectedHideOlderComments: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := compiler.parseCommentsConfig(tt.configMap)

			if config == nil {
				t.Fatal("Expected valid config, but got nil")
			}

			if config.HideOlderComments != tt.expectedHideOlderComments {
				t.Errorf("Expected HideOlderComments = %v, got %v", tt.expectedHideOlderComments, config.HideOlderComments)
			}
		})
	}
}

func TestAddCommentsConfigAllowedReasons(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name             string
		configMap        map[string]any
		expectedReasons  []string
		shouldBeNonEmpty bool
	}{
		{
			name: "allowed-reasons with multiple values",
			configMap: map[string]any{
				"add-comment": map[string]any{
					"hide-older-comments": true,
					"allowed-reasons":     []any{"OUTDATED", "RESOLVED"},
				},
			},
			expectedReasons:  []string{"OUTDATED", "RESOLVED"},
			shouldBeNonEmpty: true,
		},
		{
			name: "allowed-reasons with single value",
			configMap: map[string]any{
				"add-comment": map[string]any{
					"hide-older-comments": true,
					"allowed-reasons":     []any{"SPAM"},
				},
			},
			expectedReasons:  []string{"SPAM"},
			shouldBeNonEmpty: true,
		},
		{
			name: "allowed-reasons not specified",
			configMap: map[string]any{
				"add-comment": map[string]any{
					"hide-older-comments": true,
				},
			},
			expectedReasons:  nil,
			shouldBeNonEmpty: false,
		},
		{
			name: "allowed-reasons empty array",
			configMap: map[string]any{
				"add-comment": map[string]any{
					"hide-older-comments": true,
					"allowed-reasons":     []any{},
				},
			},
			expectedReasons:  nil,
			shouldBeNonEmpty: false,
		},
		{
			name: "allowed-reasons with all valid values",
			configMap: map[string]any{
				"add-comment": map[string]any{
					"hide-older-comments": true,
					"allowed-reasons":     []any{"SPAM", "ABUSE", "OFF_TOPIC", "OUTDATED", "RESOLVED"},
				},
			},
			expectedReasons:  []string{"SPAM", "ABUSE", "OFF_TOPIC", "OUTDATED", "RESOLVED"},
			shouldBeNonEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := compiler.parseCommentsConfig(tt.configMap)

			if config == nil {
				t.Fatal("Expected valid config, but got nil")
			}

			if tt.shouldBeNonEmpty {
				if len(config.AllowedReasons) == 0 {
					t.Errorf("Expected non-empty AllowedReasons, got empty")
				}
				if len(config.AllowedReasons) != len(tt.expectedReasons) {
					t.Errorf("Expected %d reasons, got %d", len(tt.expectedReasons), len(config.AllowedReasons))
				}
				for i, reason := range tt.expectedReasons {
					if i >= len(config.AllowedReasons) || config.AllowedReasons[i] != reason {
						t.Errorf("Expected reason[%d] = %q, got %q", i, reason, config.AllowedReasons[i])
					}
				}
			} else {
				if len(config.AllowedReasons) != 0 {
					t.Errorf("Expected empty AllowedReasons, got %v", config.AllowedReasons)
				}
			}
		})
	}
}
