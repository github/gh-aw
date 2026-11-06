package workflow

import (
	"fmt"
	"maps"
	"regexp"
)

var durableDependencyVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?$`)

func (e *BehaviorDefinedEngine) GetDurableExecutionPlan(data *WorkflowData, logFile string) (*DurableExecutionPlan, error) {
	behavior := e.behavior()
	if behavior == nil || behavior.Durable == nil {
		return e.BaseEngine.GetDurableExecutionPlan(data, logFile)
	}
	durable := behavior.Durable
	if durable.Execution == nil || durable.Execution.CommandName == "" {
		return nil, fmt.Errorf("engine %q behaviors.durable requires execution.command-name and session files", e.GetID())
	}
	if durable.Execution.ProtectedSessionCapture != e.protectedSessionCaptureEnabled() {
		return nil, fmt.Errorf("engine %q behaviors.durable.execution.protected-session-capture must match behaviors.execution.protected-session-capture so capture and publication use the same source", e.GetID())
	}
	if data.EngineConfig.Command != "" || data.EngineConfig.Driver != "" || data.EngineConfig.HarnessScript != "" {
		return nil, fmt.Errorf("engine %q durable execution cannot be overridden by command, driver or harness", e.GetID())
	}
	env, err := durableExecutionEnv(data)
	if err != nil {
		return nil, err
	}
	definition := deepCopyEngineDefinition(*e.definition)
	definition.Behaviors.Execution = definition.Behaviors.Durable.Execution
	definition.Behaviors.HarnessScript = durable.HarnessScript
	definition.Behaviors.Durable = nil
	if definition.Behaviors.Execution.Env == nil {
		definition.Behaviors.Execution.Env = make(map[string]string)
	}
	maps.Copy(definition.Behaviors.Execution.Env, env)
	engine, err := NewBehaviorDefinedEngine(&definition)
	if err != nil {
		return nil, err
	}
	files, err := resolveDurableFilePaths(durable.Files, env["GH_AW_DURABLE_CHECKPOINT"])
	if err != nil {
		return nil, fmt.Errorf("engine %q behaviors.durable.files: %w", e.GetID(), err)
	}
	plan := &DurableExecutionPlan{Files: files, ExecutionSteps: engine.GetExecutionSteps(data, logFile)}
	for _, dependency := range durable.Installation {
		if dependency.PackageManager != "npm" || !durableDependencyVersion.MatchString(dependency.Version) ||
			dependency.PackageName == "" || dependency.VerifyCommand == "" {
			return nil, fmt.Errorf("engine %q behaviors.durable.installation requires npm dependencies with a literal pinned version, package-name and verify-command", e.GetID())
		}
		if err := validateNpmPackageName(dependency.PackageName); err != nil {
			return nil, fmt.Errorf("engine %q durable dependency: %w", e.GetID(), err)
		}
		depData := *data
		depConfig := *data.EngineConfig
		depConfig.Version = dependency.Version
		depData.EngineConfig = &depConfig
		depDefinition := deepCopyEngineDefinition(definition)
		depDefinition.Behaviors.Installation = &dependency
		depEngine, err := NewBehaviorDefinedEngine(&depDefinition)
		if err != nil {
			return nil, err
		}
		plan.InstallationSteps = append(plan.InstallationSteps, depEngine.GetInstallationSteps(&depData)...)
	}
	return plan, nil
}

func resolveDurableFilePaths(declared []string, checkpoint string) ([]string, error) {
	files := make([]string, 0, len(declared))
	for _, file := range declared {
		if file == "$GH_AW_DURABLE_CHECKPOINT" {
			file = checkpoint
		}
		files = append(files, file)
	}
	return files, validateDurableFiles(files)
}
