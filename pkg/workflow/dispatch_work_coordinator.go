package workflow

import (
	"errors"
)

// DispatchWorkCoordinatorConfig enables the built-in durable Work queue.
type DispatchWorkCoordinatorConfig struct{}

func parseDispatchWorkCoordinatorConfig(raw any) (*DispatchWorkCoordinatorConfig, error) {
	if raw == nil {
		return &DispatchWorkCoordinatorConfig{}, nil
	}
	config, ok := raw.(map[string]any)
	if !ok {
		return nil, errors.New("tools.dispatch-work-coordinator must be an object")
	}
	if len(config) > 0 {
		return nil, errors.New("tools.dispatch-work-coordinator does not support properties")
	}
	return &DispatchWorkCoordinatorConfig{}, nil
}

func validateDispatchWorkCoordinatorPermissions(data *WorkflowData) error {
	permissions := NewPermissionsParser(data.Permissions).ToPermissions()
	if level, ok := permissions.Get(PermissionContents); !ok || level != PermissionWrite {
		return errors.New("tools.dispatch-work-coordinator requires contents: write permission")
	}
	if githubTool, ok := data.Tools["github"]; ok && githubTool != false && !isGitHubCLIModeEnabled(data) {
		githubConfig := parseGitHubTool(githubTool)
		if githubConfig == nil || !githubConfig.ReadOnly {
			return errors.New("tools.dispatch-work-coordinator requires tools.github.read-only: true to prevent direct writes outside coordinator reconciliation")
		}
	}
	return nil
}
