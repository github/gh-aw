//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// workflowsDir is the path to the .github/workflows directory relative to this test file.
const workflowsDir = "../../.github/workflows"

// lockFileOutputMapping describes what outputs a compiled lock file must expose, derived from
// the SafeOutputsConfig parsed from the source .md file.
type lockFileOutputMapping struct {
	// configPresent returns true when the safe-output type is configured.
	configPresent func(*SafeOutputsConfig) bool
	// jobOutputKeys are the keys that must appear in the safe_outputs job's outputs section.
	jobOutputKeys []string
	// workflowCallOutputKeys are the keys that must appear in on.workflow_call.outputs when the
	// workflow uses workflow_call as a trigger.
	workflowCallOutputKeys []string
}

// lockFileOutputMappings enumerates all safe-output types that produce named outputs.
var lockFileOutputMappings = []lockFileOutputMapping{
	{
		configPresent:          func(s *SafeOutputsConfig) bool { return s.CreateIssues != nil },
		jobOutputKeys:          []string{"created_issue_number:", "created_issue_url:"},
		workflowCallOutputKeys: []string{"created_issue_number:", "created_issue_url:"},
	},
	{
		configPresent:          func(s *SafeOutputsConfig) bool { return s.CreatePullRequests != nil },
		jobOutputKeys:          []string{"created_pr_number:", "created_pr_url:"},
		workflowCallOutputKeys: []string{"created_pr_number:", "created_pr_url:"},
	},
	{
		configPresent:          func(s *SafeOutputsConfig) bool { return s.AddComments != nil },
		jobOutputKeys:          []string{"comment_id:", "comment_url:"},
		workflowCallOutputKeys: []string{"comment_id:", "comment_url:"},
	},
	{
		configPresent:          func(s *SafeOutputsConfig) bool { return s.PushToPullRequestBranch != nil },
		jobOutputKeys:          []string{"push_commit_sha:", "push_commit_url:"},
		workflowCallOutputKeys: []string{"push_commit_sha:", "push_commit_url:"},
	},
}

// extractSafeOutputsJobSection returns the text of the safe_outputs job block from a lock file.
func extractSafeOutputsJobSection(lockContent string) string {
	return extractJobSection(lockContent, "safe_outputs")
}

// extractWorkflowCallSection returns the text of the on section (up to but not including the
// first top-level non-on key) from a compiled lock file.
func extractWorkflowCallSection(lockContent string) string {
	for _, trigger := range []string{"\n\"on\":\n", "\non:\n"} {
		idx := strings.Index(lockContent, trigger)
		if idx < 0 {
			continue
		}
		onStart := idx + len(trigger)
		rest := lockContent[onStart:]
		lines := strings.Split(rest, "\n")
		var sb strings.Builder
		for _, line := range lines {
			// Top-level YAML key (no leading spaces) ends the "on" block.
			if len(line) > 0 && line[0] != ' ' && strings.Contains(line, ":") {
				break
			}
			sb.WriteString(line + "\n")
		}
		return sb.String()
	}
	return ""
}

// parseSafeOutputsForLockFileTest parses the source .md workflow file and returns the
// SafeOutputsConfig and the "on" section text, using the compiler's own parsing logic.
// Returns nil config when the workflow does not have safe-outputs configured.
func parseSafeOutputsForLockFileTest(mdPath string) (*SafeOutputsConfig, string, error) {
	compiler := NewCompiler()
	data, err := compiler.ParseWorkflowFile(mdPath)
	if err != nil {
		return nil, "", err
	}
	return data.SafeOutputs, data.On, nil
}

// TestCompiledLockFiles_SafeOutputsJobOutputs validates that every compiled lock file exposes
// the expected individual named outputs on its safe_outputs job, based on what safe-output
// types are configured in the corresponding source .md file.
func TestCompiledLockFiles_SafeOutputsJobOutputs(t *testing.T) {
	mdFiles, err := filepath.Glob(filepath.Join(workflowsDir, "*.md"))
	require.NoError(t, err, "should glob .md workflow files")
	require.NotEmpty(t, mdFiles, "should find at least one .md workflow file")

	checkedWorkflows := 0

	for _, mdPath := range mdFiles {
		lockPath := strings.TrimSuffix(mdPath, ".md") + ".lock.yml"

		safeOutputs, _, parseErr := parseSafeOutputsForLockFileTest(mdPath)
		if parseErr != nil || safeOutputs == nil {
			continue // skip workflows without safe-outputs or that fail to parse
		}

		lockBytes, err := os.ReadFile(lockPath)
		if err != nil {
			continue // lock file may not exist yet
		}
		lockContent := string(lockBytes)

		safeOutputsJob := extractSafeOutputsJobSection(lockContent)
		if safeOutputsJob == "" {
			continue // workflow may not produce a safe_outputs job (e.g. runtime-import)
		}

		baseName := filepath.Base(mdPath)

		for _, mapping := range lockFileOutputMappings {
			if !mapping.configPresent(safeOutputs) {
				continue
			}
			for _, outputKey := range mapping.jobOutputKeys {
				assert.Contains(t, safeOutputsJob, outputKey,
					"lock file %s: safe_outputs job should expose %s", baseName, outputKey)
			}
		}

		checkedWorkflows++
	}

	assert.Positive(t, checkedWorkflows, "should have validated at least one workflow with safe-outputs")
	t.Logf("Validated safe_outputs job outputs for %d workflow(s)", checkedWorkflows)
}

func TestSmokeClaudeAllowsRequiredChromeServiceDomains(t *testing.T) {
	data, err := NewCompiler().ParseWorkflowFile(filepath.Join(workflowsDir, "smoke-claude.md"))
	require.NoError(t, err, "should parse smoke-claude workflow")
	require.NotNil(t, data.NetworkPermissions, "smoke-claude should configure network permissions")

	for _, domain := range []string{
		"content-autofill.googleapis.com",
		"www.google.com",
		"accounts.google.com",
		"android.clients.google.com",
		"www.gstatic.com",
	} {
		assert.Contains(t, data.NetworkPermissions.Allowed, domain,
			"smoke-claude should allow the Chrome service domain %s", domain)
	}
	assert.NotContains(t, data.NetworkPermissions.Allowed, "chrome",
		"smoke-claude should not allow the broad chrome network ecosystem")
}

func TestCompiledLockFiles_SmokePiOmitsYoloArg(t *testing.T) {
	lockPath := filepath.Join(workflowsDir, "smoke-pi.lock.yml")
	lockBytes, err := os.ReadFile(lockPath)
	require.NoError(t, err, "should read smoke-pi lock file")

	assert.NotContains(t, string(lockBytes), "--yolo",
		"smoke-pi should not pass a redundant --yolo arg because Pi runs in yolo mode by default")
}

func TestCompiledLockFiles_SmokePiKeepsCLIProxySafeoutputsWiring(t *testing.T) {
	sourcePath := filepath.Join(workflowsDir, "smoke-pi.md")
	sourceBytes, err := os.ReadFile(sourcePath)
	require.NoError(t, err, "should read smoke-pi source workflow")

	source := string(sourceBytes)
	assert.Contains(t, source, "cli-proxy: true",
		"smoke-pi source workflow should keep cli-proxy enabled so safeoutputs CLI wrappers are generated")

	lockPath := filepath.Join(workflowsDir, "smoke-pi.lock.yml")
	lockBytes, err := os.ReadFile(lockPath)
	require.NoError(t, err, "should read smoke-pi lock file")

	lockContent := string(lockBytes)
	assert.Contains(t, lockContent, "Mount MCP servers as CLIs",
		"smoke-pi lock file should mount MCP servers as CLI wrappers")
	assert.Contains(t, lockContent, `GH_AW_MCP_CLI_SERVERS='["safeoutputs"]'`,
		"smoke-pi lock file should request the safeoutputs CLI wrapper")
	assert.Contains(t, lockContent, `export PATH="${RUNNER_TEMP}/gh-aw/mcp-cli/bin:$PATH"`,
		"smoke-pi lock file should add mounted MCP CLI wrappers to PATH before executing Pi")
}

func TestCompiledLockFiles_CLIEngineInstallStepNamesIncludeCLISuffix(t *testing.T) {
	tests := []struct {
		lockFile    string
		wantNames   []string
		absentNames []string
	}{
		{
			lockFile: "smoke-crush.lock.yml",
			wantNames: []string{
				"Install Crush CLI",
			},
			absentNames: []string{
				"Install Crush",
			},
		},
		{
			lockFile: "smoke-opencode.lock.yml",
			wantNames: []string{
				"Install OpenCode CLI",
			},
			absentNames: []string{
				"Install OpenCode",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.lockFile, func(t *testing.T) {
			lockPath := filepath.Join(workflowsDir, tt.lockFile)
			lockBytes, err := os.ReadFile(lockPath)
			require.NoError(t, err, "should read %s", tt.lockFile)

			stepNames := map[string]bool{}
			for line := range strings.SplitSeq(string(lockBytes), "\n") {
				line = strings.TrimSpace(line)
				name, ok := strings.CutPrefix(line, "- name: ")
				if ok {
					stepNames[name] = true
				}
			}

			for _, wantName := range tt.wantNames {
				assert.Contains(t, stepNames, wantName)
			}
			for _, absentName := range tt.absentNames {
				assert.NotContains(t, stepNames, absentName)
			}
		})
	}
}

// TestCompiledLockFiles_WorkflowCallOutputs validates that compiled lock files for workflows
// using workflow_call + safe-outputs automatically include on.workflow_call.outputs declarations.
func TestCompiledLockFiles_WorkflowCallOutputs(t *testing.T) {
	mdFiles, err := filepath.Glob(filepath.Join(workflowsDir, "*.md"))
	require.NoError(t, err, "should glob .md workflow files")
	require.NotEmpty(t, mdFiles, "should find at least one .md workflow file")

	checkedWorkflows := 0

	for _, mdPath := range mdFiles {
		lockPath := strings.TrimSuffix(mdPath, ".md") + ".lock.yml"

		safeOutputs, onSection, parseErr := parseSafeOutputsForLockFileTest(mdPath)
		if parseErr != nil || safeOutputs == nil {
			continue
		}
		// Only check workflows that have workflow_call as a trigger.
		if !strings.Contains(onSection, "workflow_call") {
			continue
		}

		lockBytes, err := os.ReadFile(lockPath)
		if err != nil {
			t.Errorf("lock file missing for %s: %v", filepath.Base(mdPath), err)
			continue
		}
		lockContent := string(lockBytes)
		baseName := filepath.Base(mdPath)

		onLockSection := extractWorkflowCallSection(lockContent)

		// The on section must contain a workflow_call: key.
		assert.Contains(t, onLockSection, "workflow_call:", "lock file %s should have workflow_call trigger", baseName)

		workflowCallIdx := strings.Index(onLockSection, "workflow_call:")
		if workflowCallIdx < 0 {
			continue
		}
		workflowCallBlock := onLockSection[workflowCallIdx:]

		// Determine which outputs we expect based on configured safe-output types.
		expectedOutputs := buildWorkflowCallOutputsMap(safeOutputs)
		if len(expectedOutputs) > 0 {
			assert.Contains(t, workflowCallBlock, "outputs:",
				"lock file %s: on.workflow_call should contain an outputs: section", baseName)

			for _, mapping := range lockFileOutputMappings {
				if !mapping.configPresent(safeOutputs) {
					continue
				}
				for _, outputKey := range mapping.workflowCallOutputKeys {
					assert.Contains(t, workflowCallBlock, outputKey,
						"lock file %s: on.workflow_call.outputs should include %s", baseName, outputKey)
				}
			}
		}

		checkedWorkflows++
	}

	assert.Positive(t, checkedWorkflows,
		"should have validated at least one workflow with workflow_call + safe-outputs")
	t.Logf("Validated on.workflow_call.outputs for %d workflow(s)", checkedWorkflows)
}

// TestCompiledLockFiles_NoSpuriousWorkflowCallOutputs validates that workflows WITHOUT
// workflow_call do NOT have outputs injected into the on section.
func TestCompiledLockFiles_NoSpuriousWorkflowCallOutputs(t *testing.T) {
	mdFiles, err := filepath.Glob(filepath.Join(workflowsDir, "*.md"))
	require.NoError(t, err, "should glob .md workflow files")
	require.NotEmpty(t, mdFiles, "should find at least one .md workflow file")

	checkedWorkflows := 0

	for _, mdPath := range mdFiles {
		lockPath := strings.TrimSuffix(mdPath, ".md") + ".lock.yml"

		safeOutputs, onSection, parseErr := parseSafeOutputsForLockFileTest(mdPath)
		if parseErr != nil || safeOutputs == nil {
			continue
		}
		// Only check workflows that have safe-outputs but do NOT use workflow_call.
		if strings.Contains(onSection, "workflow_call") {
			continue
		}

		lockBytes, err := os.ReadFile(lockPath)
		if err != nil {
			continue
		}
		lockContent := string(lockBytes)

		onLockSection := extractWorkflowCallSection(lockContent)
		baseName := filepath.Base(mdPath)

		// If the on section somehow has workflow_call, its outputs sub-key must be absent.
		if strings.Contains(onLockSection, "workflow_call:") {
			workflowCallIdx := strings.Index(onLockSection, "workflow_call:")
			workflowCallBlock := onLockSection[workflowCallIdx:]
			assert.NotContains(t, workflowCallBlock, "outputs:",
				"lock file %s: on.workflow_call should NOT have outputs (no workflow_call trigger in source)", baseName)
		}

		checkedWorkflows++
	}

	assert.Positive(t, checkedWorkflows,
		"should have validated at least one workflow with safe-outputs but without workflow_call")
	t.Logf("Validated no spurious workflow_call outputs for %d workflow(s)", checkedWorkflows)
}

// extractDetectionJobSection returns the text of the detection job block from a lock file.
func extractDetectionJobSection(lockContent string) string {
	return extractJobSection(lockContent, "detection")
}

// TestCompiledLockFiles_SmokeWorkflowsHaveDetectionJobWithAgenticRunCall verifies that the
// smoke-copilot and smoke-claude lock files each contain a detection job with the expected
// outputs and an agentic engine execution step that uses awf.
func TestCompiledLockFiles_SmokeWorkflowsHaveDetectionJobWithAgenticRunCall(t *testing.T) {
	smokeWorkflows := []string{
		"smoke-copilot.lock.yml",
		"smoke-claude.lock.yml",
	}

	for _, lockFile := range smokeWorkflows {
		t.Run(lockFile, func(t *testing.T) {
			lockPath := filepath.Join(workflowsDir, lockFile)
			lockBytes, err := os.ReadFile(lockPath)
			require.NoError(t, err, "should read lock file %s", lockFile)
			lockContent := string(lockBytes)

			detectionJob := extractDetectionJobSection(lockContent)
			require.NotEmpty(t, detectionJob, "lock file %s should contain a detection job", lockFile)

			t.Run("HasDetectionConclusionOutput", func(t *testing.T) {
				assert.Contains(t, detectionJob, "detection_conclusion:",
					"detection job should expose detection_conclusion output")
			})

			t.Run("HasDetectionSuccessOutput", func(t *testing.T) {
				assert.Contains(t, detectionJob, "detection_success:",
					"detection job should expose detection_success output")
			})

			t.Run("HasAgenticExecutionStepID", func(t *testing.T) {
				assert.Contains(t, detectionJob, "id: detection_agentic_execution",
					"detection job should have an agentic execution step with id: detection_agentic_execution")
			})

			t.Run("AgenticExecutionStepHasContinueOnError", func(t *testing.T) {
				// The detection_agentic_execution step must have continue-on-error: true so that
				// infrastructure failures (e.g. unhealthy AWF container, CLI errors) do not mark
				// the detection job as failed. The "Parse and conclude" step always runs and
				// handles missing/incomplete detection logs as parse_error in warn mode (exit 0).
				stepName := "- name: Execute"
				stepID := "id: detection_agentic_execution"

				startIdx := strings.Index(detectionJob, stepName)
				require.NotEqual(t, -1, startIdx, "detection job must contain an Execute step")

				// Find the last occurrence of the Execute step before detection_agentic_execution.
				idIdx := strings.Index(detectionJob, stepID)
				require.NotEqual(t, -1, idIdx, "detection job must contain %q", stepID)

				// Extract the step block from the Execute step name to the id line.
				stepBlock := detectionJob[strings.LastIndex(detectionJob[:idIdx], stepName):]
				nextStep := strings.Index(stepBlock, "\n      - ")
				if nextStep != -1 {
					stepBlock = stepBlock[:nextStep]
				}

				assert.Contains(t, stepBlock, "continue-on-error: true",
					"detection_agentic_execution step must have continue-on-error: true to prevent infrastructure failures from failing the detection job")
			})

			t.Run("AgenticExecutionStepUsesAWF", func(t *testing.T) {
				// Narrow the check to the detection_agentic_execution step block.
				stepID := "id: detection_agentic_execution"
				startIdx := strings.Index(detectionJob, stepID)
				require.NotEqual(t, -1, startIdx, "detection job must contain %q", stepID)

				agenticStepSection := detectionJob[startIdx:]

				// Heuristically end the block at the start of the next step.
				if nextStepIdx := strings.Index(agenticStepSection, "\n      - "); nextStepIdx != -1 {
					agenticStepSection = agenticStepSection[:nextStepIdx]
				}

				assert.Contains(t, agenticStepSection, "awf",
					"detection_agentic_execution step should use awf for sandboxed execution")
			})
		})
	}
}

// TestCompiledLockFiles_SmokeWorkflowCallHasExpectedOutputs is a focused test on the
// smoke-workflow-call workflow, the canonical workflow_call example in this repo.
func TestCompiledLockFiles_SmokeWorkflowCallHasExpectedOutputs(t *testing.T) {
	lockPath := filepath.Join(workflowsDir, "smoke-workflow-call.lock.yml")
	mdPath := filepath.Join(workflowsDir, "smoke-workflow-call.md")

	safeOutputs, onSection, err := parseSafeOutputsForLockFileTest(mdPath)
	require.NoError(t, err, "should parse smoke-workflow-call.md")
	require.NotNil(t, safeOutputs, "smoke-workflow-call.md should have safe-outputs")

	lockBytes, err := os.ReadFile(lockPath)
	require.NoError(t, err, "smoke-workflow-call.lock.yml should be readable")
	lockContent := string(lockBytes)

	t.Run("SourceHasWorkflowCallTrigger", func(t *testing.T) {
		assert.Contains(t, onSection, "workflow_call", "source md should have workflow_call trigger")
	})

	t.Run("LockHasWorkflowCallTrigger", func(t *testing.T) {
		assert.Contains(t, lockContent, "workflow_call:", "lock file should contain workflow_call trigger")
	})

	t.Run("LockHasAwContextWorkflowCallInput", func(t *testing.T) {
		onLockSection := extractWorkflowCallSection(lockContent)
		workflowCallIdx := strings.Index(onLockSection, "workflow_call:")
		require.GreaterOrEqual(t, workflowCallIdx, 0, "on section should contain workflow_call:")
		workflowCallBlock := onLockSection[workflowCallIdx:]
		assert.Contains(t, workflowCallBlock, "inputs:", "on.workflow_call should have inputs")
		assert.Contains(t, workflowCallBlock, "aw_context:", "on.workflow_call.inputs should include aw_context")
		assert.Contains(t, workflowCallBlock, "type: string", "aw_context workflow_call input should be typed as string")
	})

	t.Run("LockHasSafeOutputsJob", func(t *testing.T) {
		assert.Contains(t, lockContent, "safe_outputs:", "lock file should contain safe_outputs job")
	})

	t.Run("LockUploadsOTELMirrorInAgentArtifact", func(t *testing.T) {
		assert.Contains(t, lockContent, "/tmp/gh-aw/otel.jsonl",
			"smoke-workflow-call agent artifact should include the OTEL JSONL mirror")
		assert.Contains(t, lockContent, "/tmp/gh-aw/otlp-export-errors.jsonl",
			"smoke-workflow-call agent artifact should include OTLP export failure details")
	})

	// The smoke workflow uses add-comment – verify its outputs appear in both places.
	require.NotNil(t, safeOutputs.AddComments, "smoke-workflow-call.md should have add-comment configured")

	t.Run("WorkflowCallOutputs_CommentID", func(t *testing.T) {
		onLockSection := extractWorkflowCallSection(lockContent)
		workflowCallIdx := strings.Index(onLockSection, "workflow_call:")
		require.GreaterOrEqual(t, workflowCallIdx, 0, "on section should contain workflow_call:")
		workflowCallBlock := onLockSection[workflowCallIdx:]
		assert.Contains(t, workflowCallBlock, "outputs:", "on.workflow_call should have outputs")
		assert.Contains(t, workflowCallBlock, "comment_id:", "on.workflow_call.outputs should include comment_id")
		assert.Contains(t, workflowCallBlock, "comment_url:", "on.workflow_call.outputs should include comment_url")
		assert.Contains(t, workflowCallBlock, "jobs.safe_outputs.outputs.comment_id",
			"workflow_call output value should reference safe_outputs job")
	})

	t.Run("SafeOutputsJobOutputs_CommentID", func(t *testing.T) {
		safeOutputsJob := extractSafeOutputsJobSection(lockContent)
		assert.Contains(t, safeOutputsJob, "comment_id:", "safe_outputs job should expose comment_id output")
		assert.Contains(t, safeOutputsJob, "comment_url:", "safe_outputs job should expose comment_url output")
		assert.Contains(t, safeOutputsJob, "steps.process_safe_outputs.outputs.comment_id",
			"safe_outputs job output should reference process_safe_outputs step")
	})
}

func TestCompiledLockFiles_SmokeCallWorkflowForwardsPayload(t *testing.T) {
	lockPath := filepath.Join(workflowsDir, "smoke-call-workflow.lock.yml")

	lockBytes, err := os.ReadFile(lockPath)
	require.NoError(t, err, "smoke-call-workflow.lock.yml should be readable")
	lockContent := string(lockBytes)

	t.Run("CallWorkflowJobForwardsPayloadAndTaskDescription", func(t *testing.T) {
		assert.Contains(t, lockContent, "call-smoke-workflow-call:", "lock file should contain the call-workflow job")
		// smoke-workflow-call.md declares 'payload' and 'task-description' as workflow_call inputs;
		// the caller job forwards both from the safe_outputs payload.
		assert.Contains(t, lockContent, "payload: ${{ needs.safe_outputs.outputs.call_workflow_payload }}", "call-workflow job should forward payload from safe_outputs")
		assert.Contains(t, lockContent, "${{ fromJSON(needs.safe_outputs.outputs.call_workflow_payload)['task-description'] }}", "call-workflow job should forward task-description from the handler payload")
	})
}
