//go:build !integration

package workflow

import (
	"testing"
)

func TestSafeOutputsMaxConfiguration(t *testing.T) {
	compiler := &Compiler{}

	t.Run("Default configuration should use max: 1", func(t *testing.T) {
		testSingular := map[string]any{
			"safe-outputs": map[string]any{
				"create-issue":        nil,
				"add-comment":         nil,
				"create-pull-request": nil,
				"update-issue":        nil,
			},
		}

		config := compiler.extractSafeOutputsConfig(testSingular)
		if config == nil {
			t.Fatal("Expected config to be parsed")
		}

		if config.CreateIssues == nil {
			t.Fatal("Expected CreateIssues to be parsed")
		}
		if config.CreateIssues.Max != 1 {
			t.Errorf("Expected CreateIssues.Max to be 1 by default, got %d", config.CreateIssues.Max)
		}

		if config.AddComments == nil {
			t.Fatal("Expected AddComments to be parsed")
		}
		if config.AddComments.Max != 1 {
			t.Errorf("Expected AddComments.Max to be 1 by default, got %d", config.AddComments.Max)
		}

		if config.CreatePullRequests == nil {
			t.Fatal("Expected CreatePullRequests to be parsed")
		}
		if config.CreatePullRequests.Max != 1 {
			t.Errorf("Expected CreatePullRequests.Max to be 1 by default, got %d", config.CreatePullRequests.Max)
		}

		if config.UpdateIssues == nil {
			t.Fatal("Expected UpdateIssues to be parsed")
		}
		if config.UpdateIssues.Max != 1 {
			t.Errorf("Expected UpdateIssues.Max to be 1 by default, got %d", config.UpdateIssues.Max)
		}
	})

	t.Run("Explicit max values should be used", func(t *testing.T) {
		testWithMax := map[string]any{
			"safe-outputs": map[string]any{
				"create-issue": map[string]any{
					"max": 3,
				},
				"add-comment": map[string]any{
					"max": 5,
				},
				"create-pull-request": map[string]any{
					// max parameter is ignored for pull requests
					"max": 2,
				},
				"update-issue": map[string]any{
					"max": 4,
				},
			},
		}

		config := compiler.extractSafeOutputsConfig(testWithMax)
		if config == nil {
			t.Fatal("Expected config to be parsed")
		}

		if config.CreateIssues == nil {
			t.Fatal("Expected CreateIssues to be parsed")
		}
		if config.CreateIssues.Max != 3 {
			t.Errorf("Expected CreateIssues.Max to be 3, got %d", config.CreateIssues.Max)
		}

		if config.AddComments == nil {
			t.Fatal("Expected AddComments to be parsed")
		}
		if config.AddComments.Max != 5 {
			t.Errorf("Expected AddComments.Max to be 5, got %d", config.AddComments.Max)
		}

		if config.CreatePullRequests == nil {
			t.Fatal("Expected CreatePullRequests to be parsed")
		}
		if config.CreatePullRequests.Max != 1 {
			t.Errorf("Expected CreatePullRequests.Max to be 1 (max ignored for pull requests), got %d", config.CreatePullRequests.Max)
		}

		if config.UpdateIssues == nil {
			t.Fatal("Expected UpdateIssues to be parsed")
		}
		if config.UpdateIssues.Max != 4 {
			t.Errorf("Expected UpdateIssues.Max to be 4, got %d", config.UpdateIssues.Max)
		}
	})

	t.Run("Complete configuration with all options", func(t *testing.T) {
		testComplete := map[string]any{
			"safe-outputs": map[string]any{
				"create-issue": map[string]any{
					"title-prefix": "[Auto] ",
					"labels":       []any{"bug", "auto-generated"},
					"max":          2,
				},
				"add-comment": map[string]any{
					"max":    3,
					"target": "*",
				},
				"create-pull-request": map[string]any{
					"title-prefix": "[Fix] ",
					"labels":       []any{"fix"},
					"draft":        true,
				},
				"update-issue": map[string]any{
					"max":    2,
					"target": "456",
					"status": nil,
					"title":  nil,
					"body":   nil,
				},
			},
		}

		config := compiler.extractSafeOutputsConfig(testComplete)
		if config == nil {
			t.Fatal("Expected config to be parsed")
		}

		// Check create-issue
		if config.CreateIssues == nil {
			t.Fatal("Expected CreateIssues to be parsed")
		}
		if config.CreateIssues.Max != 2 {
			t.Errorf("Expected CreateIssues.Max to be 2, got %d", config.CreateIssues.Max)
		}
		if config.CreateIssues.TitlePrefix != "[Auto] " {
			t.Errorf("Expected CreateIssues.TitlePrefix to be '[Auto] ', got '%s'", config.CreateIssues.TitlePrefix)
		}
		if len(config.CreateIssues.Labels) != 2 || config.CreateIssues.Labels[0] != "bug" || config.CreateIssues.Labels[1] != "auto-generated" {
			t.Errorf("Expected CreateIssues.Labels to be ['bug', 'auto-generated'], got %v", config.CreateIssues.Labels)
		}

		// Check add-comment
		if config.AddComments == nil {
			t.Fatal("Expected AddComments to be parsed")
		}
		if config.AddComments.Max != 3 {
			t.Errorf("Expected AddComments.Max to be 3, got %d", config.AddComments.Max)
		}
		if config.AddComments.Target != "*" {
			t.Errorf("Expected AddComments.Target to be '*', got '%s'", config.AddComments.Target)
		}

		// Check create-pull-request
		if config.CreatePullRequests == nil {
			t.Fatal("Expected CreatePullRequests to be parsed")
		}
		if config.CreatePullRequests.Max != 1 {
			t.Errorf("Expected CreatePullRequests.Max to be 1, got %d", config.CreatePullRequests.Max)
		}
		if config.CreatePullRequests.TitlePrefix != "[Fix] " {
			t.Errorf("Expected CreatePullRequests.TitlePrefix to be '[Fix] ', got '%s'", config.CreatePullRequests.TitlePrefix)
		}
		if len(config.CreatePullRequests.Labels) != 1 || config.CreatePullRequests.Labels[0] != "fix" {
			t.Errorf("Expected CreatePullRequests.Labels to be ['fix'], got %v", config.CreatePullRequests.Labels)
		}
		if config.CreatePullRequests.Draft == nil || *config.CreatePullRequests.Draft != true {
			t.Errorf("Expected CreatePullRequests.Draft to be true, got %v", config.CreatePullRequests.Draft)
		}

		// Check update-issue
		if config.UpdateIssues == nil {
			t.Fatal("Expected UpdateIssues to be parsed")
		}
		if config.UpdateIssues.Max != 2 {
			t.Errorf("Expected UpdateIssues.Max to be 2, got %d", config.UpdateIssues.Max)
		}
		if config.UpdateIssues.Target != "456" {
			t.Errorf("Expected UpdateIssues.Target to be '456', got '%s'", config.UpdateIssues.Target)
		}
		if config.UpdateIssues.Status == nil {
			t.Error("Expected UpdateIssues.Status to be non-nil (updatable)")
		}
		if config.UpdateIssues.Title == nil {
			t.Error("Expected UpdateIssues.Title to be non-nil (updatable)")
		}
		if config.UpdateIssues.Body == nil {
			t.Error("Expected UpdateIssues.Body to be non-nil (updatable)")
		}
	})
}
