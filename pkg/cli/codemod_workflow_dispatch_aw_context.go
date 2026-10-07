package cli

import "github.com/github/gh-aw/pkg/logger"

var workflowDispatchAwContextCodemodLog = logger.New("cli:codemod_workflow_dispatch_aw_context")

func getWorkflowDispatchAwContextRemovalCodemod() Codemod {
	return Codemod{
		ID:           "workflow-dispatch-aw-context-removal",
		Name:         "Remove reserved workflow_dispatch aw_context input",
		Description:  "Removes on.workflow_dispatch.inputs.aw_context, which is reserved and managed by the compiler.",
		IntroducedIn: "1.0.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			on, ok := frontmatter["on"].(map[string]any)
			if !ok {
				return content, false, nil
			}
			dispatch, ok := on["workflow_dispatch"].(map[string]any)
			if !ok {
				return content, false, nil
			}
			inputs, ok := dispatch["inputs"].(map[string]any)
			if !ok {
				return content, false, nil
			}
			if _, exists := inputs["aw_context"]; !exists {
				return content, false, nil
			}

			newContent, applied, err := removeYAMLMappingPath(content, []string{"on", "workflow_dispatch", "inputs", "aw_context"}, true)
			if applied {
				workflowDispatchAwContextCodemodLog.Print("Removed reserved on.workflow_dispatch.inputs.aw_context")
			}
			return newContent, applied, err
		},
	}
}
