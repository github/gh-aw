//go:build !integration

package workflow

import (
	"testing"
)

// TestEngineVersionTypeHandling tests that engine.version correctly handles
// numeric types (int, float) and string types as specified in the schema.
// This is a regression test to prevent type handling inconsistencies.
func TestEngineVersionTypeHandling(t *testing.T) {
	tests := []struct {
		name            string
		frontmatter     map[string]any
		expectedVersion string
	}{
		{
			name: "string version",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":      "copilot",
					"version": "beta",
				},
			},
			expectedVersion: "beta",
		},
		{
			name: "integer version",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":      "copilot",
					"version": 20,
				},
			},
			expectedVersion: "20",
		},
		{
			name: "float version",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":      "copilot",
					"version": 3.11,
				},
			},
			expectedVersion: "3.11",
		},
		{
			name: "int64 version",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":      "copilot",
					"version": int64(142),
				},
			},
			expectedVersion: "142",
		},
		{
			name: "uint64 version",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":      "copilot",
					"version": uint64(999),
				},
			},
			expectedVersion: "999",
		},
		{
			name: "version with semantic versioning",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":      "copilot",
					"version": "v1.2.3",
				},
			},
			expectedVersion: "v1.2.3",
		},
		{
			name: "version with build metadata",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":      "copilot",
					"version": "1.0.0-beta.1+build.123",
				},
			},
			expectedVersion: "1.0.0-beta.1+build.123",
		},
	}

	compiler := NewCompiler()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, config := compiler.ExtractEngineConfig(tt.frontmatter)

			if config == nil {
				t.Fatal("Expected config to be non-nil")
			}

			if config.Version != tt.expectedVersion {
				t.Errorf("Expected version %q, got %q", tt.expectedVersion, config.Version)
			}
		})
	}
}

// TestEngineVersionNotProvided tests that when no version is provided,
// the Version field remains empty (default behavior).
func TestEngineVersionNotProvided(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
	}{
		{
			name: "engine without version field",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id": "copilot",
				},
			},
		},
		{
			name: "engine as string (backward compatibility)",
			frontmatter: map[string]any{
				"engine": "copilot",
			},
		},
	}

	compiler := NewCompiler()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, config := compiler.ExtractEngineConfig(tt.frontmatter)

			if config == nil {
				t.Fatal("Expected config to be non-nil")
			}

			if config.Version != "" {
				t.Errorf("Expected empty version, got %q", config.Version)
			}
		})
	}
}

// TestEngineVersionWithOtherFields tests that version works correctly
// alongside other engine configuration fields.
func TestEngineVersionWithOtherFields(t *testing.T) {
	frontmatter := map[string]any{
		"engine": map[string]any{
			"id":      "copilot",
			"version": "0.0.369",
			"model":   "gpt-4",
			"env": map[string]any{
				"DEBUG": "true",
			},
		},
	}

	compiler := NewCompiler()
	_, config := compiler.ExtractEngineConfig(frontmatter)

	if config == nil {
		t.Fatal("Expected config to be non-nil")
	}

	if config.ID != "copilot" {
		t.Errorf("Expected ID 'copilot', got %q", config.ID)
	}

	if config.Version != "0.0.369" {
		t.Errorf("Expected version '0.0.369', got %q", config.Version)
	}

	if config.Model != "gpt-4" {
		t.Errorf("Expected model 'gpt-4', got %q", config.Model)
	}

	if len(config.Env) != 1 {
		t.Errorf("Expected 1 env var, got %d", len(config.Env))
	}

	if config.Env["DEBUG"] != "true" {
		t.Errorf("Expected DEBUG='true', got %q", config.Env["DEBUG"])
	}
}

// TestEngineCommandField tests that the command field is correctly extracted
func TestEngineCommandField(t *testing.T) {
	tests := []struct {
		name            string
		frontmatter     map[string]any
		expectedCommand string
	}{
		{
			name: "command field provided",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":      "copilot",
					"command": "/usr/local/bin/custom-copilot",
				},
			},
			expectedCommand: "/usr/local/bin/custom-copilot",
		},
		{
			name: "command field not provided",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id": "copilot",
				},
			},
			expectedCommand: "",
		},
		{
			name: "command with relative path",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":      "claude",
					"command": "./bin/claude-cli",
				},
			},
			expectedCommand: "./bin/claude-cli",
		},
		{
			name: "command with environment variable",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":      "codex",
					"command": "$HOME/.local/bin/codex",
				},
			},
			expectedCommand: "$HOME/.local/bin/codex",
		},
	}

	compiler := NewCompiler()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, config := compiler.ExtractEngineConfig(tt.frontmatter)

			if config == nil {
				t.Fatal("Expected config to be non-nil")
			}

			if config.Command != tt.expectedCommand {
				t.Errorf("Expected command %q, got %q", tt.expectedCommand, config.Command)
			}
		})
	}
}
