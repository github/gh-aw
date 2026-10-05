package workflow

import (
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var safeOutputsDispatchWorkflowLog = logger.New("workflow:safe_outputs_dispatch")

// ========================================
// Safe Output Dispatch Workflow Handling
// ========================================
//
// This file contains functions for managing dispatch-workflow safe output
// configurations: mapping workflow names to their file extensions so the
// runtime handler knows which file to use when dispatching a workflow.

// populateDispatchWorkflowFiles resolves the file extension for each dispatch
// workflow listed in SafeOutputsConfig.DispatchWorkflow.Workflows. The resolved
// extension is stored in WorkflowFiles for later use by the runtime handler.
// It also detects which workflows accept compiler-managed aw_context and work_queue_claim
// inputs and which additionally enable the work-queue protocol, so runtime dispatch can
// enforce each worker's contract.
//
// Priority order: .lock.yml > .yml > .md (same-batch compilation target)
func populateDispatchWorkflowFiles(data *WorkflowData, markdownPath string) {
	if data.SafeOutputs == nil || data.SafeOutputs.DispatchWorkflow == nil {
		return
	}
	data.SafeOutputs.WorkQueueEnabled = isWorkQueueEnabled(data)

	if len(data.SafeOutputs.DispatchWorkflow.Workflows) == 0 {
		return
	}

	safeOutputsConfigLog.Printf("Populating workflow files for %d dispatch workflows", len(data.SafeOutputs.DispatchWorkflow.Workflows))

	// Initialize WorkflowFiles map if not already initialized
	if data.SafeOutputs.DispatchWorkflow.WorkflowFiles == nil {
		data.SafeOutputs.DispatchWorkflow.WorkflowFiles = make(map[string]string)
	}

	for _, workflowName := range data.SafeOutputs.DispatchWorkflow.Workflows {
		// Find the workflow file
		fileResult, err := findWorkflowFile(workflowName, markdownPath)
		if err != nil {
			safeOutputsConfigLog.Printf("Warning: error finding workflow %s: %v", workflowName, err)
			continue
		}

		// Determine which file to use - priority: .lock.yml > .yml > .md (batch target)
		extension, found := resolveWorkflowExtension(fileResult)
		if !found {
			safeOutputsConfigLog.Printf("Warning: no workflow file found for %s (checked .lock.yml, .yml, .md)", workflowName)
			continue
		}

		// Store the file extension for runtime use
		data.SafeOutputs.DispatchWorkflow.WorkflowFiles[workflowName] = extension
		safeOutputsConfigLog.Printf("Mapped workflow %s to extension %s", workflowName, extension)

		// Compiler-generated workflows accept caller context and queue claims through
		// reserved inputs. External YAML workflows must declare their supported inputs.
		if workflowHasAwContextInput(fileResult, workflowName) {
			data.SafeOutputs.DispatchWorkflow.AwContextWorkflows = append(
				data.SafeOutputs.DispatchWorkflow.AwContextWorkflows, workflowName,
			)
			safeOutputsConfigLog.Printf("Workflow %s accepts aw_context input", workflowName)
			if data.SafeOutputs.WorkQueueEnabled &&
				workflowHasWorkQueueClaimInput(fileResult, workflowName) &&
				workflowHasWorkQueueTools(fileResult, workflowName) &&
				workflowHasWorkQueueWorker(fileResult, workflowName) {
				data.SafeOutputs.DispatchWorkflow.WorkQueueWorkflows = append(
					data.SafeOutputs.DispatchWorkflow.WorkQueueWorkflows, workflowName,
				)
				safeOutputsConfigLog.Printf("Workflow %s enables the work-queue protocol", workflowName)
			}
		}
	}
}

// workflowHasWorkQueueTools reports whether an agentic workflow source enables the queue protocol.
func workflowHasWorkQueueTools(fileResult *findWorkflowFileResult, workflowName string) bool {
	if !fileResult.mdExists {
		return false
	}
	enabled, err := mdHasWorkQueueTools(fileResult.mdPath)
	if err != nil {
		safeOutputsConfigLog.Printf("Warning: error checking work-queue tools for %s: %v", workflowName, err)
		return false
	}
	if enabled && fileResult.lockExists {
		enabled, err = lockHasWorkQueueProtocol(fileResult.lockPath)
		if err != nil {
			safeOutputsConfigLog.Printf("Warning: error checking compiled work-queue support for %s: %v", workflowName, err)
			return false
		}
	}
	return enabled
}

func workflowHasWorkQueueWorker(fileResult *findWorkflowFileResult, workflowName string) bool {
	if !fileResult.mdExists {
		return false
	}
	worker, err := mdHasWorkQueueWorker(fileResult.mdPath)
	if err != nil {
		safeOutputsConfigLog.Printf("Warning: error checking work-queue worker opt-in for %s: %v", workflowName, err)
		return false
	}
	return worker
}

// workflowHasAwContextInput reports whether the target accepts compiler-managed
// caller context or declares an aw_context input in an external workflow file.
func workflowHasAwContextInput(fileResult *findWorkflowFileResult, workflowName string) bool {
	var inputs map[string]any
	var err error

	if fileResult.lockExists {
		inputs, err = extractWorkflowDispatchInputs(fileResult.lockPath)
	} else if fileResult.ymlExists {
		inputs, err = extractWorkflowDispatchInputs(fileResult.ymlPath)
	} else if fileResult.mdExists {
		hasDispatch, dispatchErr := mdHasWorkflowDispatch(fileResult.mdPath)
		if dispatchErr != nil || !hasDispatch {
			return false
		}
		// The compiler injects aw_context into every workflow_dispatch source.
		return true
	} else {
		return false
	}
	if err != nil {
		safeOutputsConfigLog.Printf("Warning: error extracting inputs for %s: %v", workflowName, err)
		return false
	}
	_, hasAwContext := inputs["aw_context"]
	return hasAwContext
}

func workflowHasWorkQueueClaimInput(fileResult *findWorkflowFileResult, workflowName string) bool {
	var inputs map[string]any
	var err error

	if fileResult.lockExists {
		inputs, err = extractWorkflowDispatchInputs(fileResult.lockPath)
	} else if fileResult.ymlExists {
		inputs, err = extractWorkflowDispatchInputs(fileResult.ymlPath)
	} else if fileResult.mdExists {
		hasDispatch, dispatchErr := mdHasWorkflowDispatch(fileResult.mdPath)
		if dispatchErr != nil || !hasDispatch {
			return false
		}
		worker, workerErr := mdHasWorkQueueWorker(fileResult.mdPath)
		return workerErr == nil && worker
	} else {
		return false
	}
	if err != nil {
		safeOutputsConfigLog.Printf("Warning: error extracting queue claim input for %s: %v", workflowName, err)
		return false
	}
	_, hasClaimInput := inputs[WorkQueueClaimInputName]
	return hasClaimInput
}

// generateDispatchWorkflowTool generates an MCP tool definition for a specific workflow.
// The tool will be named after the workflow (normalized to underscores) and accept
// the workflow's defined workflow_dispatch inputs as parameters.
// When allowedRefs is non-empty, a 'ref' parameter is added to let the agent
// specify which branch/tag/SHA to dispatch to, validated against the configured globs.
func generateDispatchWorkflowTool(workflowName string, workflowInputs map[string]any, allowedRefs []string, workQueueEnabled bool) map[string]any {
	safeOutputsDispatchWorkflowLog.Printf("Generating dispatch-workflow tool: workflow=%s, inputs=%d, allowedRefs=%d", workflowName, len(workflowInputs), len(allowedRefs))

	descriptionFormat := "Dispatch the '%s' workflow with workflow_dispatch trigger. This workflow must support workflow_dispatch and be in .github/workflows/ directory in the same repository."

	tool := generateWorkflowToolDefinition(workflowToolDefinitionOptions{
		workflowName:      workflowName,
		workflowInputs:    workflowInputs,
		descriptionFormat: descriptionFormat,
		metadataKey:       "_workflow_name",
	})
	inputSchema, ok := tool["inputSchema"].(map[string]any)
	if !ok {
		return tool
	}
	properties, ok := inputSchema["properties"].(map[string]any)
	if !ok {
		return tool
	}
	// These values are compiler-managed and must never be agent-selected inputs.
	delete(properties, AwContextInputName)
	delete(properties, WorkQueueClaimInputName)
	if required, ok := inputSchema["required"].([]string); ok {
		inputSchema["required"] = slices.DeleteFunc(required, func(name string) bool {
			return name == AwContextInputName || name == WorkQueueClaimInputName
		})
	}
	addWorkQueueDispatchProperty(properties, workflowInputs, workQueueEnabled)

	// When allowed-refs is configured, inject a 'ref' property so the agent can
	// specify the target branch/tag/SHA. The runtime handler validates the value
	// against the configured glob patterns before dispatching.
	if len(allowedRefs) > 0 {
		allowedRefsDesc := strings.Join(allowedRefs, ", ")

		refDesc := fmt.Sprintf("The git ref (branch, tag, or SHA) to dispatch the workflow on. Must match one of the configured allowed ref patterns: %s. If omitted, the ref is resolved from the triggering context, including the pull request head for pull request comments.", allowedRefsDesc)
		properties["ref"] = map[string]any{
			"type":        "string",
			"description": refDesc,
		}

		if desc, ok := tool["description"].(string); ok {
			tool["description"] = desc + fmt.Sprintf(" Use the 'ref' parameter to target a specific branch or tag (allowed patterns: %s).", allowedRefsDesc)
		}
	}

	requiredCount := 0
	if required, ok := inputSchema["required"].([]string); ok {
		requiredCount = len(required)
	}
	safeOutputsDispatchWorkflowLog.Printf("Generated dispatch-workflow tool: name=%s, properties=%d, required=%d", tool["name"], len(properties), requiredCount)
	return tool
}

func addWorkQueueDispatchProperty(properties, workflowInputs map[string]any, workQueueEnabled bool) {
	if _, acceptsQueueClaim := workflowInputs[WorkQueueClaimInputName]; !workQueueEnabled || !acceptsQueueClaim {
		return
	}
	properties["work_queue"] = map[string]any{
		"type":        "object",
		"description": "Optional queue Work to claim before dispatching this worker. Use a work_id returned by work_queue_read.",
		"properties": map[string]any{
			"work_id": map[string]any{"type": "string", "minLength": 1},
		},
		"required":             []string{"work_id"},
		"additionalProperties": false,
	}
}
