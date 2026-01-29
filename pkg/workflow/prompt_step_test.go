//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestAppendPromptStep(t *testing.T) {
	tests := []struct {
		name      string
		stepName  string
		condition string
		wantSteps []string
	}{
		{
			name:      "basic step without condition",
			stepName:  "Append test instructions to prompt",
			condition: "",
			wantSteps: []string{
				"- name: Append test instructions to prompt",
				"env:",
				"GH_AW_PROMPT: /tmp/gh-aw/aw-prompts/prompt.txt",
				"run: |",
				`cat << 'PROMPT_EOF' >> "$GH_AW_PROMPT"`,
				"Test prompt content",
				"PROMPT_EOF",
			},
		},
		{
			name:      "step with condition",
			stepName:  "Append conditional instructions to prompt",
			condition: "github.event.issue != null",
			wantSteps: []string{
				"- name: Append conditional instructions to prompt",
				"if: github.event.issue != null",
				"env:",
				"GH_AW_PROMPT: /tmp/gh-aw/aw-prompts/prompt.txt",
				"run: |",
				`cat << 'PROMPT_EOF' >> "$GH_AW_PROMPT"`,
				"Conditional prompt content",
				"PROMPT_EOF",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var yaml strings.Builder

			// Call the helper with a simple renderer
			var promptContent string
			if tt.condition == "" {
				promptContent = "Test prompt content"
			} else {
				promptContent = "Conditional prompt content"
			}

			appendPromptStep(&yaml, tt.stepName, func(y *strings.Builder, indent string) {
				WritePromptTextToYAML(y, promptContent, indent)
			}, tt.condition, "          ")

			result := yaml.String()

			// Check that all expected strings are present
			for _, want := range tt.wantSteps {
				if !strings.Contains(result, want) {
					t.Errorf("Expected output to contain %q, but it didn't.\nGot:\n%s", want, result)
				}
			}
		})
	}
}

func TestAppendPromptStepWithHeredoc(t *testing.T) {
	tests := []struct {
		name      string
		stepName  string
		content   string
		wantSteps []string
	}{
		{
			name:     "basic heredoc step",
			stepName: "Append structured data to prompt",
			content:  "Structured content line 1\nStructured content line 2",
			wantSteps: []string{
				"- name: Append structured data to prompt",
				"env:",
				"GH_AW_PROMPT: /tmp/gh-aw/aw-prompts/prompt.txt",
				"run: |",
				`cat << 'PROMPT_EOF' >> "$GH_AW_PROMPT"`,
				"Structured content line 1",
				"Structured content line 2",
				"PROMPT_EOF",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var yaml strings.Builder

			appendPromptStepWithHeredoc(&yaml, tt.stepName, func(y *strings.Builder) {
				y.WriteString(tt.content)
			})

			result := yaml.String()

			// Check that all expected strings are present
			for _, want := range tt.wantSteps {
				if !strings.Contains(result, want) {
					t.Errorf("Expected output to contain %q, but it didn't.\nGot:\n%s", want, result)
				}
			}
		})
	}
}

func TestPromptStepRefactoringConsistency(t *testing.T) {
	// Test that the unified prompt step includes temp folder instructions
	// (Previously tested individual prompt steps, now tests unified approach)

	t.Run("unified_prompt_step includes temp_folder", func(t *testing.T) {
		var yaml strings.Builder
		compiler := &Compiler{}
		data := &WorkflowData{
			ParsedTools: NewTools(map[string]any{}),
		}
		compiler.generateUnifiedPromptStep(&yaml, data)

		result := yaml.String()

		// Verify key elements are present
		if !strings.Contains(result, "Create prompt with built-in context") {
			t.Error("Expected unified step name not found")
		}
		if !strings.Contains(result, "GH_AW_PROMPT: /tmp/gh-aw/aw-prompts/prompt.txt") {
			t.Error("Expected GH_AW_PROMPT env variable not found")
		}
		// Verify temp folder instructions are included
		if !strings.Contains(result, `cat "/opt/gh-aw/prompts/temp_folder_prompt.md" >> "$GH_AW_PROMPT"`) {
			t.Error("Expected cat command for temp folder prompt file not found")
		}
	})
}
