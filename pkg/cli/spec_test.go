//go:build !integration

package cli_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/cli"
)

// TestSpec_PublicAPI_ValidateWorkflowName validates the documented behavior.
// Spec: empty names and names with invalid characters return errors.
func TestSpec_PublicAPI_ValidateWorkflowName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "valid alphanumeric-hyphen name", input: "my-workflow", wantErr: false},
		{name: "valid name with underscores and digits", input: "my_workflow_123", wantErr: false},
		{name: "empty name returns error", input: "", wantErr: true},
		{name: "name with spaces returns error", input: "my workflow", wantErr: true},
		{name: "name with slashes returns error", input: "my/workflow", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cli.ValidateWorkflowName(tt.input)
			if tt.wantErr {
				assert.Error(t, err, "ValidateWorkflowName(%q) should return an error", tt.input)
			} else {
				require.NoError(t, err, "ValidateWorkflowName(%q) should not return an error", tt.input)
			}
		})
	}
}

// TestSpec_PublicAPI_IsCommitSHA validates that IsCommitSHA returns true only for 40-char hex strings.
// Spec: "Returns true if the string is a full Git commit SHA"
func TestSpec_PublicAPI_IsCommitSHA(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{"valid SHA lowercase", "abc123def456789012345678901234567890abcd", true},
		{"valid SHA uppercase", "ABCDEF1234567890123456789012345678901234", true},
		{"valid SHA mixed case", "AbCdEf1234567890123456789012345678901234", true},
		{"invalid - too short", "abc123def456", false},
		{"invalid - too long", "abc123def456789012345678901234567890abcdef", false},
		{"invalid - contains non-hex", "abc123def456789012345678901234567890abcg", false},
		{"invalid - empty", "", false},
		{"invalid - branch name", "main", false},
		{"invalid - version tag", "v1.0.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cli.IsCommitSHA(tt.version)
			assert.Equal(t, tt.want, got, "IsCommitSHA(%q) mismatch", tt.version)
		})
	}
}

// TestSpec_PublicAPI_SetVersionInfo validates that SetVersionInfo stores the version returned by GetVersion.
// Spec: "Sets the version at startup"
func TestSpec_PublicAPI_SetVersionInfo(t *testing.T) {
	original := cli.GetVersion()
	t.Cleanup(func() { cli.SetVersionInfo(original) })

	cli.SetVersionInfo("v99.99.99-spec-test")
	assert.Equal(t, "v99.99.99-spec-test", cli.GetVersion(), "GetVersion should return the value set by SetVersionInfo")
}

// TestSpec_PublicAPI_IsRunningInCI validates that IsRunningInCI returns a bool without panicking.
// Spec: "Detects CI environment"
func TestSpec_PublicAPI_IsRunningInCI(t *testing.T) {
	t.Parallel()
	result := cli.IsRunningInCI()
	_ = result // result is environment-dependent; ensure no panic
}

// TestSpec_Types_ShellType validates the documented ShellType string alias and its constants.
// Spec: ShellType string alias with values "bash", "zsh", "fish", "powershell", "unknown"
func TestSpec_Types_ShellType(t *testing.T) {
	t.Parallel()
	assert.Equal(t, cli.ShellBash, cli.ShellType("bash"), "ShellBash constant should be \"bash\"")
	assert.Equal(t, cli.ShellZsh, cli.ShellType("zsh"), "ShellZsh constant should be \"zsh\"")
	assert.Equal(t, cli.ShellFish, cli.ShellType("fish"), "ShellFish constant should be \"fish\"")
	assert.Equal(t, cli.ShellPowerShell, cli.ShellType("powershell"), "ShellPowerShell constant should be \"powershell\"")
	assert.Equal(t, cli.ShellUnknown, cli.ShellType("unknown"), "ShellUnknown constant should be \"unknown\"")
}

// TestSpec_Constants_CheckState validates the documented normalized CI state values.
func TestSpec_Constants_CheckState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		actual   cli.CheckState
		expected cli.CheckState
	}{
		{name: "failed", actual: cli.CheckStateFailed, expected: "failed"},
		{name: "pending", actual: cli.CheckStatePending, expected: "pending"},
		{name: "no checks", actual: cli.CheckStateNoChecks, expected: "no_checks"},
		{name: "policy blocked", actual: cli.CheckStatePolicyBlocked, expected: "policy_blocked"},
		{name: "success", actual: cli.CheckStateSuccess, expected: "success"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.actual, "CheckState constant %q should match the README specification", tt.name)
		})
	}
}

// TestSpec_Constants_DockerImages validates the documented default scanner images.
func TestSpec_Constants_DockerImages(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "ghcr.io/zizmorcore/zizmor:1.30.1@sha256:a2eb396d886c053073405c7a980f2139ba2248ec172243cfa3841e57196e8101", cli.ZizmorImage, "ZizmorImage should match the README specification")
	assert.Equal(t, "ghcr.io/boostsecurityio/poutine:1.1.6@sha256:722a8e0999b583c1540fe2974e691032b2d9d21b9256a17965132b6bfd0081b0", cli.PoutineImage, "PoutineImage should match the README specification")
	assert.Equal(t, "rhysd/actionlint:1.7.12@sha256:b1934ee5f1c509618f2508e6eb47ee0d3520686341fec936f3b79331f9315667", cli.ActionlintImage, "ActionlintImage should match the README specification")
	assert.Equal(t, "ghcr.io/vigilant-llc/runner-guard:3.1.5@sha256:2df426ef96d21f1622e05b21329f26bd263fc46110609cefb6afe43457613ac0", cli.RunnerGuardImage, "RunnerGuardImage should match the README specification")
}

// TestSpec_Constants_TimelineEvents validates the documented timeline source and kind values.
func TestSpec_Constants_TimelineEvents(t *testing.T) {
	t.Parallel()
	assert.Equal(t, cli.TimelineSourceGateway, cli.TimelineEventSource("gateway"), "gateway source should match the README specification")
	assert.Equal(t, cli.TimelineSourceFirewall, cli.TimelineEventSource("firewall"), "firewall source should match the README specification")
	assert.Equal(t, cli.TimelineSourceAgent, cli.TimelineEventSource("agent"), "agent source should match the README specification")
	assert.Equal(t, cli.TimelineKindToolCall, cli.TimelineEventKind("tool_call"), "tool-call kind should match the README specification")
	assert.Equal(t, cli.TimelineKindDIFCFiltered, cli.TimelineEventKind("difc_filtered"), "DIFC kind should match the README specification")
	assert.Equal(t, cli.TimelineKindGuardPolicyBlocked, cli.TimelineEventKind("guard_blocked"), "guard-policy kind should match the README specification")
	assert.Equal(t, cli.TimelineKindNetworkAllowed, cli.TimelineEventKind("net_allowed"), "allowed-network kind should match the README specification")
	assert.Equal(t, cli.TimelineKindNetworkBlocked, cli.TimelineEventKind("net_blocked"), "blocked-network kind should match the README specification")
	assert.Equal(t, cli.TimelineKindAgentTurn, cli.TimelineEventKind("agent_turn"), "agent-turn kind should match the README specification")
	assert.Equal(t, cli.TimelineKindAgentToolStart, cli.TimelineEventKind("agent_tool_start"), "agent tool-start kind should match the README specification")
	assert.Equal(t, cli.TimelineKindAgentToolDone, cli.TimelineEventKind("agent_tool_done"), "agent tool-done kind should match the README specification")
}

// TestSpec_Constants_WorkflowIDExplanation validates the documented workflow-ID explanation.
func TestSpec_Constants_WorkflowIDExplanation(t *testing.T) {
	t.Parallel()
	assert.Contains(t, cli.WorkflowIDExplanation, "basename", "workflow-ID explanation should describe the Markdown filename basename")
	assert.Contains(t, cli.WorkflowIDExplanation, ".md extension", "workflow-ID explanation should describe removal of the .md extension")
}

// TestSpec_PublicAPI_DetectShell validates DetectShell returns one of the documented ShellType values.
// Spec: "Detects the user's current shell"
func TestSpec_PublicAPI_DetectShell(t *testing.T) {
	t.Parallel()
	shell := cli.DetectShell()
	validShells := []cli.ShellType{cli.ShellBash, cli.ShellZsh, cli.ShellFish, cli.ShellPowerShell, cli.ShellUnknown}
	assert.Contains(t, validShells, shell, "DetectShell should return one of the documented ShellType values")
}

// TestSpec_PublicAPI_ValidEngineNames validates the documented function returns a non-empty list.
// Spec: "Returns the supported engine names for shell completion"
func TestSpec_PublicAPI_ValidEngineNames(t *testing.T) {
	t.Parallel()
	engines := cli.ValidEngineNames()
	assert.NotEmpty(t, engines, "ValidEngineNames should return at least one engine name")
	for _, name := range engines {
		assert.NotEmpty(t, name, "each engine name should be non-empty")
	}
}

// TestSpec_PublicAPI_ValidArtifactSetNames validates the documented function returns known artifact sets.
// Spec: "Returns the valid artifact set name strings"
func TestSpec_PublicAPI_ValidArtifactSetNames(t *testing.T) {
	t.Parallel()
	names := cli.ValidArtifactSetNames()
	assert.NotEmpty(t, names, "ValidArtifactSetNames should return a non-empty list")
	assert.Contains(t, names, "all", "ValidArtifactSetNames should include \"all\"")
}

// TestSpec_PublicAPI_ValidateArtifactSets validates known and unknown artifact sets.
// Spec: "Validates that all provided artifact set names are known"
func TestSpec_PublicAPI_ValidateArtifactSets(t *testing.T) {
	t.Parallel()
	t.Run("known artifact set returns no error", func(t *testing.T) {
		err := cli.ValidateArtifactSets([]string{"all"})
		require.NoError(t, err, "ValidateArtifactSets should not error for known set \"all\"")
	})

	t.Run("unknown artifact set returns error", func(t *testing.T) {
		err := cli.ValidateArtifactSets([]string{"unknown-artifact-set-xyz"})
		assert.Error(t, err, "ValidateArtifactSets should error for unknown artifact set")
	})

	t.Run("empty list returns no error", func(t *testing.T) {
		err := cli.ValidateArtifactSets([]string{})
		require.NoError(t, err, "ValidateArtifactSets should not error for empty list")
	})
}

// TestSpec_PublicAPI_ExtractWorkflowDescription validates extraction of the description field.
// Spec: "Extracts the description field from workflow markdown content"
func TestSpec_PublicAPI_ExtractWorkflowDescription(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "extracts description from frontmatter",
			content:  "---\ndescription: My workflow description\n---\n\n# Content",
			expected: "My workflow description",
		},
		{
			name:     "returns empty string when no description field",
			content:  "---\nengine: copilot\n---\n\n# Content",
			expected: "",
		},
		{
			name:     "returns empty string for content without frontmatter",
			content:  "# Just markdown",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cli.ExtractWorkflowDescription(tt.content)
			assert.Equal(t, tt.expected, result, "ExtractWorkflowDescription mismatch for %q", tt.name)
		})
	}
}

// TestSpec_PublicAPI_ExtractWorkflowEngine validates extraction of the engine field.
// Spec: "Extracts the engine field from workflow markdown content"
func TestSpec_PublicAPI_ExtractWorkflowEngine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "extracts engine in string format",
			content:  "---\nengine: copilot\n---\n\n# Content",
			expected: "copilot",
		},
		{
			name:     "returns empty string when no engine field",
			content:  "---\ndescription: My workflow\n---\n\n# Content",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cli.ExtractWorkflowEngine(tt.content)
			assert.Equal(t, tt.expected, result, "ExtractWorkflowEngine mismatch for %q", tt.name)
		})
	}
}

// TestSpec_PublicAPI_ExtractWorkflowPrivate validates extraction of the private flag.
// Spec: "Returns true if the workflow is marked private"
func TestSpec_PublicAPI_ExtractWorkflowPrivate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{
			name:     "returns true when private: true",
			content:  "---\nprivate: true\n---\n\n# Content",
			expected: true,
		},
		{
			name:     "returns false when private: false",
			content:  "---\nprivate: false\n---\n\n# Content",
			expected: false,
		},
		{
			name:     "returns false when no private field",
			content:  "---\nengine: copilot\n---\n\n# Content",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cli.ExtractWorkflowPrivate(tt.content)
			assert.Equal(t, tt.expected, result, "ExtractWorkflowPrivate mismatch for %q", tt.name)
		})
	}
}

// TestSpec_PublicAPI_ResolveArtifactFilter validates that ResolveArtifactFilter expands aliases.
// Spec: "Expands artifact set aliases to concrete artifact names"
func TestSpec_PublicAPI_ResolveArtifactFilter(t *testing.T) {
	t.Run("all returns nil meaning no filter applied", func(t *testing.T) {
		result := cli.ResolveArtifactFilter([]string{"all"})
		assert.Nil(t, result, "\"all\" should return nil (no filter — download all artifacts)")
	})

	t.Run("empty list returns nil meaning no filter applied", func(t *testing.T) {
		result := cli.ResolveArtifactFilter([]string{})
		assert.Nil(t, result, "empty input should return nil (no filter — download all artifacts)")
	})

	t.Run("non-all named set expands to concrete artifact list", func(t *testing.T) {
		sets := cli.ValidArtifactSetNames()
		for _, s := range sets {
			if s == "all" {
				continue
			}
			result := cli.ResolveArtifactFilter([]string{s})
			assert.NotNil(t, result, "artifact set %q should expand to a concrete list", s)
			assert.NotEmpty(t, result, "artifact set %q should expand to at least one artifact name", s)
			break
		}
	})
}

// TestSpec_PublicAPI_GroupRunsByWorkflow validates that a flat slice of runs is grouped by workflow name.
// Spec: "Groups a flat slice of runs by workflow name"
func TestSpec_PublicAPI_GroupRunsByWorkflow(t *testing.T) {
	runs := []cli.WorkflowRun{
		{WorkflowName: "workflow-a"},
		{WorkflowName: "workflow-b"},
		{WorkflowName: "workflow-a"},
	}
	grouped := cli.GroupRunsByWorkflow(runs)
	assert.Len(t, grouped, 2, "should produce two groups for two distinct workflow names")
	assert.Len(t, grouped["workflow-a"], 2, "workflow-a group should contain two runs")
	assert.Len(t, grouped["workflow-b"], 1, "workflow-b group should contain one run")
}

// TestSpec_PublicAPI_UpdateFieldInFrontmatter validates the documented frontmatter field update.
// Spec: "Sets a field in frontmatter YAML"
func TestSpec_PublicAPI_UpdateFieldInFrontmatter(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		fieldName     string
		fieldValue    string
		wantErr       bool
		checkContains string
	}{
		{
			name:          "updates existing field",
			content:       "---\ndescription: old description\n---\n\n# Content",
			fieldName:     "description",
			fieldValue:    "new description",
			wantErr:       false,
			checkContains: "new description",
		},
		{
			name:          "adds new field when absent",
			content:       "---\nengine: copilot\n---\n\n# Content",
			fieldName:     "description",
			fieldValue:    "my workflow",
			wantErr:       false,
			checkContains: "my workflow",
		},
		{
			// SPEC_AMBIGUITY: The README spec says "Sets a field in frontmatter YAML" without
			// specifying the error-path for content without frontmatter. The implementation
			// creates a new frontmatter block in this case rather than returning an error.
			name:          "creates frontmatter when content has none",
			content:       "# Just markdown with no frontmatter",
			fieldName:     "description",
			fieldValue:    "value",
			wantErr:       false,
			checkContains: "value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := cli.UpdateFieldInFrontmatter(tt.content, tt.fieldName, tt.fieldValue)
			if tt.wantErr {
				assert.Error(t, err, "UpdateFieldInFrontmatter should return error for %q", tt.name)
				return
			}
			require.NoError(t, err, "UpdateFieldInFrontmatter should not error for %q", tt.name)
			assert.Contains(t, result, tt.checkContains, "result should contain updated value for %q", tt.name)
		})
	}
}

// TestSpec_PublicAPI_SetFieldInOnTrigger validates the documented on: trigger field update.
// Spec: "Sets a field inside the on: trigger block"
func TestSpec_PublicAPI_SetFieldInOnTrigger(t *testing.T) {
	t.Run("adds on: block when not present", func(t *testing.T) {
		content := "---\ndescription: my workflow\n---\n\n# Content"
		result, err := cli.SetFieldInOnTrigger(content, "schedule", "daily")
		require.NoError(t, err, "SetFieldInOnTrigger should not error when on: block is absent")
		assert.Contains(t, result, "on:", "result should contain on: block")
		assert.Contains(t, result, "schedule", "result should contain the new field")
	})

	t.Run("sets field inside existing on: block", func(t *testing.T) {
		content := "---\ndescription: my workflow\non:\n    push: true\n---\n\n# Content"
		result, err := cli.SetFieldInOnTrigger(content, "schedule", "daily")
		require.NoError(t, err, "SetFieldInOnTrigger should not error with existing on: block")
		assert.Contains(t, result, "schedule", "result should contain the new field in the on: block")
	})

	t.Run("returns error when no frontmatter found", func(t *testing.T) {
		content := "# No frontmatter here"
		_, err := cli.SetFieldInOnTrigger(content, "schedule", "daily")
		assert.Error(t, err, "SetFieldInOnTrigger should return error when no frontmatter found")
	})
}

// TestSpec_PublicAPI_RemoveFieldFromOnTrigger validates the documented on: trigger field removal.
// Spec: "Removes a field from the on: trigger block"
func TestSpec_PublicAPI_RemoveFieldFromOnTrigger(t *testing.T) {
	t.Run("removes field from existing on: block", func(t *testing.T) {
		content := "---\ndescription: my workflow\non:\n    schedule: daily\n    push: true\n---\n\n# Content"
		result, err := cli.RemoveFieldFromOnTrigger(content, "schedule")
		require.NoError(t, err, "RemoveFieldFromOnTrigger should not error for valid content")
		assert.NotContains(t, result, "schedule:", "result should not contain removed field")
		assert.Contains(t, result, "push", "result should retain other on: fields")
	})

	t.Run("no-op when field is not present", func(t *testing.T) {
		content := "---\ndescription: my workflow\non:\n    push: true\n---\n\n# Content"
		result, err := cli.RemoveFieldFromOnTrigger(content, "schedule")
		require.NoError(t, err, "RemoveFieldFromOnTrigger should not error when field absent")
		assert.Contains(t, result, "push", "result should retain existing on: fields")
	})
}

// TestSpec_PublicAPI_UpdateScheduleInOnBlock validates the documented schedule update.
// Spec: "Updates the cron schedule in the on: block"
func TestSpec_PublicAPI_UpdateScheduleInOnBlock(t *testing.T) {
	t.Run("updates existing schedule expression", func(t *testing.T) {
		content := "---\ndescription: my workflow\non:\n    schedule:\n    - cron: 0 9 * * 1-5\n---\n\n# Content"
		result, err := cli.UpdateScheduleInOnBlock(content, "0 10 * * 1-5")
		require.NoError(t, err, "UpdateScheduleInOnBlock should not error for valid content")
		assert.Contains(t, result, "0 10 * * 1-5", "result should contain the updated cron expression")
	})

	t.Run("returns error for content without frontmatter lines", func(t *testing.T) {
		content := "# Just markdown"
		_, err := cli.UpdateScheduleInOnBlock(content, "0 9 * * *")
		assert.Error(t, err, "UpdateScheduleInOnBlock should return error when no frontmatter lines present")
	})
}

// TestSpec_PublicAPI_CalculateWorkflowHealth validates the pure health computation documented in the spec.
// Spec: "Pure health computation for a single workflow"
func TestSpec_PublicAPI_CalculateWorkflowHealth(t *testing.T) {
	t.Run("returns N/A display values for empty runs", func(t *testing.T) {
		health := cli.CalculateWorkflowHealth("my-workflow", nil, 80.0)
		assert.Equal(t, "my-workflow", health.WorkflowName, "WorkflowName should be the provided name")
		assert.Equal(t, "N/A", health.DisplayRate, "DisplayRate should be N/A for empty runs")
		assert.Equal(t, "→", health.Trend, "Trend should be stable for empty runs")
		assert.Equal(t, 0, health.TotalRuns, "TotalRuns should be 0 for empty runs")
	})

	t.Run("counts successful and failed runs correctly", func(t *testing.T) {
		runs := []cli.WorkflowRun{
			{WorkflowName: "my-workflow", Conclusion: "success"},
			{WorkflowName: "my-workflow", Conclusion: "success"},
			{WorkflowName: "my-workflow", Conclusion: "failure"},
		}
		health := cli.CalculateWorkflowHealth("my-workflow", runs, 80.0)
		assert.Equal(t, 3, health.TotalRuns, "TotalRuns should count all runs")
		assert.Equal(t, 2, health.SuccessCount, "SuccessCount should count runs with success conclusion")
		assert.Equal(t, 1, health.FailureCount, "FailureCount should count failure-conclusion runs")
		assert.InDelta(t, 66.67, health.SuccessRate, 0.1, "SuccessRate should be the percentage of successes")
	})

	t.Run("sets BelowThresh true when success rate is below threshold", func(t *testing.T) {
		runs := []cli.WorkflowRun{
			{WorkflowName: "my-workflow", Conclusion: "failure"},
			{WorkflowName: "my-workflow", Conclusion: "failure"},
		}
		health := cli.CalculateWorkflowHealth("my-workflow", runs, 80.0)
		assert.True(t, health.BelowThresh, "BelowThresh should be true when success rate is below threshold")
	})

	t.Run("sets BelowThresh false when success rate meets threshold", func(t *testing.T) {
		runs := []cli.WorkflowRun{
			{WorkflowName: "my-workflow", Conclusion: "success"},
			{WorkflowName: "my-workflow", Conclusion: "success"},
			{WorkflowName: "my-workflow", Conclusion: "success"},
			{WorkflowName: "my-workflow", Conclusion: "success"},
		}
		health := cli.CalculateWorkflowHealth("my-workflow", runs, 80.0)
		assert.False(t, health.BelowThresh, "BelowThresh should be false when all runs succeed")
		assert.Equal(t, 4, health.SuccessCount, "SuccessCount should count all successful runs")
	})
}

// TestSpec_PublicAPI_CalculateHealthSummary validates the aggregate health computation documented in the spec.
// Spec: "Aggregate health computation"
func TestSpec_PublicAPI_CalculateHealthSummary(t *testing.T) {
	t.Run("returns correct totals for empty workflow list", func(t *testing.T) {
		summary := cli.CalculateHealthSummary(nil, "30d", 80.0)
		assert.Equal(t, "30d", summary.Period, "Period should match the input period")
		assert.Equal(t, 0, summary.TotalWorkflows, "TotalWorkflows should be 0 for empty input")
		assert.Equal(t, 0, summary.HealthyWorkflows, "HealthyWorkflows should be 0 for empty input")
	})

	t.Run("counts healthy workflows and preserves period", func(t *testing.T) {
		whs := []cli.WorkflowHealth{
			{WorkflowName: "wf-a", SuccessRate: 90.0, BelowThresh: false},
			{WorkflowName: "wf-b", SuccessRate: 50.0, BelowThresh: true},
			{WorkflowName: "wf-c", SuccessRate: 100.0, BelowThresh: false},
		}
		summary := cli.CalculateHealthSummary(whs, "7d", 80.0)
		assert.Equal(t, "7d", summary.Period, "Period should be preserved in the summary")
		assert.Equal(t, 3, summary.TotalWorkflows, "TotalWorkflows should equal input workflow count")
		assert.Equal(t, 2, summary.HealthyWorkflows, "HealthyWorkflows should count workflows with SuccessRate >= threshold")
		assert.Equal(t, 1, summary.BelowThreshold, "BelowThreshold should count workflows with BelowThresh set")
		assert.Len(t, summary.Workflows, 3, "Workflows should include all input workflows")
	})
}
