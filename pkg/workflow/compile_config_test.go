//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestAllowedDomainsParsing(t *testing.T) {
	tests := []struct {
		name            string
		frontmatter     map[string]any
		expectedDomains []string
	}{
		{
			name: "no output config",
			frontmatter: map[string]any{
				"engine": "claude",
			},
			expectedDomains: nil,
		},
		{
			name: "output config with allowed-domains",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"allowed-domains": []any{"example.com", "trusted.org"},
				},
			},
			expectedDomains: []string{"example.com", "trusted.org"},
		},
		{
			name: "output config with create-issue and allowed-domains",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-issue": map[string]any{
						"title-prefix": "[auto] ",
					},
					"allowed-domains": []any{"github.com", "api.github.com"},
				},
			},
			expectedDomains: []string{"github.com", "api.github.com"},
		},
		{
			name: "output config without allowed-domains",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-issue": map[string]any{
						"title-prefix": "[auto] ",
					},
				},
			},
			expectedDomains: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCompiler()
			config := c.extractSafeOutputsConfig(tt.frontmatter)

			if tt.expectedDomains == nil {
				if config == nil {
					return // expected case
				}
				if len(config.AllowedDomains) == 0 {
					return // expected case
				}
				t.Errorf("Expected no allowed domains, but got %v", config.AllowedDomains)
				return
			}

			if config == nil {
				t.Errorf("Expected output config, but got nil")
				return
			}

			if len(config.AllowedDomains) != len(tt.expectedDomains) {
				t.Errorf("Expected %d allowed domains, but got %d", len(tt.expectedDomains), len(config.AllowedDomains))
				return
			}

			for i, expected := range tt.expectedDomains {
				if config.AllowedDomains[i] != expected {
					t.Errorf("Expected domain %s at index %d, but got %s", expected, i, config.AllowedDomains[i])
				}
			}
		})
	}
}

func TestAllowedDomainsInWorkflow(t *testing.T) {
	// Create a test compiler with verbose output to check generated workflow
	c := NewCompiler()

	// Test workflow with allowed domains
	frontmatter := map[string]any{
		"engine": "claude",
		"safe-outputs": map[string]any{
			"allowed-domains": []any{"example.com", "trusted.org"},
		},
	}

	config := c.extractSafeOutputsConfig(frontmatter)
	if config == nil {
		t.Fatal("Expected output config, but got nil")
	}

	if len(config.AllowedDomains) != 2 {
		t.Errorf("Expected 2 allowed domains, but got %d", len(config.AllowedDomains))
	}

	expectedDomains := []string{"example.com", "trusted.org"}
	for i, expected := range expectedDomains {
		if config.AllowedDomains[i] != expected {
			t.Errorf("Expected domain %s at index %d, but got %s", expected, i, config.AllowedDomains[i])
		}
	}
}

func TestSafeOutputsConfigGeneration(t *testing.T) {
	tests := []struct {
		name               string
		frontmatter        map[string]any
		expectedInConfig   []string
		unexpectedInConfig []string
	}{
		{
			name: "create-discussion config",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-discussion": map[string]any{
						"title-prefix": "[discussion] ",
						"max":          2,
					},
				},
			},
			expectedInConfig: []string{"create_discussion"},
		},
		{
			name: "create-pull-request-review-comment config",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-pull-request-review-comment": map[string]any{
						"max": 5,
					},
				},
			},
			expectedInConfig: []string{"create_pull_request_review_comment"},
		},
		{
			name: "create-code-scanning-alert config",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-code-scanning-alert": map[string]any{},
				},
			},
			expectedInConfig: []string{"create_code_scanning_alert"},
		},
		{
			name: "multiple safe outputs including previously missing ones",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-issue":                       map[string]any{"max": 1},
					"create-discussion":                  map[string]any{"max": 3},
					"create-pull-request-review-comment": map[string]any{"max": 10},
					"create-code-scanning-alert":         map[string]any{},
					"add-comment":                        map[string]any{},
				},
			},
			expectedInConfig: []string{
				"create_issue",
				"create_discussion",
				"create_pull_request_review_comment",
				"create_code_scanning_alert",
				"add_comment",
			},
		},
		{
			name: "no safe outputs config",
			frontmatter: map[string]any{
				"engine": "claude",
			},
			expectedInConfig:   []string{},
			unexpectedInConfig: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			config := compiler.extractSafeOutputsConfig(tt.frontmatter)

			// Test the config generation in generateOutputCollectionStep by creating a mock workflow data
			workflowData := &WorkflowData{
				SafeOutputs: config,
			}

			// Use the compiler's generateOutputCollectionStep to verify config is not in env vars
			var yamlBuilder strings.Builder
			compiler.generateOutputCollectionStep(&yamlBuilder, workflowData)
			generatedYAML := yamlBuilder.String()

			// Config should NOT be in environment variables anymore - it's in a file
			configLinePresent := strings.Contains(generatedYAML, "GH_AW_SAFE_OUTPUTS_CONFIG:")

			if len(tt.expectedInConfig) > 0 {
				// Even if we expect items in config, they should be written to file, not env var
				if configLinePresent {
					t.Errorf("GH_AW_SAFE_OUTPUTS_CONFIG should NOT be in environment variables - config is now in file")
					return
				}
			}
		})
	}
}

func TestCreateDiscussionConfigParsing(t *testing.T) {
	tests := []struct {
		name                string
		frontmatter         map[string]any
		expectedTitlePrefix string
		expectedCategory    string
		expectedMax         int
		expectConfig        bool
	}{
		{
			name: "no create-discussion config",
			frontmatter: map[string]any{
				"engine": "claude",
			},
			expectConfig: false,
		},
		{
			name: "basic create-discussion config",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-discussion": map[string]any{},
				},
			},
			expectedTitlePrefix: "",
			expectedCategory:    "",
			expectedMax:         1, // default
			expectConfig:        true,
		},
		{
			name: "create-discussion with title-prefix",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-discussion": map[string]any{
						"title-prefix": "[ai] ",
					},
				},
			},
			expectedTitlePrefix: "[ai] ",
			expectedCategory:    "",
			expectedMax:         1,
			expectConfig:        true,
		},
		{
			name: "create-discussion with category (string)",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-discussion": map[string]any{
						"category": "DIC_kwDOGFsHUM4BsUn3",
					},
				},
			},
			expectedTitlePrefix: "",
			expectedCategory:    "DIC_kwDOGFsHUM4BsUn3",
			expectedMax:         1,
			expectConfig:        true,
		},
		{
			name: "create-discussion with category (name)",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-discussion": map[string]any{
						"category": "general",
					},
				},
			},
			expectedTitlePrefix: "",
			expectedCategory:    "general",
			expectedMax:         1,
			expectConfig:        true,
		},
		{
			name: "create-discussion with category (number)",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-discussion": map[string]any{
						"category": 12345,
					},
				},
			},
			expectedTitlePrefix: "",
			expectedCategory:    "12345",
			expectedMax:         1,
			expectConfig:        true,
		},
		{
			name: "create-discussion with all options",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-discussion": map[string]any{
						"title-prefix": "[research] ",
						"category":     "DIC_kwDOGFsHUM4BsUn3",
						"max":          3,
					},
				},
			},
			expectedTitlePrefix: "[research] ",
			expectedCategory:    "DIC_kwDOGFsHUM4BsUn3",
			expectedMax:         3,
			expectConfig:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCompiler()
			config := c.extractSafeOutputsConfig(tt.frontmatter)

			if !tt.expectConfig {
				if config != nil && config.CreateDiscussions != nil {
					t.Errorf("Expected no create-discussion config, but got one")
				}
				return
			}

			if config == nil || config.CreateDiscussions == nil {
				t.Errorf("Expected create-discussion config, but got nil")
				return
			}

			discussionConfig := config.CreateDiscussions

			if discussionConfig.TitlePrefix != tt.expectedTitlePrefix {
				t.Errorf("Expected title prefix %q, but got %q", tt.expectedTitlePrefix, discussionConfig.TitlePrefix)
			}

			if discussionConfig.Category != tt.expectedCategory {
				t.Errorf("Expected category %q, but got %q", tt.expectedCategory, discussionConfig.Category)
			}

			if discussionConfig.Max != tt.expectedMax {
				t.Errorf("Expected max %d, but got %d", tt.expectedMax, discussionConfig.Max)
			}
		})
	}
}
