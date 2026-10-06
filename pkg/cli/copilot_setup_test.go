//go:build !integration

package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/actionpins"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow"

	"github.com/goccy/go-yaml"
)

// mockSHAResolver is a test double for workflow.SHAResolver that returns a fixed SHA
type mockSHAResolver struct {
	sha string
	err error
}

func (m *mockSHAResolver) ResolveSHA(_ context.Context, _, _ string) (string, error) {
	return m.sha, m.err
}
func TestEnsureCopilotSetupSteps(t *testing.T) {
	tests := []struct {
		name             string
		existingWorkflow *workflow.WorkflowFile
		verbose          bool
		wantErr          bool
		validateContent  func(*testing.T, []byte)
	}{
		{
			name:    "creates new copilot-setup-steps.yml",
			verbose: false,
			wantErr: false,
			validateContent: func(t *testing.T, content []byte) {
				if !strings.Contains(string(content), "copilot-setup-steps") {
					t.Error("Expected workflow to contain 'copilot-setup-steps' job name")
				}
				if !strings.Contains(string(content), "install-gh-aw.sh") {
					t.Error("Expected workflow to contain install-gh-aw.sh bash script")
				}
				if !strings.Contains(string(content), "curl -fsSL") {
					t.Error("Expected workflow to contain curl command")
				}
			},
		},
		{
			name: "skips update when extension install already exists",
			existingWorkflow: &workflow.WorkflowFile{
				Name: "Copilot Setup Steps",
				On:   "workflow_dispatch",
				Jobs: map[string]workflow.WorkflowFileJob{
					"copilot-setup-steps": {
						RunsOn: "ubuntu-latest",
						Steps: []workflow.WorkflowStep{
							{
								Name: "Checkout code",
								Uses: "actions/checkout@v5",
							},
							{
								Name: "Install gh-aw extension",
								Run:  "curl -fsSL https://raw.githubusercontent.com/github/gh-aw/refs/heads/main/install-gh-aw.sh | bash",
							},
						},
					},
				},
			},
			verbose: true,
			wantErr: false,
			validateContent: func(t *testing.T, content []byte) {
				// Should not modify existing correct config
				count := strings.Count(string(content), "Install gh-aw extension")
				if count != 1 {
					t.Errorf("Expected exactly 1 occurrence of 'Install gh-aw extension', got %d", count)
				}
			},
		},
		{
			name: "skips update when new download+verify install already exists",
			existingWorkflow: &workflow.WorkflowFile{
				Name: "Copilot Setup Steps",
				On:   "workflow_dispatch",
				Jobs: map[string]workflow.WorkflowFileJob{
					"copilot-setup-steps": {
						RunsOn: "ubuntu-latest",
						Steps: []workflow.WorkflowStep{
							{
								Name: "Install gh-aw extension",
								Run: "install_script=\"$(mktemp)\"\n" +
									"trap 'rm -f \"$install_script\"' EXIT\n" +
									"curl -fsSL https://raw.githubusercontent.com/github/gh-aw/" + copilotSetupStepsStaticSHA + "/install-gh-aw.sh -o \"$install_script\"\n" +
									"printf '%s  %s\\n' \"" + copilotSetupStepsStaticSHA256 + "\" \"$install_script\" | sha256sum -c -\n" +
									"bash \"$install_script\"",
							},
						},
					},
				},
			},
			verbose: true,
			wantErr: false,
			validateContent: func(t *testing.T, content []byte) {
				// File should remain unchanged when download+verify syntax is already present
				count := strings.Count(string(content), "Install gh-aw extension")
				if count != 1 {
					t.Errorf("Expected exactly 1 occurrence of 'Install gh-aw extension', got %d", count)
				}
				// sha256sum check should be preserved
				if !strings.Contains(string(content), "sha256sum") {
					t.Error("Expected sha256sum check to be preserved in existing download+verify file")
				}
			},
		},
		{
			name: "renders instructions for existing workflow without install step",
			existingWorkflow: &workflow.WorkflowFile{
				Name: "Copilot Setup Steps",
				On:   "workflow_dispatch",
				Jobs: map[string]workflow.WorkflowFileJob{
					"copilot-setup-steps": {
						RunsOn: "ubuntu-latest",
						Steps: []workflow.WorkflowStep{
							{
								Name: "Some existing step",
								Run:  "echo 'existing'",
							},
							{
								Name: "Build",
								Run:  "echo 'build'",
							},
						},
					},
				},
			},
			verbose: false,
			wantErr: false,
			validateContent: func(t *testing.T, content []byte) {
				// File should NOT be modified - should remain with only 2 steps
				var wf workflow.WorkflowFile
				if err := yaml.Unmarshal(content, &wf); err != nil {
					t.Fatalf("Failed to unmarshal workflow YAML: %v", err)
				}
				job, ok := wf.Jobs["copilot-setup-steps"]
				if !ok {
					t.Fatalf("Expected job 'copilot-setup-steps' not found")
				}

				// File should remain unchanged with only 2 existing steps
				if len(job.Steps) != 2 {
					t.Errorf("Expected 2 steps (file should not be modified), got %d", len(job.Steps))
				}

				// Verify the install step was NOT injected
				if job.Steps[0].Name == "Install gh-aw extension" {
					t.Errorf("Expected 'Install gh-aw extension' step to NOT be injected (instructions should be rendered)")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "test-*")

			originalDir, err := os.Getwd()
			if err != nil {
				t.Fatalf("Failed to get current directory: %v", err)
			}
			defer func() {
				_ = os.Chdir(originalDir)
			}()

			if err := os.Chdir(tmpDir); err != nil {
				t.Fatalf("Failed to change to temp directory: %v", err)
			}

			// Create existing workflow if specified
			if tt.existingWorkflow != nil {
				workflowsDir := filepath.Join(".github", "workflows")
				if err := os.MkdirAll(workflowsDir, 0755); err != nil {
					t.Fatalf("Failed to create workflows directory: %v", err)
				}

				data, err := yaml.Marshal(tt.existingWorkflow)
				if err != nil {
					t.Fatalf("Failed to marshal existing workflow: %v", err)
				}

				setupStepsPath := filepath.Join(workflowsDir, "copilot-setup-steps.yml")
				if err := os.WriteFile(setupStepsPath, data, 0644); err != nil {
					t.Fatalf("Failed to write existing workflow: %v", err)
				}
			}

			// Call the function
			err = ensureCopilotSetupSteps(context.Background(), tt.verbose, workflow.ActionModeDev, "dev")

			if (err != nil) != tt.wantErr {
				t.Errorf("ensureCopilotSetupSteps(context.Background()) error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			// Verify the file was created/updated
			setupStepsPath := filepath.Join(".github", "workflows", "copilot-setup-steps.yml")
			content, err := os.ReadFile(setupStepsPath)
			if err != nil {
				t.Fatalf("Failed to read copilot-setup-steps.yml: %v", err)
			}

			// Run custom validation if provided
			if tt.validateContent != nil {
				tt.validateContent(t, content)
			}
		})
	}
}

func TestWorkflowFileMarshaling(t *testing.T) {
	t.Parallel()

	workflowFile := workflow.WorkflowFile{
		Name: "Test Workflow",
		On:   "push",
		Jobs: map[string]workflow.WorkflowFileJob{
			"test-job": {
				RunsOn: "ubuntu-latest",
				Permissions: &workflow.WorkflowFilePermissions{
					Scopes: map[string]string{
						"contents": "read",
					},
				},
				Steps: []workflow.WorkflowStep{
					{
						Name: "Checkout",
						Uses: "actions/checkout@v5",
					},
					{
						Name: "Run script",
						Run:  "echo 'test'",
						Env: map[string]string{
							"TEST_VAR": "value",
						},
					},
				},
			},
		},
	}

	// Marshal to YAML
	data, err := yaml.Marshal(&workflowFile)
	if err != nil {
		t.Fatalf("Failed to marshal workflow: %v", err)
	}

	// Unmarshal back
	var unmarshaledWorkflow workflow.WorkflowFile
	if err := yaml.Unmarshal(data, &unmarshaledWorkflow); err != nil {
		t.Fatalf("Failed to unmarshal workflow: %v", err)
	}

	// Verify structure
	if unmarshaledWorkflow.Name != "Test Workflow" {
		t.Errorf("Expected name 'Test Workflow', got %q", unmarshaledWorkflow.Name)
	}

	job, exists := unmarshaledWorkflow.Jobs["test-job"]
	if !exists {
		t.Fatal("Expected 'test-job' to exist")
	}

	if len(job.Steps) != 2 {
		t.Errorf("Expected 2 steps, got %d", len(job.Steps))
	}
}

func TestCopilotSetupStepsYAMLConstant(t *testing.T) {
	t.Parallel()

	// Verify the constant can be parsed
	var workflowFile workflow.WorkflowFile
	if err := yaml.Unmarshal([]byte(copilotSetupStepsYAML), &workflowFile); err != nil {
		t.Fatalf("Failed to parse copilotSetupStepsYAML constant: %v", err)
	}

	// Verify key elements
	if workflowFile.Name != "Copilot Setup Steps" {
		t.Errorf("Expected workflow name 'Copilot Setup Steps', got %q", workflowFile.Name)
	}

	job, exists := workflowFile.Jobs["copilot-setup-steps"]
	if !exists {
		t.Fatal("Expected 'copilot-setup-steps' job to exist")
	}

	// Verify it has the extension install step
	hasExtensionInstall := false
	hasSecurePattern := false
	for _, step := range job.Steps {
		if strings.Contains(step.Run, "install-gh-aw.sh") || strings.Contains(step.Run, "curl -fsSL") {
			hasExtensionInstall = true
		}
		// Secure pattern: download to temp file (-o <path>) AND sha256sum integrity check
		if strings.Contains(step.Run, " -o ") && strings.Contains(step.Run, "sha256sum") {
			hasSecurePattern = true
		}
		// Ensure no direct curl|bash pipe on the same line (RGS-018 security issue)
		for line := range strings.SplitSeq(step.Run, "\n") {
			if strings.Contains(line, "curl") && strings.Contains(line, "| bash") {
				t.Errorf("Template must not use curl|bash direct pipe pattern on line %q (RGS-018 security issue)", line)
			}
		}
	}

	if !hasExtensionInstall {
		t.Error("Expected copilotSetupStepsYAML to contain extension install step with bash script")
	}
	if !hasSecurePattern {
		t.Error("Expected copilotSetupStepsYAML to use download-to-file (-o) with sha256sum integrity verification (RGS-018 fix)")
	}

	// Verify it does NOT have checkout, Go setup or build steps (for universal use)
	for _, step := range job.Steps {
		if strings.Contains(step.Name, "Checkout") || strings.Contains(step.Uses, "checkout@") {
			t.Error("Template should not contain 'Checkout' step - not mandatory for extension install")
		}
		if strings.Contains(step.Name, "Set up Go") {
			t.Error("Template should not contain 'Set up Go' step for universal use")
		}
		if strings.Contains(step.Name, "Build gh-aw from source") {
			t.Error("Template should not contain 'Build gh-aw from source' step for universal use")
		}
		if strings.Contains(step.Run, "make build") {
			t.Error("Template should not contain 'make build' command for universal use")
		}
	}
}

func TestEnsureCopilotSetupStepsFilePermissions(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	err = ensureCopilotSetupSteps(context.Background(), false, workflow.ActionModeDev, "dev")
	if err != nil {
		t.Fatalf("ensureCopilotSetupSteps(context.Background()) failed: %v", err)
	}

	// Check file permissions
	setupStepsPath := filepath.Join(".github", "workflows", "copilot-setup-steps.yml")
	info, err := os.Stat(setupStepsPath)
	if err != nil {
		t.Fatalf("Failed to stat copilot-setup-steps.yml: %v", err)
	}

	// Verify file is readable and writable
	mode := info.Mode()
	if mode.Perm()&0600 != 0600 {
		t.Errorf("Expected file to have at least 0600 permissions, got %o", mode.Perm())
	}
}

func TestWorkflowStepYAMLStructure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		step workflow.WorkflowStep
	}{
		{
			name: "step with uses",
			step: workflow.WorkflowStep{
				Name: "Checkout",
				Uses: "actions/checkout@v5",
			},
		},
		{
			name: "step with run",
			step: workflow.WorkflowStep{
				Name: "Run command",
				Run:  "echo 'test'",
			},
		},
		{
			name: "step with environment",
			step: workflow.WorkflowStep{
				Name: "Run with env",
				Run:  "echo $TEST",
				Env: map[string]string{
					"TEST": "value",
				},
			},
		},
		{
			name: "step with with parameters",
			step: workflow.WorkflowStep{
				Name: "Setup",
				Uses: "actions/setup-go@v6",
				With: map[string]any{
					"go-version": "1.21",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal to YAML
			data, err := yaml.Marshal(&tt.step)
			if err != nil {
				t.Fatalf("Failed to marshal step: %v", err)
			}

			// Unmarshal back
			var unmarshaledStep workflow.WorkflowStep
			if err := yaml.Unmarshal(data, &unmarshaledStep); err != nil {
				t.Fatalf("Failed to unmarshal step: %v", err)
			}

			// Verify name is preserved
			if unmarshaledStep.Name != tt.step.Name {
				t.Errorf("Expected name %q, got %q", tt.step.Name, unmarshaledStep.Name)
			}
		})
	}
}

func TestEnsureCopilotSetupStepsDirectoryCreation(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Call function when .github/workflows doesn't exist
	err = ensureCopilotSetupSteps(context.Background(), false, workflow.ActionModeDev, "dev")
	if err != nil {
		t.Fatalf("ensureCopilotSetupSteps(context.Background()) failed: %v", err)
	}

	// Verify directory structure was created
	workflowsDir := filepath.Join(".github", "workflows")
	info, err := os.Stat(workflowsDir)
	if os.IsNotExist(err) {
		t.Error("Expected .github/workflows directory to be created")
		return
	}

	if !info.IsDir() {
		t.Error("Expected .github/workflows to be a directory")
	}

	// Verify file was created
	setupStepsPath := filepath.Join(workflowsDir, "copilot-setup-steps.yml")
	if _, err := os.Stat(setupStepsPath); os.IsNotExist(err) {
		t.Error("Expected copilot-setup-steps.yml to be created")
	}
}

// TestEnsureCopilotSetupSteps_ReleaseMode tests that release mode uses the actions/setup-cli action
func TestEnsureCopilotSetupSteps_ReleaseMode(t *testing.T) {
	// Create temporary directory
	tmpDir := t.TempDir()

	// Change to temp directory
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Call function with release mode
	testVersion := "v1.2.3"
	err = ensureCopilotSetupSteps(context.Background(), false, workflow.ActionModeRelease, testVersion)
	if err != nil {
		t.Fatalf("ensureCopilotSetupSteps(context.Background()) failed: %v", err)
	}

	// Read generated file
	setupStepsPath := filepath.Join(".github", "workflows", "copilot-setup-steps.yml")
	content, err := os.ReadFile(setupStepsPath)
	if err != nil {
		t.Fatalf("Failed to read copilot-setup-steps.yml: %v", err)
	}

	contentStr := string(content)

	// Verify it uses actions/setup-cli with the correct version tag
	if !strings.Contains(contentStr, "actions/setup-cli@v1.2.3") {
		t.Errorf("Expected copilot-setup-steps.yml to use actions/setup-cli@v1.2.3 in release mode, got:\n%s", contentStr)
	}

	// Verify it uses the correct version in the with parameter
	if !strings.Contains(contentStr, "version: v1.2.3") {
		t.Errorf("Expected copilot-setup-steps.yml to have version: v1.2.3, got:\n%s", contentStr)
	}

	// Verify it has a pinned checkout step
	if !strings.Contains(contentStr, "uses: "+actionpins.ResolveLatestActionPin("actions/checkout", nil)) {
		t.Error("Expected copilot-setup-steps.yml to have pinned checkout step in release mode")
	}
	if !strings.Contains(contentStr, "persist-credentials: false") {
		t.Error("Expected copilot-setup-steps.yml checkout to disable credential persistence")
	}

	// Verify it doesn't use curl/install-gh-aw.sh
	if strings.Contains(contentStr, "install-gh-aw.sh") || strings.Contains(contentStr, "curl -fsSL") {
		t.Error("Expected copilot-setup-steps.yml to NOT use curl method in release mode")
	}
}

// TestEnsureCopilotSetupSteps_DevMode tests that dev mode uses curl install method
func TestEnsureCopilotSetupSteps_DevMode(t *testing.T) {
	// Create temporary directory
	tmpDir := t.TempDir()

	// Change to temp directory
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Call function with dev mode
	err = ensureCopilotSetupSteps(context.Background(), false, workflow.ActionModeDev, "dev")
	if err != nil {
		t.Fatalf("ensureCopilotSetupSteps(context.Background()) failed: %v", err)
	}

	// Read generated file
	setupStepsPath := filepath.Join(".github", "workflows", "copilot-setup-steps.yml")
	content, err := os.ReadFile(setupStepsPath)
	if err != nil {
		t.Fatalf("Failed to read copilot-setup-steps.yml: %v", err)
	}

	contentStr := string(content)

	// Verify it uses curl method
	if !strings.Contains(contentStr, "install-gh-aw.sh") {
		t.Error("Expected copilot-setup-steps.yml to use install-gh-aw.sh in dev mode")
	}

	// Verify it doesn't use actions/setup-cli
	if strings.Contains(contentStr, "actions/setup-cli") {
		t.Error("Expected copilot-setup-steps.yml to NOT use actions/setup-cli in dev mode")
	}
}

// TestEnsureCopilotSetupSteps_CreateWithReleaseMode tests creating a new file with release mode
func TestEnsureCopilotSetupSteps_CreateWithReleaseMode(t *testing.T) {
	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Create new file with release mode and specific version
	testVersion := "v2.0.0"
	err = ensureCopilotSetupSteps(context.Background(), false, workflow.ActionModeRelease, testVersion)
	if err != nil {
		t.Fatalf("ensureCopilotSetupSteps(context.Background()) failed: %v", err)
	}

	setupStepsPath := filepath.Join(".github", "workflows", "copilot-setup-steps.yml")
	content, err := os.ReadFile(setupStepsPath)
	if err != nil {
		t.Fatalf("Failed to read copilot-setup-steps.yml: %v", err)
	}

	contentStr := string(content)

	// Verify release mode characteristics
	if !strings.Contains(contentStr, "actions/setup-cli@v2.0.0") {
		t.Errorf("Expected action reference with version tag @v2.0.0, got:\n%s", contentStr)
	}
	if !strings.Contains(contentStr, "version: v2.0.0") {
		t.Errorf("Expected version parameter v2.0.0, got:\n%s", contentStr)
	}
	if !strings.Contains(contentStr, "uses: "+actionpins.ResolveLatestActionPin("actions/checkout", nil)) {
		t.Errorf("Expected pinned checkout step in release mode")
	}
	if !strings.Contains(contentStr, "persist-credentials: false") {
		t.Error("Expected checkout step to disable credential persistence")
	}
}

// TestEnsureCopilotSetupSteps_CreateWithDevMode tests creating a new file with dev mode
func TestEnsureCopilotSetupSteps_CreateWithDevMode(t *testing.T) {
	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Create new file with dev mode
	err = ensureCopilotSetupSteps(context.Background(), false, workflow.ActionModeDev, "dev")
	if err != nil {
		t.Fatalf("ensureCopilotSetupSteps(context.Background()) failed: %v", err)
	}

	setupStepsPath := filepath.Join(".github", "workflows", "copilot-setup-steps.yml")
	content, err := os.ReadFile(setupStepsPath)
	if err != nil {
		t.Fatalf("Failed to read copilot-setup-steps.yml: %v", err)
	}

	contentStr := string(content)

	// Verify dev mode characteristics
	if !strings.Contains(contentStr, "curl -fsSL") {
		t.Errorf("Expected curl command in dev mode")
	}
	if !strings.Contains(contentStr, "install-gh-aw.sh") {
		t.Errorf("Expected install-gh-aw.sh reference in dev mode")
	}
	if strings.Contains(contentStr, "actions/setup-cli") {
		t.Errorf("Did not expect actions/setup-cli in dev mode")
	}
	if strings.Contains(contentStr, "actions/checkout") {
		t.Errorf("Did not expect checkout step in dev mode")
	}
	// Verify download-to-file pattern (not direct curl pipe)
	for line := range strings.SplitSeq(contentStr, "\n") {
		if strings.Contains(line, "curl") && strings.Contains(line, "| bash") {
			t.Errorf("Expected download-to-file pattern, not direct curl|bash pipe on line %q (RGS-018 security fix)", line)
		}
	}
	if !strings.Contains(contentStr, `-o "`+installScriptTempVariable+`"`) {
		t.Errorf("Expected download to a mktemp file in dev mode")
	}
	if !strings.Contains(contentStr, `trap 'rm -f "$install_script"' EXIT`) {
		t.Error("Expected the temporary installer to be removed on exit")
	}
}

func TestGenerateCopilotSetupStepsYAMLDevModeUsesDefaultBranchFromGitHubAPI(t *testing.T) {
	const (
		defaultBranch = "stable"
		resolvedSHA   = "1111111111111111111111111111111111111111"
		sha256Digest  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)

	defaultBranchCalled := false
	resolveRefCalled := false
	originalDefaultBranch := resolveGhAwDefaultBranchForCopilotSetup
	originalResolveRef := resolveGhAwRefForCopilotSetup
	originalSHA256 := resolveInstallScriptSHA256ForCopilotSetup
	resolveGhAwDefaultBranchForCopilotSetup = func(_ context.Context, repo string) (string, error) {
		defaultBranchCalled = true
		if repo != "github/gh-aw" {
			t.Fatalf("repo = %q, want github/gh-aw", repo)
		}
		return defaultBranch, nil
	}
	resolveGhAwRefForCopilotSetup = func(_ context.Context, ref string) (string, error) {
		resolveRefCalled = true
		if ref != defaultBranch {
			t.Fatalf("ref = %q, want %q", ref, defaultBranch)
		}
		return resolvedSHA, nil
	}
	resolveInstallScriptSHA256ForCopilotSetup = func(_ context.Context, commitSHA string) string {
		if commitSHA != resolvedSHA {
			t.Fatalf("commitSHA = %q, want %q", commitSHA, resolvedSHA)
		}
		return sha256Digest
	}
	t.Cleanup(func() {
		resolveGhAwDefaultBranchForCopilotSetup = originalDefaultBranch
		resolveGhAwRefForCopilotSetup = originalResolveRef
		resolveInstallScriptSHA256ForCopilotSetup = originalSHA256
	})

	content := generateCopilotSetupStepsYAML(context.Background(), workflow.ActionModeDev, "dev", nil)

	if !defaultBranchCalled {
		t.Fatal("expected default branch resolver to be called")
	}
	if !resolveRefCalled {
		t.Fatal("expected default branch ref to be resolved")
	}
	if !strings.Contains(content, "https://raw.githubusercontent.com/github/gh-aw/"+resolvedSHA+"/install-gh-aw.sh") {
		t.Fatalf("expected install script URL to use resolved SHA %q, got:\n%s", resolvedSHA, content)
	}
	if strings.Contains(content, "refs/heads/main") {
		t.Fatalf("expected generated content not to hard-code refs/heads/main, got:\n%s", content)
	}
	if !strings.Contains(content, `printf '%s  %s\n' "`+sha256Digest+`" "`+installScriptTempVariable+`" | sha256sum -c -`) {
		t.Fatalf("expected generated content to include SHA256 integrity check, got:\n%s", content)
	}
}

func TestGenerateCopilotSetupStepsYAMLDevModeFallsBackToStaticPinnedInstaller(t *testing.T) {
	const defaultBranch = "stable"

	originalDefaultBranch := resolveGhAwDefaultBranchForCopilotSetup
	originalResolveRef := resolveGhAwRefForCopilotSetup
	originalSHA256 := resolveInstallScriptSHA256ForCopilotSetup
	resolveGhAwDefaultBranchForCopilotSetup = func(_ context.Context, _ string) (string, error) {
		return defaultBranch, nil
	}
	resolveGhAwRefForCopilotSetup = func(_ context.Context, ref string) (string, error) {
		if ref != defaultBranch {
			t.Fatalf("ref = %q, want %q", ref, defaultBranch)
		}
		return "", errors.New("resolution failed")
	}
	resolveInstallScriptSHA256ForCopilotSetup = func(context.Context, string) string {
		t.Fatal("SHA256 resolver should not be called when ref resolution fails")
		return ""
	}
	t.Cleanup(func() {
		resolveGhAwDefaultBranchForCopilotSetup = originalDefaultBranch
		resolveGhAwRefForCopilotSetup = originalResolveRef
		resolveInstallScriptSHA256ForCopilotSetup = originalSHA256
	})

	content := generateCopilotSetupStepsYAML(context.Background(), workflow.ActionModeDev, "dev", nil)

	if !strings.Contains(content, "https://raw.githubusercontent.com/github/gh-aw/"+copilotSetupStepsStaticSHA+"/install-gh-aw.sh") {
		t.Fatalf("expected install script URL to fall back to the static commit SHA, got:\n%s", content)
	}
	if !strings.Contains(content, copilotSetupStepsStaticSHA256) || !strings.Contains(content, "sha256sum -c -") {
		t.Fatalf("expected static SHA256 integrity check with static commit fallback, got:\n%s", content)
	}
	if strings.Contains(content, "refs/heads/") {
		t.Fatalf("did not expect a mutable branch reference, got:\n%s", content)
	}
}

func TestEnsureCopilotSetupSteps_UsesWorkflowDirEnvOverride(t *testing.T) {
	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	t.Setenv("GH_AW_WORKFLOWS_DIR", filepath.Join("custom", "dir"))

	err = ensureCopilotSetupSteps(context.Background(), false, workflow.ActionModeDev, "dev")
	if err != nil {
		t.Fatalf("ensureCopilotSetupSteps(context.Background()) failed: %v", err)
	}

	overridePath := filepath.Join("custom", "dir", "copilot-setup-steps.yml")
	if _, err := os.Stat(overridePath); err != nil {
		t.Fatalf("Expected %s to exist: %v", overridePath, err)
	}

	defaultPath := filepath.Join(".github", "workflows", "copilot-setup-steps.yml")
	if _, err := os.Stat(defaultPath); !os.IsNotExist(err) {
		t.Fatalf("Expected default path %s to not exist when override is set", defaultPath)
	}
}

// TestEnsureCopilotSetupSteps_UpdateExistingWithReleaseMode tests updating an existing file with release mode
func TestEnsureCopilotSetupSteps_UpdateExistingWithReleaseMode(t *testing.T) {
	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Create .github/workflows directory
	workflowsDir := filepath.Join(".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Write existing workflow without gh-aw install step
	existingContent := `name: "Copilot Setup Steps"
on: workflow_dispatch
jobs:
  copilot-setup-steps:
    runs-on: ubuntu-latest
    permissions:
      contents: read
    steps:
      - name: Some other step
        run: echo "test"
`
	setupStepsPath := filepath.Join(workflowsDir, "copilot-setup-steps.yml")
	if err := os.WriteFile(setupStepsPath, []byte(existingContent), 0644); err != nil {
		t.Fatalf("Failed to write existing workflow: %v", err)
	}

	// Call with release mode - should render instructions instead of modifying
	testVersion := "v3.0.0"
	err = ensureCopilotSetupSteps(context.Background(), false, workflow.ActionModeRelease, testVersion)
	if err != nil {
		t.Fatalf("ensureCopilotSetupSteps(context.Background()) failed: %v", err)
	}

	// Read file - should remain unchanged
	content, err := os.ReadFile(setupStepsPath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	contentStr := string(content)

	// Verify file was NOT modified - should remain identical to existingContent
	if contentStr != existingContent {
		t.Errorf("Expected file to remain unchanged (instructions should be rendered instead), got:\n%s", contentStr)
	}

	// Verify the install step was NOT injected
	if strings.Contains(contentStr, "actions/setup-cli") {
		t.Errorf("Expected 'actions/setup-cli' to NOT be injected (instructions should be rendered)")
	}
	if strings.Contains(contentStr, "Install gh-aw extension") {
		t.Errorf("Expected 'Install gh-aw extension' step to NOT be injected (instructions should be rendered)")
	}
}

// TestEnsureCopilotSetupSteps_UpdateExistingWithDevMode tests updating an existing file with dev mode
func TestEnsureCopilotSetupSteps_UpdateExistingWithDevMode(t *testing.T) {
	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Create .github/workflows directory
	workflowsDir := filepath.Join(".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Write existing workflow without gh-aw install step
	existingContent := `name: "Copilot Setup Steps"
on: workflow_dispatch
jobs:
  copilot-setup-steps:
    runs-on: ubuntu-latest
    permissions:
      contents: read
    steps:
      - name: Some other step
        run: echo "test"
`
	setupStepsPath := filepath.Join(workflowsDir, "copilot-setup-steps.yml")
	if err := os.WriteFile(setupStepsPath, []byte(existingContent), 0644); err != nil {
		t.Fatalf("Failed to write existing workflow: %v", err)
	}

	// Call with dev mode - should render instructions instead of modifying
	err = ensureCopilotSetupSteps(context.Background(), false, workflow.ActionModeDev, "dev")
	if err != nil {
		t.Fatalf("ensureCopilotSetupSteps(context.Background()) failed: %v", err)
	}

	// Read file - should remain unchanged
	content, err := os.ReadFile(setupStepsPath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	contentStr := string(content)

	// Verify file was NOT modified - should remain identical to existingContent
	if contentStr != existingContent {
		t.Errorf("Expected file to remain unchanged (instructions should be rendered instead), got:\n%s", contentStr)
	}

	// Verify the install step was NOT injected
	if strings.Contains(contentStr, "curl -fsSL") {
		t.Errorf("Expected 'curl' command to NOT be injected (instructions should be rendered)")
	}
	if strings.Contains(contentStr, "install-gh-aw.sh") {
		t.Errorf("Expected 'install-gh-aw.sh' to NOT be injected (instructions should be rendered)")
	}
	if strings.Contains(contentStr, "actions/setup-cli") {
		t.Errorf("Did not expect actions/setup-cli in dev mode")
	}
	// Verify original step is preserved
	if !strings.Contains(contentStr, "Some other step") {
		t.Errorf("Expected original step to be preserved")
	}
}

// TestEnsureCopilotSetupSteps_SkipsUpdateWhenActionExists tests that update is skipped when action already exists
func TestEnsureCopilotSetupSteps_SkipsUpdateWhenActionExists(t *testing.T) {
	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Create .github/workflows directory
	workflowsDir := filepath.Join(".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Write existing workflow WITH actions/setup-cli (release mode)
	existingContent := `name: "Copilot Setup Steps"
on: workflow_dispatch
jobs:
  copilot-setup-steps:
    runs-on: ubuntu-latest
    steps:
      - uses: github/gh-aw-actions/setup-cli@v1.0.0
        with:
          version: v1.0.0
`
	setupStepsPath := filepath.Join(workflowsDir, "copilot-setup-steps.yml")
	if err := os.WriteFile(setupStepsPath, []byte(existingContent), 0644); err != nil {
		t.Fatalf("Failed to write existing workflow: %v", err)
	}

	// Attempt to update - should skip
	err = ensureCopilotSetupSteps(context.Background(), false, workflow.ActionModeRelease, "v2.0.0")
	if err != nil {
		t.Fatalf("ensureCopilotSetupSteps(context.Background()) failed: %v", err)
	}

	// Read file - should be unchanged
	content, err := os.ReadFile(setupStepsPath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	contentStr := string(content)

	// Verify file was not modified (still has v1.0.0)
	if !strings.Contains(contentStr, "v1.0.0") {
		t.Errorf("Expected file to remain unchanged with v1.0.0")
	}
	if strings.Contains(contentStr, "v2.0.0") {
		t.Errorf("File should not have been updated to v2.0.0")
	}
}

// TestEnsureCopilotSetupSteps_SkipsUpdateWhenCurlExists tests that update is skipped when curl install exists
func TestEnsureCopilotSetupSteps_SkipsUpdateWhenCurlExists(t *testing.T) {
	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Create .github/workflows directory
	workflowsDir := filepath.Join(".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Write existing workflow WITH curl install (dev mode)
	existingContent := `name: "Copilot Setup Steps"
on: workflow_dispatch
jobs:
  copilot-setup-steps:
    runs-on: ubuntu-latest
    steps:
      - name: Install gh-aw extension
        run: curl -fsSL https://raw.githubusercontent.com/github/gh-aw/refs/heads/main/install-gh-aw.sh | bash
`
	setupStepsPath := filepath.Join(workflowsDir, "copilot-setup-steps.yml")
	if err := os.WriteFile(setupStepsPath, []byte(existingContent), 0644); err != nil {
		t.Fatalf("Failed to write existing workflow: %v", err)
	}

	// Attempt to update - should skip
	err = ensureCopilotSetupSteps(context.Background(), false, workflow.ActionModeDev, "dev")
	if err != nil {
		t.Fatalf("ensureCopilotSetupSteps(context.Background()) failed: %v", err)
	}

	// Verify file content matches expected (should be unchanged)
	content, err := os.ReadFile(setupStepsPath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if string(content) != existingContent {
		t.Errorf("Expected file to remain unchanged")
	}
}

// TestEnsureCopilotSetupSteps_SkipsUpdateWhenDownloadVerifyExists tests that update is skipped
// when the new download+verify syntax (RGS-018 fix) already exists in the file.
func TestEnsureCopilotSetupSteps_SkipsUpdateWhenDownloadVerifyExists(t *testing.T) {
	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Create .github/workflows directory
	workflowsDir := filepath.Join(".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Write existing workflow WITH the new download+verify syntax (no direct curl|bash pipe)
	existingContent := "name: \"Copilot Setup Steps\"\n" +
		"on: workflow_dispatch\n" +
		"jobs:\n" +
		"  copilot-setup-steps:\n" +
		"    runs-on: ubuntu-latest\n" +
		"    steps:\n" +
		"      - name: Install gh-aw extension\n" +
		"        run: |\n" +
		"          install_script=\"$(mktemp)\"\n" +
		"          trap 'rm -f \"$install_script\"' EXIT\n" +
		"          curl -fsSL https://raw.githubusercontent.com/github/gh-aw/" + copilotSetupStepsStaticSHA + "/install-gh-aw.sh -o \"$install_script\"\n" +
		"          printf '%s  %s\\n' \"" + copilotSetupStepsStaticSHA256 + "\" \"$install_script\" | sha256sum -c -\n" +
		"          bash \"$install_script\"\n"

	setupStepsPath := filepath.Join(workflowsDir, "copilot-setup-steps.yml")
	if err := os.WriteFile(setupStepsPath, []byte(existingContent), 0644); err != nil {
		t.Fatalf("Failed to write existing workflow: %v", err)
	}

	// Attempt to update - should skip since install step already exists
	err = ensureCopilotSetupSteps(context.Background(), false, workflow.ActionModeDev, "dev")
	if err != nil {
		t.Fatalf("ensureCopilotSetupSteps(context.Background()) failed: %v", err)
	}

	// Verify file content is unchanged (download+verify syntax is recognized as already configured)
	content, err := os.ReadFile(setupStepsPath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if string(content) != existingContent {
		t.Errorf("Expected file to remain unchanged when download+verify syntax already present,\ngot:\n%s", string(content))
	}
	// Confirm sha256sum check is still there
	if !strings.Contains(string(content), "sha256sum") {
		t.Error("Expected sha256sum integrity check to be preserved")
	}
}

// TestUpgradeCopilotSetupSteps tests upgrading version in existing copilot-setup-steps.yml
func TestUpgradeCopilotSetupSteps(t *testing.T) {
	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Create .github/workflows directory
	workflowsDir := filepath.Join(".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Write existing workflow WITH actions/setup-cli at v1.0.0
	existingContent := `name: "Copilot Setup Steps"
on: workflow_dispatch
jobs:
  copilot-setup-steps:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout repository
        uses: actions/checkout@v4
      - name: Install gh-aw extension
        uses: github/gh-aw-actions/setup-cli@v1.0.0
        with:
          version: v1.0.0
      - name: Verify gh-aw installation
        run: gh aw version
`
	setupStepsPath := filepath.Join(workflowsDir, "copilot-setup-steps.yml")
	if err := os.WriteFile(setupStepsPath, []byte(existingContent), 0644); err != nil {
		t.Fatalf("Failed to write existing workflow: %v", err)
	}

	// Upgrade to v2.0.0
	err = upgradeCopilotSetupSteps(context.Background(), false, workflow.ActionModeRelease, "v2.0.0")
	if err != nil {
		t.Fatalf("upgradeCopilotSetupSteps(context.Background()) failed: %v", err)
	}

	// Read updated file
	content, err := os.ReadFile(setupStepsPath)
	if err != nil {
		t.Fatalf("Failed to read updated file: %v", err)
	}

	contentStr := string(content)

	// Verify version was upgraded
	if !strings.Contains(contentStr, "actions/setup-cli@v2.0.0") {
		t.Errorf("Expected action reference to be upgraded to @v2.0.0, got:\n%s", contentStr)
	}
	if !strings.Contains(contentStr, "version: v2.0.0") {
		t.Errorf("Expected version parameter to be v2.0.0, got:\n%s", contentStr)
	}

	// Verify old version is gone
	if strings.Contains(contentStr, "v1.0.0") {
		t.Errorf("Old version v1.0.0 should not be present, got:\n%s", contentStr)
	}
}

// TestUpgradeCopilotSetupSteps_NoFile tests upgrading when file doesn't exist
func TestUpgradeCopilotSetupSteps_NoFile(t *testing.T) {
	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Attempt to upgrade when file doesn't exist - should create new file
	err = upgradeCopilotSetupSteps(context.Background(), false, workflow.ActionModeRelease, "v2.0.0")
	if err != nil {
		t.Fatalf("upgradeCopilotSetupSteps(context.Background()) failed: %v", err)
	}

	// Verify file was created with the new version
	setupStepsPath := filepath.Join(".github", "workflows", "copilot-setup-steps.yml")
	content, err := os.ReadFile(setupStepsPath)
	if err != nil {
		t.Fatalf("Failed to read created file: %v", err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "actions/setup-cli@v2.0.0") {
		t.Errorf("Expected new file to have @v2.0.0, got:\n%s", contentStr)
	}
}

// TestUpgradeCopilotSetupSteps_DevMode tests that dev mode doesn't use actions/setup-cli
func TestUpgradeCopilotSetupSteps_DevMode(t *testing.T) {
	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Create .github/workflows directory
	workflowsDir := filepath.Join(".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Write existing workflow with curl install (dev mode)
	existingContent := `name: "Copilot Setup Steps"
on: workflow_dispatch
jobs:
  copilot-setup-steps:
    runs-on: ubuntu-latest
    steps:
      - name: Install gh-aw extension
        run: curl -fsSL https://raw.githubusercontent.com/github/gh-aw/refs/heads/main/install-gh-aw.sh | bash
      - name: Verify gh-aw installation
        run: gh aw version
`
	setupStepsPath := filepath.Join(workflowsDir, "copilot-setup-steps.yml")
	if err := os.WriteFile(setupStepsPath, []byte(existingContent), 0644); err != nil {
		t.Fatalf("Failed to write existing workflow: %v", err)
	}

	// Attempt upgrade in dev mode - should not modify file
	err = upgradeCopilotSetupSteps(context.Background(), false, workflow.ActionModeDev, "dev")
	if err != nil {
		t.Fatalf("upgradeCopilotSetupSteps(context.Background()) failed: %v", err)
	}

	// Verify file was not changed (dev mode doesn't upgrade curl-based installs)
	content, err := os.ReadFile(setupStepsPath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if string(content) != existingContent {
		t.Errorf("File should remain unchanged in dev mode")
	}
}

// TestUpgradeSetupCliVersionInContent tests the regex-based content upgrade helper.
func TestUpgradeSetupCliVersionInContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		content       string
		actionMode    workflow.ActionMode
		version       string
		resolver      workflow.SHAResolver
		expectUpgrade bool
		validate      func(*testing.T, string)
	}{
		{
			name: "upgrades version-tag ref",
			content: `name: "Copilot Setup Steps"
on: workflow_dispatch
jobs:
  copilot-setup-steps:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4
      - name: Install gh-aw extension
        uses: github/gh-aw-actions/setup-cli@v1.0.0
        with:
          version: v1.0.0
`,
			actionMode:    workflow.ActionModeRelease,
			version:       "v2.0.0",
			resolver:      nil,
			expectUpgrade: true,
			validate: func(t *testing.T, got string) {
				wantCheckoutRef := "uses: " + actionpins.ResolveLatestActionPin("actions/checkout", nil)
				if !strings.Contains(got, wantCheckoutRef) {
					t.Errorf("Expected updated checkout uses: line %q, got:\n%s", wantCheckoutRef, got)
				}
				if strings.Contains(got, "uses: actions/checkout@v4") {
					t.Errorf("Old checkout tag should be gone, got:\n%s", got)
				}
				if !strings.Contains(got, "persist-credentials: false") {
					t.Errorf("Expected checkout to disable credential persistence, got:\n%s", got)
				}
				if !strings.Contains(got, "uses: github/gh-aw-actions/setup-cli@v2.0.0") {
					t.Errorf("Expected updated uses: line, got:\n%s", got)
				}
				if !strings.Contains(got, "version: v2.0.0") {
					t.Errorf("Expected updated version: parameter, got:\n%s", got)
				}
				if strings.Contains(got, "v1.0.0") {
					t.Errorf("Old version v1.0.0 should be gone, got:\n%s", got)
				}
				// File structure must be preserved (comment line, on: key, etc.)
				if !strings.Contains(got, "on: workflow_dispatch") {
					t.Errorf("Expected on: field to be preserved, got:\n%s", got)
				}
			},
		},
		{
			name: "upgrades SHA-pinned ref and produces unquoted uses: value",
			content: `name: "Copilot Setup Steps"
on: workflow_dispatch
jobs:
  copilot-setup-steps:
    runs-on: ubuntu-latest
    steps:
      - name: Install gh-aw extension
        uses: github/gh-aw-actions/setup-cli@v1.0.0
        with:
          version: v1.0.0
`,
			actionMode:    workflow.ActionModeRelease,
			version:       "v2.0.0",
			resolver:      &mockSHAResolver{sha: "bd9c0ca491e6334a2797ef56ad6ee89958d54ab9"},
			expectUpgrade: true,
			validate: func(t *testing.T, got string) {
				want := "uses: github/gh-aw-actions/setup-cli@bd9c0ca491e6334a2797ef56ad6ee89958d54ab9 # v2.0.0"
				if !strings.Contains(got, want) {
					t.Errorf("Expected unquoted SHA-pinned uses: line %q, got:\n%s", want, got)
				}
				// Confirm NO quoted form is present
				if strings.Contains(got, `uses: "github/gh-aw`) {
					t.Errorf("uses: value must not be quoted, got:\n%s", got)
				}
				if !strings.Contains(got, "version: v2.0.0") {
					t.Errorf("Expected updated version: parameter, got:\n%s", got)
				}
			},
		},
		{
			name: "strips existing quotes from uses: value",
			content: `jobs:
  copilot-setup-steps:
    steps:
      - name: Install gh-aw extension
        uses: "github/gh-aw-actions/setup-cli@oldsha # v0.53.2"
        with:
          version: v0.53.2
`,
			actionMode:    workflow.ActionModeRelease,
			version:       "v2.0.0",
			resolver:      nil,
			expectUpgrade: true,
			validate: func(t *testing.T, got string) {
				if strings.Contains(got, `"github/gh-aw`) {
					t.Errorf("Quotes must be stripped from uses: value, got:\n%s", got)
				}
				if !strings.Contains(got, "uses: github/gh-aw-actions/setup-cli@v2.0.0") {
					t.Errorf("Expected updated unquoted uses: line, got:\n%s", got)
				}
				if !strings.Contains(got, "version: v2.0.0") {
					t.Errorf("Expected version: to be updated to v2.0.0, got:\n%s", got)
				}
			},
		},
		{
			name: "no upgrade when no setup-cli step",
			content: `jobs:
  copilot-setup-steps:
    steps:
      - run: echo hello
`,
			actionMode:    workflow.ActionModeRelease,
			version:       "v2.0.0",
			resolver:      nil,
			expectUpgrade: false,
		},
		{
			name: "no upgrade in dev mode",
			content: `jobs:
  copilot-setup-steps:
    steps:
      - uses: github/gh-aw-actions/setup-cli@v1.0.0
        with:
          version: v1.0.0
`,
			actionMode:    workflow.ActionModeDev,
			version:       "v2.0.0",
			resolver:      nil,
			expectUpgrade: false,
		},
		{
			name: "corrects drift: SHA-pinned uses comment ahead of with: version:",
			content: `jobs:
  copilot-setup-steps:
    steps:
      - name: Install gh-aw extension
        uses: github/gh-aw-actions/setup-cli@cb7966564184443e601bd6135d5fbb534300070e # v0.58.0
        with:
          version: v0.53.6
`,
			actionMode:    workflow.ActionModeRelease,
			version:       "v0.60.0",
			resolver:      &mockSHAResolver{sha: "newsha123"},
			expectUpgrade: true,
			validate: func(t *testing.T, got string) {
				if !strings.Contains(got, "uses: github/gh-aw-actions/setup-cli@newsha123 # v0.60.0") {
					t.Errorf("Expected updated SHA-pinned uses: line, got:\n%s", got)
				}
				if !strings.Contains(got, "version: v0.60.0") {
					t.Errorf("Expected with: version: updated to v0.60.0, got:\n%s", got)
				}
				if strings.Contains(got, "v0.53.6") {
					t.Errorf("Stale version v0.53.6 should be gone, got:\n%s", got)
				}
				if strings.Contains(got, "v0.58.0") {
					t.Errorf("Old comment version v0.58.0 should be gone, got:\n%s", got)
				}
			},
		},
		{
			name: "corrects drift: version-tag uses ahead of with: version:",
			content: `jobs:
  copilot-setup-steps:
    steps:
      - name: Install gh-aw extension
        uses: github/gh-aw-actions/setup-cli@v0.58.0
        with:
          version: v0.53.6
`,
			actionMode:    workflow.ActionModeRelease,
			version:       "v0.60.0",
			resolver:      nil,
			expectUpgrade: true,
			validate: func(t *testing.T, got string) {
				if !strings.Contains(got, "uses: github/gh-aw-actions/setup-cli@v0.60.0") {
					t.Errorf("Expected updated uses: line, got:\n%s", got)
				}
				if !strings.Contains(got, "version: v0.60.0") {
					t.Errorf("Expected with: version: updated to v0.60.0, got:\n%s", got)
				}
				if strings.Contains(got, "v0.53.6") {
					t.Errorf("Stale version v0.53.6 should be gone, got:\n%s", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upgraded, got, err := upgradeSetupCliVersionInContent(context.Background(), []byte(tt.content), tt.actionMode, tt.version, tt.resolver)
			if err != nil {
				t.Fatalf("upgradeSetupCliVersionInContent(context.Background()) error: %v", err)
			}
			if upgraded != tt.expectUpgrade {
				t.Errorf("upgraded = %v, want %v", upgraded, tt.expectUpgrade)
			}
			if tt.validate != nil {
				tt.validate(t, string(got))
			}
		})
	}
}

// TestUpgradeSetupCliVersionInContent_ExactPreservation verifies that
// upgradeSetupCliVersionInContent changes ONLY the two target lines
// (uses: and version:) and leaves every other byte of the file intact —
// including YAML comments at all positions, blank lines, field ordering,
// indentation, and unrelated step entries.
func TestUpgradeSetupCliVersionInContent_ExactPreservation(t *testing.T) {
	t.Parallel()

	// A deliberately rich workflow file:
	// - top-level comment before the name field
	// - inline comment on the on: trigger
	// - comment inside the jobs block
	// - multiple steps with their own comments
	// - a step after setup-cli with its own comment
	// - trailing comment at end of file
	input := `# Top-level workflow comment — must survive the upgrade.
name: "Copilot Setup Steps"

# Trigger comment: dispatched manually or on push.
on: # inline comment on on:
  workflow_dispatch:
  push:
    paths:
      - .github/workflows/copilot-setup-steps.yml # path filter comment

jobs:
  # Job-level comment that must not be lost.
  copilot-setup-steps:
    runs-on: ubuntu-latest
    # Permission comment.
    permissions:
      contents: read # read-only is sufficient

    steps:
      # Step 1 comment.
      - name: Checkout repository
        uses: actions/checkout@v4 # pin to stable tag
        with:
          fetch-depth: 0 # full history

      # Step 2 comment — this step should be updated.
      - name: Install gh-aw extension
        uses: github/gh-aw-actions/setup-cli@v1.0.0
        with:
          version: v1.0.0
          extra-param: keep-me # this param must not be touched

      # Step 3 comment — must be fully preserved.
      - name: Run something else
        run: echo "hello" # inline run comment
`

	checkoutRef := actionpins.ResolveLatestActionPin("actions/checkout", nil)

	// Expected output: identical to input except the setup-cli, checkout, and version lines.
	expected := `# Top-level workflow comment — must survive the upgrade.
name: "Copilot Setup Steps"

# Trigger comment: dispatched manually or on push.
on: # inline comment on on:
  workflow_dispatch:
  push:
    paths:
      - .github/workflows/copilot-setup-steps.yml # path filter comment

jobs:
  # Job-level comment that must not be lost.
  copilot-setup-steps:
    runs-on: ubuntu-latest
    # Permission comment.
    permissions:
      contents: read # read-only is sufficient

    steps:
      # Step 1 comment.
      - name: Checkout repository
        uses: ` + checkoutRef + `
        with:
          persist-credentials: false
          fetch-depth: 0 # full history

      # Step 2 comment — this step should be updated.
      - name: Install gh-aw extension
        uses: github/gh-aw-actions/setup-cli@v2.0.0
        with:
          version: v2.0.0
          extra-param: keep-me # this param must not be touched

      # Step 3 comment — must be fully preserved.
      - name: Run something else
        run: echo "hello" # inline run comment
`

	upgraded, got, err := upgradeSetupCliVersionInContent(context.Background(), []byte(input), workflow.ActionModeRelease, "v2.0.0", nil)
	if err != nil {
		t.Fatalf("upgradeSetupCliVersionInContent(context.Background()) error: %v", err)
	}
	if !upgraded {
		t.Fatal("Expected upgrade to occur")
	}

	gotStr := string(got)
	if gotStr != expected {
		// Show a line-by-line diff to make failures easy to diagnose.
		inputLines := strings.Split(input, "\n")
		expectedLines := strings.Split(expected, "\n")
		gotLines := strings.Split(gotStr, "\n")

		t.Errorf("Output does not match expected (only checkout/setup-cli uses: and version: lines should differ).\n")
		for i := 0; i < len(expectedLines) || i < len(gotLines); i++ {
			var exp, act string
			if i < len(expectedLines) {
				exp = expectedLines[i]
			}
			if i < len(gotLines) {
				act = gotLines[i]
			}
			if exp != act {
				orig := ""
				if i < len(inputLines) {
					orig = inputLines[i]
				}
				t.Errorf("  line %d:\n    input:    %q\n    expected: %q\n    got:      %q", i+1, orig, exp, act)
			}
		}
	}
}

// SHA-pinned reference writes an unquoted uses: line, preserving the rest of the file.
// Regression test for: gh aw upgrade wraps uses value in quotes including inline comment.
func TestUpgradeCopilotSetupSteps_SHAPinnedNoQuotes(t *testing.T) {
	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() { _ = os.Chdir(originalDir) }()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	workflowsDir := filepath.Join(".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Pre-existing file with a version-tagged reference and extra comments/fields
	// that must be preserved unchanged.
	existingContent := `name: "Copilot Setup Steps"

# This workflow configures the environment for GitHub Copilot Agent
on:
  workflow_dispatch:
  push:
    paths:
      - .github/workflows/copilot-setup-steps.yml

jobs:
  copilot-setup-steps:
    runs-on: ubuntu-latest
    permissions:
      contents: read
    steps:
      - name: Checkout repository
        uses: actions/checkout@v4
      - name: Install gh-aw extension
        uses: github/gh-aw-actions/setup-cli@v1.0.0
        with:
          version: v1.0.0
`
	setupStepsPath := filepath.Join(workflowsDir, "copilot-setup-steps.yml")
	if err := os.WriteFile(setupStepsPath, []byte(existingContent), 0644); err != nil {
		t.Fatalf("Failed to write existing workflow: %v", err)
	}

	// upgradeSetupCliVersionInContent with a SHA resolver — the result must be unquoted
	sha := "bd9c0ca491e6334a2797ef56ad6ee89958d54ab9"
	resolver := &mockSHAResolver{sha: sha}
	upgraded, updated, err := upgradeSetupCliVersionInContent(context.Background(), []byte(existingContent), workflow.ActionModeRelease, "v2.0.0", resolver)
	if err != nil {
		t.Fatalf("upgradeSetupCliVersionInContent(context.Background()) error: %v", err)
	}
	if !upgraded {
		t.Fatal("Expected upgrade to occur")
	}

	updatedStr := string(updated)

	// The uses: line must be unquoted
	wantUses := "uses: github/gh-aw-actions/setup-cli@" + sha + " # v2.0.0"
	if !strings.Contains(updatedStr, wantUses) {
		t.Errorf("Expected unquoted uses: line %q, got:\n%s", wantUses, updatedStr)
	}
	if strings.Contains(updatedStr, `uses: "github/gh-aw`) {
		t.Errorf("uses: value must not be quoted, got:\n%s", updatedStr)
	}

	// version: parameter updated
	if !strings.Contains(updatedStr, "version: v2.0.0") {
		t.Errorf("Expected version: v2.0.0, got:\n%s", updatedStr)
	}

	// All other content must be preserved exactly
	for _, preserved := range []string{
		`# This workflow configures the environment for GitHub Copilot Agent`,
		`workflow_dispatch:`,
		`- .github/workflows/copilot-setup-steps.yml`,
		`permissions:`,
		`contents: read`,
	} {
		if !strings.Contains(updatedStr, preserved) {
			t.Errorf("Expected content %q to be preserved, got:\n%s", preserved, updatedStr)
		}
	}
	wantCheckoutRef := "uses: " + actionpins.ResolveLatestActionPin("actions/checkout", nil)
	if !strings.Contains(updatedStr, wantCheckoutRef) {
		t.Errorf("Expected checkout uses: line %q, got:\n%s", wantCheckoutRef, updatedStr)
	}
	if strings.Contains(updatedStr, "uses: actions/checkout@v4") {
		t.Errorf("Old checkout tag should be gone, got:\n%s", updatedStr)
	}
	if !strings.Contains(updatedStr, "persist-credentials: false") {
		t.Errorf("Expected checkout to disable credential persistence, got:\n%s", updatedStr)
	}
}

func TestPinCheckoutUsesInContent(t *testing.T) {
	t.Parallel()

	checkoutRef := actionpins.ResolveLatestActionPin("actions/checkout", nil)

	t.Run("preserves missing trailing newline", func(t *testing.T) {
		t.Parallel()
		input := "        uses: actions/checkout@v4"
		got, changed := pinCheckoutUsesInContent([]byte(input))
		if !changed {
			t.Fatal("expected checkout line to be updated")
		}
		expected := "        uses: " + checkoutRef + "\n        with:\n          persist-credentials: false"
		if string(got) != expected {
			t.Fatalf("expected %q, got %q", expected, string(got))
		}
	})

	t.Run("blank line ends existing with block", func(t *testing.T) {
		t.Parallel()
		input := `      - name: Checkout
        uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Other step
        with:
          persist-credentials: true
`
		got, changed := pinCheckoutUsesInContent([]byte(input))
		if !changed {
			t.Fatal("expected checkout line to be updated")
		}
		gotStr := string(got)
		if !strings.Contains(gotStr, "        uses: "+checkoutRef) {
			t.Fatalf("expected checkout to use %q, got:\n%s", checkoutRef, gotStr)
		}
		if !strings.Contains(gotStr, "          persist-credentials: false\n          fetch-depth: 0") {
			t.Fatalf("expected persist-credentials in checkout with block, got:\n%s", gotStr)
		}
	})

	t.Run("blank line inside existing with block", func(t *testing.T) {
		t.Parallel()
		input := `      - name: Checkout
        uses: actions/checkout@v4
        with:
          fetch-depth: 0

          clean: false
`
		got, changed := pinCheckoutUsesInContent([]byte(input))
		if !changed {
			t.Fatal("expected checkout line to be updated")
		}
		gotStr := string(got)
		if !strings.Contains(gotStr, "          persist-credentials: false\n          fetch-depth: 0\n\n          clean: false") {
			t.Fatalf("expected persist-credentials at the top of checkout with block, got:\n%s", gotStr)
		}
	})
}

// TestGetActionRef tests the getActionRef helper with and without a resolver
func TestGetActionRef(t *testing.T) {
	tests := []struct {
		name        string
		actionMode  workflow.ActionMode
		version     string
		resolver    workflow.SHAResolver
		expectedRef string
	}{
		{
			name:        "release mode without resolver uses version tag",
			actionMode:  workflow.ActionModeRelease,
			version:     "v1.2.3",
			resolver:    nil,
			expectedRef: "@v1.2.3",
		},
		{
			name:        "release mode with resolver uses SHA-pinned reference",
			actionMode:  workflow.ActionModeRelease,
			version:     "v1.2.3",
			resolver:    &mockSHAResolver{sha: "abc1234567890123456789012345678901234567890"},
			expectedRef: "@abc1234567890123456789012345678901234567890 # v1.2.3",
		},
		{
			name:        "release mode with failing resolver falls back to version tag",
			actionMode:  workflow.ActionModeRelease,
			version:     "v1.2.3",
			resolver:    &mockSHAResolver{sha: "", err: errors.New("resolution failed")},
			expectedRef: "@v1.2.3",
		},
		{
			name:        "dev mode uses @main",
			actionMode:  workflow.ActionModeDev,
			version:     "v1.2.3",
			resolver:    nil,
			expectedRef: "@main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref := getActionRef(context.Background(), tt.actionMode, tt.version, tt.resolver)
			if ref != tt.expectedRef {
				t.Errorf("getActionRef(context.Background()) = %q, want %q", ref, tt.expectedRef)
			}
		})
	}
}

func TestValidateCopilotSetupStepsContent(t *testing.T) {
	t.Parallel()

	validWorkflow := `name: "Copilot Setup Steps"
on:
  workflow_dispatch:
jobs:
  copilot-setup-steps:
    runs-on: ubuntu-latest
    steps:
      - name: Install gh-aw extension
        run: echo install
`

	tests := []struct {
		name        string
		content     string
		wantErr     string
		wantNoError bool
	}{
		{name: "valid workflow", content: validWorkflow, wantNoError: true},
		{
			name: "quoted on key",
			content: `name: "Copilot Setup Steps"
"on":
  push:
    paths:
      - .github/workflows/copilot-setup-steps.yml
jobs:
  copilot-setup-steps:
    runs-on: ubuntu-latest
    steps:
      - run: echo install
`,
			wantNoError: true,
		},
		{
			name: "string trigger",
			content: `on: push
jobs:
  copilot-setup-steps:
    runs-on: ubuntu-latest
    steps:
      - run: echo install
`,
			wantNoError: true,
		},
		{name: "invalid yaml", content: "name: [unterminated\n", wantErr: "invalid YAML"},
		{name: "empty workflow", content: "\n", wantErr: "workflow is empty"},
		{
			name: "unsupported trigger only",
			content: `on: workflow_call
jobs:
  copilot-setup-steps:
    runs-on: ubuntu-latest
    steps:
      - run: echo install
`,
			wantErr: "'on' section must include one of",
		},
		{
			name: "missing on section",
			content: `jobs:
  copilot-setup-steps:
    runs-on: ubuntu-latest
    steps:
      - run: echo install
`,
			wantErr: "missing 'on' section",
		},
		{
			name: "missing jobs section",
			content: `on:
  workflow_dispatch:
`,
			wantErr: "missing 'jobs' section",
		},
		{
			name: "wrong job name",
			content: `on:
  workflow_dispatch:
jobs:
  setup:
    runs-on: ubuntu-latest
    steps:
      - run: echo install
`,
			wantErr: "missing 'copilot-setup-steps' job",
		},
		{
			name: "reusable workflow call",
			content: `on:
  workflow_dispatch:
jobs:
  copilot-setup-steps:
    uses: ./.github/workflows/shared-setup.yml
`,
			wantErr: "reusable workflow calls",
		},
		{
			name: "missing runs-on",
			content: `on:
  workflow_dispatch:
jobs:
  copilot-setup-steps:
    steps:
      - run: echo install
`,
			wantErr: "missing 'runs-on'",
		},
		{
			name: "no steps",
			content: `on:
  workflow_dispatch:
jobs:
  copilot-setup-steps:
    runs-on: ubuntu-latest
    steps: []
`,
			wantErr: "has no steps",
		},
		{
			name: "empty runs-on",
			content: `on:
  workflow_dispatch:
jobs:
  copilot-setup-steps:
    runs-on:
    steps:
      - run: echo install
`,
			wantErr: "empty 'runs-on'",
		},
		{
			name: "empty runs-on list",
			content: `on:
  workflow_dispatch:
jobs:
  copilot-setup-steps:
    runs-on: []
    steps:
      - run: echo install
`,
			wantErr: "empty 'runs-on'",
		},
		{
			name: "runs-on group object",
			content: `on:
  workflow_dispatch:
jobs:
  copilot-setup-steps:
    runs-on:
      group: my-group
    steps:
      - run: echo install
`,
			wantNoError: true,
		},
		{
			name: "runs-on object without runner",
			content: `on:
  workflow_dispatch:
jobs:
  copilot-setup-steps:
    runs-on:
      group: ""
    steps:
      - run: echo install
`,
			wantErr: "empty 'runs-on'",
		},
		{
			name: "null step",
			content: `on:
  workflow_dispatch:
jobs:
  copilot-setup-steps:
    runs-on: ubuntu-latest
    steps:
      -
`,
			wantErr: "step 1 is not a map",
		},
		{
			name: "step without run or uses",
			content: `on:
  workflow_dispatch:
jobs:
  copilot-setup-steps:
    runs-on: ubuntu-latest
    steps:
      - name: no action
`,
			wantErr: "step 1 must define 'run' or 'uses'",
		},
		{
			name: "yaml 1.1 boolean on key",
			content: `name: "Copilot Setup Steps"
true:
  workflow_dispatch:
jobs:
  copilot-setup-steps:
    runs-on: ubuntu-latest
    steps:
      - run: echo install
`,
			wantErr: "missing 'on' section",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateCopilotSetupStepsContent([]byte(tt.content))
			if tt.wantNoError {
				if err != nil {
					t.Fatalf("Expected content to be valid, got error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestGeneratedCopilotSetupStepsIsValid(t *testing.T) {
	t.Parallel()

	modes := []workflow.ActionMode{
		workflow.ActionModeRelease,
		workflow.ActionModeAction,
		workflow.ActionModeScript,
		workflow.ActionModeDev,
	}

	for _, mode := range modes {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			content := generateCopilotSetupStepsYAML(context.Background(), mode, "v1.2.3", &mockSHAResolver{sha: "abc123"})
			if err := validateCopilotSetupStepsContent([]byte(content)); err != nil {
				t.Errorf("Generated copilot-setup-steps.yml for mode %s is not valid: %v\n%s", mode, err, content)
			}
		})
	}
}

func TestGeneratedCopilotSetupStepsPinsCheckout(t *testing.T) {
	t.Parallel()

	expectedCheckoutRef := actionpins.ResolveLatestActionPin("actions/checkout", nil)
	if expectedCheckoutRef == "" {
		t.Fatal("expected embedded actions/checkout pin")
	}

	modes := []workflow.ActionMode{
		workflow.ActionModeRelease,
		workflow.ActionModeAction,
	}

	for _, mode := range modes {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			content := generateCopilotSetupStepsYAML(context.Background(), mode, "v1.2.3", &mockSHAResolver{sha: "bd9c0ca491e6334a2797ef56ad6ee89958d54ab9"})
			if strings.Contains(content, "uses: actions/checkout@v6") {
				t.Fatalf("generated copilot setup steps must not use unpinned checkout tag:\n%s", content)
			}
			if !strings.Contains(content, "uses: "+expectedCheckoutRef) {
				t.Fatalf("generated copilot setup steps should use pinned checkout ref %q:\n%s", expectedCheckoutRef, content)
			}
			for _, want := range []string{
				"permissions:\n  contents: read",
				"concurrency:\n  group: ${{ github.workflow }}-${{ github.ref }}\n  cancel-in-progress: true",
				"name: Copilot Setup Steps",
				"persist-credentials: false",
			} {
				if !strings.Contains(content, want) {
					t.Fatalf("generated copilot setup steps should contain %q for Zizmor-clean output:\n%s", want, content)
				}
			}
		})
	}
}

func TestEnsureCopilotSetupStepsWritesValidWorkflow(t *testing.T) {
	tmpDir := testutil.TempDir(t, "copilot-setup-valid-*")
	t.Setenv("GH_AW_WORKFLOWS_DIR", filepath.Join(tmpDir, ".github", "workflows"))

	if err := ensureCopilotSetupSteps(context.Background(), false, workflow.ActionModeRelease, "v1.2.3"); err != nil {
		t.Fatalf("ensureCopilotSetupSteps failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(tmpDir, ".github", "workflows", "copilot-setup-steps.yml"))
	if err != nil {
		t.Fatalf("Failed to read generated file: %v", err)
	}
	if err := validateCopilotSetupStepsContent(content); err != nil {
		t.Errorf("Generated copilot-setup-steps.yml is not valid: %v\n%s", err, content)
	}
}
