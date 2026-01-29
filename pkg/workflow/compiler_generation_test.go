//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// Test convertStepToYAML function
func TestConvertStepToYAML(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name     string
		stepMap  map[string]any
		expected string
		hasError bool
	}{
		{
			name: "step with name only",
			stepMap: map[string]any{
				"name": "Test Step",
			},
			expected: "      - name: Test Step\n",
			hasError: false,
		},
		{
			name: "step with name and uses",
			stepMap: map[string]any{
				"name": "Checkout Code",
				"uses": "actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd",
			},
			expected: "      - name: Checkout Code\n        uses: actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd\n",
			hasError: false,
		},
		{
			name: "step with name and run command",
			stepMap: map[string]any{
				"name": "Run Tests",
				"run":  "go test ./...",
			},
			expected: "      - name: Run Tests\n        run: go test ./...\n",
			hasError: false,
		},
		{
			name: "step with name, run command and env variables",
			stepMap: map[string]any{
				"name": "Build Project",
				"run":  "make build",
				"env": map[string]string{
					"GO_VERSION": "1.21",
					"ENV":        "test",
				},
			},
			expected: "      - name: Build Project\n        run: make build\n        env:\n          ENV: test\n          GO_VERSION: \"1.21\"\n",
			hasError: false,
		},
		{
			name: "step with working-directory",
			stepMap: map[string]any{
				"name":              "Test in Subdirectory",
				"run":               "npm test",
				"working-directory": "./frontend",
			},
			expected: "      - name: Test in Subdirectory\n        run: npm test\n        working-directory: ./frontend\n",
			hasError: false,
		},
		{
			name: "step with complex with parameters",
			stepMap: map[string]any{
				"name": "Setup Node",
				"uses": "actions/setup-node@395ad3262231945c25e8478fd5baf05154b1d79f",
				"with": map[string]any{
					"node-version": "18",
					"cache":        "npm",
				},
			},
			expected: "      - name: Setup Node\n        uses: actions/setup-node@395ad3262231945c25e8478fd5baf05154b1d79f\n        with:\n          cache: npm\n          node-version: \"18\"\n",
			hasError: false,
		},
		{
			name:     "empty step map",
			stepMap:  map[string]any{},
			expected: "",
			hasError: false,
		},
		{
			name: "step without name",
			stepMap: map[string]any{
				"run": "echo 'no name'",
			},
			expected: "        run: echo 'no name'\n",
			hasError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compiler.convertStepToYAML(tt.stepMap)

			if tt.hasError {
				if err == nil {
					t.Errorf("convertStepToYAML() expected error, but got none")
				}
			} else {
				if err != nil {
					t.Errorf("convertStepToYAML() unexpected error: %v", err)
				}

				if !strings.Contains(result, strings.TrimSpace(strings.Split(tt.expected, "\n")[0])) {
					t.Errorf("convertStepToYAML() result doesn't contain expected content\nGot: %q\nExpected to contain: %q",
						result, tt.expected)
				}
			}
		})
	}
}
