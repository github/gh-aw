//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestMultilineStringHandling tests that multiline strings in with parameters
// are correctly serialized with proper YAML indentation
func TestMultilineStringHandling(t *testing.T) {
	compiler := NewCompiler()

	testCases := []struct {
		name             string
		stepMap          map[string]any
		expectedYAML     string
		shouldContain    []string
		shouldNotContain []string
	}{
		{
			name: "multiline script in with parameters",
			stepMap: map[string]any{
				"name": "Test Script",
				"uses": "actions/github-script@ed597411d8f924073f98dfc5c65a23a2325f34cd",
				"with": map[string]any{
					"script": `const fs = require('fs');
const data = {
  key: "value",
  number: 42
};
console.log(data);`,
					"timeout": "30000",
				},
			},
			shouldContain: []string{
				"name: Test Script",
				"uses: actions/github-script@ed597411d8f924073f98dfc5c65a23a2325f34cd",
				"with:",
				"script: |-", // goccy/go-yaml uses |- (literal strip scalar)
				"  const fs = require('fs');",
				"  const data = {",
				"  console.log(data);",
				"timeout: \"30000\"", // goccy/go-yaml quotes numeric strings
			},
			shouldNotContain: []string{
				"script: const fs = require('fs');\\nconst data", // Should not have raw newlines
				"\\n", // Should not have literal \n in output
			},
		},
		{
			name: "simple single-line with parameters",
			stepMap: map[string]any{
				"name": "Simple Test",
				"uses": "actions/setup-node@395ad3262231945c25e8478fd5baf05154b1d79f",
				"with": map[string]any{
					"node-version": "18",
					"cache":        "npm",
				},
			},
			shouldContain: []string{
				"name: Simple Test",
				"uses: actions/setup-node@395ad3262231945c25e8478fd5baf05154b1d79f",
				"with:",
				"node-version: \"18\"", // goccy/go-yaml quotes numeric strings
				"cache: npm",
			},
		},
		{
			name: "multiline run command",
			stepMap: map[string]any{
				"name": "Multi-line run",
				"run": `echo "Starting build"
npm install
npm run build
echo "Build complete"`,
			},
			shouldContain: []string{
				"name: Multi-line run",
				"run: |-", // goccy/go-yaml uses |- (literal strip scalar)
				"  echo \"Starting build\"",
				"  npm install",
				"  npm run build",
				"  echo \"Build complete\"",
			},
			shouldNotContain: []string{
				"run: echo \"Starting build\"\\n", // Should not have raw newlines
			},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compiler.convertStepToYAML(tt.stepMap)
			if err != nil {
				t.Fatalf("convertStepToYAML failed: %v", err)
			}

			t.Logf("Generated YAML:\n%s", result)

			// Check that required strings are present
			for _, required := range tt.shouldContain {
				if !strings.Contains(result, required) {
					t.Errorf("Expected YAML to contain '%s', but it didn't.\nGenerated:\n%s", required, result)
				}
			}

			// Check that forbidden strings are not present
			for _, forbidden := range tt.shouldNotContain {
				if strings.Contains(result, forbidden) {
					t.Errorf("Expected YAML to NOT contain '%s', but it did.\nGenerated:\n%s", forbidden, result)
				}
			}
		})
	}
}

// TestEngineStepSerialization tests that engine-specific step serialization
// also handles multiline strings correctly
func TestEngineStepSerialization(t *testing.T) {
	engines := []struct {
		name   string
		engine CodingAgentEngine
	}{
		{"Claude", NewClaudeEngine()},
		{"Codex", NewCodexEngine()},
		{"Custom", NewCustomEngine()},
	}

	stepMap := map[string]any{
		"name": "Test multiline in engine",
		"uses": "actions/github-script@ed597411d8f924073f98dfc5c65a23a2325f34cd",
		"with": map[string]any{
			"script": `const multiline = 'hello';
This is a multiline
template literal with
proper indentation
console.log(multiline);`,
		},
	}

	for _, tt := range engines {
		t.Run(tt.name, func(t *testing.T) {
			// Since engines have their own convertStepToYAML methods,
			// we need to test them through their interface
			// For now, just verify they don't panic and produce some output

			// We'll use reflection or type assertion to call the method
			// This is a bit hacky but necessary for testing the engines directly
			switch engine := tt.engine.(type) {
			case *ClaudeEngine:
				result, err := engine.convertStepToYAML(stepMap)
				if err != nil {
					t.Fatalf("Claude engine convertStepToYAML failed: %v", err)
				}
				if !strings.Contains(result, "script:") {
					t.Errorf("Claude engine output should contain 'script:', got:\n%s", result)
				}
				t.Logf("Claude engine output:\n%s", result)

			case *CodexEngine:
				result, err := engine.convertStepToYAML(stepMap)
				if err != nil {
					t.Fatalf("Codex engine convertStepToYAML failed: %v", err)
				}
				if !strings.Contains(result, "script:") {
					t.Errorf("Codex engine output should contain 'script:', got:\n%s", result)
				}
				t.Logf("Codex engine output:\n%s", result)

			case *CustomEngine:
				result, err := engine.convertStepToYAML(stepMap)
				if err != nil {
					t.Fatalf("Custom engine convertStepToYAML failed: %v", err)
				}
				if !strings.Contains(result, "script:") {
					t.Errorf("Custom engine output should contain 'script:', got:\n%s", result)
				}
				t.Logf("Custom engine output:\n%s", result)
			}
		})
	}
}
