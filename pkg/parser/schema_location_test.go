//go:build !integration

package parser

import (
	"strings"
	"testing"
)

func TestValidateWithSchemaAndLocation(t *testing.T) {
	tests := []struct {
		name           string
		frontmatter    map[string]any
		schema         string
		context        string
		filePath       string
		wantErr        bool
		errContains    []string
		errNotContains []string
	}{
		{
			name: "valid data should not error",
			frontmatter: map[string]any{
				"name": "test",
			},
			schema: `{
				"type": "object",
				"properties": {
					"name": {"type": "string"}
				},
				"additionalProperties": false
			}`,
			context:  "test context",
			filePath: "/test/file.md",
			wantErr:  false,
		},
		{
			name: "invalid data should show file location and clean error",
			frontmatter: map[string]any{
				"name":    "test",
				"invalid": "value",
			},
			schema: `{
				"type": "object",
				"properties": {
					"name": {"type": "string"}
				},
				"additionalProperties": false
			}`,
			context:  "test context",
			filePath: "/test/file.md",
			wantErr:  true,
			errContains: []string{
				"/test/file.md:2:1:",
				"Unknown property: invalid",
			},
			errNotContains: []string{
				"contoso.com",
				"example.com",
				"http://",
			},
		},
		{
			name: "schema error without location should still work",
			frontmatter: map[string]any{
				"name":    "test",
				"invalid": "value",
			},
			schema: `{
				"type": "object",
				"properties": {
					"name": {"type": "string"}
				},
				"additionalProperties": false
			}`,
			context:  "test context",
			filePath: "", // No file path
			wantErr:  true,
			errContains: []string{
				"Unknown property: invalid",
			},
			errNotContains: []string{
				"contoso.com",
				"example.com",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWithSchemaAndLocation(tt.frontmatter, tt.schema, tt.context, tt.filePath)

			if tt.wantErr && err == nil {
				t.Errorf("validateWithSchemaAndLocation() expected error, got nil")
				return
			}

			if !tt.wantErr && err != nil {
				t.Errorf("validateWithSchemaAndLocation() error = %v", err)
				return
			}

			if tt.wantErr && err != nil {
				errorMsg := err.Error()

				// Check that expected strings are present
				for _, expected := range tt.errContains {
					if !strings.Contains(errorMsg, expected) {
						t.Errorf("validateWithSchemaAndLocation() error = %v, expected to contain %v", errorMsg, expected)
					}
				}

				// Check that unwanted strings are not present
				for _, unwanted := range tt.errNotContains {
					if strings.Contains(errorMsg, unwanted) {
						t.Errorf("validateWithSchemaAndLocation() error = %v, should not contain %v", errorMsg, unwanted)
					}
				}
			}
		})
	}
}

func TestSchemaURLDomainChange(t *testing.T) {
	// Test that the schema URL no longer uses example.com
	frontmatter := map[string]any{
		"invalid": "value",
	}

	err := validateWithSchema(frontmatter, `{
		"type": "object",
		"additionalProperties": false
	}`, "test")

	if err == nil {
		t.Fatal("Expected validation error")
	}

	errorMsg := err.Error()

	// Should not contain example.com
	if strings.Contains(errorMsg, "example.com") {
		t.Errorf("Error message should not contain 'example.com', got: %s", errorMsg)
	}

	// Should contain contoso.com (in the basic validation, before cleanup)
	if !strings.Contains(errorMsg, "contoso.com") {
		t.Errorf("Error message should contain 'contoso.com', got: %s", errorMsg)
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		filePath    string
		wantErr     bool
		errContains string
	}{
		{
			name: "valid workflow frontmatter",
			frontmatter: map[string]any{
				"on":     "push",
				"engine": "claude",
			},
			filePath: "/test/workflow.md",
			wantErr:  false,
		},
		{
			name: "invalid workflow frontmatter with location",
			frontmatter: map[string]any{
				"on":      "push",
				"invalid": "field",
			},
			filePath:    "/test/workflow.md",
			wantErr:     true,
			errContains: "/test/workflow.md:2:1:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(tt.frontmatter, tt.filePath)

			if tt.wantErr && err == nil {
				t.Errorf("ValidateMainWorkflowFrontmatterWithSchemaAndLocation() expected error, got nil")
				return
			}

			if !tt.wantErr && err != nil {
				t.Errorf("ValidateMainWorkflowFrontmatterWithSchemaAndLocation() error = %v", err)
				return
			}

			if tt.wantErr && err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ValidateMainWorkflowFrontmatterWithSchemaAndLocation() error = %v, expected to contain %v", err, tt.errContains)
				}
			}
		})
	}
}

func TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_AdditionalProperties(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		filePath    string
		wantErr     bool
		errContains string
	}{
		{
			name: "invalid permissions with additional property shows location",
			frontmatter: map[string]any{
				"on": "push",
				"permissions": map[string]any{
					"contents":     "read",
					"invalid_perm": "write",
				},
			},
			filePath:    "/test/workflow.md",
			wantErr:     true,
			errContains: "/test/workflow.md:2:1:",
		},
		{
			name: "invalid trigger with additional property shows location",
			frontmatter: map[string]any{
				"on": map[string]any{
					"push": map[string]any{
						"branches":     []string{"main"},
						"invalid_prop": "value",
					},
				},
			},
			filePath:    "/test/workflow.md",
			wantErr:     true,
			errContains: "/test/workflow.md:2:1:",
		},
		{
			name: "invalid tools configuration with additional property shows location",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"github": map[string]any{
						"allowed":      []string{"create_issue"},
						"invalid_prop": "value",
					},
				},
			},
			filePath:    "/test/workflow.md",
			wantErr:     true,
			errContains: "/test/workflow.md:2:1:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(tt.frontmatter, tt.filePath)

			if tt.wantErr && err == nil {
				t.Errorf("ValidateMainWorkflowFrontmatterWithSchemaAndLocation() expected error, got nil")
				return
			}

			if !tt.wantErr && err != nil {
				t.Errorf("ValidateMainWorkflowFrontmatterWithSchemaAndLocation() error = %v", err)
				return
			}

			if tt.wantErr && err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ValidateMainWorkflowFrontmatterWithSchemaAndLocation() error = %v, expected to contain %v", err, tt.errContains)
				}
			}
		})
	}
}
