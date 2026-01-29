//go:build !integration

package workflow

import (
	"testing"
)

func TestSafeOutputsEnvConfiguration(t *testing.T) {
	compiler := NewCompiler()

	t.Run("Should parse env configuration in safe-outputs", func(t *testing.T) {
		frontmatter := map[string]any{
			"name": "Test Workflow",
			"safe-outputs": map[string]any{
				"create-issue": nil,
				"env": map[string]any{
					"GITHUB_TOKEN":   "${{ secrets.SOME_PAT_FOR_AGENTIC_WORKFLOWS }}",
					"CUSTOM_API_KEY": "${{ secrets.CUSTOM_API_KEY }}",
					"DEBUG_MODE":     "true",
				},
			},
		}

		config := compiler.extractSafeOutputsConfig(frontmatter)
		if config == nil {
			t.Fatal("Expected SafeOutputsConfig to be parsed")
		}

		if config.Env == nil {
			t.Fatal("Expected Env to be parsed")
		}

		expected := map[string]string{
			"GITHUB_TOKEN":   "${{ secrets.SOME_PAT_FOR_AGENTIC_WORKFLOWS }}",
			"CUSTOM_API_KEY": "${{ secrets.CUSTOM_API_KEY }}",
			"DEBUG_MODE":     "true",
		}

		for key, expectedValue := range expected {
			if actualValue, exists := config.Env[key]; !exists {
				t.Errorf("Expected env key %s to exist", key)
			} else if actualValue != expectedValue {
				t.Errorf("Expected env[%s] to be %q, got %q", key, expectedValue, actualValue)
			}
		}
	})

	t.Run("Should include custom env vars in create-issue job", func(t *testing.T) {
		data := &WorkflowData{
			Name:            "Test",
			FrontmatterName: "Test Workflow",
			SafeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1}},
				Env: map[string]string{
					"GITHUB_TOKEN": "${{ secrets.SOME_PAT_FOR_AGENTIC_WORKFLOWS }}",
					"DEBUG_MODE":   "true",
				},
			},
		}

		job, err := compiler.buildCreateOutputIssueJob(data, "main_job")
		if err != nil {
			t.Fatalf("Failed to build create issue job: %v", err)
		}

		expectedEnvVars := []string{
			"GITHUB_TOKEN: ${{ secrets.SOME_PAT_FOR_AGENTIC_WORKFLOWS }}",
			"DEBUG_MODE: true",
		}
		assertEnvVarsInSteps(t, job.Steps, expectedEnvVars)
	})

	t.Run("Should include custom env vars in create-pull-request job", func(t *testing.T) {
		data := &WorkflowData{
			Name:            "Test",
			FrontmatterName: "Test Workflow",
			SafeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1}},
				Env: map[string]string{
					"GITHUB_TOKEN": "${{ secrets.SOME_PAT_FOR_AGENTIC_WORKFLOWS }}",
					"API_ENDPOINT": "https://api.example.com",
				},
			},
		}

		job, err := compiler.buildCreateOutputPullRequestJob(data, "main_job")
		if err != nil {
			t.Fatalf("Failed to build create pull request job: %v", err)
		}

		expectedEnvVars := []string{
			"GITHUB_TOKEN: ${{ secrets.SOME_PAT_FOR_AGENTIC_WORKFLOWS }}",
			"API_ENDPOINT: https://api.example.com",
		}
		assertEnvVarsInSteps(t, job.Steps, expectedEnvVars)
	})

	t.Run("Should work without env configuration", func(t *testing.T) {
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

		// Env should be nil when not specified
		if config.Env != nil {
			t.Error("Expected Env to be nil when not configured")
		}

		// Job creation should still work
		data := &WorkflowData{
			Name:            "Test",
			FrontmatterName: "Test Workflow",
			SafeOutputs:     config,
		}

		_, err := compiler.buildCreateOutputIssueJob(data, "main_job")
		if err != nil {
			t.Errorf("Job creation should work without env configuration: %v", err)
		}
	})

	t.Run("Should handle empty env configuration", func(t *testing.T) {
		frontmatter := map[string]any{
			"name": "Test Workflow",
			"safe-outputs": map[string]any{
				"create-issue": nil,
				"env":          map[string]any{},
			},
		}

		config := compiler.extractSafeOutputsConfig(frontmatter)
		if config == nil {
			t.Fatal("Expected SafeOutputsConfig to be parsed")
		}

		if config.Env == nil {
			t.Error("Expected Env to be empty map, not nil")
		}

		if len(config.Env) != 0 {
			t.Errorf("Expected Env to be empty, got %d entries", len(config.Env))
		}
	})

	t.Run("Should handle non-string env values gracefully", func(t *testing.T) {
		frontmatter := map[string]any{
			"name": "Test Workflow",
			"safe-outputs": map[string]any{
				"create-issue": nil,
				"env": map[string]any{
					"STRING_VALUE": "valid",
					"INT_VALUE":    123,  // should be ignored
					"BOOL_VALUE":   true, // should be ignored
					"NULL_VALUE":   nil,  // should be ignored
				},
			},
		}

		config := compiler.extractSafeOutputsConfig(frontmatter)
		if config == nil {
			t.Fatal("Expected SafeOutputsConfig to be parsed")
		}

		if config.Env == nil {
			t.Fatal("Expected Env to be parsed")
		}

		// Only string values should be included
		if len(config.Env) != 1 {
			t.Errorf("Expected only 1 env var (string values only), got %d", len(config.Env))
		}

		if config.Env["STRING_VALUE"] != "valid" {
			t.Error("Expected STRING_VALUE to be preserved")
		}

		// Non-string values should be ignored
		if _, exists := config.Env["INT_VALUE"]; exists {
			t.Error("Expected INT_VALUE to be ignored")
		}
		if _, exists := config.Env["BOOL_VALUE"]; exists {
			t.Error("Expected BOOL_VALUE to be ignored")
		}
		if _, exists := config.Env["NULL_VALUE"]; exists {
			t.Error("Expected NULL_VALUE to be ignored")
		}
	})
}
