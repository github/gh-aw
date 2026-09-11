//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestBuiltinJobsPreStepsInsertionOrder(t *testing.T) {
	tmpDir := testutil.TempDir(t, "builtin-pre-steps")

	workflowContent := `---
on:
  issue_comment:
    types: [created]
  roles: [admin]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
jobs:
  pre-activation:
    pre-steps:
      - name: Pre-activation pre-step
        run: echo "pre-activation"
  activation:
    pre-steps:
      - name: Activation pre-step
        run: echo "activation"
---

# Builtin pre-steps ordering

Run builtin pre-step ordering checks.
`

	workflowFile := filepath.Join(tmpDir, "builtin-pre-steps.md")
	if err := os.WriteFile(workflowFile, []byte(workflowContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowFile); err != nil {
		t.Fatalf("CompileWorkflow() returned error: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "builtin-pre-steps.lock.yml")
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockYAML := string(lockContent)

	activationSection := extractJobSection(lockYAML, "activation")
	if activationSection == "" {
		t.Fatal("Expected activation job section")
	}
	assertStepOrderInSection(t, activationSection,
		"id: setup",
		"- name: Activation pre-step",
		"- name: Checkout .github and .agents folders",
	)

	preActivationSection := extractJobSection(lockYAML, "pre_activation")
	if preActivationSection == "" {
		t.Fatal("Expected pre_activation job section")
	}
	assertStepOrderInSection(t, preActivationSection,
		"id: setup",
		"- name: Pre-activation pre-step",
		"- name: Check team membership",
	)

}

func TestBuiltinJobsSetupStepsRunBeforeCompilerSetup(t *testing.T) {
	tmpDir := testutil.TempDir(t, "builtin-setup-steps-token-order")

	workflowContent := `---
on:
  issue_comment:
    types: [created]
  roles: [admin]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
safe-outputs:
  github-app:
    app-id: "${{ vars.ACTIONS_APP_ID }}"
    private-key: "${{ secrets.ACTIONS_PRIVATE_KEY }}"
  add-comment:
jobs:
  agent:
    setup-steps:
      - name: Agent setup-step
        run: echo "agent-setup"
    pre-steps:
      - name: Agent pre-step
        run: echo "agent-pre"
  safe_outputs:
    setup-steps:
      - name: Safe outputs setup-step
        run: echo "safe-outputs-setup"
    pre-steps:
      - name: Safe outputs pre-step
        run: echo "safe-outputs-pre"
  conclusion:
    setup-steps:
      - name: Conclusion setup-step
        run: echo "conclusion-setup"
    pre-steps:
      - name: Conclusion pre-step
        run: echo "conclusion-pre"
---

# Builtin setup-steps ordering
`

	workflowFile := filepath.Join(tmpDir, "builtin-setup-steps.md")
	if err := os.WriteFile(workflowFile, []byte(workflowContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowFile); err != nil {
		t.Fatalf("CompileWorkflow() returned error: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "builtin-setup-steps.lock.yml")
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockYAML := string(lockContent)

	agentSection := extractJobSection(lockYAML, "agent")
	if agentSection == "" {
		t.Fatal("Expected agent job section")
	}
	assertStepOrderInSection(t, agentSection,
		"- name: Agent setup-step",
		"id: setup",
		"- name: Agent pre-step",
		"- name: Set runtime paths",
	)

	safeOutputsSection := extractJobSection(lockYAML, "safe_outputs")
	if safeOutputsSection == "" {
		t.Fatal("Expected safe_outputs job section")
	}
	assertStepOrderInSection(t, safeOutputsSection,
		"- name: Safe outputs setup-step",
		"id: setup",
		"- name: Safe outputs pre-step",
		"- name: Generate GitHub App token",
	)

	conclusionSection := extractJobSection(lockYAML, "conclusion")
	if conclusionSection == "" {
		t.Fatal("Expected conclusion job section")
	}
	assertStepOrderInSection(t, conclusionSection,
		"- name: Conclusion setup-step",
		"id: setup",
		"- name: Conclusion pre-step",
		"- name: Generate GitHub App token",
	)
}

func TestBuiltinJobSetupCheckoutAddsContentsReadPermission(t *testing.T) {
	tmpDir := testutil.TempDir(t, "builtin-setup-steps-checkout-permission")

	workflowContent := `---
on:
  issues:
    types: [opened]
engine: claude
strict: false
safe-outputs:
  add-comment:
jobs:
  safe_outputs:
    permissions:
      id-token: write
    setup-steps:
      - uses: actions/checkout@v4
        with:
          persist-credentials: false
      - uses: ./.github/actions/get-app-credentials
  conclusion:
    permissions:
      id-token: write
    setup-steps:
      - uses: ACTIONS/CHECKOUT@V4
        with:
          persist-credentials: false
      - uses: ./.github/actions/get-app-credentials
---

# Builtin checkout permissions
`

	workflowFile := filepath.Join(tmpDir, "builtin-checkout-permissions.md")
	if err := os.WriteFile(workflowFile, []byte(workflowContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowFile); err != nil {
		t.Fatalf("CompileWorkflow() returned error: %v", err)
	}

	lockContent, err := os.ReadFile(filepath.Join(tmpDir, "builtin-checkout-permissions.lock.yml"))
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockYAML := string(lockContent)

	for _, jobName := range []string{"safe_outputs", "conclusion"} {
		jobSection := extractJobSection(lockYAML, jobName)
		if jobSection == "" {
			t.Fatalf("Expected %s job section", jobName)
		}
		if !strings.Contains(jobSection, "contents: read") {
			t.Errorf("Expected %s setup checkout to add contents: read, got:\n%s", jobName, jobSection)
		}
		if !strings.Contains(jobSection, "id-token: write") {
			t.Errorf("Expected %s to preserve id-token: write, got:\n%s", jobName, jobSection)
		}
	}
}

func TestBuiltinJobsPreStepsRunBeforeTokenMinting(t *testing.T) {
	tmpDir := testutil.TempDir(t, "builtin-pre-steps-token-order")

	workflowContent := `---
on:
  issue_comment:
    types: [created]
  roles: [admin]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
safe-outputs:
  github-app:
    app-id: "${{ vars.ACTIONS_APP_ID }}"
    private-key: "${{ secrets.ACTIONS_PRIVATE_KEY }}"
  add-comment:
jobs:
  agent:
    pre-steps:
      - name: Agent pre-step
        run: echo "agent-pre"
  safe_outputs:
    pre-steps:
      - name: Safe outputs pre-step
        run: echo "safe-outputs-pre"
  conclusion:
    pre-steps:
      - name: Conclusion pre-step
        run: echo "conclusion-pre"
---

# Builtin pre-steps ordering with app token minting
`

	workflowFile := filepath.Join(tmpDir, "builtin-pre-steps-token-order.md")
	if err := os.WriteFile(workflowFile, []byte(workflowContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowFile); err != nil {
		t.Fatalf("CompileWorkflow() returned error: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "builtin-pre-steps-token-order.lock.yml")
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockYAML := string(lockContent)

	agentSection := extractJobSection(lockYAML, "agent")
	if agentSection == "" {
		t.Fatal("Expected agent job section")
	}
	assertStepOrderInSection(t, agentSection,
		"id: setup",
		"- name: Agent pre-step",
		"- name: Set runtime paths",
	)

	safeOutputsSection := extractJobSection(lockYAML, "safe_outputs")
	if safeOutputsSection == "" {
		t.Fatal("Expected safe_outputs job section")
	}
	assertStepOrderInSection(t, safeOutputsSection,
		"- name: Safe outputs pre-step",
		"- name: Generate GitHub App token",
	)

	conclusionSection := extractJobSection(lockYAML, "conclusion")
	if conclusionSection == "" {
		t.Fatal("Expected conclusion job section")
	}
	assertStepOrderInSection(t, conclusionSection,
		"- name: Conclusion pre-step",
		"- name: Generate GitHub App token",
	)
}

func TestImportedBuiltinJobSetupStepsMergeBeforeTokenMinting(t *testing.T) {
	tmpDir := testutil.TempDir(t, "imported-builtin-setup-steps")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatal(err)
	}

	sharedWorkflowContent := `---
jobs:
  safe_outputs:
    setup-steps:
      - name: Imported safe outputs setup-step
        run: echo "imported-safe-outputs-setup"
---

# Shared setup steps
`
	sharedWorkflowFile := filepath.Join(workflowsDir, "shared-safe-outputs.md")
	if err := os.WriteFile(sharedWorkflowFile, []byte(sharedWorkflowContent), 0644); err != nil {
		t.Fatal(err)
	}

	mainWorkflowContent := `---
on:
  issue_comment:
    types: [created]
  roles: [admin]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
imports:
  - ./shared-safe-outputs.md
safe-outputs:
  github-app:
    app-id: "${{ vars.ACTIONS_APP_ID }}"
    private-key: "${{ secrets.ACTIONS_PRIVATE_KEY }}"
  add-comment:
jobs:
  safe_outputs:
    setup-steps:
      - name: Main safe outputs setup-step
        run: echo "main-safe-outputs-setup"
---

# Imported builtin setup-steps ordering
`
	mainWorkflowFile := filepath.Join(workflowsDir, "main.md")
	if err := os.WriteFile(mainWorkflowFile, []byte(mainWorkflowContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(mainWorkflowFile); err != nil {
		t.Fatalf("CompileWorkflow() returned error: %v", err)
	}

	lockFile := filepath.Join(workflowsDir, "main.lock.yml")
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockYAML := string(lockContent)

	safeOutputsSection := extractJobSection(lockYAML, "safe_outputs")
	if safeOutputsSection == "" {
		t.Fatal("Expected safe_outputs job section")
	}
	assertStepOrderInSection(t, safeOutputsSection,
		"- name: Imported safe outputs setup-step",
		"- name: Main safe outputs setup-step",
		"- name: Generate GitHub App token",
	)
}

func TestBuiltinJobsSetupStepsRejectedForActivationAndPreActivation(t *testing.T) {
	tests := []struct {
		name   string
		jobKey string
	}{
		{name: "activation", jobKey: "activation"},
		{name: "pre-activation alias", jobKey: "pre-activation"},
		{name: "pre_activation", jobKey: "pre_activation"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "reject-setup-steps-"+tt.jobKey)
			workflowContent := `---
on:
  workflow_dispatch:
engine: claude
strict: false
jobs:
  ` + tt.jobKey + `:
    setup-steps:
      - name: blocked setup
        run: echo "blocked"
---

# Reject setup-steps on protected jobs
`

			workflowFile := filepath.Join(tmpDir, "reject.md")
			if err := os.WriteFile(workflowFile, []byte(workflowContent), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()
			err := compiler.CompileWorkflow(workflowFile)
			if err == nil {
				t.Fatalf("expected compile error for jobs.%s.setup-steps, got nil", tt.jobKey)
			}

			want := "jobs." + tt.jobKey + ".setup-steps is not allowed"
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("expected error containing %q, got: %v", want, err)
			}
		})
	}
}
