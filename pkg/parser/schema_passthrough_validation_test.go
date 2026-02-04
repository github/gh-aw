//go:build !integration

package parser

import (
	"strings"
	"testing"
)

// TestPassThroughFieldValidation tests that pass-through YAML fields
// (concurrency, container, environment, env, secrets, runs-on, services) are
// properly validated by the schema during frontmatter parsing.
//
// These fields are "pass-through" in that they are extracted from
// frontmatter and passed directly to GitHub Actions without modification,
// but they still need basic structure validation to catch obvious errors
// at compile time rather than at GitHub Actions runtime.
func TestPassThroughFieldValidation(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		wantErr     bool
		errContains string
	}{
		// Concurrency field tests
		{
			name: "valid concurrency - simple string",
			frontmatter: map[string]any{
				"on":          "push",
				"concurrency": "my-group",
			},
			wantErr: false,
		},
		{
			name: "valid concurrency - object with group",
			frontmatter: map[string]any{
				"on": "push",
				"concurrency": map[string]any{
					"group": "my-group",
				},
			},
			wantErr: false,
		},
		{
			name: "valid concurrency - object with group and cancel-in-progress",
			frontmatter: map[string]any{
				"on": "push",
				"concurrency": map[string]any{
					"group":              "my-group",
					"cancel-in-progress": true,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid concurrency - array",
			frontmatter: map[string]any{
				"on":          "push",
				"concurrency": []string{"invalid"},
			},
			wantErr:     true,
			errContains: "oneOf",
		},
		{
			name: "invalid concurrency - object missing required group",
			frontmatter: map[string]any{
				"on": "push",
				"concurrency": map[string]any{
					"cancel-in-progress": true,
				},
			},
			wantErr:     true,
			errContains: "missing property 'group'",
		},
		{
			name: "invalid concurrency - object with invalid field",
			frontmatter: map[string]any{
				"on": "push",
				"concurrency": map[string]any{
					"group":   "my-group",
					"invalid": "field",
				},
			},
			wantErr:     true,
			errContains: "additional properties 'invalid' not allowed",
		},

		// Container field tests
		{
			name: "valid container - simple string",
			frontmatter: map[string]any{
				"on":        "push",
				"container": "ubuntu:latest",
			},
			wantErr: false,
		},
		{
			name: "valid container - object with image",
			frontmatter: map[string]any{
				"on": "push",
				"container": map[string]any{
					"image": "ubuntu:latest",
				},
			},
			wantErr: false,
		},
		{
			name: "valid container - object with image and credentials",
			frontmatter: map[string]any{
				"on": "push",
				"container": map[string]any{
					"image": "ubuntu:latest",
					"credentials": map[string]any{
						"username": "user",
						"password": "${{ secrets.PASSWORD }}",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid container - array",
			frontmatter: map[string]any{
				"on":        "push",
				"container": []string{"invalid"},
			},
			wantErr:     true,
			errContains: "got array",
		},
		{
			name: "invalid container - object missing required image",
			frontmatter: map[string]any{
				"on": "push",
				"container": map[string]any{
					"env": map[string]string{"TEST": "value"},
				},
			},
			wantErr:     true,
			errContains: "missing property 'image'",
		},
		{
			name: "invalid container - object with invalid field",
			frontmatter: map[string]any{
				"on": "push",
				"container": map[string]any{
					"image":   "ubuntu:latest",
					"invalid": "field",
				},
			},
			wantErr:     true,
			errContains: "additional properties 'invalid' not allowed",
		},

		// Environment field tests
		{
			name: "valid environment - simple string",
			frontmatter: map[string]any{
				"on":          "push",
				"environment": "production",
			},
			wantErr: false,
		},
		{
			name: "valid environment - object with name",
			frontmatter: map[string]any{
				"on": "push",
				"environment": map[string]any{
					"name": "production",
				},
			},
			wantErr: false,
		},
		{
			name: "valid environment - object with name and url",
			frontmatter: map[string]any{
				"on": "push",
				"environment": map[string]any{
					"name": "production",
					"url":  "https://prod.example.com",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid environment - array",
			frontmatter: map[string]any{
				"on":          "push",
				"environment": []string{"invalid"},
			},
			wantErr:     true,
			errContains: "oneOf",
		},
		{
			name: "invalid environment - object missing required name",
			frontmatter: map[string]any{
				"on": "push",
				"environment": map[string]any{
					"url": "https://prod.example.com",
				},
			},
			wantErr:     true,
			errContains: "missing property 'name'",
		},
		{
			name: "invalid environment - object with invalid field",
			frontmatter: map[string]any{
				"on": "push",
				"environment": map[string]any{
					"name":    "production",
					"invalid": "field",
				},
			},
			wantErr:     true,
			errContains: "additional properties 'invalid' not allowed",
		},

		// Env field tests
		{
			name: "valid env - object with string values",
			frontmatter: map[string]any{
				"on": "push",
				"env": map[string]any{
					"NODE_ENV": "production",
					"API_KEY":  "${{ secrets.API_KEY }}",
				},
			},
			wantErr: false,
		},
		{
			name: "valid env - string (pass-through)",
			frontmatter: map[string]any{
				"on":  "push",
				"env": "some-string",
			},
			wantErr: false,
		},
		{
			name: "invalid env - array",
			frontmatter: map[string]any{
				"on":  "push",
				"env": []string{"invalid"},
			},
			wantErr:     true,
			errContains: "oneOf",
		},

		// Secrets field tests
		{
			name: "valid secrets - simple string values",
			frontmatter: map[string]any{
				"on": "push",
				"secrets": map[string]any{
					"API_TOKEN":    "${{ secrets.API_TOKEN }}",
					"DATABASE_URL": "${{ secrets.DB_URL }}",
				},
			},
			wantErr: false,
		},
		{
			name: "valid secrets - object with value and description",
			frontmatter: map[string]any{
				"on": "push",
				"secrets": map[string]any{
					"API_TOKEN": map[string]any{
						"value":       "${{ secrets.API_TOKEN }}",
						"description": "API token for external service",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid secrets - mixed simple and object values",
			frontmatter: map[string]any{
				"on": "push",
				"secrets": map[string]any{
					"API_TOKEN": "${{ secrets.API_TOKEN }}",
					"DB_URL": map[string]any{
						"value":       "${{ secrets.DB_URL }}",
						"description": "Database connection string",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid secrets - object missing required value field",
			frontmatter: map[string]any{
				"on": "push",
				"secrets": map[string]any{
					"API_TOKEN": map[string]any{
						"description": "Missing value field",
					},
				},
			},
			wantErr:     true,
			errContains: "missing property 'value'",
		},
		{
			name: "invalid secrets - object with additional properties",
			frontmatter: map[string]any{
				"on": "push",
				"secrets": map[string]any{
					"API_TOKEN": map[string]any{
						"value":   "${{ secrets.API_TOKEN }}",
						"invalid": "field",
					},
				},
			},
			wantErr:     true,
			errContains: "additional properties 'invalid' not allowed",
		},
		{
			name: "invalid secrets - array",
			frontmatter: map[string]any{
				"on":      "push",
				"secrets": []string{"invalid"},
			},
			wantErr:     true,
			errContains: "got array, want object",
		},
		{
			name: "invalid secrets - non-string, non-object value",
			frontmatter: map[string]any{
				"on": "push",
				"secrets": map[string]any{
					"API_TOKEN": 123,
				},
			},
			wantErr:     true,
			errContains: "oneOf",
		},

		// Runs-on field tests
		{
			name: "valid runs-on - simple string",
			frontmatter: map[string]any{
				"on":      "push",
				"runs-on": "ubuntu-latest",
			},
			wantErr: false,
		},
		{
			name: "valid runs-on - array of strings",
			frontmatter: map[string]any{
				"on":      "push",
				"runs-on": []string{"ubuntu-latest", "self-hosted"},
			},
			wantErr: false,
		},
		{
			name: "valid runs-on - object with labels",
			frontmatter: map[string]any{
				"on": "push",
				"runs-on": map[string]any{
					"labels": []string{"ubuntu-latest"},
				},
			},
			wantErr: false,
		},
		{
			name: "valid runs-on - object with group and labels",
			frontmatter: map[string]any{
				"on": "push",
				"runs-on": map[string]any{
					"group":  "larger-runners",
					"labels": []string{"ubuntu-latest-8-cores"},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid runs-on - number",
			frontmatter: map[string]any{
				"on":      "push",
				"runs-on": 123,
			},
			wantErr:     true,
			errContains: "oneOf",
		},
		{
			name: "invalid runs-on - object with invalid field",
			frontmatter: map[string]any{
				"on": "push",
				"runs-on": map[string]any{
					"labels":  []string{"ubuntu-latest"},
					"invalid": "field",
				},
			},
			wantErr:     true,
			errContains: "additional properties 'invalid' not allowed",
		},

		// Services field tests
		{
			name: "valid services - object with service names",
			frontmatter: map[string]any{
				"on": "push",
				"services": map[string]any{
					"postgres": "postgres:14",
				},
			},
			wantErr: false,
		},
		{
			name: "valid services - object with service configuration",
			frontmatter: map[string]any{
				"on": "push",
				"services": map[string]any{
					"postgres": map[string]any{
						"image": "postgres:14",
						"env": map[string]any{
							"POSTGRES_PASSWORD": "${{ secrets.DB_PASSWORD }}",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid services - string",
			frontmatter: map[string]any{
				"on":       "push",
				"services": "invalid",
			},
			wantErr:     true,
			errContains: "got string, want object",
		},
		{
			name: "invalid services - array",
			frontmatter: map[string]any{
				"on":       "push",
				"services": []string{"invalid"},
			},
			wantErr:     true,
			errContains: "got array, want object",
		},
		{
			name: "invalid services - service object missing required image",
			frontmatter: map[string]any{
				"on": "push",
				"services": map[string]any{
					"postgres": map[string]any{
						"env": map[string]any{
							"POSTGRES_PASSWORD": "secret",
						},
					},
				},
			},
			wantErr:     true,
			errContains: "missing property 'image'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMainWorkflowFrontmatterWithSchema(tt.frontmatter)

			if tt.wantErr && err == nil {
				t.Errorf("Expected error but got none")
				return
			}

			if !tt.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if tt.wantErr && err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Error = %v, expected to contain %q", err, tt.errContains)
				}
			}
		})
	}
}

// TestPassThroughFieldEdgeCases tests additional edge cases for pass-through fields
func TestPassThroughFieldEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		wantErr     bool
		errContains string
	}{
		{
			name: "concurrency with expression in group",
			frontmatter: map[string]any{
				"on": "push",
				"concurrency": map[string]any{
					"group": "workflow-${{ github.ref }}",
				},
			},
			wantErr: false,
		},
		{
			name: "runs-on with empty labels array is valid",
			frontmatter: map[string]any{
				"on": "push",
				"runs-on": map[string]any{
					"labels": []string{},
				},
			},
			wantErr: false,
		},
		{
			name: "container with all optional fields",
			frontmatter: map[string]any{
				"on": "push",
				"container": map[string]any{
					"image": "ubuntu:latest",
					"env": map[string]any{
						"TEST": "value",
					},
					"ports":   []any{8080, "9090"},
					"volumes": []string{"/tmp:/tmp"},
					"options": "--cpus 1",
				},
			},
			wantErr: false,
		},
		{
			name: "environment with expression in url",
			frontmatter: map[string]any{
				"on": "push",
				"environment": map[string]any{
					"name": "staging",
					"url":  "${{ steps.deploy.outputs.url }}",
				},
			},
			wantErr: false,
		},
		{
			name: "services with credentials",
			frontmatter: map[string]any{
				"on": "push",
				"services": map[string]any{
					"redis": map[string]any{
						"image": "redis:alpine",
						"credentials": map[string]any{
							"username": "user",
							"password": "${{ secrets.PASSWORD }}",
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMainWorkflowFrontmatterWithSchema(tt.frontmatter)

			if tt.wantErr && err == nil {
				t.Errorf("Expected error but got none")
				return
			}

			if !tt.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if tt.wantErr && err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Error = %v, expected to contain %q", err, tt.errContains)
				}
			}
		})
	}
}
