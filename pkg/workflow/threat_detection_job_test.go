//go:build !integration

package workflow

import (
	"slices"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

// TestDetectionJobLevelCondition verifies that the detection job-level `if:` condition
// always runs the detection job when the agent ran (not skipped), regardless of whether
// the agent produced any outputs. This ensures detection is never bypassed for noop/boop runs;
// the detection_guard step inside the job handles the no-output case.
func TestDetectionJobLevelCondition(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		Name: "test-workflow",
		AI:   "copilot",
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
			CreateIssues: &CreateIssuesConfig{
				TitlePrefix: "[Test]",
			},
		},
	}

	job, err := compiler.buildDetectionJob(data)
	if err != nil {
		t.Fatalf("Unexpected error building detection job: %v", err)
	}
	if job == nil {
		t.Fatal("Expected detection job to be built, got nil")
	}

	condition := job.If

	// Must use always() so the job runs even when the agent job fails
	if !strings.Contains(condition, "always()") {
		t.Errorf("Expected detection job condition to include always(), got: %q", condition)
	}

	// Must skip when agent was skipped
	if !strings.Contains(condition, "needs."+string(constants.AgentJobName)+".result") {
		t.Errorf("Expected detection job condition to check agent result, got: %q", condition)
	}
	if !strings.Contains(condition, "'skipped'") {
		t.Errorf("Expected detection job condition to check for skipped status, got: %q", condition)
	}

	// Must NOT require output_types or has_patch — detection runs unconditionally when the agent ran,
	// and the detection_guard step inside the job handles the no-output case.
	if strings.Contains(condition, "outputs.output_types") {
		t.Errorf("Detection job condition must not gate on output_types; got: %q", condition)
	}
	if strings.Contains(condition, "outputs.has_patch") {
		t.Errorf("Detection job condition must not gate on has_patch; got: %q", condition)
	}
}

// TestDetectionJobPermissionsIndentation verifies that the detection job's permissions block
// is correctly indented in the rendered YAML output.
// Regression test for the indentation bug where c.indentYAMLLines was called on
// RenderToYAML() output which already uses 6-space indentation for permission values,
// resulting in 10-space indentation instead of the correct 6.
func TestDetectionJobPermissionsIndentation(t *testing.T) {
	tests := []struct {
		name            string
		data            *WorkflowData
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "copilot-requests permission produces correctly indented permissions",
			data: &WorkflowData{
				Name: "test-workflow",
				AI:   "copilot",
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{},
				},
				Permissions: "permissions:\n  copilot-requests: write",
			},
			// permission values must be indented by exactly 6 spaces (4 for job key + 2 for sub-key)
			wantContains: []string{
				"      copilot-requests: write",
				"COPILOT_GITHUB_TOKEN: ${{ github.token }}",
			},
			// Over-indented value (10 spaces) must not appear - this was the bug
			wantNotContains: []string{
				"          copilot-requests: write",
				"COPILOT_GITHUB_TOKEN: ${{ secrets.COPILOT_GITHUB_TOKEN }}",
			},
		},
		{
			name: "copilot-requests permission omitted from output when not configured",
			data: &WorkflowData{
				Name: "test-workflow",
				AI:   "copilot",
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{},
				},
			},
			// copilot-requests should not be in the output when the permission is not set
			wantContains:    []string{},
			wantNotContains: []string{"copilot-requests: write"},
		},
		{
			name: "github-oidc engine auth adds id-token: write to detection job",
			data: &WorkflowData{
				Name: "test-workflow",
				AI:   "claude",
				EngineConfig: &EngineConfig{
					ID:   "claude",
					Auth: &EngineAuthConfig{Type: "github-oidc"},
				},
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{},
				},
				Permissions: "permissions:\n  id-token: write",
			},
			wantContains:    []string{"      id-token: write"},
			wantNotContains: []string{},
		},
		{
			name: "observability.otlp.github-app auth adds id-token: write to detection job",
			data: &WorkflowData{
				Name: "test-workflow",
				AI:   "copilot",
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{},
				},
				RawFrontmatter: map[string]any{
					"observability": map[string]any{
						"otlp": map[string]any{
							"github-app": map[string]any{},
						},
					},
				},
			},
			wantContains:    []string{"      id-token: write"},
			wantNotContains: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()

			job, err := compiler.buildDetectionJob(tt.data)
			if err != nil {
				t.Fatalf("buildDetectionJob() error: %v", err)
			}
			if job == nil {
				t.Fatal("buildDetectionJob() returned nil job")
			}

			if err := compiler.jobManager.AddJob(job); err != nil {
				t.Fatalf("AddJob() error: %v", err)
			}

			var yamlBuf strings.Builder
			compiler.jobManager.WriteJobsYAML(&yamlBuf)
			yamlOutput := yamlBuf.String()

			for _, expected := range tt.wantContains {
				if !strings.Contains(yamlOutput, expected) {
					t.Errorf("YAML output should contain %q, but got:\n%s", expected, yamlOutput)
				}
			}
			for _, unexpected := range tt.wantNotContains {
				if strings.Contains(yamlOutput, unexpected) {
					t.Errorf("YAML output should NOT contain %q, but got:\n%s", unexpected, yamlOutput)
				}
			}
		})
	}
}

// TestWorkspaceCheckoutForDetectionStep verifies that a conditional checkout step
// is added to the detection job when threat detection is enabled, allowing the
// engine to see patches in the context of the full repository.
func TestWorkspaceCheckoutForDetectionStep(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		Name: "test-workflow",
		AI:   "copilot",
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	job, err := compiler.buildDetectionJob(data)
	if err != nil {
		t.Fatalf("buildDetectionJob() error: %v", err)
	}
	if job == nil {
		t.Fatal("buildDetectionJob() returned nil job")
	}

	stepsString := strings.Join(job.Steps, "")

	// Workspace checkout step should be present
	if !strings.Contains(stepsString, "Checkout repository for patch context") {
		t.Error("Detection job should include workspace checkout step")
	}

	// Step should be conditional on has_patch
	expectedCondition := "if: needs." + string(constants.AgentJobName) + ".outputs.has_patch == 'true'"
	if !strings.Contains(stepsString, expectedCondition) {
		t.Errorf("Workspace checkout step should have has_patch condition, expected %q in steps", expectedCondition)
	}

	// Step should disable credential persistence
	if !strings.Contains(stepsString, "persist-credentials: false") {
		t.Error("Workspace checkout step should set persist-credentials: false")
	}

	// Step should use pinned actions/checkout
	checkoutPin := getActionPin("actions/checkout")
	if checkoutPin == "" {
		t.Fatal("Expected actions/checkout to have a pin")
	}
	if !strings.Contains(stepsString, checkoutPin) {
		t.Errorf("Workspace checkout step should use pinned action %q", checkoutPin)
	}
}

// TestDetectionJobAlwaysHasContentsRead verifies that the detection job always
// receives contents: read permission (required for the workspace checkout step),
// even in production mode.
func TestDetectionJobAlwaysHasContentsRead(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		Name: "test-workflow",
		AI:   "copilot",
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	job, err := compiler.buildDetectionJob(data)
	if err != nil {
		t.Fatalf("buildDetectionJob() error: %v", err)
	}
	if job == nil {
		t.Fatal("buildDetectionJob() returned nil job")
	}

	// contents: read should be present in all modes
	if !strings.Contains(job.Permissions, "contents: read") {
		t.Errorf("Detection job should always have contents: read permission, got permissions:\n%s", job.Permissions)
	}
}

// TestWorkspaceCheckoutPresentWithCustomSteps verifies that when the
// detection engine is disabled but custom steps exist, the detection job
// still includes the workspace checkout step (custom steps may also need context).
func TestWorkspaceCheckoutPresentWithCustomSteps(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		Name: "test-workflow",
		AI:   "copilot",
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{
				EngineDisabled: true,
				Steps: []any{
					map[string]any{"name": "Custom check", "run": "echo custom"},
				},
			},
		},
	}

	job, err := compiler.buildDetectionJob(data)
	if err != nil {
		t.Fatalf("buildDetectionJob() error: %v", err)
	}
	if job == nil {
		t.Fatal("buildDetectionJob() returned nil job, but custom steps are configured")
	}

	stepsString := strings.Join(job.Steps, "")
	if !strings.Contains(stepsString, "Checkout repository for patch context") {
		t.Error("Detection job with custom steps should still include workspace checkout step")
	}
}

// TestWorkspaceCheckoutStepOrdering verifies that the workspace checkout step
// appears after the artifact download and before the detection steps.
func TestWorkspaceCheckoutStepOrdering(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		Name: "test-workflow",
		AI:   "copilot",
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	job, err := compiler.buildDetectionJob(data)
	if err != nil {
		t.Fatalf("buildDetectionJob() error: %v", err)
	}
	if job == nil {
		t.Fatal("buildDetectionJob() returned nil job")
	}

	stepsString := strings.Join(job.Steps, "")

	downloadIdx := strings.Index(stepsString, "Download agent output artifact")
	checkoutIdx := strings.Index(stepsString, "Checkout repository for patch context")
	guardIdx := strings.Index(stepsString, "Check if detection needed")

	if downloadIdx < 0 {
		t.Fatal("Expected 'Download agent output artifact' step in detection job")
	}
	if checkoutIdx < 0 {
		t.Fatal("Expected 'Checkout repository for patch context' step in detection job")
	}
	if guardIdx < 0 {
		t.Fatal("Expected 'Check if detection needed' step in detection job")
	}

	if checkoutIdx < downloadIdx {
		t.Error("Workspace checkout step should appear after artifact download step")
	}
	if checkoutIdx > guardIdx {
		t.Error("Workspace checkout step should appear before detection guard step")
	}
}

func TestDetectionJobDownloadsActivationArtifactBeforeAgentOutput(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		Name: "test-workflow",
		AI:   "copilot",
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	job, err := compiler.buildDetectionJob(data)
	if err != nil {
		t.Fatalf("buildDetectionJob() error: %v", err)
	}
	if job == nil {
		t.Fatal("buildDetectionJob() returned nil job")
	}

	stepsString := strings.Join(job.Steps, "")
	activationDownloadIdx := strings.Index(stepsString, "Download activation artifact")
	agentDownloadIdx := strings.Index(stepsString, "Download agent output artifact")
	prepareIdx := strings.Index(stepsString, "Prepare threat detection files")

	if activationDownloadIdx < 0 {
		t.Fatal("Expected 'Download activation artifact' step in detection job")
	}
	if agentDownloadIdx < 0 {
		t.Fatal("Expected 'Download agent output artifact' step in detection job")
	}
	if prepareIdx < 0 {
		t.Fatal("Expected 'Prepare threat detection files' step in detection job")
	}
	if activationDownloadIdx > agentDownloadIdx {
		t.Error("Activation artifact download should appear before agent output download")
	}
	if agentDownloadIdx > prepareIdx {
		t.Error("Agent output download should appear before detection file preparation")
	}
	if !strings.Contains(stepsString, "name: activation") {
		t.Error("Detection job should download the activation artifact so prompt files are available")
	}
	if !strings.Contains(stepsString, "path: /tmp/gh-aw") {
		t.Error("Activation artifact should download into /tmp/gh-aw for prompt staging")
	}
}

func TestDetectionActivationArtifactDownloadUsesActivationPrefixForWorkflowCall(t *testing.T) {
	steps := strings.Join(buildDetectionActivationArtifactDownloadSteps(&WorkflowData{
		On: "workflow_call",
	}, getActionPin), "")

	expected := "name: ${{ steps.artifact-prefix.outputs.prefix }}activation"
	if !strings.Contains(steps, expected) {
		t.Fatalf("Expected workflow_call detection activation download to use %q, got:\n%s", expected, steps)
	}
	if strings.Contains(steps, "needs.agent.outputs.artifact_prefix") {
		t.Fatalf("Detection activation artifact download must not use the agent prefix, got:\n%s", steps)
	}
}

// TestBuildDetectionJobNeedsIncludesMainEngineEnvJobs verifies that when the main
// engine env contains a needs expression and a detection-specific engine config also
// exists, the referenced custom job is still added to the detection job's needs.
// This tests the merged-env dependency scan path.
func TestBuildDetectionJobNeedsIncludesMainEngineEnvJobs(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		AI: "codex",
		EngineConfig: &EngineConfig{
			ID: "codex",
			Env: map[string]string{
				// This expression references a custom job "router" from the main engine env.
				"OPENAI_BASE_URL": "${{ needs.router.outputs.url }}",
			},
		},
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{
				// Detection-specific config exists, which previously caused the scan
				// to use only this env, missing the main engine env expression above.
				EngineConfig: &EngineConfig{
					ID: "codex",
					Env: map[string]string{
						"CUSTOM_FLAG": "1",
					},
				},
			},
		},
		Jobs: map[string]any{
			"router": map[string]any{},
		},
	}

	job, err := compiler.buildDetectionJob(data)
	if err != nil {
		t.Fatalf("buildDetectionJob() error: %v", err)
	}
	if job == nil {
		t.Fatal("buildDetectionJob() returned nil job")
	}

	if !slices.Contains(job.Needs, "router") {
		t.Fatalf("expected detection job needs to include 'router' (referenced via main engine OPENAI_BASE_URL); got needs: %v", job.Needs)
	}

	data.SafeOutputs.ThreatDetection.EngineConfig.ID = "copilot"
	data.SafeOutputs.ThreatDetection.EngineConfig.Env["CUSTOM_FLAG"] = "${{ needs.detection_router.outputs.flag }}"
	data.Jobs["detection_router"] = map[string]any{}
	job, err = compiler.buildDetectionJob(data)
	if err != nil {
		t.Fatalf("buildDetectionJob() with different engine error: %v", err)
	}
	if slices.Contains(job.Needs, "router") {
		t.Fatalf("different detection engine must not depend on main provider job; got needs: %v", job.Needs)
	}
	if !slices.Contains(job.Needs, "detection_router") {
		t.Fatalf("detection-specific env job dependency was lost; got needs: %v", job.Needs)
	}
}

// TestDetectionJobEnvironmentInheritance verifies that the detection job correctly
// handles all three environment wiring scenarios:
//  1. No environment configured → detection job has no environment field.
//  2. Top-level data.Environment is set → detection job inherits it unconditionally.
//  3. ThreatDetectionConfig.Environment override is set → raw name is normalised to
//     "environment: <name>" and takes precedence over data.Environment.
//
// Also verifies that multi-line environment blocks (environment:\n  name: …\n  url: …)
// are indented correctly so the compiled YAML remains valid.
func TestDetectionJobEnvironmentInheritance(t *testing.T) {
	tests := []struct {
		name            string
		topLevelEnv     string
		detectionEnv    string
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:            "no environment configured",
			topLevelEnv:     "",
			detectionEnv:    "",
			wantNotContains: []string{"environment:"},
		},
		{
			name:            "inherits top-level simple environment",
			topLevelEnv:     "environment: production",
			detectionEnv:    "",
			wantContains:    []string{"    environment: production"},
			wantNotContains: []string{},
		},
		{
			name:         "inherits top-level multi-line environment and indents correctly",
			topLevelEnv:  "environment:\n  name: production\n  url: https://example.com",
			detectionEnv: "",
			// After indentYAMLLines("    "), lines 2+ gain 4 extra spaces.
			wantContains: []string{
				"    environment:",
				"      name: production",
				"      url: https://example.com",
			},
			wantNotContains: []string{},
		},
		{
			name:            "threat-detection environment override normalises raw name",
			topLevelEnv:     "",
			detectionEnv:    "aoai-model",
			wantContains:    []string{"    environment: aoai-model"},
			wantNotContains: []string{},
		},
		{
			name:            "threat-detection environment override takes precedence over top-level",
			topLevelEnv:     "environment: production",
			detectionEnv:    "aoai-model",
			wantContains:    []string{"    environment: aoai-model"},
			wantNotContains: []string{"environment: production"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()

			data := &WorkflowData{
				Name:        "test-workflow",
				AI:          "copilot",
				Environment: tt.topLevelEnv,
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{
						Environment: tt.detectionEnv,
					},
				},
			}

			job, err := compiler.buildDetectionJob(data)
			if err != nil {
				t.Fatalf("buildDetectionJob() error: %v", err)
			}
			if job == nil {
				t.Fatal("buildDetectionJob() returned nil job")
			}

			if err := compiler.jobManager.AddJob(job); err != nil {
				t.Fatalf("AddJob() error: %v", err)
			}

			var yamlBuf strings.Builder
			compiler.jobManager.WriteJobsYAML(&yamlBuf)
			yamlOutput := yamlBuf.String()

			for _, want := range tt.wantContains {
				if !strings.Contains(yamlOutput, want) {
					t.Errorf("YAML output should contain %q\ngot:\n%s", want, yamlOutput)
				}
			}
			for _, notWant := range tt.wantNotContains {
				if strings.Contains(yamlOutput, notWant) {
					t.Errorf("YAML output should NOT contain %q\ngot:\n%s", notWant, yamlOutput)
				}
			}
		})
	}
}
