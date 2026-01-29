//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestValidateStrictTools_SerenaLocalMode tests that serena local mode is rejected in strict mode
func TestValidateStrictTools_SerenaLocalMode(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"on": "push",
		"tools": map[string]any{
			"serena": map[string]any{
				"mode": "local",
				"languages": map[string]any{
					"go": map[string]any{},
				},
			},
		},
	}

	err := compiler.validateStrictTools(frontmatter)
	if err == nil {
		t.Error("Expected error for serena local mode in strict mode, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "serena tool with 'mode: local' is not allowed") {
		t.Errorf("Expected error about serena local mode, got: %v", err)
	}
}

// TestValidateStrictTools_SerenaDockerMode tests that serena docker mode is allowed in strict mode
func TestValidateStrictTools_SerenaDockerMode(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"on": "push",
		"tools": map[string]any{
			"serena": map[string]any{
				"mode": "docker",
				"languages": map[string]any{
					"go": map[string]any{},
				},
			},
		},
	}

	err := compiler.validateStrictTools(frontmatter)
	if err != nil {
		t.Errorf("Expected no error for serena docker mode in strict mode, got: %v", err)
	}
}

// TestValidateStrictTools_SerenaNoMode tests that serena without mode is allowed (defaults to docker)
func TestValidateStrictTools_SerenaNoMode(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"on": "push",
		"tools": map[string]any{
			"serena": map[string]any{
				"languages": map[string]any{
					"go": map[string]any{},
				},
			},
		},
	}

	err := compiler.validateStrictTools(frontmatter)
	if err != nil {
		t.Errorf("Expected no error for serena without mode in strict mode, got: %v", err)
	}
}

// TestValidateStrictTools_NoSerena tests that validation passes without serena
func TestValidateStrictTools_NoSerena(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"on": "push",
		"tools": map[string]any{
			"bash": []string{"*"},
		},
	}

	err := compiler.validateStrictTools(frontmatter)
	if err != nil {
		t.Errorf("Expected no error without serena tool, got: %v", err)
	}
}
