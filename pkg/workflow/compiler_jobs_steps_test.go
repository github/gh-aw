//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
)

// ========================================
// Custom Job Setup and Pre-Step Tests
// ========================================

// TestBuildCustomJobsWithActivation tests building custom jobs with activation dependency
func TestBuildCustomJobsWithActivation(t *testing.T) {
	tmpDir := testutil.TempDir(t, "custom-jobs-test")

	frontmatter := `---
on: push
permissions:
  contents: read
engine: copilot
strict: false
jobs:
  custom_lint:
    runs-on: ubuntu-latest
    steps:
      - run: echo "lint"
  custom_build:
    runs-on: ubuntu-latest
    needs: custom_lint
    steps:
      - run: echo "build"
---

# Test Workflow

Test content`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("CompileWorkflow() error: %v", err)
	}

	// Read compiled output
	lockFile := filepath.Join(tmpDir, "test.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	yamlStr := string(content)

	// Check that custom jobs exist
	if !strings.Contains(yamlStr, "custom_lint:") {
		t.Error("Expected custom_lint job")
	}
	if !strings.Contains(yamlStr, "custom_build:") {
		t.Error("Expected custom_build job")
	}

	// custom_lint without explicit needs should depend on activation
	// custom_build has explicit needs so should keep that
}

// TestCustomJobPreStepsAreInsertedBeforeCheckout verifies jobs.<job-id>.pre-steps
// are emitted after setup-injected steps and before checkout in custom jobs.
func TestCustomJobPreStepsAreInsertedBeforeCheckout(t *testing.T) {
	tmpDir := testutil.TempDir(t, "custom-job-pre-steps")

	frontmatter := `---
on: push
permissions:
  contents: read
engine: copilot
strict: false
jobs:
  custom_job:
    runs-on: ubuntu-latest
    pre-steps:
      - name: Pre setup
        run: echo "pre"
      - name: Prepare token
        run: echo "token"
    steps:
      - name: Checkout repo
        uses: actions/checkout@v6
      - name: Main work
        run: echo "work"
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("CompileWorkflow() error: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "test.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	yamlStr := string(content)
	customJobSection := extractJobSection(yamlStr, "custom_job")
	if customJobSection == "" {
		t.Fatal("Expected custom_job section in lock file")
	}

	assertStepOrderInSection(t, customJobSection,
		"- name: Configure GH_HOST for enterprise compatibility",
		"- name: Pre setup",
		"- name: Prepare token",
		"- name: Checkout repo",
		"- name: Main work",
	)
	assert.Contains(t, customJobSection, "persist-credentials: false", "custom job checkout should disable credential persistence by default")
}

func TestCustomJobCheckoutPreservesExplicitPersistCredentialsTrue(t *testing.T) {
	tmpDir := testutil.TempDir(t, "custom-job-checkout-persist-true")

	frontmatter := `---
on: push
permissions:
  contents: read
engine: copilot
strict: false
jobs:
  custom_job:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout repo
        uses: actions/checkout@v6
        with:
          persist-credentials: true
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("CompileWorkflow() error: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "test.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	yamlStr := string(content)
	customJobSection := extractJobSection(yamlStr, "custom_job")
	if customJobSection == "" {
		t.Fatal("Expected custom_job section in lock file")
	}

	assert.Contains(t, customJobSection, "persist-credentials: true", "explicit persist-credentials: true should be preserved")
	assert.NotContains(t, customJobSection, "persist-credentials: false", "compiler should not override explicit persist-credentials: true")
}

func TestCustomJobCheckoutHardensNullPersistCredentials(t *testing.T) {
	// A nil value for persist-credentials (YAML null) must be treated as absent
	// and hardened to false. The compiler rejects YAML null in with-fields before
	// reaching this function, but direct unit coverage guards against future
	// code paths that could pass a nil value through.
	stepMap := map[string]any{
		"uses": "actions/checkout@v6",
		"with": map[string]any{
			"persist-credentials": nil,
		},
	}
	ensureCheckoutPersistCredentials(stepMap)
	withMap, _ := stepMap["with"].(map[string]any)
	assert.Equal(t, false, withMap["persist-credentials"], "null persist-credentials should be hardened to false")
}

func TestPreStepsInsertAfterSetupBoundary(t *testing.T) {
	tmpDir := testutil.TempDir(t, "builtin-job-pre-steps-setup-boundary")

	frontmatter := `---
on: push
permissions:
  contents: read
engine: copilot
strict: false
jobs:
  pre_activation:
    pre-steps:
      - name: Pre-activation uses pre-step
        uses: actions/setup-node@v4
        with:
          node-version: "24"
  activation:
    pre-steps:
      - name: Activation run pre-step
        run: echo "activation prep"
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("CompileWorkflow() error: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "test.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	yamlStr := string(content)
	var lockFileYAML map[string]any
	if err := yaml.Unmarshal(content, &lockFileYAML); err != nil {
		t.Fatalf("Expected generated lock file to be valid YAML: %v", err)
	}
	jobsNode, ok := lockFileYAML["jobs"].(map[string]any)
	if !ok {
		t.Fatalf("Expected generated lock file to contain jobs map, got: %T", lockFileYAML["jobs"])
	}
	if _, ok := jobsNode["pre_activation"]; !ok {
		t.Fatalf("Expected pre_activation job in parsed lock file YAML")
	}
	if _, ok := jobsNode["activation"]; !ok {
		t.Fatalf("Expected activation job in parsed lock file YAML")
	}

	preActivationSection := extractJobSection(yamlStr, "pre_activation")
	if preActivationSection == "" {
		t.Fatal("Expected pre_activation section in lock file")
	}
	preActivationJobNameIdx := indexInNonCommentLinesInSection(preActivationSection, "job-name: ${{ github.job }}")
	preActivationPreStepIdx := indexInNonCommentLinesInSection(preActivationSection, "- name: Pre-activation uses pre-step")
	preActivationMembershipCheckIdx := indexInNonCommentLinesInSection(preActivationSection, "- name: Check team membership for workflow")
	if preActivationJobNameIdx == -1 || preActivationPreStepIdx == -1 || preActivationMembershipCheckIdx == -1 {
		t.Fatalf("Expected setup body, pre-step, and membership check in pre_activation section:\n%s", preActivationSection)
	}
	if preActivationPreStepIdx <= preActivationJobNameIdx {
		t.Fatalf("Expected pre_activation pre-step to be inserted after setup step body in section:\n%s", preActivationSection)
	}
	if preActivationPreStepIdx >= preActivationMembershipCheckIdx {
		t.Fatalf("Expected pre_activation pre-step before the first regular step in section:\n%s", preActivationSection)
	}

	activationSection := extractJobSection(yamlStr, "activation")
	if activationSection == "" {
		t.Fatal("Expected activation section in lock file")
	}
	activationJobNameIdx := indexInNonCommentLinesInSection(activationSection, "job-name: ${{ github.job }}")
	activationPreStepIdx := indexInNonCommentLinesInSection(activationSection, "- name: Activation run pre-step")
	activationCheckoutIdx := indexInNonCommentLinesInSection(activationSection, "- name: Checkout .github and .agents folders")
	if activationJobNameIdx == -1 || activationPreStepIdx == -1 || activationCheckoutIdx == -1 {
		t.Fatalf("Expected setup body, pre-step, and repository checkout in activation section:\n%s", activationSection)
	}
	if activationPreStepIdx <= activationJobNameIdx {
		t.Fatalf("Expected activation pre-step to be inserted after setup step body in section:\n%s", activationSection)
	}
	if activationPreStepIdx >= activationCheckoutIdx {
		t.Fatalf("Expected activation pre-step before checkout in section:\n%s", activationSection)
	}
}

func TestInsertPreStepsAfterSetupBeforeCheckout(t *testing.T) {
	tests := []struct {
		name     string
		steps    []string
		preSteps []string
		want     []string
	}{
		{
			name: "insert at next step boundary after setup id",
			steps: []string{
				"      - name: Setup Scripts",
				"        uses: actions/github-script@v7",
				"        with:",
				"          job-name: ${{ github.job }}",
				"        id: setup",
				"      - name: Checkout repository",
				"        uses: actions/checkout@v6",
			},
			preSteps: []string{
				"      - name: Pre setup",
				"        run: echo \"pre\"",
			},
			want: []string{
				"      - name: Setup Scripts",
				"        uses: actions/github-script@v7",
				"        with:",
				"          job-name: ${{ github.job }}",
				"        id: setup",
				"      - name: Pre setup",
				"        run: echo \"pre\"",
				"      - name: Checkout repository",
				"        uses: actions/checkout@v6",
			},
		},
		{
			name: "append when setup is final step and no boundary exists",
			steps: []string{
				"      - name: Setup Scripts",
				"        uses: actions/github-script@v7",
				"        id: setup",
			},
			preSteps: []string{
				"      - name: Pre setup",
				"        run: echo \"pre\"",
			},
			want: []string{
				"      - name: Setup Scripts",
				"        uses: actions/github-script@v7",
				"        id: setup",
				"      - name: Pre setup",
				"        run: echo \"pre\"",
			},
		},
		{
			name: "insert before checkout when setup step is not present",
			steps: []string{
				"      - name: Checkout repository",
				"        uses: actions/checkout@v6",
				"      - name: Main work",
				"        run: echo \"work\"",
			},
			preSteps: []string{
				"      - name: Pre setup",
				"        run: echo \"pre\"",
			},
			want: []string{
				"      - name: Pre setup",
				"        run: echo \"pre\"",
				"      - name: Checkout repository",
				"        uses: actions/checkout@v6",
				"      - name: Main work",
				"        run: echo \"work\"",
			},
		},
		{
			name: "insert before token mint when no setup or checkout exists",
			steps: []string{
				"      - name: Generate GitHub App token",
				"        uses: actions/create-github-app-token@v3",
				"      - name: Main work",
				"        run: echo \"work\"",
			},
			preSteps: []string{
				"      - name: Pre setup",
				"        run: echo \"pre\"",
			},
			want: []string{
				"      - name: Pre setup",
				"        run: echo \"pre\"",
				"      - name: Generate GitHub App token",
				"        uses: actions/create-github-app-token@v3",
				"      - name: Main work",
				"        run: echo \"work\"",
			},
		},
		{
			name: "insert before token mint shorthand step without name",
			steps: []string{
				"      - uses: actions/create-github-app-token@v3",
				"      - name: Main work",
				"        run: echo \"work\"",
			},
			preSteps: []string{
				"      - name: Pre setup",
				"        run: echo \"pre\"",
			},
			want: []string{
				"      - name: Pre setup",
				"        run: echo \"pre\"",
				"      - uses: actions/create-github-app-token@v3",
				"      - name: Main work",
				"        run: echo \"work\"",
			},
		},
		{
			name: "insert after setup scaffold when token mint and checkout also present",
			steps: []string{
				"      - name: Setup Scripts",
				"        uses: actions/github-script@v7",
				"        id: setup",
				"      - name: Generate GitHub App token",
				"        uses: actions/create-github-app-token@v3",
				"      - name: Checkout repository",
				"        uses: actions/checkout@v6",
				"      - name: Main work",
				"        run: echo \"work\"",
			},
			preSteps: []string{
				"      - name: Pre setup",
				"        run: echo \"pre\"",
			},
			want: []string{
				"      - name: Setup Scripts",
				"        uses: actions/github-script@v7",
				"        id: setup",
				"      - name: Pre setup",
				"        run: echo \"pre\"",
				"      - name: Generate GitHub App token",
				"        uses: actions/create-github-app-token@v3",
				"      - name: Checkout repository",
				"        uses: actions/checkout@v6",
				"      - name: Main work",
				"        run: echo \"work\"",
			},
		},
		{
			name: "insert before checkout shorthand step without name",
			steps: []string{
				"      - uses: actions/checkout@v6",
				"      - name: Main work",
				"        run: echo \"work\"",
			},
			preSteps: []string{
				"      - name: Pre setup",
				"        run: echo \"pre\"",
			},
			want: []string{
				"      - name: Pre setup",
				"        run: echo \"pre\"",
				"      - uses: actions/checkout@v6",
				"      - name: Main work",
				"        run: echo \"work\"",
			},
		},
		{
			name: "return input steps unchanged when pre-steps are empty",
			steps: []string{
				"      - name: Main work",
				"        run: echo \"work\"",
			},
			preSteps: []string{},
			want: []string{
				"      - name: Main work",
				"        run: echo \"work\"",
			},
		},
		{
			name:  "insert pre-steps when steps are empty",
			steps: []string{},
			preSteps: []string{
				"      - name: Pre setup",
				"        run: echo \"pre\"",
			},
			want: []string{
				"      - name: Pre setup",
				"        run: echo \"pre\"",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := insertPreStepsAtEarliestBoundary(tt.steps, tt.preSteps)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("insertPreStepsAtEarliestBoundary() mismatch\nwant:\n%q\ngot:\n%q", tt.want, got)
			}
		})
	}
}

func TestInsertSetupStepsAtStart(t *testing.T) {
	steps := []string{
		"      - name: Setup Scripts",
		"        uses: actions/github-script@v7",
		"        id: setup",
		"      - name: Set runtime paths",
		"        run: echo runtime",
		"      - name: Main work",
		"        run: echo work",
	}
	setupSteps := []string{
		"      - name: Setup extension",
		"        run: echo setup",
	}

	got := insertSetupStepsAtStart(steps, setupSteps)
	want := []string{
		"      - name: Setup extension",
		"        run: echo setup",
		"      - name: Setup Scripts",
		"        uses: actions/github-script@v7",
		"        id: setup",
		"      - name: Set runtime paths",
		"        run: echo runtime",
		"      - name: Main work",
		"        run: echo work",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("insertSetupStepsAtStart() mismatch\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestCustomJobPreStepsSchemaValidation(t *testing.T) {
	tmpDir := testutil.TempDir(t, "custom-job-pre-steps-schema")

	frontmatter := `---
on: push
permissions:
  contents: read
engine: copilot
strict: false
jobs:
  custom_job:
    runs-on: ubuntu-latest
    pre-steps:
      name: Invalid pre-steps
      run: echo "invalid"
    steps:
      - run: echo "work"
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(testFile)
	if err == nil {
		t.Fatal("Expected schema validation error for non-array jobs.<job-id>.pre-steps, got nil")
	}
	if !strings.Contains(err.Error(), "pre-steps") {
		t.Fatalf("Expected error to mention pre-steps, got: %v", err)
	}
}

func TestCustomJobSetupStepsSchemaValidation(t *testing.T) {
	tmpDir := testutil.TempDir(t, "custom-job-setup-steps-schema")

	frontmatter := `---
on: push
permissions:
  contents: read
engine: copilot
strict: false
jobs:
  custom_job:
    runs-on: ubuntu-latest
    setup-steps:
      name: Invalid setup-steps
      run: echo "invalid"
    steps:
      - run: echo "work"
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	err := compiler.CompileWorkflow(testFile)
	if err == nil {
		t.Fatal("Expected schema validation error for non-array jobs.<job-id>.setup-steps, got nil")
	}
	if !strings.Contains(err.Error(), "setup-steps") {
		t.Fatalf("Expected error to mention setup-steps, got: %v", err)
	}
}

func TestCustomJobSetupAndPreStepsOrdering(t *testing.T) {
	tmpDir := testutil.TempDir(t, "custom-job-setup-and-pre-steps")

	frontmatter := `---
on: push
permissions:
  contents: read
engine: copilot
strict: false
jobs:
  custom_job:
    runs-on: ubuntu-latest
    setup-steps:
      - name: Setup step
        run: echo "setup"
    pre-steps:
      - name: Pre step
        run: echo "pre"
    steps:
      - name: Main step
        run: echo "work"
---

# Test Workflow
`

	testFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(testFile, []byte(frontmatter), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("CompileWorkflow() returned error: %v", err)
	}

	lockFile := filepath.Join(tmpDir, "test.lock.yml")
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	customJobSection := extractJobSection(string(lockContent), "custom_job")
	if customJobSection == "" {
		t.Fatal("Expected custom_job section")
	}
	assertStepOrderInSection(t, customJobSection,
		"- name: Setup step",
		"- name: Configure GH_HOST for enterprise compatibility",
		"- name: Pre step",
		"- name: Main step",
	)
}

func assertStepOrderInSection(t *testing.T, section string, orderedSteps ...string) {
	t.Helper()

	prev := -1
	for _, step := range orderedSteps {
		idx := indexInNonCommentLinesInSection(section, step)
		if idx == -1 {
			t.Fatalf("Expected step %q in section:\n%s", step, section)
		}
		if prev >= idx {
			t.Fatalf("Expected step order %v in section, but %q appeared at %d after previous index %d\n%s",
				orderedSteps, step, idx, prev, section)
		}
		prev = idx
	}
}

func indexInNonCommentLinesInSection(content string, target string) int {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(line, target) {
			return i
		}
	}
	return -1
}
