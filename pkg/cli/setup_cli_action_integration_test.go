//go:build integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSetupCLIAction tests the setup-cli action's install.sh script
func TestSetupCLIAction(t *testing.T) {
	// Get project root
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	projectRoot := filepath.Join(wd, "..", "..")
	installScript := filepath.Join(projectRoot, "actions", "setup-cli", "install.sh")

	// Verify script exists
	if _, err := os.Stat(installScript); os.IsNotExist(err) {
		t.Fatalf("install.sh script not found at: %s", installScript)
	}

	// Verify script is executable
	info, err := os.Stat(installScript)
	if err != nil {
		t.Fatalf("Failed to stat install.sh: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Errorf("install.sh is not executable")
	}

	// Test script syntax
	t.Run("script_syntax_valid", func(t *testing.T) {
		cmd := exec.Command("bash", "-n", installScript)
		if err := cmd.Run(); err != nil {
			t.Errorf("Script has syntax errors: %v", err)
		}
	})

	// Test that script can fetch latest version when INPUT_VERSION is not provided
	t.Run("can_fetch_latest_without_input_version", func(t *testing.T) {
		// This test would actually try to fetch from GitHub API
		// We just verify the script doesn't immediately fail
		content, err := os.ReadFile(installScript)
		if err != nil {
			t.Fatalf("Failed to read install.sh: %v", err)
		}
		// Verify script has fallback to fetch latest
		if !strings.Contains(string(content), "No version specified") || !strings.Contains(string(content), "using 'latest'") {
			t.Errorf("Script should support fetching latest release when no version is provided")
		}
	})

	// Test INPUT_VERSION environment variable support
	t.Run("supports_input_version_env", func(t *testing.T) {
		content, err := os.ReadFile(installScript)
		if err != nil {
			t.Fatalf("Failed to read install.sh: %v", err)
		}
		if !strings.Contains(string(content), "INPUT_VERSION") {
			t.Errorf("Script does not support INPUT_VERSION environment variable")
		}
	})

	// Test gh extension install logic exists
	t.Run("has_gh_extension_install_logic", func(t *testing.T) {
		content, err := os.ReadFile(installScript)
		if err != nil {
			t.Fatalf("Failed to read install.sh: %v", err)
		}
		if !strings.Contains(string(content), "gh extension install") {
			t.Errorf("Script does not include gh extension install logic")
		}
	})

	// Test release validation logic exists
	t.Run("has_release_validation", func(t *testing.T) {
		content, err := os.ReadFile(installScript)
		if err != nil {
			t.Fatalf("Failed to read install.sh: %v", err)
		}
		// Verify script has binary verification logic
		if !strings.Contains(string(content), "Verifying binary") {
			t.Errorf("Script does not include release validation")
		}
	})

	// Test checksum validation is enabled for GitHub Actions
	t.Run("checksum_enabled_for_github_actions", func(t *testing.T) {
		content, err := os.ReadFile(installScript)
		if err != nil {
			t.Fatalf("Failed to read install.sh: %v", err)
		}
		// Check that SKIP_CHECKSUM is set to false when INPUT_VERSION is set
		if !strings.Contains(string(content), "SKIP_CHECKSUM=false") {
			t.Errorf("Script does not enable checksum validation for GitHub Actions context")
		}
	})

	// Test that script is synced from install-gh-aw.sh
	t.Run("synced_from_install_gh_aw", func(t *testing.T) {
		installGhAwScript := filepath.Join(projectRoot, "install-gh-aw.sh")

		installContent, err := os.ReadFile(installScript)
		if err != nil {
			t.Fatalf("Failed to read install.sh: %v", err)
		}

		installGhAwContent, err := os.ReadFile(installGhAwScript)
		if err != nil {
			t.Fatalf("Failed to read install-gh-aw.sh: %v", err)
		}

		// They should be identical
		if string(installContent) != string(installGhAwContent) {
			t.Errorf("install.sh is not synced with install-gh-aw.sh. Run 'make sync-action-scripts'")
		}
	})
}

// TestSetupCLIActionYAML tests the action.yml file structure
func TestSetupCLIActionYAML(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	projectRoot := filepath.Join(wd, "..", "..")
	actionYAML := filepath.Join(projectRoot, "actions", "setup-cli", "action.yml")

	// Verify action.yml exists
	if _, err := os.Stat(actionYAML); os.IsNotExist(err) {
		t.Fatalf("action.yml not found at: %s", actionYAML)
	}

	// Read and validate action.yml content
	content, err := os.ReadFile(actionYAML)
	if err != nil {
		t.Fatalf("Failed to read action.yml: %v", err)
	}

	contentStr := string(content)

	// Verify required fields
	requiredFields := []string{
		"name:",
		"description:",
		"inputs:",
		"version:",
		"required: true",
		"outputs:",
		"installed-version:",
		"runs:",
		"using: 'composite'",
		"github-token:",
	}

	for _, field := range requiredFields {
		if !strings.Contains(contentStr, field) {
			t.Errorf("action.yml missing required field: %s", field)
		}
	}

	// Verify version input is required
	if !strings.Contains(contentStr, "required: true") {
		t.Errorf("version input should be required")
	}

	// Verify github-token has default value
	if !strings.Contains(contentStr, "github-token:") {
		t.Errorf("action.yml should define github-token input")
	}
	if !strings.Contains(contentStr, "default: ${{ github.token }}") {
		t.Errorf("github-token should have default value of github.token")
	}

	// Verify GH_TOKEN environment variable is set
	if !strings.Contains(contentStr, "GH_TOKEN:") {
		t.Errorf("action.yml should set GH_TOKEN environment variable")
	}

	// Verify no SHA mention (only release tags)
	if strings.Contains(strings.ToLower(contentStr), "sha") && !strings.Contains(contentStr, "SHA256") {
		t.Errorf("action.yml should not mention SHA support (only release tags)")
	}
}
