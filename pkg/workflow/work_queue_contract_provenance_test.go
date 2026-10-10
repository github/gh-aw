package workflow

import (
	"testing"

	"github.com/github/gh-aw/pkg/parser"
)

func TestWorkQueueParsedWorkerStampMatchesCompleteRegistrySource(t *testing.T) {
	path := "contract-worker.md"
	frontmatter := "on: workflow_dispatch\npermissions:\n  contents: read\ntools:\n  work-queue:\n    worker: true\nsafe-outputs:\n  create-issue:\n    max: 1"
	body := "Process assigned Claims."
	data := &WorkflowData{
		Tools:           map[string]any{"work-queue": map[string]any{"worker": true}},
		WorkQueuePolicy: &WorkQueuePolicyConfig{},
		FrontmatterYAML: frontmatter,
		RawMarkdown:     body,
	}
	expected, err := workQueueLogicalContractFromContent(path, "---\n"+frontmatter+"\n---\n"+body)
	if err != nil {
		t.Fatal(err)
	}
	if err := configureWorkQueueWorkerContract(data, path, parser.NewImportCache(".")); err != nil {
		t.Fatal(err)
	}
	if data.WorkQueuePolicy.WorkerContract != expected {
		t.Fatal("parsed body-only worker stamp omitted registry execution authority")
	}
	original := data.WorkQueuePolicy.WorkerContract
	data.RawMarkdown = "A compatible prompt replacement."
	if err := configureWorkQueueWorkerContract(data, path, parser.NewImportCache(".")); err != nil {
		t.Fatal(err)
	}
	if data.WorkQueuePolicy.WorkerContract != original {
		t.Fatal("prompt replacement changed the worker authority stamp")
	}
	data.FrontmatterYAML += "\nnetwork:\n  allowed:\n    - example.com"
	if err := configureWorkQueueWorkerContract(data, path, parser.NewImportCache(".")); err != nil {
		t.Fatal(err)
	}
	if data.WorkQueuePolicy.WorkerContract == original {
		t.Fatal("authority expansion retained the old worker contract")
	}
}
