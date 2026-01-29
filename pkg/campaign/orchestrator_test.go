//go:build !integration

package campaign

import (
	"strings"
	"testing"
)

func TestBuildOrchestrator_BasicShape(t *testing.T) {
	withTempGitRepoWithInstalledCampaignPrompts(t, func(_ string) {
		spec := &CampaignSpec{
			ID:          "go-file-size-reduction-project64",
			Name:        "Campaign: Go File Size Reduction (Project 64)",
			Description: "Reduce oversized non-test Go files under pkg/ to ≤800 LOC via tracked refactors.",
			ProjectURL:  "https://github.com/orgs/githubnext/projects/64",
			Workflows:   []string{"daily-file-diet"},
			MemoryPaths: []string{"memory/campaigns/go-file-size-reduction-project64/**"},
			MetricsGlob: "memory/campaigns/go-file-size-reduction-project64/metrics/*.json",
		}

		mdPath := ".github/workflows/go-file-size-reduction-project64.campaign.md"
		data, orchestratorPath := BuildOrchestrator(spec, mdPath)

		if orchestratorPath != ".github/workflows/go-file-size-reduction-project64.campaign.g.md" {
			t.Fatalf("unexpected orchestrator path: got %q", orchestratorPath)
		}

		if data == nil {
			t.Fatalf("expected non-nil WorkflowData")
		}

		if data.Name != spec.Name {
			t.Fatalf("unexpected workflow name: got %q, want %q", data.Name, spec.Name)
		}

		if strings.TrimSpace(data.On) == "" || !strings.Contains(data.On, "workflow_dispatch") {
			t.Fatalf("expected On section with workflow_dispatch trigger, got %q", data.On)
		}

		if !strings.Contains(data.On, "schedule:") || !strings.Contains(data.On, "0 * * * *") {
			t.Fatalf("expected On section with hourly schedule cron, got %q", data.On)
		}

		if strings.TrimSpace(data.Concurrency) == "" || !strings.Contains(data.Concurrency, "concurrency:") {
			t.Fatalf("expected workflow-level concurrency to be set, got %q", data.Concurrency)
		}
		if !strings.Contains(data.Concurrency, "campaign-go-file-size-reduction-project64-orchestrator") {
			t.Fatalf("expected concurrency group to include campaign id, got %q", data.Concurrency)
		}

		if !strings.Contains(data.MarkdownContent, "Go File Size Reduction") {
			t.Fatalf("expected markdown content to mention campaign name, got: %q", data.MarkdownContent)
		}

		// Campaign orchestrators intentionally omit permissions from the generated markdown.
		// Job permissions are computed during compilation.
		if strings.TrimSpace(data.Permissions) != "" {
			t.Fatalf("expected no permissions in generated orchestrator data, got: %q", data.Permissions)
		}
	})
}

func TestBuildOrchestrator_CompletionInstructions(t *testing.T) {
	withTempGitRepoWithInstalledCampaignPrompts(t, func(_ string) {
		spec := &CampaignSpec{
			ID:          "test-campaign",
			Name:        "Test Campaign",
			Description: "A test campaign",
			ProjectURL:  "https://github.com/orgs/test/projects/1",
			Workflows:   []string{"test-workflow"},
		}

		mdPath := ".github/workflows/test-campaign.campaign.md"
		data, _ := BuildOrchestrator(spec, mdPath)

		if data == nil {
			t.Fatalf("expected non-nil WorkflowData")
		}

		// Governed invariant: completion is reported explicitly in Step 5.
		expectedPhrases := []string{
			"### Step 5 — Report",
			"**Completion:**",
		}
		for _, expected := range expectedPhrases {
			if !strings.Contains(data.MarkdownContent, expected) {
				t.Errorf("expected markdown to contain %q, got: %q", expected, data.MarkdownContent)
			}
		}
	})
}

func TestBuildOrchestrator_WorkflowsInDiscovery(t *testing.T) {
	withTempGitRepoWithInstalledCampaignPrompts(t, func(_ string) {
		spec := &CampaignSpec{
			ID:          "test-campaign",
			Name:        "Test Campaign",
			Description: "A test campaign",
			ProjectURL:  "https://github.com/orgs/test/projects/1",
			Workflows: []string{
				"daily-doc-updater",
				"docs-noob-tester",
				"daily-multi-device-docs-tester",
			},
		}

		mdPath := ".github/workflows/test-campaign.campaign.md"
		data, _ := BuildOrchestrator(spec, mdPath)

		if data == nil {
			t.Fatalf("expected non-nil WorkflowData")
		}

		// Verify each workflow is mentioned in the header list
		for _, workflow := range spec.Workflows {
			if !strings.Contains(data.MarkdownContent, workflow) {
				t.Errorf("expected markdown to mention workflow %q, got: %q", workflow, data.MarkdownContent)
			}
		}

		// Verify that discovery is now precomputed (not agent-side)
		if !strings.Contains(data.MarkdownContent, "Discovery has been precomputed") {
			t.Errorf("expected markdown to indicate precomputed discovery, got: %q", data.MarkdownContent)
		}
		if !strings.Contains(data.MarkdownContent, "./.gh-aw/campaign.discovery.json") {
			t.Errorf("expected markdown to reference discovery manifest, got: %q", data.MarkdownContent)
		}

		// Verify that discovered results reference normalized items from manifest
		if !strings.Contains(data.MarkdownContent, "Parse discovered items from the manifest") {
			t.Errorf("expected markdown to mention parsing items from manifest, got: %q", data.MarkdownContent)
		}
	})
}

func TestBuildOrchestrator_DispatchOnlyPolicy(t *testing.T) {
	withTempGitRepoWithInstalledCampaignPrompts(t, func(_ string) {
		spec := &CampaignSpec{
			ID:          "dispatch-only-campaign",
			Name:        "Dispatch Only Campaign",
			Description: "Campaign orchestrator with dispatch and project capabilities",
			ProjectURL:  "https://github.com/orgs/test/projects/1",
			Workflows:   []string{"worker-a", "worker-b"},
			MemoryPaths: []string{"memory/campaigns/dispatch-only-campaign/**"},
		}

		mdPath := ".github/workflows/dispatch-only-campaign.campaign.md"
		data, _ := BuildOrchestrator(spec, mdPath)
		if data == nil {
			t.Fatalf("expected non-nil WorkflowData")
		}

		if data.SafeOutputs == nil {
			t.Fatalf("expected SafeOutputs to be set")
		}
		if data.SafeOutputs.DispatchWorkflow == nil {
			t.Fatalf("expected dispatch-workflow safe output to be enabled")
		}
		if len(data.SafeOutputs.DispatchWorkflow.Workflows) != 2 {
			t.Fatalf("expected 2 allowlisted workflows, got %d", len(data.SafeOutputs.DispatchWorkflow.Workflows))
		}

		// Orchestrators should have update-project and create-project-status-update for dashboard maintenance
		if data.SafeOutputs.UpdateProjects == nil {
			t.Fatalf("expected update-project safe output to be enabled")
		}
		if data.SafeOutputs.CreateProjectStatusUpdates == nil {
			t.Fatalf("expected create-project-status-update safe output to be enabled")
		}

		// Orchestrators should NOT have create-issue or add-comment (workers handle those)
		if data.SafeOutputs.CreateIssues != nil || data.SafeOutputs.AddComments != nil {
			t.Fatalf("expected orchestrator to omit create-issue and add-comment safe outputs")
		}

		// Orchestrators should have GitHub tool access for discovery operations
		if data.Tools == nil {
			t.Fatalf("expected Tools to be configured")
		}
		if _, ok := data.Tools["github"]; !ok {
			t.Fatalf("expected orchestrator to have github tools configured")
		}
	})
}

func TestBuildOrchestrator_TrackerIDMonitoring(t *testing.T) {
	withTempGitRepoWithInstalledCampaignPrompts(t, func(_ string) {
		spec := &CampaignSpec{
			ID:          "test-campaign",
			Name:        "Test Campaign",
			Description: "A test campaign",
			ProjectURL:  "https://github.com/orgs/test/projects/1",
			Workflows:   []string{"daily-file-diet"},
		}

		mdPath := ".github/workflows/test-campaign.campaign.md"
		data, _ := BuildOrchestrator(spec, mdPath)

		if data == nil {
			t.Fatalf("expected non-nil WorkflowData")
		}

		// Verify that the orchestrator uses manifest-based discovery (not agent-side search)
		if !strings.Contains(data.MarkdownContent, "Correlation is explicit (tracker-id AND labels)") {
			t.Errorf("expected markdown to mention tracker-id and labels correlation rule, got: %q", data.MarkdownContent)
		}
		if !strings.Contains(data.MarkdownContent, "Read the precomputed discovery manifest") {
			t.Errorf("expected markdown to include manifest-based discovery instructions, got: %q", data.MarkdownContent)
		}
		if !strings.Contains(data.MarkdownContent, "./.gh-aw/campaign.discovery.json") {
			t.Errorf("expected markdown to reference discovery manifest file, got: %q", data.MarkdownContent)
		}

		// Verify that orchestrator does NOT monitor workflow runs by file name
		if strings.Contains(data.MarkdownContent, "list_workflow_runs") {
			t.Errorf("expected markdown to NOT use list_workflow_runs for monitoring, but it does: %q", data.MarkdownContent)
		}

		if strings.Contains(data.MarkdownContent, ".lock.yml") {
			t.Errorf("expected markdown to NOT reference .lock.yml files for monitoring, but it does: %q", data.MarkdownContent)
		}

		// Verify it follows system-agnostic rules
		if !strings.Contains(data.MarkdownContent, "Core Principles") {
			t.Errorf("expected markdown to contain core principles section, got: %q", data.MarkdownContent)
		}

		// Verify separation of steps (read / decide / dispatch / report)
		if !strings.Contains(data.MarkdownContent, "Step 1") || !strings.Contains(data.MarkdownContent, "Read State") {
			t.Errorf("expected markdown to contain Step 1 Read State, got: %q", data.MarkdownContent)
		}
		if !strings.Contains(data.MarkdownContent, "Step 2") || !strings.Contains(data.MarkdownContent, "Make Decisions") {
			t.Errorf("expected markdown to contain Step 2 Make Decisions, got: %q", data.MarkdownContent)
		}
		if !strings.Contains(data.MarkdownContent, "Step 3") || !strings.Contains(data.MarkdownContent, "Dispatch Workers") {
			t.Errorf("expected markdown to contain Step 3 Dispatch Workers, got: %q", data.MarkdownContent)
		}
		if !strings.Contains(data.MarkdownContent, "Step 4") || !strings.Contains(data.MarkdownContent, "Report") {
			t.Errorf("expected markdown to contain Step 4 Report, got: %q", data.MarkdownContent)
		}
	})
}

func TestBuildOrchestrator_GovernanceDoesNotGrantWriteSafeOutputs(t *testing.T) {
	withTempGitRepoWithInstalledCampaignPrompts(t, func(_ string) {
		spec := &CampaignSpec{
			ID:         "test-campaign",
			Name:       "Test Campaign",
			ProjectURL: "https://github.com/orgs/test/projects/1",
			Workflows:  []string{"test-workflow"},
			Governance: &CampaignGovernancePolicy{
				MaxCommentsPerRun:       3,
				MaxProjectUpdatesPerRun: 4,
			},
		}

		mdPath := ".github/workflows/test-campaign.campaign.md"
		data, _ := BuildOrchestrator(spec, mdPath)
		if data == nil {
			t.Fatalf("expected non-nil WorkflowData")
		}
		if data.SafeOutputs == nil || data.SafeOutputs.DispatchWorkflow == nil {
			t.Fatalf("expected dispatch-workflow safe output to be enabled")
		}
		if data.SafeOutputs.DispatchWorkflow.Max != 3 {
			t.Fatalf("unexpected dispatch-workflow max: got %d, want %d", data.SafeOutputs.DispatchWorkflow.Max, 3)
		}

		// Governance should control update-project max
		if data.SafeOutputs.UpdateProjects == nil {
			t.Fatalf("expected update-project safe output to be enabled")
		}
		if data.SafeOutputs.UpdateProjects.Max != 4 {
			t.Fatalf("unexpected update-project max: got %d, want %d", data.SafeOutputs.UpdateProjects.Max, 4)
		}

		// create-project-status-update should always be enabled
		if data.SafeOutputs.CreateProjectStatusUpdates == nil {
			t.Fatalf("expected create-project-status-update safe output to be enabled")
		}
		if data.SafeOutputs.CreateProjectStatusUpdates.Max != 1 {
			t.Fatalf("unexpected create-project-status-update max: got %d, want %d", data.SafeOutputs.CreateProjectStatusUpdates.Max, 1)
		}

		// Orchestrators should NOT have create-issue or add-comment (governance MaxCommentsPerRun doesn't grant add-comment)
		if data.SafeOutputs.CreateIssues != nil || data.SafeOutputs.AddComments != nil {
			t.Fatalf("expected orchestrator to omit create-issue and add-comment safe outputs regardless of governance")
		}
	})
}

func TestExtractFileGlobPatterns(t *testing.T) {
	tests := []struct {
		name           string
		spec           *CampaignSpec
		expectedGlobs  []string
		expectedLogMsg string
	}{
		{
			name: "flexible pattern matching both dated and non-dated",
			spec: &CampaignSpec{
				ID:          "go-file-size-reduction-project64",
				MemoryPaths: []string{"memory/campaigns/go-file-size-reduction-project64*/**"},
				MetricsGlob: "memory/campaigns/go-file-size-reduction-project64-*/metrics/*.json",
			},
			expectedGlobs:  []string{"go-file-size-reduction-project64*/**"},
			expectedLogMsg: "Extracted file-glob pattern from memory-paths",
		},
		{
			name: "dated pattern in memory-paths",
			spec: &CampaignSpec{
				ID:          "go-file-size-reduction-project64",
				MemoryPaths: []string{"memory/campaigns/go-file-size-reduction-project64-*/**"},
				MetricsGlob: "memory/campaigns/go-file-size-reduction-project64-*/metrics/*.json",
			},
			expectedGlobs:  []string{"go-file-size-reduction-project64-*/**"},
			expectedLogMsg: "Extracted file-glob pattern from memory-paths",
		},
		{
			name: "multiple patterns in memory-paths",
			spec: &CampaignSpec{
				ID: "go-file-size-reduction-project64",
				MemoryPaths: []string{
					"memory/campaigns/go-file-size-reduction-project64-*/**",
					"memory/campaigns/go-file-size-reduction-project64/**",
				},
				MetricsGlob: "memory/campaigns/go-file-size-reduction-project64-*/metrics/*.json",
			},
			expectedGlobs:  []string{"go-file-size-reduction-project64-*/**", "go-file-size-reduction-project64/**"},
			expectedLogMsg: "Extracted file-glob pattern from memory-paths",
		},
		{
			name: "dated pattern in metrics-glob only",
			spec: &CampaignSpec{
				ID:          "go-file-size-reduction-project64",
				MetricsGlob: "memory/campaigns/go-file-size-reduction-project64-*/metrics/*.json",
			},
			expectedGlobs:  []string{"go-file-size-reduction-project64-*/**"},
			expectedLogMsg: "Extracted file-glob pattern from metrics-glob",
		},
		{
			name: "simple pattern without wildcards",
			spec: &CampaignSpec{
				ID:          "simple-campaign",
				MemoryPaths: []string{"memory/campaigns/simple-campaign/**"},
			},
			expectedGlobs:  []string{"simple-campaign/**"},
			expectedLogMsg: "Extracted file-glob pattern from memory-paths",
		},
		{
			name: "no memory paths or metrics glob",
			spec: &CampaignSpec{
				ID: "minimal-campaign",
			},
			expectedGlobs:  []string{"minimal-campaign/**"},
			expectedLogMsg: "Using fallback file-glob pattern",
		},
		{
			name: "multiple memory paths with wildcard",
			spec: &CampaignSpec{
				ID: "multi-path",
				MemoryPaths: []string{
					"memory/campaigns/multi-path-staging/**",
					"memory/campaigns/multi-path-*/data/**",
				},
			},
			expectedGlobs:  []string{"multi-path-staging/**", "multi-path-*/data/**"},
			expectedLogMsg: "Extracted file-glob pattern from memory-paths",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractFileGlobPatterns(tt.spec)
			if len(result) != len(tt.expectedGlobs) {
				t.Errorf("extractFileGlobPatterns(%q) returned %d patterns, want %d", tt.spec.ID, len(result), len(tt.expectedGlobs))
				return
			}
			for i, expected := range tt.expectedGlobs {
				if result[i] != expected {
					t.Errorf("extractFileGlobPatterns(%q)[%d] = %q, want %q", tt.spec.ID, i, result[i], expected)
				}
			}
		})
	}
}

func TestBuildOrchestrator_FileGlobMatchesMemoryPaths(t *testing.T) {
	withTempGitRepoWithInstalledCampaignPrompts(t, func(_ string) {
		// This test verifies that the file-glob pattern in repo-memory configuration
		// matches the pattern defined in memory-paths, including wildcards
		spec := &CampaignSpec{
			ID:          "go-file-size-reduction-project64",
			Name:        "Go File Size Reduction Campaign",
			Description: "Test campaign with dated memory paths",
			ProjectURL:  "https://github.com/orgs/githubnext/projects/64",
			Workflows:   []string{"daily-file-diet"},
			MemoryPaths: []string{"memory/campaigns/go-file-size-reduction-project64-*/**"},
			MetricsGlob: "memory/campaigns/go-file-size-reduction-project64-*/metrics/*.json",
		}

		mdPath := ".github/workflows/go-file-size-reduction-project64.campaign.md"
		data, _ := BuildOrchestrator(spec, mdPath)

		if data == nil {
			t.Fatalf("expected non-nil WorkflowData")
		}

		// Extract repo-memory configuration from Tools
		repoMemoryConfig, ok := data.Tools["repo-memory"]
		if !ok {
			t.Fatalf("expected repo-memory to be configured in Tools")
		}

		repoMemoryArray, ok := repoMemoryConfig.([]any)
		if !ok || len(repoMemoryArray) == 0 {
			t.Fatalf("expected repo-memory to be an array with at least one entry")
		}

		repoMemoryEntry, ok := repoMemoryArray[0].(map[string]any)
		if !ok {
			t.Fatalf("expected repo-memory entry to be a map")
		}

		fileGlob, ok := repoMemoryEntry["file-glob"]
		if !ok {
			t.Fatalf("expected file-glob to be present in repo-memory entry")
		}

		fileGlobArray, ok := fileGlob.([]any)
		if !ok || len(fileGlobArray) == 0 {
			t.Fatalf("expected file-glob to be an array with at least one entry")
		}

		fileGlobPattern, ok := fileGlobArray[0].(string)
		if !ok {
			t.Fatalf("expected file-glob pattern to be a string")
		}

		// Verify that the file-glob pattern includes the wildcard for dated directories
		expectedPattern := "go-file-size-reduction-project64-*/**"
		if fileGlobPattern != expectedPattern {
			t.Errorf("file-glob pattern = %q, want %q", fileGlobPattern, expectedPattern)
		}

		// Verify that the pattern would match dated directories
		if !strings.Contains(fileGlobPattern, "*") {
			t.Errorf("file-glob pattern should include wildcard for dated directories, got %q", fileGlobPattern)
		}
	})
}
