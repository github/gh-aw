//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestJobDependenciesWithCycleDetection(t *testing.T) {
	// Test cycle detection at the JobManager level to avoid frontmatter validation issues
	tests := []struct {
		name        string
		jobs        []*Job
		expectError bool
		errorMsg    string
		description string
	}{
		{
			name: "valid job dependencies",
			jobs: []*Job{
				{Name: "build", RunsOn: "ubuntu-latest"},
				{Name: "test", RunsOn: "ubuntu-latest", Needs: []string{"build"}},
				{Name: "deploy", RunsOn: "ubuntu-latest", Needs: []string{"build", "test"}},
			},
			expectError: false,
			description: "Valid linear job dependencies should work",
		},
		{
			name: "simple cycle detection",
			jobs: []*Job{
				{Name: "job1", RunsOn: "ubuntu-latest", Needs: []string{"job2"}},
				{Name: "job2", RunsOn: "ubuntu-latest", Needs: []string{"job1"}},
			},
			expectError: true,
			errorMsg:    "cycle detected",
			description: "Simple cycle between two jobs should be detected",
		},
		{
			name: "complex cycle detection",
			jobs: []*Job{
				{Name: "job1", RunsOn: "ubuntu-latest", Needs: []string{"job2"}},
				{Name: "job2", RunsOn: "ubuntu-latest", Needs: []string{"job3"}},
				{Name: "job3", RunsOn: "ubuntu-latest", Needs: []string{"job1"}},
			},
			expectError: true,
			errorMsg:    "cycle detected",
			description: "Complex cycle through multiple jobs should be detected",
		},
		{
			name: "dependency on non-existent job",
			jobs: []*Job{
				{Name: "job1", RunsOn: "ubuntu-latest", Needs: []string{"nonexistent_job"}},
			},
			expectError: true,
			errorMsg:    "depends on non-existent job",
			description: "Dependency on non-existent job should be detected",
		},
		{
			name: "self-dependency cycle",
			jobs: []*Job{
				{Name: "job1", RunsOn: "ubuntu-latest", Needs: []string{"job1"}},
			},
			expectError: true,
			errorMsg:    "cycle detected",
			description: "Self-dependency should be detected as a cycle",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := NewJobManager()

			// Add all jobs to the manager
			for _, job := range tt.jobs {
				if err := jm.AddJob(job); err != nil {
					t.Fatalf("Failed to add job %s: %v", job.Name, err)
				}
			}

			// Validate dependencies
			err := jm.ValidateDependencies()

			if tt.expectError {
				if err == nil {
					t.Errorf("%s: expected error but validation succeeded", tt.description)
					return
				}
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("%s: expected error containing '%s', but got: %v", tt.description, tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("%s: unexpected error during validation: %v", tt.description, err)
					return
				}

				// For valid cases, test topological ordering
				order, err := jm.GetTopologicalOrder()
				if err != nil {
					t.Errorf("%s: failed to get topological order: %v", tt.description, err)
					return
				}

				// Verify all jobs are included in the order
				if len(order) != len(tt.jobs) {
					t.Errorf("%s: topological order length %d, expected %d", tt.description, len(order), len(tt.jobs))
				}

				// Verify dependencies are respected in the order
				if tt.name == "valid job dependencies" {
					buildIndex := -1
					testIndex := -1
					deployIndex := -1

					for i, jobName := range order {
						switch jobName {
						case "build":
							buildIndex = i
						case "test":
							testIndex = i
						case "deploy":
							deployIndex = i
						}
					}

					// build should come before test and deploy
					if buildIndex >= testIndex {
						t.Errorf("%s: build (index %d) should come before test (index %d)", tt.description, buildIndex, testIndex)
					}
					if buildIndex >= deployIndex {
						t.Errorf("%s: build (index %d) should come before deploy (index %d)", tt.description, buildIndex, deployIndex)
					}
					// test should come before deploy
					if testIndex >= deployIndex {
						t.Errorf("%s: test (index %d) should come before deploy (index %d)", tt.description, testIndex, deployIndex)
					}
				}
			}
		})
	}
}

func TestJobDependencyTopologicalOrder(t *testing.T) {
	// Test topological ordering at the JobManager level
	jm := NewJobManager()

	// Create a complex dependency graph: build -> [unit-test, integration-test] -> deploy
	jobs := []*Job{
		{Name: "build", RunsOn: "ubuntu-latest"},
		{Name: "unit-test", RunsOn: "ubuntu-latest", Needs: []string{"build"}},
		{Name: "integration-test", RunsOn: "ubuntu-latest", Needs: []string{"build"}},
		{Name: "deploy", RunsOn: "ubuntu-latest", Needs: []string{"unit-test", "integration-test"}},
	}

	for _, job := range jobs {
		if err := jm.AddJob(job); err != nil {
			t.Fatalf("Failed to add job %s: %v", job.Name, err)
		}
	}

	// Get topological order
	order, err := jm.GetTopologicalOrder()
	if err != nil {
		t.Fatalf("Failed to get topological order: %v", err)
	}

	// Verify all jobs are included
	if len(order) != len(jobs) {
		t.Errorf("Topological order length %d, expected %d", len(order), len(jobs))
	}

	// Find positions of jobs in the order
	positions := make(map[string]int)
	for i, jobName := range order {
		positions[jobName] = i
	}

	// Verify dependency constraints are satisfied
	// build should come before unit-test and integration-test
	if positions["build"] >= positions["unit-test"] {
		t.Errorf("build (position %d) should come before unit-test (position %d)", positions["build"], positions["unit-test"])
	}
	if positions["build"] >= positions["integration-test"] {
		t.Errorf("build (position %d) should come before integration-test (position %d)", positions["build"], positions["integration-test"])
	}

	// unit-test and integration-test should come before deploy
	if positions["unit-test"] >= positions["deploy"] {
		t.Errorf("unit-test (position %d) should come before deploy (position %d)", positions["unit-test"], positions["deploy"])
	}
	if positions["integration-test"] >= positions["deploy"] {
		t.Errorf("integration-test (position %d) should come before deploy (position %d)", positions["integration-test"], positions["deploy"])
	}

	t.Logf("Topological order: %v", order)
}

func TestJobDependsOnAgent(t *testing.T) {
	tests := []struct {
		name     string
		config   map[string]any
		expected bool
	}{
		{
			name:     "no needs field",
			config:   map[string]any{"runs-on": "ubuntu-latest"},
			expected: false,
		},
		{
			name:     "depends on agent as string",
			config:   map[string]any{"needs": "agent"},
			expected: true,
		},
		{
			name:     "depends on agent in array",
			config:   map[string]any{"needs": []any{"agent"}},
			expected: true,
		},
		{
			name:     "depends on agent and others",
			config:   map[string]any{"needs": []any{"activation", "agent"}},
			expected: true,
		},
		{
			name:     "depends on activation only",
			config:   map[string]any{"needs": []any{"activation"}},
			expected: false,
		},
		{
			name:     "depends on pre_activation only",
			config:   map[string]any{"needs": "pre_activation"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := jobDependsOnAgent(tt.config)
			if result != tt.expected {
				t.Errorf("jobDependsOnAgent(%v) = %v, expected %v", tt.config, result, tt.expected)
			}
		})
	}
}
