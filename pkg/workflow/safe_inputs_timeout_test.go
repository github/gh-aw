//go:build !integration

package workflow

import (
	"encoding/json"
	"testing"
)

// TestSafeInputsTimeoutParsing tests that timeout is correctly parsed from frontmatter
func TestSafeInputsTimeoutParsing(t *testing.T) {
	tests := []struct {
		name            string
		frontmatter     map[string]any
		toolName        string
		expectedTimeout int
	}{
		{
			name: "default timeout when not specified",
			frontmatter: map[string]any{
				"safe-inputs": map[string]any{
					"test-tool": map[string]any{
						"description": "Test tool",
						"script":      "return 'hello';",
					},
				},
			},
			toolName:        "test-tool",
			expectedTimeout: 60, // Default timeout
		},
		{
			name: "explicit timeout as integer",
			frontmatter: map[string]any{
				"safe-inputs": map[string]any{
					"slow-tool": map[string]any{
						"description": "Slow tool",
						"script":      "return 'slow';",
						"timeout":     120,
					},
				},
			},
			toolName:        "slow-tool",
			expectedTimeout: 120,
		},
		{
			name: "explicit timeout as float",
			frontmatter: map[string]any{
				"safe-inputs": map[string]any{
					"fast-tool": map[string]any{
						"description": "Fast tool",
						"run":         "echo 'fast'",
						"timeout":     30.0,
					},
				},
			},
			toolName:        "fast-tool",
			expectedTimeout: 30,
		},
		{
			name: "timeout for shell script",
			frontmatter: map[string]any{
				"safe-inputs": map[string]any{
					"shell-tool": map[string]any{
						"description": "Shell tool",
						"run":         "sleep 5",
						"timeout":     10,
					},
				},
			},
			toolName:        "shell-tool",
			expectedTimeout: 10,
		},
		{
			name: "timeout for python script",
			frontmatter: map[string]any{
				"safe-inputs": map[string]any{
					"python-tool": map[string]any{
						"description": "Python tool",
						"py":          "print('hello')",
						"timeout":     45,
					},
				},
			},
			toolName:        "python-tool",
			expectedTimeout: 45,
		},
		{
			name: "timeout for go script",
			frontmatter: map[string]any{
				"safe-inputs": map[string]any{
					"go-tool": map[string]any{
						"description": "Go tool",
						"go":          "fmt.Println(\"hello\")",
						"timeout":     90,
					},
				},
			},
			toolName:        "go-tool",
			expectedTimeout: 90,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := ParseSafeInputs(tt.frontmatter)
			if config == nil {
				t.Fatalf("Expected config, got nil")
			}

			tool, exists := config.Tools[tt.toolName]
			if !exists {
				t.Fatalf("Expected tool %s to exist", tt.toolName)
			}

			if tool.Timeout != tt.expectedTimeout {
				t.Errorf("Expected timeout %d, got %d", tt.expectedTimeout, tool.Timeout)
			}
		})
	}
}

// TestSafeInputsTimeoutInJSON tests that timeout is included in the generated tools.json
func TestSafeInputsTimeoutInJSON(t *testing.T) {
	config := &SafeInputsConfig{
		Tools: map[string]*SafeInputToolConfig{
			"fast-tool": {
				Name:        "fast-tool",
				Description: "Fast tool",
				Script:      "return 'fast';",
				Timeout:     30,
			},
			"slow-tool": {
				Name:        "slow-tool",
				Description: "Slow tool",
				Run:         "echo 'slow'",
				Timeout:     120,
			},
			"default-tool": {
				Name:        "default-tool",
				Description: "Default timeout tool",
				Py:          "print('default')",
				Timeout:     60,
			},
			"go-tool": {
				Name:        "go-tool",
				Description: "Go timeout tool",
				Go:          "fmt.Println(\"hello\")",
				Timeout:     180,
			},
		},
	}

	jsonStr := generateSafeInputsToolsConfig(config)

	// Parse the JSON to verify structure
	var parsedConfig SafeInputsConfigJSON
	if err := json.Unmarshal([]byte(jsonStr), &parsedConfig); err != nil {
		t.Fatalf("Failed to parse generated JSON: %v", err)
	}

	// Verify timeouts are present
	toolTimeouts := make(map[string]int)
	for _, tool := range parsedConfig.Tools {
		toolTimeouts[tool.Name] = tool.Timeout
	}

	expected := map[string]int{
		"fast-tool":    30,
		"slow-tool":    120,
		"default-tool": 60,
		"go-tool":      180,
	}

	for toolName, expectedTimeout := range expected {
		actualTimeout, exists := toolTimeouts[toolName]
		if !exists {
			t.Errorf("Tool %s not found in generated JSON", toolName)
			continue
		}
		if actualTimeout != expectedTimeout {
			t.Errorf("Tool %s: expected timeout %d, got %d", toolName, expectedTimeout, actualTimeout)
		}
	}
}

// TestSafeInputsMergePreservesTimeout tests that timeout is preserved when merging configs
func TestSafeInputsMergePreservesTimeout(t *testing.T) {
	compiler := &Compiler{}

	// Main config with one tool
	main := &SafeInputsConfig{
		Tools: map[string]*SafeInputToolConfig{
			"main-tool": {
				Name:        "main-tool",
				Description: "Main tool",
				Script:      "return 'main';",
				Timeout:     90,
			},
		},
	}

	// Imported config with a different tool
	importedJSON := `{
		"imported-tool": {
			"description": "Imported tool",
			"run": "echo 'imported'",
			"timeout": 45
		}
	}`

	merged := compiler.mergeSafeInputs(main, []string{importedJSON})

	// Verify main tool timeout is preserved
	if merged.Tools["main-tool"].Timeout != 90 {
		t.Errorf("Expected main-tool timeout 90, got %d", merged.Tools["main-tool"].Timeout)
	}

	// Verify imported tool timeout is set
	if merged.Tools["imported-tool"].Timeout != 45 {
		t.Errorf("Expected imported-tool timeout 45, got %d", merged.Tools["imported-tool"].Timeout)
	}
}

// TestSafeInputsDefaultTimeoutWhenMerging tests that default timeout is used when not specified in imported config
func TestSafeInputsDefaultTimeoutWhenMerging(t *testing.T) {
	compiler := &Compiler{}

	main := &SafeInputsConfig{
		Tools: make(map[string]*SafeInputToolConfig),
	}

	// Imported config without timeout specified
	importedJSON := `{
		"imported-tool": {
			"description": "Imported tool without timeout",
			"script": "return 'imported';"
		}
	}`

	merged := compiler.mergeSafeInputs(main, []string{importedJSON})

	// Verify default timeout is used
	if merged.Tools["imported-tool"].Timeout != 60 {
		t.Errorf("Expected default timeout 60, got %d", merged.Tools["imported-tool"].Timeout)
	}
}
