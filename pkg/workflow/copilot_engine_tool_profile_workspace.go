package workflow

import (
	"errors"
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
)

func validateGoRepositoryEnvironment(data *WorkflowData) error {
	reserved := []string{"GH_AW_ENGINE_CWD", "GITHUB_WORKSPACE", "GITHUB_REPOSITORY", "RUNNER_TEMP"}
	envs := []map[string]string{data.EngineConfig.Env}
	if agent := getAgentConfig(data); agent != nil {
		envs = append(envs, agent.Env)
	}
	for _, env := range envs {
		for _, name := range reserved {
			if _, exists := env[name]; exists {
				return fmt.Errorf("engine.tool-profile: go-repository does not support overriding %s in engine or sandbox agent env", name)
			}
		}
	}
	if data.Env == "" {
		return nil
	}
	var section map[string]any
	if err := yaml.Unmarshal([]byte(data.Env), &section); err != nil {
		return fmt.Errorf("engine.tool-profile: go-repository could not validate workflow env: %w", err)
	}
	if env, ok := section["env"].(map[string]any); ok {
		section = env
	}
	for _, name := range reserved {
		if _, exists := section[name]; exists {
			return fmt.Errorf("engine.tool-profile: go-repository does not support overriding %s in workflow env", name)
		}
	}
	return nil
}

func validateGoRepositoryCheckoutSteps(data *WorkflowData) error {
	for _, raw := range []string{data.CustomSteps, data.PreSteps, data.PreAgentSteps} {
		if raw == "" {
			continue
		}
		var section any
		if err := yaml.Unmarshal([]byte(raw), &section); err != nil {
			return fmt.Errorf("engine.tool-profile: go-repository could not validate checkout steps: %w", err)
		}
		if containsGoRepositoryCheckoutAction(section) {
			return errors.New("engine.tool-profile: go-repository requires the compiler-managed checkout; actions/checkout in steps, pre-steps, or pre-agent steps is not supported")
		}
	}
	return nil
}

func containsGoRepositoryCheckoutAction(value any) bool {
	switch value := value.(type) {
	case map[string]any:
		for _, child := range value {
			if steps, ok := child.([]any); ok && containsGoRepositoryCheckoutAction(steps) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			step, ok := child.(map[string]any)
			if !ok {
				continue
			}
			if action, ok := step["uses"].(string); ok && strings.HasPrefix(strings.ToLower(action), "actions/checkout@") {
				return true
			}
		}
	}
	return false
}
