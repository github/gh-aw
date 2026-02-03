//go:build !integration

package workflow

import (
	"fmt"
	"strings"
	"testing"
)

func TestGenerateSecretValidationStep(t *testing.T) {
	tests := []struct {
		name        string
		secretName  string
		engineName  string
		docsURL     string
		wantStrings []string
	}{
		{
			name:       "ANTHROPIC_API_KEY validation",
			secretName: "ANTHROPIC_API_KEY",
			engineName: "Claude Code",
			docsURL:    "https://github.github.com/gh-aw/reference/engines/#anthropic-claude-code",
			wantStrings: []string{
				"Validate ANTHROPIC_API_KEY secret",
				"Error: ANTHROPIC_API_KEY secret is not set",
				"The Claude Code engine requires the ANTHROPIC_API_KEY secret to be configured",
				"Please configure this secret in your repository settings",
				"Documentation: https://github.github.com/gh-aw/reference/engines/#anthropic-claude-code",
				"<details>",
				"<summary>Agent Environment Validation</summary>",
				"✅ ANTHROPIC_API_KEY: Configured",
				"</details>",
				"ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := GenerateSecretValidationStep(tt.secretName, tt.engineName, tt.docsURL)
			stepContent := strings.Join(step, "\n")

			for _, want := range tt.wantStrings {
				if !strings.Contains(stepContent, want) {
					t.Errorf("GenerateSecretValidationStep() missing expected string:\nwant: %s\ngot: %s", want, stepContent)
				}
			}

			// Verify it has a run block
			if !strings.Contains(stepContent, "run: |") {
				t.Error("Expected step to have 'run: |' block")
			}

			// Verify it has an env section
			if !strings.Contains(stepContent, "env:") {
				t.Error("Expected step to have 'env:' section")
			}

			// Verify it exits with code 1 on failure
			if !strings.Contains(stepContent, "exit 1") {
				t.Error("Expected step to exit with code 1 on validation failure")
			}
		})
	}
}

func TestGenerateMultiSecretValidationStep(t *testing.T) {
	tests := []struct {
		name        string
		secretNames []string
		engineName  string
		docsURL     string
		wantStrings []string
	}{
		{
			name:        "Codex dual secret validation",
			secretNames: []string{"CODEX_API_KEY", "OPENAI_API_KEY"},
			engineName:  "Codex",
			docsURL:     "https://github.github.com/gh-aw/reference/engines/#openai-codex",
			wantStrings: []string{
				"Validate CODEX_API_KEY or OPENAI_API_KEY secret",
				"run: /opt/gh-aw/actions/validate_multi_secret.sh CODEX_API_KEY OPENAI_API_KEY Codex https://github.github.com/gh-aw/reference/engines/#openai-codex",
				"CODEX_API_KEY: ${{ secrets.CODEX_API_KEY }}",
				"OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}",
			},
		},
		{
			name:        "GitHub Copilot CLI with multi-word engine name",
			secretNames: []string{"COPILOT_GITHUB_TOKEN"},
			engineName:  "GitHub Copilot CLI",
			docsURL:     "https://github.github.com/gh-aw/reference/engines/#github-copilot-default",
			wantStrings: []string{
				"Validate COPILOT_GITHUB_TOKEN secret",
				"run: /opt/gh-aw/actions/validate_multi_secret.sh COPILOT_GITHUB_TOKEN 'GitHub Copilot CLI' https://github.github.com/gh-aw/reference/engines/#github-copilot-default",
				"COPILOT_GITHUB_TOKEN: ${{ secrets.COPILOT_GITHUB_TOKEN }}",
			},
		},
		{
			name:        "Claude Code with multi-word engine name and dual secrets",
			secretNames: []string{"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY"},
			engineName:  "Claude Code",
			docsURL:     "https://github.github.com/gh-aw/reference/engines/#anthropic-claude-code",
			wantStrings: []string{
				"Validate CLAUDE_CODE_OAUTH_TOKEN or ANTHROPIC_API_KEY secret",
				"run: /opt/gh-aw/actions/validate_multi_secret.sh CLAUDE_CODE_OAUTH_TOKEN ANTHROPIC_API_KEY 'Claude Code' https://github.github.com/gh-aw/reference/engines/#anthropic-claude-code",
				"CLAUDE_CODE_OAUTH_TOKEN: ${{ secrets.CLAUDE_CODE_OAUTH_TOKEN }}",
				"ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := GenerateMultiSecretValidationStep(tt.secretNames, tt.engineName, tt.docsURL)
			stepContent := strings.Join(step, "\n")

			for _, want := range tt.wantStrings {
				if !strings.Contains(stepContent, want) {
					t.Errorf("GenerateMultiSecretValidationStep() missing expected string:\nwant: %s\ngot: %s", want, stepContent)
				}
			}

			// Verify it calls the validate_multi_secret.sh script
			if !strings.Contains(stepContent, "/opt/gh-aw/actions/validate_multi_secret.sh") {
				t.Error("Expected step to call validate_multi_secret.sh script")
			}

			// Verify it has an env section
			if !strings.Contains(stepContent, "env:") {
				t.Error("Expected step to have 'env:' section")
			}

			// Verify all secrets are passed as environment variables
			for _, secretName := range tt.secretNames {
				expectedEnvVar := fmt.Sprintf("%s: ${{ secrets.%s }}", secretName, secretName)
				if !strings.Contains(stepContent, expectedEnvVar) {
					t.Errorf("Expected step to have environment variable: %s", expectedEnvVar)
				}
			}

			// Verify step has id field
			if !strings.Contains(stepContent, "id: validate-secret") {
				t.Error("Expected step to have 'id: validate-secret' field")
			}
		})
	}
}

func TestClaudeEngineHasSecretValidation(t *testing.T) {
	engine := NewClaudeEngine()
	workflowData := &WorkflowData{}

	steps := engine.GetInstallationSteps(workflowData)
	if len(steps) < 1 {
		t.Fatal("Expected at least one installation step")
	}

	// First step should be secret validation (now supports both CLAUDE_CODE_OAUTH_TOKEN and ANTHROPIC_API_KEY)
	firstStep := strings.Join(steps[0], "\n")
	if !strings.Contains(firstStep, "Validate CLAUDE_CODE_OAUTH_TOKEN or ANTHROPIC_API_KEY secret") {
		t.Error("First installation step should validate CLAUDE_CODE_OAUTH_TOKEN or ANTHROPIC_API_KEY secret")
	}
	if !strings.Contains(firstStep, "CLAUDE_CODE_OAUTH_TOKEN: ${{ secrets.CLAUDE_CODE_OAUTH_TOKEN }}") {
		t.Error("Secret validation step should reference secrets.CLAUDE_CODE_OAUTH_TOKEN")
	}
	if !strings.Contains(firstStep, "ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}") {
		t.Error("Secret validation step should reference secrets.ANTHROPIC_API_KEY")
	}
}

func TestCopilotEngineHasSecretValidation(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{}

	steps := engine.GetInstallationSteps(workflowData)
	if len(steps) < 1 {
		t.Fatal("Expected at least one installation step")
	}

	// First step should be secret validation
	firstStep := strings.Join(steps[0], "\n")
	if !strings.Contains(firstStep, "Validate COPILOT_GITHUB_TOKEN secret") {
		t.Error("First installation step should validate COPILOT_GITHUB_TOKEN secret")
	}
	if !strings.Contains(firstStep, "COPILOT_GITHUB_TOKEN: ${{ secrets.COPILOT_GITHUB_TOKEN }}") {
		t.Error("Secret validation step should reference secrets.COPILOT_GITHUB_TOKEN")
	}
}

func TestCodexEngineHasSecretValidation(t *testing.T) {
	engine := NewCodexEngine()
	workflowData := &WorkflowData{}

	steps := engine.GetInstallationSteps(workflowData)
	if len(steps) < 1 {
		t.Fatal("Expected at least one installation step")
	}

	// First step should be secret validation
	firstStep := strings.Join(steps[0], "\n")
	if !strings.Contains(firstStep, "Validate CODEX_API_KEY or OPENAI_API_KEY secret") {
		t.Error("First installation step should validate CODEX_API_KEY or OPENAI_API_KEY secret")
	}

	// Should check for both secrets
	if !strings.Contains(firstStep, "CODEX_API_KEY: ${{ secrets.CODEX_API_KEY }}") {
		t.Error("Secret validation step should reference secrets.CODEX_API_KEY")
	}
	if !strings.Contains(firstStep, "OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}") {
		t.Error("Secret validation step should reference secrets.OPENAI_API_KEY")
	}

	// Should call the validate_multi_secret.sh script with both secret names
	if !strings.Contains(firstStep, "/opt/gh-aw/actions/validate_multi_secret.sh") {
		t.Error("Should call validate_multi_secret.sh script")
	}
	if !strings.Contains(firstStep, "CODEX_API_KEY OPENAI_API_KEY") {
		t.Error("Should pass both CODEX_API_KEY and OPENAI_API_KEY to the script")
	}
}

func TestCustomEngineDoesNotHaveSecretValidation(t *testing.T) {
	engine := NewCustomEngine()
	workflowData := &WorkflowData{
		EngineConfig: &EngineConfig{
			ID: "custom",
		},
	}

	steps := engine.GetInstallationSteps(workflowData)

	// Custom engine should not have any installation steps
	if len(steps) != 0 {
		t.Errorf("Custom engine should not have installation steps, got %d", len(steps))
	}
}
