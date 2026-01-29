//go:build !integration

// This file provides workflow compilation and safe-output configuration.
// This file contains tests for the safe-outputs messages configuration feature,
// which allows customizing footer and notification messages in safe-output jobs.

package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSafeOutputsMessagesConfiguration(t *testing.T) {
	compiler := NewCompiler()

	t.Run("Should parse messages configuration in safe-outputs", func(t *testing.T) {
		frontmatter := map[string]any{
			"name": "Test Workflow",
			"safe-outputs": map[string]any{
				"create-issue": nil,
				"messages": map[string]any{
					"footer":             "> Custom footer by [{workflow_name}]({run_url})",
					"footer-install":     "> Install: `gh aw add {workflow_source}`",
					"staged-title":       "## 🔍 Preview: {operation}",
					"staged-description": "Preview of {operation}:",
				},
			},
		}

		config := compiler.extractSafeOutputsConfig(frontmatter)
		if config == nil {
			t.Fatal("Expected SafeOutputsConfig to be parsed")
		}

		if config.Messages == nil {
			t.Fatal("Expected Messages to be parsed")
		}

		if config.Messages.Footer != "> Custom footer by [{workflow_name}]({run_url})" {
			t.Errorf("Expected Footer to be custom template, got %q", config.Messages.Footer)
		}

		if config.Messages.FooterInstall != "> Install: `gh aw add {workflow_source}`" {
			t.Errorf("Expected FooterInstall to be custom template, got %q", config.Messages.FooterInstall)
		}

		if config.Messages.StagedTitle != "## 🔍 Preview: {operation}" {
			t.Errorf("Expected StagedTitle to be custom template, got %q", config.Messages.StagedTitle)
		}

		if config.Messages.StagedDescription != "Preview of {operation}:" {
			t.Errorf("Expected StagedDescription to be custom template, got %q", config.Messages.StagedDescription)
		}
	})

	t.Run("Should handle partial messages configuration", func(t *testing.T) {
		frontmatter := map[string]any{
			"name": "Test Workflow",
			"safe-outputs": map[string]any{
				"create-issue": nil,
				"messages": map[string]any{
					"footer": "> Custom footer",
				},
			},
		}

		config := compiler.extractSafeOutputsConfig(frontmatter)
		if config == nil {
			t.Fatal("Expected SafeOutputsConfig to be parsed")
		}

		if config.Messages == nil {
			t.Fatal("Expected Messages to be parsed")
		}

		if config.Messages.Footer != "> Custom footer" {
			t.Errorf("Expected Footer to be custom template, got %q", config.Messages.Footer)
		}

		// Other fields should be empty
		if config.Messages.FooterInstall != "" {
			t.Errorf("Expected FooterInstall to be empty, got %q", config.Messages.FooterInstall)
		}
	})

	t.Run("Should handle missing messages configuration", func(t *testing.T) {
		frontmatter := map[string]any{
			"name": "Test Workflow",
			"safe-outputs": map[string]any{
				"create-issue": nil,
			},
		}

		config := compiler.extractSafeOutputsConfig(frontmatter)
		if config == nil {
			t.Fatal("Expected SafeOutputsConfig to be parsed")
		}

		if config.Messages != nil {
			t.Error("Expected Messages to be nil when not configured")
		}
	})
}

func TestSerializeMessagesConfig(t *testing.T) {
	t.Run("Should serialize nil config to empty string", func(t *testing.T) {
		result, err := serializeMessagesConfig(nil)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if result != "" {
			t.Errorf("Expected empty string for nil config, got %q", result)
		}
	})

	t.Run("Should serialize messages config to JSON with camelCase keys", func(t *testing.T) {
		config := &SafeOutputMessagesConfig{
			Footer:            "> Custom footer",
			FooterInstall:     "> Install instructions",
			StagedTitle:       "## Preview",
			StagedDescription: "Description",
			RunStarted:        "Started",
			RunSuccess:        "Success",
			RunFailure:        "Failure",
		}

		result, err := serializeMessagesConfig(config)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// Verify it's valid JSON
		var parsed SafeOutputMessagesConfig
		if err := json.Unmarshal([]byte(result), &parsed); err != nil {
			t.Fatalf("Result is not valid JSON: %v", err)
		}

		if parsed.Footer != "> Custom footer" {
			t.Errorf("Expected Footer to be preserved, got %q", parsed.Footer)
		}

		// Verify JSON uses camelCase keys (not PascalCase)
		if !strings.Contains(result, `"footer"`) {
			t.Errorf("Expected JSON to contain camelCase key 'footer', got: %s", result)
		}
		if !strings.Contains(result, `"footerInstall"`) {
			t.Errorf("Expected JSON to contain camelCase key 'footerInstall', got: %s", result)
		}
		if !strings.Contains(result, `"stagedTitle"`) {
			t.Errorf("Expected JSON to contain camelCase key 'stagedTitle', got: %s", result)
		}
		if !strings.Contains(result, `"stagedDescription"`) {
			t.Errorf("Expected JSON to contain camelCase key 'stagedDescription', got: %s", result)
		}
		if !strings.Contains(result, `"runStarted"`) {
			t.Errorf("Expected JSON to contain camelCase key 'runStarted', got: %s", result)
		}
		if !strings.Contains(result, `"runSuccess"`) {
			t.Errorf("Expected JSON to contain camelCase key 'runSuccess', got: %s", result)
		}
		if !strings.Contains(result, `"runFailure"`) {
			t.Errorf("Expected JSON to contain camelCase key 'runFailure', got: %s", result)
		}

		// Verify JSON does NOT use PascalCase keys
		if strings.Contains(result, `"Footer"`) {
			t.Errorf("Expected JSON to NOT contain PascalCase key 'Footer', got: %s", result)
		}
		if strings.Contains(result, `"FooterInstall"`) {
			t.Errorf("Expected JSON to NOT contain PascalCase key 'FooterInstall', got: %s", result)
		}
	})

	t.Run("Should handle empty config fields", func(t *testing.T) {
		config := &SafeOutputMessagesConfig{
			Footer: "> Only footer",
		}

		result, err := serializeMessagesConfig(config)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		var parsed SafeOutputMessagesConfig
		if err := json.Unmarshal([]byte(result), &parsed); err != nil {
			t.Fatalf("Result is not valid JSON: %v", err)
		}

		if parsed.Footer != "> Only footer" {
			t.Errorf("Expected Footer to be preserved, got %q", parsed.Footer)
		}
	})
}

func TestMessagesEnvVarInSafeOutputJobs(t *testing.T) {
	compiler := NewCompiler()

	t.Run("Should include GH_AW_SAFE_OUTPUT_MESSAGES env var when messages configured", func(t *testing.T) {
		data := &WorkflowData{
			Name:            "Test",
			FrontmatterName: "Test Workflow",
			SafeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1}},
				Messages: &SafeOutputMessagesConfig{
					Footer: "> Custom footer [{workflow_name}]({run_url})",
				},
			},
		}

		job, err := compiler.buildCreateOutputIssueJob(data, "main_job")
		if err != nil {
			t.Fatalf("Failed to build create issue job: %v", err)
		}

		stepsStr := strings.Join(job.Steps, "")
		if !strings.Contains(stepsStr, "GH_AW_SAFE_OUTPUT_MESSAGES:") {
			t.Error("Expected GH_AW_SAFE_OUTPUT_MESSAGES to be included in job steps")
		}

		// Verify it contains the serialized footer
		if !strings.Contains(stepsStr, "Custom footer") {
			t.Error("Expected serialized messages to contain the custom footer text")
		}
	})

	t.Run("Should not include GH_AW_SAFE_OUTPUT_MESSAGES when messages not configured", func(t *testing.T) {
		data := &WorkflowData{
			Name:            "Test",
			FrontmatterName: "Test Workflow",
			SafeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1}},
			},
		}

		job, err := compiler.buildCreateOutputIssueJob(data, "main_job")
		if err != nil {
			t.Fatalf("Failed to build create issue job: %v", err)
		}

		stepsStr := strings.Join(job.Steps, "")
		if strings.Contains(stepsStr, "GH_AW_SAFE_OUTPUT_MESSAGES:") {
			t.Error("Expected GH_AW_SAFE_OUTPUT_MESSAGES to NOT be included when messages not configured")
		}
	})
}
