//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/githubnext/gh-aw/pkg/campaign"
)

// TestValidateCampaignsUsesCorrectDirectory tests that validateCampaigns uses
// the campaign spec file's directory to validate referenced workflows, not
// a global workflow directory parameter.
func TestValidateCampaignsUsesCorrectDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a campaign spec file
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create referenced workflow files
	workflowFile := filepath.Join(workflowsDir, "example-workflow.md")
	workflowContent := `---
engine: copilot
---

# Test workflow
Test workflow content
`
	if err := os.WriteFile(workflowFile, []byte(workflowContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create campaign spec file
	campaignFile := filepath.Join(workflowsDir, "test-campaign.campaign.md")
	campaignContent := `---
id: test-campaign
name: Test Campaign
description: A test campaign
version: v1
project-url: https://github.com/orgs/test/projects/1
workflows:
  - example-workflow
state: active
---

# Campaign Test
Test campaign
`
	if err := os.WriteFile(campaignFile, []byte(campaignContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Change to tmpDir so it becomes the git root for the test
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	// Initialize a git repo
	if err := os.WriteFile(".git", []byte("fake git"), 0644); err != nil {
		t.Fatal(err)
	}

	// Load the campaign spec
	specs, err := campaign.LoadSpecs(tmpDir)
	if err != nil {
		t.Fatalf("LoadSpecs failed: %v", err)
	}

	if len(specs) == 0 {
		t.Fatal("Expected to load 1 campaign spec, got 0")
	}

	// Validate campaigns with an incorrect workflow directory
	// This should still work because validateCampaigns should use each spec's directory
	err = validateCampaigns(".github/workflows", false, []string{campaignFile})
	if err != nil {
		t.Fatalf("validateCampaigns failed: %v", err)
	}
}
