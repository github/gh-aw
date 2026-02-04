//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow"

	"github.com/goccy/go-yaml"
)

func TestEnsureCopilotSetupSteps(t *testing.T) {
	tests := []struct {
		name             string
		existingWorkflow *Workflow
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
			existingWorkflow: &Workflow{
				Name: "Copilot Setup Steps",
				On:   "workflow_dispatch",
				Jobs: map[string]WorkflowJob{
					"copilot-setup-steps": {
						RunsOn: "ubuntu-latest",
						Steps: []CopilotWorkflowStep{
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
			name: "renders instructions for existing workflow without install step",
			existingWorkflow: &Workflow{
				Name: "Copilot Setup Steps",
				On:   "workflow_dispatch",
				Jobs: map[string]WorkflowJob{
					"copilot-setup-steps": {
						RunsOn: "ubuntu-latest",
						Steps: []CopilotWorkflowStep{
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
				var wf Workflow
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
			err = ensureCopilotSetupSteps(tt.verbose, workflow.ActionModeDev, "dev")

			if (err != nil) != tt.wantErr {
				t.Errorf("ensureCopilotSetupSteps() error = %v, wantErr %v", err, tt.wantErr)
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

func TestInjectExtensionInstallStep(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		workflow      *Workflow
		wantErr       bool
		expectedSteps int
		validateFunc  func(*testing.T, *Workflow)
	}{
		{
			name: "injects at beginning of existing steps",
			workflow: &Workflow{
				Jobs: map[string]WorkflowJob{
					"copilot-setup-steps": {
						Steps: []CopilotWorkflowStep{
							{Name: "Some step"},
							{Name: "Build"},
						},
					},
				},
			},
			wantErr:       false,
			expectedSteps: 3, // 1 injected step + 2 existing steps
			validateFunc: func(t *testing.T, w *Workflow) {
				steps := w.Jobs["copilot-setup-steps"].Steps
				// Extension install should be at index 0 (beginning)
				if steps[0].Name != "Install gh-aw extension" {
					t.Errorf("Expected step 0 to be 'Install gh-aw extension', got %q", steps[0].Name)
				}
			},
		},
		{
			name: "injects when no existing steps",
			workflow: &Workflow{
				Jobs: map[string]WorkflowJob{
					"copilot-setup-steps": {
						Steps: []CopilotWorkflowStep{},
					},
				},
			},
			wantErr:       false,
			expectedSteps: 1, // 1 injected step (install only)
			validateFunc: func(t *testing.T, w *Workflow) {
				steps := w.Jobs["copilot-setup-steps"].Steps
				// Should have 1 step
				if len(steps) != 1 {
					t.Errorf("Expected 1 step, got %d", len(steps))
				}
				if steps[0].Name != "Install gh-aw extension" {
					t.Errorf("Expected step 0 to be 'Install gh-aw extension', got %q", steps[0].Name)
				}
			},
		},
		{
			name: "returns error when job not found",
			workflow: &Workflow{
				Jobs: map[string]WorkflowJob{
					"other-job": {
						Steps: []CopilotWorkflowStep{},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := injectExtensionInstallStep(tt.workflow, workflow.ActionModeDev, "dev")

			if (err != nil) != tt.wantErr {
				t.Errorf("injectExtensionInstallStep() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			job := tt.workflow.Jobs["copilot-setup-steps"]
			if len(job.Steps) != tt.expectedSteps {
				t.Errorf("Expected %d steps, got %d", tt.expectedSteps, len(job.Steps))
			}

			if tt.validateFunc != nil {
				tt.validateFunc(t, tt.workflow)
			}
		})
	}
}

func TestWorkflowStructMarshaling(t *testing.T) {
	t.Parallel()

	workflow := Workflow{
		Name: "Test Workflow",
		On:   "push",
		Jobs: map[string]WorkflowJob{
			"test-job": {
				RunsOn: "ubuntu-latest",
				Permissions: map[string]any{
					"contents": "read",
				},
				Steps: []CopilotWorkflowStep{
					{
						Name: "Checkout",
						Uses: "actions/checkout@v5",
					},
					{
						Name: "Run script",
						Run:  "echo 'test'",
						Env: map[string]any{
							"TEST_VAR": "value",
						},
					},
				},
			},
		},
	}

	// Marshal to YAML
	data, err := yaml.Marshal(&workflow)
	if err != nil {
		t.Fatalf("Failed to marshal workflow: %v", err)
	}

	// Unmarshal back
	var unmarshaledWorkflow Workflow
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
	var workflow Workflow
	if err := yaml.Unmarshal([]byte(copilotSetupStepsYAML), &workflow); err != nil {
		t.Fatalf("Failed to parse copilotSetupStepsYAML constant: %v", err)
	}

	// Verify key elements
	if workflow.Name != "Copilot Setup Steps" {
		t.Errorf("Expected workflow name 'Copilot Setup Steps', got %q", workflow.Name)
	}

	job, exists := workflow.Jobs["copilot-setup-steps"]
	if !exists {
		t.Fatal("Expected 'copilot-setup-steps' job to exist")
	}

	// Verify it has the extension install step
	hasExtensionInstall := false
	for _, step := range job.Steps {
		if strings.Contains(step.Run, "install-gh-aw.sh") || strings.Contains(step.Run, "curl -fsSL") {
			hasExtensionInstall = true
			break
		}
	}

	if !hasExtensionInstall {
		t.Error("Expected copilotSetupStepsYAML to contain extension install step with bash script")
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

	err = ensureCopilotSetupSteps(false, workflow.ActionModeDev, "dev")
	if err != nil {
		t.Fatalf("ensureCopilotSetupSteps() failed: %v", err)
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

func TestCopilotWorkflowStepStructure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		step CopilotWorkflowStep
	}{
		{
			name: "step with uses",
			step: CopilotWorkflowStep{
				Name: "Checkout",
				Uses: "actions/checkout@v5",
			},
		},
		{
			name: "step with run",
			step: CopilotWorkflowStep{
				Name: "Run command",
				Run:  "echo 'test'",
			},
		},
		{
			name: "step with environment",
			step: CopilotWorkflowStep{
				Name: "Run with env",
				Run:  "echo $TEST",
				Env: map[string]any{
					"TEST": "value",
				},
			},
		},
		{
			name: "step with with parameters",
			step: CopilotWorkflowStep{
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
			var unmarshaledStep CopilotWorkflowStep
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
	err = ensureCopilotSetupSteps(false, workflow.ActionModeDev, "dev")
	if err != nil {
		t.Fatalf("ensureCopilotSetupSteps() failed: %v", err)
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
	err = ensureCopilotSetupSteps(false, workflow.ActionModeRelease, testVersion)
	if err != nil {
		t.Fatalf("ensureCopilotSetupSteps() failed: %v", err)
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

	// Verify it has checkout step
	if !strings.Contains(contentStr, "actions/checkout@v4") {
		t.Error("Expected copilot-setup-steps.yml to have checkout step in release mode")
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
	err = ensureCopilotSetupSteps(false, workflow.ActionModeDev, "dev")
	if err != nil {
		t.Fatalf("ensureCopilotSetupSteps() failed: %v", err)
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
	err = ensureCopilotSetupSteps(false, workflow.ActionModeRelease, testVersion)
	if err != nil {
		t.Fatalf("ensureCopilotSetupSteps() failed: %v", err)
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
	if !strings.Contains(contentStr, "actions/checkout@v4") {
		t.Errorf("Expected checkout step in release mode")
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
	err = ensureCopilotSetupSteps(false, workflow.ActionModeDev, "dev")
	if err != nil {
		t.Fatalf("ensureCopilotSetupSteps() failed: %v", err)
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
	err = ensureCopilotSetupSteps(false, workflow.ActionModeRelease, testVersion)
	if err != nil {
		t.Fatalf("ensureCopilotSetupSteps() failed: %v", err)
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
	err = ensureCopilotSetupSteps(false, workflow.ActionModeDev, "dev")
	if err != nil {
		t.Fatalf("ensureCopilotSetupSteps() failed: %v", err)
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
      - uses: github/gh-aw/actions/setup-cli@v1.0.0
        with:
          version: v1.0.0
`
	setupStepsPath := filepath.Join(workflowsDir, "copilot-setup-steps.yml")
	if err := os.WriteFile(setupStepsPath, []byte(existingContent), 0644); err != nil {
		t.Fatalf("Failed to write existing workflow: %v", err)
	}

	// Attempt to update - should skip
	err = ensureCopilotSetupSteps(false, workflow.ActionModeRelease, "v2.0.0")
	if err != nil {
		t.Fatalf("ensureCopilotSetupSteps() failed: %v", err)
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
	err = ensureCopilotSetupSteps(false, workflow.ActionModeDev, "dev")
	if err != nil {
		t.Fatalf("ensureCopilotSetupSteps() failed: %v", err)
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

// TestInjectExtensionInstallStep_ReleaseMode tests injecting in release mode with version
func TestInjectExtensionInstallStep_ReleaseMode(t *testing.T) {
	wf := &Workflow{
		Jobs: map[string]WorkflowJob{
			"copilot-setup-steps": {
				Steps: []CopilotWorkflowStep{
					{Name: "Existing step", Run: "echo test"},
				},
			},
		},
	}

	testVersion := "v4.5.6"
	err := injectExtensionInstallStep(wf, workflow.ActionModeRelease, testVersion)
	if err != nil {
		t.Fatalf("injectExtensionInstallStep() failed: %v", err)
	}

	job := wf.Jobs["copilot-setup-steps"]

	// Should have 3 steps: checkout, install, existing (no verify)
	if len(job.Steps) != 3 {
		t.Fatalf("Expected 3 steps (checkout, install, existing), got %d", len(job.Steps))
	}

	// Verify checkout step
	if job.Steps[0].Name != "Checkout repository" {
		t.Errorf("First step should be checkout, got: %s", job.Steps[0].Name)
	}
	if job.Steps[0].Uses != "actions/checkout@v4" {
		t.Errorf("Checkout should use actions/checkout@v4, got: %s", job.Steps[0].Uses)
	}

	// Verify install step
	if job.Steps[1].Name != "Install gh-aw extension" {
		t.Errorf("Second step should be install, got: %s", job.Steps[1].Name)
	}
	expectedUses := "github/gh-aw/actions/setup-cli@v4.5.6"
	if job.Steps[1].Uses != expectedUses {
		t.Errorf("Install should use %s, got: %s", expectedUses, job.Steps[1].Uses)
	}
	if version, ok := job.Steps[1].With["version"]; !ok || version != testVersion {
		t.Errorf("Install step should have version: %s in with, got: %v", testVersion, job.Steps[1].With)
	}

	// Verify original step is preserved
	if job.Steps[2].Name != "Existing step" {
		t.Errorf("Third step should be existing step, got: %s", job.Steps[2].Name)
	}
}

// TestInjectExtensionInstallStep_DevMode tests injecting in dev mode
func TestInjectExtensionInstallStep_DevMode(t *testing.T) {
	wf := &Workflow{
		Jobs: map[string]WorkflowJob{
			"copilot-setup-steps": {
				Steps: []CopilotWorkflowStep{
					{Name: "Existing step", Run: "echo test"},
				},
			},
		},
	}

	err := injectExtensionInstallStep(wf, workflow.ActionModeDev, "dev")
	if err != nil {
		t.Fatalf("injectExtensionInstallStep() failed: %v", err)
	}

	job := wf.Jobs["copilot-setup-steps"]

	// Should have 2 steps: install, existing (no verify, no checkout in dev mode)
	if len(job.Steps) != 2 {
		t.Fatalf("Expected 2 steps (install, existing), got %d", len(job.Steps))
	}

	// Verify install step uses curl
	if job.Steps[0].Name != "Install gh-aw extension" {
		t.Errorf("First step should be install, got: %s", job.Steps[0].Name)
	}
	if !strings.Contains(job.Steps[0].Run, "curl -fsSL") {
		t.Errorf("Install should use curl, got: %s", job.Steps[0].Run)
	}
	if !strings.Contains(job.Steps[0].Run, "install-gh-aw.sh") {
		t.Errorf("Install should reference install-gh-aw.sh, got: %s", job.Steps[0].Run)
	}

	// Verify original step is preserved
	if job.Steps[1].Name != "Existing step" {
		t.Errorf("Second step should be existing step, got: %s", job.Steps[1].Name)
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
        uses: github/gh-aw/actions/setup-cli@v1.0.0
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
	err = upgradeCopilotSetupSteps(false, workflow.ActionModeRelease, "v2.0.0")
	if err != nil {
		t.Fatalf("upgradeCopilotSetupSteps() failed: %v", err)
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
	err = upgradeCopilotSetupSteps(false, workflow.ActionModeRelease, "v2.0.0")
	if err != nil {
		t.Fatalf("upgradeCopilotSetupSteps() failed: %v", err)
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
	err = upgradeCopilotSetupSteps(false, workflow.ActionModeDev, "dev")
	if err != nil {
		t.Fatalf("upgradeCopilotSetupSteps() failed: %v", err)
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

// TestUpgradeSetupCliVersion tests the upgradeSetupCliVersion helper function
func TestUpgradeSetupCliVersion(t *testing.T) {
	tests := []struct {
		name          string
		workflow      *Workflow
		actionMode    workflow.ActionMode
		version       string
		expectUpgrade bool
		expectError   bool
		validateFunc  func(*testing.T, *Workflow)
	}{
		{
			name: "upgrades release mode version",
			workflow: &Workflow{
				Jobs: map[string]WorkflowJob{
					"copilot-setup-steps": {
						Steps: []CopilotWorkflowStep{
							{
								Name: "Checkout",
								Uses: "actions/checkout@v4",
							},
							{
								Name: "Install gh-aw",
								Uses: "github/gh-aw/actions/setup-cli@v1.0.0",
								With: map[string]any{"version": "v1.0.0"},
							},
						},
					},
				},
			},
			actionMode:    workflow.ActionModeRelease,
			version:       "v2.0.0",
			expectUpgrade: true,
			expectError:   false,
			validateFunc: func(t *testing.T, wf *Workflow) {
				job := wf.Jobs["copilot-setup-steps"]
				installStep := job.Steps[1]
				if !strings.Contains(installStep.Uses, "@v2.0.0") {
					t.Errorf("Expected Uses to contain @v2.0.0, got: %s", installStep.Uses)
				}
				if installStep.With["version"] != "v2.0.0" {
					t.Errorf("Expected version to be v2.0.0, got: %v", installStep.With["version"])
				}
			},
		},
		{
			name: "no upgrade when no setup-cli action",
			workflow: &Workflow{
				Jobs: map[string]WorkflowJob{
					"copilot-setup-steps": {
						Steps: []CopilotWorkflowStep{
							{
								Name: "Some step",
								Run:  "echo test",
							},
						},
					},
				},
			},
			actionMode:    workflow.ActionModeRelease,
			version:       "v2.0.0",
			expectUpgrade: false,
			expectError:   false,
		},
		{
			name: "error when job not found",
			workflow: &Workflow{
				Jobs: map[string]WorkflowJob{
					"other-job": {
						Steps: []CopilotWorkflowStep{},
					},
				},
			},
			actionMode:    workflow.ActionModeRelease,
			version:       "v2.0.0",
			expectUpgrade: false,
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upgraded, err := upgradeSetupCliVersion(tt.workflow, tt.actionMode, tt.version)

			if (err != nil) != tt.expectError {
				t.Errorf("upgradeSetupCliVersion() error = %v, expectError %v", err, tt.expectError)
				return
			}

			if upgraded != tt.expectUpgrade {
				t.Errorf("upgradeSetupCliVersion() upgraded = %v, expectUpgrade %v", upgraded, tt.expectUpgrade)
			}

			if tt.validateFunc != nil && !tt.expectError {
				tt.validateFunc(t, tt.workflow)
			}
		})
	}
}
