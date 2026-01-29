//go:build !integration

package campaign

import (
	"strings"
	"testing"
)

func TestRenderOrchestratorInstructions(t *testing.T) {
	withTempGitRepoWithInstalledCampaignPrompts(t, func(_ string) {
		tests := []struct {
			name          string
			data          CampaignPromptData
			shouldContain []string
		}{
			{
				name: "system-agnostic rules",
				data: CampaignPromptData{},
				shouldContain: []string{
					"Orchestrator Instructions",
					"Traffic and Rate Limits (Required)",
					"Prefer incremental discovery",
					"strict pagination budgets",
					"durable cursor/checkpoint",
					"On throttling",
					"Workers are immutable and campaign-agnostic",
					"Step 1",
					"Read State",
					"Step 2",
					"Make Decisions",
					"Step 3",
					"Dispatch Workers",
					"Step 4",
					"Report",
				},
			},
			{
				name: "explicit state management",
				data: CampaignPromptData{},
				shouldContain: []string{
					"Parse discovered items from the manifest",
					"Discovery cursor is maintained automatically",
					"Determine desired `status`",
				},
			},
			{
				name: "separation of concerns",
				data: CampaignPromptData{},
				shouldContain: []string{
					"Reads and writes are separate steps",
					"never interleave",
				},
			},
			{
				name: "date field calculation in Step 2",
				data: CampaignPromptData{},
				shouldContain: []string{
					"Calculate required date fields",
					"start_date",
					"end_date",
					"format `created_at` as `YYYY-MM-DD`",
					"format `closed_at`/`merged_at` as `YYYY-MM-DD`",
					"today's date",
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := RenderOrchestratorInstructions(tt.data)

				for _, expected := range tt.shouldContain {
					if !strings.Contains(result, expected) {
						t.Errorf("Expected result to contain %q, but it didn't. Result: %s", expected, result)
					}
				}
			})
		}
	})
}

func TestRenderProjectUpdateInstructions(t *testing.T) {
	withTempGitRepoWithInstalledCampaignPrompts(t, func(_ string) {
		tests := []struct {
			name          string
			data          CampaignPromptData
			shouldContain []string
			shouldBeEmpty bool
		}{
			{
				name: "with project URL",
				data: CampaignPromptData{
					ProjectURL: "https://github.com/orgs/test/projects/1",
				},
				shouldContain: []string{
					"Project Update Instructions (Authoritative Write Contract)",
					"update-project",
					"https://github.com/orgs/test/projects/1",
					"Hard Requirements",
					"Required Project Fields",
					"Read-Write Separation",
					"Adding an Issue or PR",
					"Updating an Existing Item",
					"Idempotency Rules",
				},
				shouldBeEmpty: false,
			},
			{
				name: "with project URL and campaign ID",
				data: CampaignPromptData{
					ProjectURL: "https://github.com/orgs/test/projects/1",
					CampaignID: "my-campaign",
				},
				shouldContain: []string{
					"Project Update Instructions (Authoritative Write Contract)",
					"update-project",
					"https://github.com/orgs/test/projects/1",
					"campaign_id",
					"campaign_id:",
					"my-campaign",
				},
				shouldBeEmpty: false,
			},
			{
				name: "without project URL",
				data: CampaignPromptData{
					ProjectURL: "",
				},
				shouldContain: []string{},
				shouldBeEmpty: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := RenderProjectUpdateInstructions(tt.data)

				if tt.shouldBeEmpty && result != "" {
					t.Errorf("Expected empty result, but got: %s", result)
				}

				for _, expected := range tt.shouldContain {
					if !strings.Contains(result, expected) {
						t.Errorf("Expected result to contain %q, but it didn't. Result: %s", expected, result)
					}
				}
			})
		}
	})
}

func TestRenderClosingInstructions(t *testing.T) {
	withTempGitRepoWithInstalledCampaignPrompts(t, func(_ string) {
		result := RenderClosingInstructions()

		expectedPhrases := []string{
			"Closing Instructions (Highest Priority)",
			"Execute all four steps in strict order",
			"Read State (no writes)",
			"Make Decisions (no writes)",
			"Apply Updates (writes)",
			"Report",
			"Workers are immutable and campaign-agnostic",
			"GitHub Project board is the single source of truth",
		}

		for _, expected := range expectedPhrases {
			if !strings.Contains(result, expected) {
				t.Errorf("Expected result to contain %q, but it didn't. Result: %s", expected, result)
			}
		}
	})
}
