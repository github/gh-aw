package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/typeutil"
)

var safeOutputsCallWorkflowLog = logger.New("workflow:safe_outputs_call_workflow")

// ========================================
// Safe Output Call Workflow Handling
// ========================================
//
// This file contains functions for managing call-workflow safe output
// configurations: mapping workflow names to their relative file paths so the
// compiler can generate the correct `uses:` declarations.

// populateCallWorkflowFiles resolves the relative file path for each call-workflow
// listed in SafeOutputsConfig.CallWorkflow.Workflows. The resolved path is stored
// in WorkflowFiles for use by the compiler when generating conditional `uses:` jobs.
//
// Priority order: .lock.yml > .yml > .md (same-batch compilation target → .lock.yml)
func populateCallWorkflowFiles(data *WorkflowData, markdownPath string) {
	if data.SafeOutputs == nil || data.SafeOutputs.CallWorkflow == nil {
		return
	}

	if len(data.SafeOutputs.CallWorkflow.Workflows) == 0 {
		return
	}

	callWorkflowLog.Printf("Populating workflow files for %d call workflows", len(data.SafeOutputs.CallWorkflow.Workflows))

	// Initialize WorkflowFiles map if not already initialized
	if data.SafeOutputs.CallWorkflow.WorkflowFiles == nil {
		data.SafeOutputs.CallWorkflow.WorkflowFiles = make(map[string]string)
	}

	for _, workflowName := range data.SafeOutputs.CallWorkflow.Workflows {
		fileResult, err := findWorkflowFile(workflowName, markdownPath)
		if err != nil {
			callWorkflowLog.Printf("Warning: error finding workflow %s: %v", workflowName, err)
			continue
		}

		// Determine which file to use - priority: .lock.yml > .yml > .md (batch target)
		extension, found := resolveWorkflowExtension(fileResult)
		if !found {
			callWorkflowLog.Printf("Warning: no workflow file found for %s (checked .lock.yml, .yml, .md)", workflowName)
			continue
		}

		// Store the relative path for runtime/compile-time use (e.g. ./.github/workflows/worker.lock.yml)
		relativePath := fmt.Sprintf("./.github/workflows/%s%s", workflowName, extension)
		data.SafeOutputs.CallWorkflow.WorkflowFiles[workflowName] = relativePath
		callWorkflowLog.Printf("Mapped workflow %s to path %s", workflowName, relativePath)
	}
}

// generateCallWorkflowTool generates an MCP tool definition for a specific call-workflow target.
// The tool will be named after the workflow (normalized to underscores) and accept
// the workflow's defined workflow_call inputs as parameters.
// The agent calls this tool to select which worker to activate; the handler writes
// call_workflow_name and call_workflow_payload outputs for the conditional `uses:` jobs.
func generateCallWorkflowTool(workflowName string, workflowInputs map[string]any) map[string]any {
	safeOutputsCallWorkflowLog.Printf("Generating call-workflow tool: workflow=%s, inputs=%d", workflowName, len(workflowInputs))
	tool := generateWorkflowToolDefinition(workflowToolDefinitionOptions{
		workflowName:      workflowName,
		workflowInputs:    workflowInputs,
		descriptionFormat: "Call the '%s' reusable workflow via workflow_call. This workflow must support workflow_call and be in .github/workflows/ directory in the same repository.",
		metadataKey:       "_call_workflow_name",
	})

	inputSchema, _ := typeutil.LookupMap(tool, "inputSchema")
	properties, _ := typeutil.LookupMap(inputSchema, "properties")
	requiredCount := 0
	if required, ok := inputSchema["required"].([]string); ok {
		requiredCount = len(required)
	}
	safeOutputsCallWorkflowLog.Printf("Generated call-workflow tool: name=%s, properties=%d, required=%d", tool["name"], len(properties), requiredCount)
	return tool
}
