//go:build !integration

package workflow

import (
	"maps"
	"reflect"
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
						DefaultVersion: "24",
					},
					Version: "24",
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
						DefaultVersion: "24",
					},
					Version: "24",
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
			name: "override existing runtime version with whole-number float",
			runtimes: map[string]any{
				"node": map[string]any{
					"version": float64(24),
				},
			},
			requirements: map[string]*RuntimeRequirement{
				"node": {
					Runtime: &Runtime{
						ID:             "node",
						DefaultVersion: "24",
					},
					Version: "24",
				},
			},
			expected: map[string]string{
				"node": "24",
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
						DefaultVersion: "24",
					},
					Version: "24",
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
						DefaultVersion: "24",
					},
					Version: "24",
				},
			},
			expected: map[string]string{
				"node": "24",
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
					Version: "24",
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

func TestApplyRuntimeOverrides_KnownRuntimeActionOverrides(t *testing.T) {
	var knownNode *Runtime
	for _, runtime := range knownRuntimes {
		if runtime.ID == "node" {
			knownNode = runtime
			break
		}
	}
	if knownNode == nil {
		t.Fatal("expected known node runtime to exist")
	}
	originalKnownNode := *knownNode
	originalKnownNode.Commands = append([]string(nil), knownNode.Commands...)
	originalKnownNode.ManifestFiles = append([]string(nil), knownNode.ManifestFiles...)
	originalKnownNode.ExtraWithFields = maps.Clone(knownNode.ExtraWithFields)

	requirements := map[string]*RuntimeRequirement{}

	applyRuntimeOverrides(map[string]any{
		"node": map[string]any{
			"version":        "22",
			"action-repo":    "custom/setup-node",
			"action-version": "v5",
		},
	}, requirements)

	nodeReq, ok := requirements["node"]
	if !ok {
		t.Fatal("expected node requirement to be added")
	}
	if nodeReq.Version != "22" {
		t.Fatalf("expected version 22, got %s", nodeReq.Version)
	}
	if nodeReq.Runtime == knownNode {
		t.Fatal("expected action overrides to clone the known runtime")
	}
	if nodeReq.Runtime.ActionRepo != "custom/setup-node" {
		t.Fatalf("expected ActionRepo custom/setup-node, got %s", nodeReq.Runtime.ActionRepo)
	}
	if nodeReq.Runtime.ActionVersion != "v5" {
		t.Fatalf("expected ActionVersion v5, got %s", nodeReq.Runtime.ActionVersion)
	}
	if knownNode.ActionRepo != "actions/setup-node" {
		t.Fatalf("expected known runtime ActionRepo to remain actions/setup-node, got %s", knownNode.ActionRepo)
	}
	if knownNode.ActionVersion != "v6" {
		t.Fatalf("expected known runtime ActionVersion to remain v6, got %s", knownNode.ActionVersion)
	}
	if !reflect.DeepEqual(*knownNode, originalKnownNode) {
		t.Fatal("expected all known runtime fields to remain unchanged")
	}
}

func TestApplyRuntimeOverrides_KnownRuntimeActionRepoOnly(t *testing.T) {
	var knownNode *Runtime
	for _, runtime := range knownRuntimes {
		if runtime.ID == "node" {
			knownNode = runtime
			break
		}
	}
	if knownNode == nil {
		t.Fatal("expected known node runtime to exist")
	}

	originalKnownNode := *knownNode
	originalKnownNode.Commands = append([]string(nil), knownNode.Commands...)
	originalKnownNode.ManifestFiles = append([]string(nil), knownNode.ManifestFiles...)
	originalKnownNode.ExtraWithFields = maps.Clone(knownNode.ExtraWithFields)

	requirements := map[string]*RuntimeRequirement{}
	applyRuntimeOverrides(map[string]any{
		"node": map[string]any{
			"action-repo": "custom/setup-node",
		},
	}, requirements)

	nodeReq, ok := requirements["node"]
	if !ok {
		t.Fatal("expected node requirement to be added")
	}
	if nodeReq.Runtime == knownNode {
		t.Fatal("expected action overrides to clone the known runtime")
	}
	if nodeReq.Runtime.ActionRepo != "custom/setup-node" {
		t.Fatalf("expected ActionRepo custom/setup-node, got %s", nodeReq.Runtime.ActionRepo)
	}
	if nodeReq.Runtime.ActionVersion != knownNode.ActionVersion {
		t.Fatalf("expected ActionVersion %s, got %s", knownNode.ActionVersion, nodeReq.Runtime.ActionVersion)
	}
	if !reflect.DeepEqual(*knownNode, originalKnownNode) {
		t.Fatal("expected all known runtime fields to remain unchanged")
	}
}

func TestApplyRuntimeOverrides_ExistingRequirementActionRepoOnlyClonesRuntime(t *testing.T) {
	existingRuntime := &Runtime{
		ID:              "node",
		ActionRepo:      "actions/setup-node",
		ActionVersion:   "v4",
		Commands:        []string{"node --version"},
		ManifestFiles:   []string{"package.json"},
		ExtraWithFields: map[string]string{"cache": "npm"},
	}
	originalExistingRuntime := *existingRuntime
	originalExistingRuntime.Commands = append([]string(nil), existingRuntime.Commands...)
	originalExistingRuntime.ManifestFiles = append([]string(nil), existingRuntime.ManifestFiles...)
	originalExistingRuntime.ExtraWithFields = maps.Clone(existingRuntime.ExtraWithFields)

	requirements := map[string]*RuntimeRequirement{
		"node": {
			Runtime: existingRuntime,
			Version: "24",
		},
	}
	applyRuntimeOverrides(map[string]any{
		"node": map[string]any{
			"action-repo": "custom/setup-node",
		},
	}, requirements)

	nodeReq := requirements["node"]
	if nodeReq.Runtime == existingRuntime {
		t.Fatal("expected existing runtime to be cloned when applying action overrides")
	}
	if nodeReq.Runtime.ActionRepo != "custom/setup-node" {
		t.Fatalf("expected ActionRepo custom/setup-node, got %s", nodeReq.Runtime.ActionRepo)
	}
	if nodeReq.Runtime.ActionVersion != "v4" {
		t.Fatalf("expected ActionVersion v4, got %s", nodeReq.Runtime.ActionVersion)
	}
	if !reflect.DeepEqual(*existingRuntime, originalExistingRuntime) {
		t.Fatal("expected existing runtime fields to remain unchanged")
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

func TestApplyRuntimeOverrides_Cooldown(t *testing.T) {
	requirements := map[string]*RuntimeRequirement{
		"node": {
			Runtime: &Runtime{
				ID: "node",
			},
			Version:  "24",
			Cooldown: true,
		},
	}

	applyRuntimeOverrides(map[string]any{
		"node": map[string]any{
			"cooldown": false,
		},
	}, requirements)

	nodeReq, ok := requirements["node"]
	if !ok {
		t.Fatal("expected node requirement to exist")
	}
	if nodeReq.Cooldown {
		t.Fatal("expected node cooldown to be disabled by override")
	}
}
