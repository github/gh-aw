//go:build !integration

package workflow

import (
	"testing"
)

func TestApplyRuntimeOverrides(t *testing.T) {
	tests := []struct {
		name         string
		runtimes     map[string]any
		requirements map[string]*RuntimeRequirement
		expected     map[string]string // map of runtime ID -> expected version
	}{
		{
			name: "override existing runtime version with string",
			runtimes: map[string]any{
				"node": map[string]any{
					"version": "22",
				},
			},
			requirements: map[string]*RuntimeRequirement{
				"node": {
					Runtime: &Runtime{
						ID:             "node",
						DefaultVersion: "20",
					},
					Version: "20",
				},
			},
			expected: map[string]string{
				"node": "22",
			},
		},
		{
			name: "override existing runtime version with number",
			runtimes: map[string]any{
				"node": map[string]any{
					"version": 22,
				},
			},
			requirements: map[string]*RuntimeRequirement{
				"node": {
					Runtime: &Runtime{
						ID:             "node",
						DefaultVersion: "20",
					},
					Version: "20",
				},
			},
			expected: map[string]string{
				"node": "22",
			},
		},
		{
			name: "override existing runtime version with float",
			runtimes: map[string]any{
				"python": map[string]any{
					"version": 3.12,
				},
			},
			requirements: map[string]*RuntimeRequirement{
				"python": {
					Runtime: &Runtime{
						ID:             "python",
						DefaultVersion: "3.11",
					},
					Version: "3.11",
				},
			},
			expected: map[string]string{
				"python": "3.12",
			},
		},
		{
			name: "add new runtime from override",
			runtimes: map[string]any{
				"ruby": map[string]any{
					"version": "3.2",
				},
			},
			requirements: map[string]*RuntimeRequirement{},
			expected: map[string]string{
				"ruby": "3.2",
			},
		},
		{
			name: "multiple runtime overrides",
			runtimes: map[string]any{
				"node": map[string]any{
					"version": "22",
				},
				"python": map[string]any{
					"version": "3.12",
				},
			},
			requirements: map[string]*RuntimeRequirement{
				"node": {
					Runtime: &Runtime{
						ID:             "node",
						DefaultVersion: "20",
					},
					Version: "20",
				},
			},
			expected: map[string]string{
				"node":   "22",
				"python": "3.12",
			},
		},
		{
			name: "ignore unknown runtime",
			runtimes: map[string]any{
				"unknown-runtime": map[string]any{
					"version": "1.0",
				},
			},
			requirements: map[string]*RuntimeRequirement{},
			expected:     map[string]string{},
		},
		{
			name: "ignore runtime without version",
			runtimes: map[string]any{
				"node": map[string]any{
					"other-field": "value",
				},
			},
			requirements: map[string]*RuntimeRequirement{
				"node": {
					Runtime: &Runtime{
						ID:             "node",
						DefaultVersion: "20",
					},
					Version: "20",
				},
			},
			expected: map[string]string{
				"node": "20",
			},
		},
		{
			name: "override action-repo",
			runtimes: map[string]any{
				"node": map[string]any{
					"version":        "22",
					"action-repo":    "custom/setup-node",
					"action-version": "v5",
				},
			},
			requirements: map[string]*RuntimeRequirement{
				"node": {
					Runtime: &Runtime{
						ID:            "node",
						ActionRepo:    "actions/setup-node",
						ActionVersion: "v4",
					},
					Version: "20",
				},
			},
			expected: map[string]string{
				"node": "22",
			},
		},
		{
			name: "override only action-version",
			runtimes: map[string]any{
				"python": map[string]any{
					"action-version": "v6",
				},
			},
			requirements: map[string]*RuntimeRequirement{
				"python": {
					Runtime: &Runtime{
						ID:            "python",
						ActionRepo:    "actions/setup-python",
						ActionVersion: "v5",
					},
					Version: "3.11",
				},
			},
			expected: map[string]string{
				"python": "3.11",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Apply overrides
			applyRuntimeOverrides(tt.runtimes, tt.requirements)

			// Verify results
			if len(tt.requirements) != len(tt.expected) {
				t.Errorf("Expected %d requirements, got %d", len(tt.expected), len(tt.requirements))
			}

			for id, expectedVersion := range tt.expected {
				req, exists := tt.requirements[id]
				if !exists {
					t.Errorf("Expected requirement for %s, but not found", id)
					continue
				}
				if req.Version != expectedVersion {
					t.Errorf("Expected version %s for %s, got %s", expectedVersion, id, req.Version)
				}
			}

			// Additional checks for action-repo and action-version overrides
			if tt.name == "override action-repo" {
				req := tt.requirements["node"]
				if req.Runtime.ActionRepo != "custom/setup-node" {
					t.Errorf("Expected ActionRepo 'custom/setup-node', got '%s'", req.Runtime.ActionRepo)
				}
				if req.Runtime.ActionVersion != "v5" {
					t.Errorf("Expected ActionVersion 'v5', got '%s'", req.Runtime.ActionVersion)
				}
			}

			if tt.name == "override only action-version" {
				req := tt.requirements["python"]
				if req.Runtime.ActionVersion != "v6" {
					t.Errorf("Expected ActionVersion 'v6', got '%s'", req.Runtime.ActionVersion)
				}
				if req.Runtime.ActionRepo != "actions/setup-python" {
					t.Errorf("Expected ActionRepo to remain 'actions/setup-python', got '%s'", req.Runtime.ActionRepo)
				}
			}
		})
	}
}

func TestDetectRuntimeRequirementsWithOverrides(t *testing.T) {
	tests := []struct {
		name     string
		workflow *WorkflowData
		expected map[string]string // map of runtime ID -> expected version
	}{
		{
			name: "detect and override node version",
			workflow: &WorkflowData{
				CustomSteps: `
- name: Test
  run: npm install
`,
				Runtimes: map[string]any{
					"node": map[string]any{
						"version": "22",
					},
				},
			},
			expected: map[string]string{
				"node": "22",
			},
		},
		{
			name: "detect python and add ruby from override",
			workflow: &WorkflowData{
				CustomSteps: `
- name: Test
  run: python test.py
`,
				Runtimes: map[string]any{
					"python": map[string]any{
						"version": "3.12",
					},
					"ruby": map[string]any{
						"version": "3.2",
					},
				},
			},
			expected: map[string]string{
				"python": "3.12",
				"ruby":   "3.2",
			},
		},
		{
			name: "no overrides",
			workflow: &WorkflowData{
				CustomSteps: `
- name: Test
  run: npm install
`,
			},
			expected: map[string]string{
				"node": "", // uses default
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requirements := DetectRuntimeRequirements(tt.workflow)

			// Convert to map for easier comparison
			resultMap := make(map[string]string)
			for _, req := range requirements {
				resultMap[req.Runtime.ID] = req.Version
			}

			if len(resultMap) != len(tt.expected) {
				t.Errorf("Expected %d requirements, got %d", len(tt.expected), len(resultMap))
			}

			for id, expectedVersion := range tt.expected {
				actualVersion, exists := resultMap[id]
				if !exists {
					t.Errorf("Expected requirement for %s, but not found", id)
					continue
				}
				if actualVersion != expectedVersion {
					t.Errorf("Expected version %s for %s, got %s", expectedVersion, id, actualVersion)
				}
			}
		})
	}
}
