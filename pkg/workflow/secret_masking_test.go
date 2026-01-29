//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestExtractSecretMaskingConfig(t *testing.T) {
	tests := []struct {
		name              string
		frontmatter       map[string]any
		expectedStepCount int
		expectedStepName  string
	}{
		{
			name: "basic secret-masking with steps",
			frontmatter: map[string]any{
				"secret-masking": map[string]any{
					"steps": []any{
						map[string]any{
							"name": "Test step",
							"run":  "echo test",
						},
					},
				},
			},
			expectedStepCount: 1,
			expectedStepName:  "Test step",
		},
		{
			name: "multiple secret-masking steps",
			frontmatter: map[string]any{
				"secret-masking": map[string]any{
					"steps": []any{
						map[string]any{
							"name": "Step 1",
							"run":  "echo step1",
						},
						map[string]any{
							"name": "Step 2",
							"run":  "echo step2",
						},
					},
				},
			},
			expectedStepCount: 2,
			expectedStepName:  "Step 1",
		},
		{
			name:              "no secret-masking field",
			frontmatter:       map[string]any{},
			expectedStepCount: 0,
		},
		{
			name: "empty secret-masking steps",
			frontmatter: map[string]any{
				"secret-masking": map[string]any{
					"steps": []any{},
				},
			},
			expectedStepCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCompiler()
			config := c.extractSecretMaskingConfig(tt.frontmatter)

			if tt.expectedStepCount == 0 {
				if config != nil {
					t.Errorf("Expected nil config but got %+v", config)
				}
				return
			}

			if config == nil {
				t.Errorf("Expected config but got nil")
				return
			}

			if len(config.Steps) != tt.expectedStepCount {
				t.Errorf("Expected %d steps but got %d", tt.expectedStepCount, len(config.Steps))
				return
			}

			if tt.expectedStepName != "" {
				if name, ok := config.Steps[0]["name"].(string); ok {
					if name != tt.expectedStepName {
						t.Errorf("Expected step name %q but got %q", tt.expectedStepName, name)
					}
				} else {
					t.Errorf("Expected step name %q but got nil", tt.expectedStepName)
				}
			}
		})
	}
}

func TestMergeSecretMasking(t *testing.T) {
	tests := []struct {
		name              string
		topConfig         *SecretMaskingConfig
		importedJSON      string
		expectedStepCount int
	}{
		{
			name:              "merge with nil top config",
			topConfig:         nil,
			importedJSON:      `{"steps":[{"name":"Imported step","run":"echo imported"}]}`,
			expectedStepCount: 1,
		},
		{
			name: "merge with existing top config",
			topConfig: &SecretMaskingConfig{
				Steps: []map[string]any{
					{"name": "Top step", "run": "echo top"},
				},
			},
			importedJSON:      `{"steps":[{"name":"Imported step","run":"echo imported"}]}`,
			expectedStepCount: 2,
		},
		{
			name:      "merge multiple imports",
			topConfig: nil,
			importedJSON: `{"steps":[{"name":"Step 1","run":"echo 1"}]}
{"steps":[{"name":"Step 2","run":"echo 2"}]}`,
			expectedStepCount: 2,
		},
		{
			name:              "merge with empty import",
			topConfig:         nil,
			importedJSON:      "",
			expectedStepCount: 0,
		},
		{
			name:              "merge with empty object",
			topConfig:         nil,
			importedJSON:      "{}",
			expectedStepCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCompiler()
			result, err := c.MergeSecretMasking(tt.topConfig, tt.importedJSON)

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if tt.expectedStepCount == 0 {
				if result != nil {
					t.Errorf("Expected nil result but got %+v", result)
				}
				return
			}

			if result == nil {
				t.Errorf("Expected result but got nil")
				return
			}

			if len(result.Steps) != tt.expectedStepCount {
				t.Errorf("Expected %d steps but got %d", tt.expectedStepCount, len(result.Steps))
			}
		})
	}
}

func TestGenerateCustomSecretMaskingStep(t *testing.T) {
	tests := []struct {
		name                string
		step                map[string]any
		expectedContains    []string
		expectedNotContains []string
	}{
		{
			name: "simple step with run command",
			step: map[string]any{
				"name": "Test step",
				"run":  "echo test",
			},
			expectedContains: []string{
				"- name: Test step",
				"run: echo test",
			},
		},
		{
			name: "step with multi-line run command",
			step: map[string]any{
				"name": "Multi-line step",
				"run":  "echo line1\necho line2\necho line3",
			},
			expectedContains: []string{
				"- name: Multi-line step",
				"run: |",
				"echo line1",
				"echo line2",
				"echo line3",
			},
		},
		{
			name: "step with if condition",
			step: map[string]any{
				"name": "Conditional step",
				"if":   "always()",
				"run":  "echo conditional",
			},
			expectedContains: []string{
				"- name: Conditional step",
				"if: always()",
				"run: echo conditional",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCompiler()
			c.stepOrderTracker = NewStepOrderTracker()
			var yaml strings.Builder

			data := &WorkflowData{}
			c.generateCustomSecretMaskingStep(&yaml, tt.step, data)

			result := yaml.String()

			for _, expected := range tt.expectedContains {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected YAML to contain %q but it didn't.\nGenerated YAML:\n%s", expected, result)
				}
			}

			for _, notExpected := range tt.expectedNotContains {
				if strings.Contains(result, notExpected) {
					t.Errorf("Expected YAML not to contain %q but it did.\nGenerated YAML:\n%s", notExpected, result)
				}
			}
		})
	}
}

func TestSecretMaskingIntegration(t *testing.T) {
	// Create a test workflow with secret-masking
	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"secret-masking": map[string]any{
			"steps": []any{
				map[string]any{
					"name": "Redact custom pattern",
					"run":  "find /tmp/gh-aw -type f -exec sed -i 's/secret123/REDACTED/g' {} +",
				},
			},
		},
	}

	c := NewCompiler()
	c.SetSkipValidation(true)

	config := c.extractSecretMaskingConfig(frontmatter)
	if config == nil {
		t.Fatal("Expected secret masking config but got nil")
	}

	if len(config.Steps) != 1 {
		t.Errorf("Expected 1 step but got %d", len(config.Steps))
	}

	if name, ok := config.Steps[0]["name"].(string); ok {
		if name != "Redact custom pattern" {
			t.Errorf("Expected step name 'Redact custom pattern' but got %q", name)
		}
	} else {
		t.Error("Expected step name but got nil")
	}
}
