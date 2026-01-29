//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestJobManager_ValidateDuplicateSteps_NoDuplicates(t *testing.T) {
	jm := NewJobManager()

	// Add a job with unique steps
	job := &Job{
		Name:   "test-job",
		RunsOn: "ubuntu-latest",
		Steps: []string{
			`      - name: Checkout code
        uses: actions/checkout@v4`,
			`      - name: Setup Node.js
        uses: actions/setup-node@v4`,
			`      - name: Install dependencies
        run: npm install`,
		},
	}

	if err := jm.AddJob(job); err != nil {
		t.Fatalf("Failed to add job: %v", err)
	}

	// Validate should pass - no duplicates
	err := jm.ValidateDuplicateSteps()
	if err != nil {
		t.Errorf("Expected no error for unique steps, got: %v", err)
	}
}

func TestJobManager_ValidateDuplicateSteps_WithDuplicates(t *testing.T) {
	jm := NewJobManager()

	// Add a job with duplicate steps (compiler bug scenario)
	job := &Job{
		Name:   "test-job",
		RunsOn: "ubuntu-latest",
		Steps: []string{
			`      - name: Checkout code
        uses: actions/checkout@v4`,
			`      - name: Setup Node.js
        uses: actions/setup-node@v4`,
			`      - name: Checkout code
        uses: actions/checkout@v4`,
		},
	}

	if err := jm.AddJob(job); err != nil {
		t.Fatalf("Failed to add job: %v", err)
	}

	// Validate should fail - duplicate "Checkout code" step
	err := jm.ValidateDuplicateSteps()
	if err == nil {
		t.Error("Expected error for duplicate steps, got nil")
		return
	}

	expectedSubstrings := []string{
		"compiler bug",
		"duplicate step",
		"Checkout code",
		"test-job",
	}

	for _, substr := range expectedSubstrings {
		if !strings.Contains(err.Error(), substr) {
			t.Errorf("Error message should contain '%s', got: %v", substr, err)
		}
	}
}

func TestJobManager_ValidateDuplicateSteps_MultipleJobs(t *testing.T) {
	jm := NewJobManager()

	// Add multiple jobs - duplicates within a job should fail, across jobs should pass
	job1 := &Job{
		Name:   "job1",
		RunsOn: "ubuntu-latest",
		Steps: []string{
			`      - name: Checkout code
        uses: actions/checkout@v4`,
			`      - name: Setup Node.js
        uses: actions/setup-node@v4`,
		},
	}

	job2 := &Job{
		Name:   "job2",
		RunsOn: "ubuntu-latest",
		Steps: []string{
			`      - name: Checkout code
        uses: actions/checkout@v4`, // Same name as in job1 - this is OK
			`      - name: Run tests
        run: npm test`,
		},
	}

	if err := jm.AddJob(job1); err != nil {
		t.Fatalf("Failed to add job1: %v", err)
	}
	if err := jm.AddJob(job2); err != nil {
		t.Fatalf("Failed to add job2: %v", err)
	}

	// Validate should pass - duplicates across jobs are OK
	err := jm.ValidateDuplicateSteps()
	if err != nil {
		t.Errorf("Expected no error for duplicate steps across jobs, got: %v", err)
	}
}

func TestJobManager_ValidateDuplicateSteps_EmptyJobs(t *testing.T) {
	jm := NewJobManager()

	// Add jobs with no steps
	job1 := &Job{
		Name:   "job1",
		RunsOn: "ubuntu-latest",
		Steps:  []string{},
	}

	job2 := &Job{
		Name:   "job2",
		RunsOn: "ubuntu-latest",
		Uses:   "./.github/workflows/reusable.yml", // Reusable workflow - no steps
	}

	if err := jm.AddJob(job1); err != nil {
		t.Fatalf("Failed to add job1: %v", err)
	}
	if err := jm.AddJob(job2); err != nil {
		t.Fatalf("Failed to add job2: %v", err)
	}

	// Validate should pass - empty jobs are OK
	err := jm.ValidateDuplicateSteps()
	if err != nil {
		t.Errorf("Expected no error for empty jobs, got: %v", err)
	}
}

func TestJobManager_ValidateDuplicateSteps_StepsWithoutNames(t *testing.T) {
	jm := NewJobManager()

	// Add a job with steps that don't have names (edge case)
	job := &Job{
		Name:   "test-job",
		RunsOn: "ubuntu-latest",
		Steps: []string{
			`      - uses: actions/checkout@v4`, // No name
			`      - run: echo "Hello"`,         // No name
			`      - name: Named step
        run: echo "World"`,
		},
	}

	if err := jm.AddJob(job); err != nil {
		t.Fatalf("Failed to add job: %v", err)
	}

	// Validate should pass - steps without names can't be checked for duplicates
	err := jm.ValidateDuplicateSteps()
	if err != nil {
		t.Errorf("Expected no error for steps without names, got: %v", err)
	}
}

func TestJobManager_ValidateDuplicateSteps_MultipleIdenticalSteps(t *testing.T) {
	jm := NewJobManager()

	// Add a job with the same step appearing three times (severe compiler bug)
	job := &Job{
		Name:   "buggy-job",
		RunsOn: "ubuntu-latest",
		Steps: []string{
			`      - name: Setup Scripts
        uses: ./actions/setup`,
			`      - name: Install tools
        run: npm install`,
			`      - name: Setup Scripts
        uses: ./actions/setup`,
			`      - name: Setup Scripts
        uses: ./actions/setup`,
		},
	}

	if err := jm.AddJob(job); err != nil {
		t.Fatalf("Failed to add job: %v", err)
	}

	// Validate should fail on the first duplicate
	err := jm.ValidateDuplicateSteps()
	if err == nil {
		t.Error("Expected error for duplicate steps, got nil")
		return
	}

	// Should report the first duplicate found
	if !strings.Contains(err.Error(), "Setup Scripts") {
		t.Errorf("Error should mention 'Setup Scripts', got: %v", err)
	}
}

func TestExtractStepName(t *testing.T) {
	tests := []struct {
		name     string
		stepYAML string
		want     string
	}{
		{
			name: "simple step with name",
			stepYAML: `      - name: Checkout code
        uses: actions/checkout@v4`,
			want: "Checkout code",
		},
		{
			name: "step with quoted name",
			stepYAML: `      - name: "Setup Node.js"
        uses: actions/setup-node@v4`,
			want: "Setup Node.js",
		},
		{
			name: "step with single quoted name",
			stepYAML: `      - name: 'Run tests'
        run: npm test`,
			want: "Run tests",
		},
		{
			name: "step without name",
			stepYAML: `      - uses: actions/checkout@v4
        with:
          token: ${{ secrets.GITHUB_TOKEN }}`,
			want: "",
		},
		{
			name:     "empty step",
			stepYAML: "",
			want:     "",
		},
		{
			name: "name with special characters",
			stepYAML: `      - name: Upload artifact (logs)
        uses: actions/upload-artifact@v4`,
			want: "Upload artifact (logs)",
		},
		{
			name: "multiline step with name on second line",
			stepYAML: `      - id: test-step
        name: Run integration tests
        run: npm test`,
			want: "Run integration tests",
		},
		{
			name: "name with GitHub expression",
			stepYAML: `      - name: Upload ${{ matrix.os }} logs
        uses: actions/upload-artifact@v4`,
			want: "Upload ${{ matrix.os }} logs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractStepName(tt.stepYAML)
			if got != tt.want {
				t.Errorf("extractStepName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestJobManager_ValidateDuplicateSteps_CaseSensitive(t *testing.T) {
	jm := NewJobManager()

	// Steps with different casing should be treated as different steps
	job := &Job{
		Name:   "test-job",
		RunsOn: "ubuntu-latest",
		Steps: []string{
			`      - name: checkout code
        uses: actions/checkout@v4`,
			`      - name: Checkout Code
        uses: actions/checkout@v4`,
			`      - name: CHECKOUT CODE
        uses: actions/checkout@v4`,
		},
	}

	if err := jm.AddJob(job); err != nil {
		t.Fatalf("Failed to add job: %v", err)
	}

	// Validate should pass - different casing means different step names
	err := jm.ValidateDuplicateSteps()
	if err != nil {
		t.Errorf("Expected no error for steps with different casing, got: %v", err)
	}
}

func TestJobManager_ValidateDuplicateSteps_ReportsCorrectPosition(t *testing.T) {
	jm := NewJobManager()

	// Add a job with duplicates at specific positions
	job := &Job{
		Name:   "position-test",
		RunsOn: "ubuntu-latest",
		Steps: []string{
			`      - name: Step A
        run: echo "A"`,
			`      - name: Step B
        run: echo "B"`,
			`      - name: Step C
        run: echo "C"`,
			`      - name: Step B
        run: echo "B again"`,
		},
	}

	if err := jm.AddJob(job); err != nil {
		t.Fatalf("Failed to add job: %v", err)
	}

	// Validate should fail and report positions 1 and 3 (0-indexed)
	err := jm.ValidateDuplicateSteps()
	if err == nil {
		t.Error("Expected error for duplicate steps, got nil")
		return
	}

	// Check that the error message includes the position information
	errorMsg := err.Error()
	if !strings.Contains(errorMsg, "positions 1 and 3") {
		t.Errorf("Error should mention 'positions 1 and 3', got: %v", errorMsg)
	}
}
