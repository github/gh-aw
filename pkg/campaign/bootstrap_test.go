//go:build !integration

package campaign

import (
	"encoding/json"
	"testing"

	"go.yaml.in/yaml/v3"
)

// TestBootstrapConfig_SeederWorker tests parsing bootstrap config with seeder-worker mode
func TestBootstrapConfig_SeederWorker(t *testing.T) {
	yamlContent := `---
id: test-bootstrap-seeder
name: Test Bootstrap Seeder
project-url: https://github.com/orgs/test/projects/1
bootstrap:
  mode: seeder-worker
  seeder-worker:
    workflow-id: security-scanner
    payload:
      scan-type: full
      max-findings: 100
    max-items: 50
---

# Test Campaign`

	var spec CampaignSpec
	err := yaml.Unmarshal([]byte(yamlContent), &spec)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap seeder config: %v", err)
	}

	if spec.Bootstrap == nil {
		t.Fatal("Bootstrap config should not be nil")
	}
	if spec.Bootstrap.Mode != "seeder-worker" {
		t.Errorf("Expected mode 'seeder-worker', got '%s'", spec.Bootstrap.Mode)
	}
	if spec.Bootstrap.SeederWorker == nil {
		t.Fatal("SeederWorker config should not be nil")
	}
	if spec.Bootstrap.SeederWorker.WorkflowID != "security-scanner" {
		t.Errorf("Expected workflow-id 'security-scanner', got '%s'", spec.Bootstrap.SeederWorker.WorkflowID)
	}
	if spec.Bootstrap.SeederWorker.MaxItems != 50 {
		t.Errorf("Expected max-items 50, got %d", spec.Bootstrap.SeederWorker.MaxItems)
	}
	if len(spec.Bootstrap.SeederWorker.Payload) == 0 {
		t.Error("Payload should not be empty")
	}
}

// TestBootstrapConfig_ProjectTodos tests parsing bootstrap config with project-todos mode
func TestBootstrapConfig_ProjectTodos(t *testing.T) {
	yamlContent := `---
id: test-bootstrap-todos
name: Test Bootstrap Todos
project-url: https://github.com/orgs/test/projects/1
bootstrap:
  mode: project-todos
  project-todos:
    status-field: Status
    todo-value: Backlog
    max-items: 10
    require-fields:
      - Priority
      - Assignee
---

# Test Campaign`

	var spec CampaignSpec
	err := yaml.Unmarshal([]byte(yamlContent), &spec)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap todos config: %v", err)
	}

	if spec.Bootstrap == nil {
		t.Fatal("Bootstrap config should not be nil")
	}
	if spec.Bootstrap.Mode != "project-todos" {
		t.Errorf("Expected mode 'project-todos', got '%s'", spec.Bootstrap.Mode)
	}
	if spec.Bootstrap.ProjectTodos == nil {
		t.Fatal("ProjectTodos config should not be nil")
	}
	if spec.Bootstrap.ProjectTodos.StatusField != "Status" {
		t.Errorf("Expected status-field 'Status', got '%s'", spec.Bootstrap.ProjectTodos.StatusField)
	}
	if spec.Bootstrap.ProjectTodos.TodoValue != "Backlog" {
		t.Errorf("Expected todo-value 'Backlog', got '%s'", spec.Bootstrap.ProjectTodos.TodoValue)
	}
	if spec.Bootstrap.ProjectTodos.MaxItems != 10 {
		t.Errorf("Expected max-items 10, got %d", spec.Bootstrap.ProjectTodos.MaxItems)
	}
	if len(spec.Bootstrap.ProjectTodos.RequireFields) != 2 {
		t.Errorf("Expected 2 require-fields, got %d", len(spec.Bootstrap.ProjectTodos.RequireFields))
	}
}

// TestBootstrapConfig_Manual tests parsing bootstrap config with manual mode
func TestBootstrapConfig_Manual(t *testing.T) {
	yamlContent := `---
id: test-bootstrap-manual
name: Test Bootstrap Manual
project-url: https://github.com/orgs/test/projects/1
bootstrap:
  mode: manual
---

# Test Campaign`

	var spec CampaignSpec
	err := yaml.Unmarshal([]byte(yamlContent), &spec)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap manual config: %v", err)
	}

	if spec.Bootstrap == nil {
		t.Fatal("Bootstrap config should not be nil")
	}
	if spec.Bootstrap.Mode != "manual" {
		t.Errorf("Expected mode 'manual', got '%s'", spec.Bootstrap.Mode)
	}
}

// TestWorkerMetadata_Basic tests parsing basic worker metadata
func TestWorkerMetadata_Basic(t *testing.T) {
	yamlContent := `---
id: test-workers
name: Test Workers
project-url: https://github.com/orgs/test/projects/1
workers:
  - id: security-fixer
    name: Security Fix Worker
    description: Fixes security vulnerabilities
    capabilities:
      - fix-security-alerts
      - create-pull-requests
    payload-schema:
      repository:
        type: string
        description: Target repository in owner/repo format
        required: true
        example: owner/repo
      work_item_id:
        type: string
        description: Unique work item identifier
        required: true
        example: alert-123
      severity:
        type: string
        description: Alert severity level
        required: false
        example: high
    output-labeling:
      labels:
        - security
        - automated
      key-in-title: true
      key-format: "campaign-{campaign_id}-{repository}-{work_item_id}"
      metadata-fields:
        - Campaign Id
        - Worker Workflow
    idempotency-strategy: pr-title-based
    priority: 10
---

# Test Campaign`

	var spec CampaignSpec
	err := yaml.Unmarshal([]byte(yamlContent), &spec)
	if err != nil {
		t.Fatalf("Failed to parse worker metadata: %v", err)
	}

	if len(spec.Workers) == 0 {
		t.Fatal("Workers should not be empty")
	}

	worker := spec.Workers[0]
	if worker.ID != "security-fixer" {
		t.Errorf("Expected worker ID 'security-fixer', got '%s'", worker.ID)
	}
	if worker.Name != "Security Fix Worker" {
		t.Errorf("Expected worker name 'Security Fix Worker', got '%s'", worker.Name)
	}
	if len(worker.Capabilities) != 2 {
		t.Errorf("Expected 2 capabilities, got %d", len(worker.Capabilities))
	}
	if len(worker.PayloadSchema) != 3 {
		t.Errorf("Expected 3 payload schema fields, got %d", len(worker.PayloadSchema))
	}

	// Check payload schema field
	repoField, exists := worker.PayloadSchema["repository"]
	if !exists {
		t.Error("Expected 'repository' field in payload schema")
	}
	if repoField.Type != "string" {
		t.Errorf("Expected repository type 'string', got '%s'", repoField.Type)
	}
	if !repoField.Required {
		t.Error("Expected repository field to be required")
	}

	// Check output labeling
	if !worker.OutputLabeling.KeyInTitle {
		t.Error("Expected key-in-title to be true")
	}
	if len(worker.OutputLabeling.Labels) != 2 {
		t.Errorf("Expected 2 labels, got %d", len(worker.OutputLabeling.Labels))
	}
	if len(worker.OutputLabeling.MetadataFields) != 2 {
		t.Errorf("Expected 2 metadata fields, got %d", len(worker.OutputLabeling.MetadataFields))
	}

	// Check idempotency strategy
	if worker.IdempotencyStrategy != "pr-title-based" {
		t.Errorf("Expected idempotency strategy 'pr-title-based', got '%s'", worker.IdempotencyStrategy)
	}

	// Check priority
	if worker.Priority != 10 {
		t.Errorf("Expected priority 10, got %d", worker.Priority)
	}
}

// TestWorkerMetadata_Multiple tests parsing multiple workers
func TestWorkerMetadata_Multiple(t *testing.T) {
	yamlContent := `---
id: test-multi-workers
name: Test Multiple Workers
project-url: https://github.com/orgs/test/projects/1
workers:
  - id: worker-one
    capabilities: [scan]
    payload-schema:
      target:
        type: string
        description: Target to scan
        required: true
    output-labeling:
      key-in-title: false
    idempotency-strategy: branch-based
  - id: worker-two
    capabilities: [fix]
    payload-schema:
      issue:
        type: number
        description: Issue number
        required: true
    output-labeling:
      key-in-title: true
      key-format: "fix-{issue}"
    idempotency-strategy: issue-title-based
    priority: 5
---

# Test Campaign`

	var spec CampaignSpec
	err := yaml.Unmarshal([]byte(yamlContent), &spec)
	if err != nil {
		t.Fatalf("Failed to parse multiple workers: %v", err)
	}

	if len(spec.Workers) != 2 {
		t.Fatalf("Expected 2 workers, got %d", len(spec.Workers))
	}

	// Check first worker
	if spec.Workers[0].ID != "worker-one" {
		t.Errorf("Expected first worker ID 'worker-one', got '%s'", spec.Workers[0].ID)
	}
	if spec.Workers[0].IdempotencyStrategy != "branch-based" {
		t.Errorf("Expected first worker idempotency 'branch-based', got '%s'", spec.Workers[0].IdempotencyStrategy)
	}

	// Check second worker
	if spec.Workers[1].ID != "worker-two" {
		t.Errorf("Expected second worker ID 'worker-two', got '%s'", spec.Workers[1].ID)
	}
	if spec.Workers[1].Priority != 5 {
		t.Errorf("Expected second worker priority 5, got %d", spec.Workers[1].Priority)
	}
}

// TestBootstrapAndWorkers_Combined tests campaign with both bootstrap and workers
func TestBootstrapAndWorkers_Combined(t *testing.T) {
	yamlContent := `---
id: test-combined
name: Test Combined
project-url: https://github.com/orgs/test/projects/1
bootstrap:
  mode: seeder-worker
  seeder-worker:
    workflow-id: scanner
    payload:
      mode: discovery
workers:
  - id: scanner
    capabilities: [scan]
    payload-schema:
      mode:
        type: string
        description: Scan mode
        required: true
    output-labeling:
      key-in-title: true
    idempotency-strategy: cursor-based
  - id: fixer
    capabilities: [fix]
    payload-schema:
      alert_id:
        type: string
        description: Alert ID
        required: true
    output-labeling:
      key-in-title: true
    idempotency-strategy: pr-title-based
    priority: 10
---

# Test Campaign`

	var spec CampaignSpec
	err := yaml.Unmarshal([]byte(yamlContent), &spec)
	if err != nil {
		t.Fatalf("Failed to parse combined config: %v", err)
	}

	// Verify bootstrap
	if spec.Bootstrap == nil {
		t.Fatal("Bootstrap should not be nil")
	}
	if spec.Bootstrap.Mode != "seeder-worker" {
		t.Errorf("Expected bootstrap mode 'seeder-worker', got '%s'", spec.Bootstrap.Mode)
	}
	if spec.Bootstrap.SeederWorker.WorkflowID != "scanner" {
		t.Errorf("Expected seeder workflow-id 'scanner', got '%s'", spec.Bootstrap.SeederWorker.WorkflowID)
	}

	// Verify workers
	if len(spec.Workers) != 2 {
		t.Fatalf("Expected 2 workers, got %d", len(spec.Workers))
	}

	// Check that seeder worker exists
	var scannerFound bool
	for _, worker := range spec.Workers {
		if worker.ID == "scanner" {
			scannerFound = true
			if len(worker.Capabilities) != 1 || worker.Capabilities[0] != "scan" {
				t.Error("Scanner worker should have 'scan' capability")
			}
		}
	}
	if !scannerFound {
		t.Error("Scanner worker not found in workers list")
	}
}

// TestBootstrapConfig_JSONSerialization tests JSON marshaling/unmarshaling
func TestBootstrapConfig_JSONSerialization(t *testing.T) {
	original := CampaignSpec{
		ID:         "test-json",
		Name:       "Test JSON",
		ProjectURL: "https://github.com/orgs/test/projects/1",
		Bootstrap: &CampaignBootstrapConfig{
			Mode: "seeder-worker",
			SeederWorker: &SeederWorkerConfig{
				WorkflowID: "scanner",
				Payload: map[string]any{
					"type": "full-scan",
					"max":  100,
				},
				MaxItems: 50,
			},
		},
		Workers: []WorkerMetadata{
			{
				ID:           "test-worker",
				Capabilities: []string{"scan"},
				PayloadSchema: map[string]WorkerPayloadField{
					"target": {
						Type:        "string",
						Description: "Target to scan",
						Required:    true,
					},
				},
				OutputLabeling: WorkerOutputLabeling{
					KeyInTitle: true,
				},
				IdempotencyStrategy: "branch-based",
			},
		},
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal to JSON: %v", err)
	}

	// Unmarshal from JSON
	var restored CampaignSpec
	err = json.Unmarshal(jsonData, &restored)
	if err != nil {
		t.Fatalf("Failed to unmarshal from JSON: %v", err)
	}

	// Verify fields
	if restored.Bootstrap == nil {
		t.Fatal("Restored bootstrap should not be nil")
	}
	if restored.Bootstrap.Mode != original.Bootstrap.Mode {
		t.Errorf("Bootstrap mode mismatch: got '%s', want '%s'", restored.Bootstrap.Mode, original.Bootstrap.Mode)
	}
	if len(restored.Workers) != len(original.Workers) {
		t.Errorf("Worker count mismatch: got %d, want %d", len(restored.Workers), len(original.Workers))
	}
}

// TestWorkerPayloadField_RequiredDefaultsFalse tests that Required defaults to false
func TestWorkerPayloadField_RequiredDefaultsFalse(t *testing.T) {
	yamlContent := `---
id: test-required
name: Test Required Default
project-url: https://github.com/orgs/test/projects/1
workers:
  - id: test
    capabilities: [test]
    payload-schema:
      optional_field:
        type: string
        description: Optional field without explicit required
    output-labeling:
      key-in-title: false
    idempotency-strategy: branch-based
---

# Test`

	var spec CampaignSpec
	err := yaml.Unmarshal([]byte(yamlContent), &spec)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	field := spec.Workers[0].PayloadSchema["optional_field"]
	if field.Required {
		t.Error("Expected Required to default to false")
	}
}
