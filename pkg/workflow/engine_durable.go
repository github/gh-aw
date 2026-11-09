package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"
)

type DurableConfig struct {
	RetentionDays int `json:"retention-days"`
}

const durableStateDirectory = "/tmp/gh-aw/agent/durable"
const defaultDurableRetentionDays = 7

type DurableExecutionPlan struct {
	Files             []string
	InstallationSteps []GitHubActionStep
	ExecutionSteps    []GitHubActionStep
}

func applyEngineDurableField(config *EngineConfig, options map[string]any) {
	raw, exists := options["durable"]
	if !exists {
		return
	}
	object, ok := raw.(map[string]any)
	if !ok {
		config.durableError = errors.New("engine.durable must be an object with optional retention-days (1-90)")
		return
	}
	for field, value := range object {
		if field != "retention-days" || value == nil {
			config.durableError = errors.New("engine.durable supports only retention-days (1-90); state is stored in a GitHub Actions artifact")
			return
		}
	}
	encoded, err := json.Marshal(object)
	if err != nil {
		config.durableError = fmt.Errorf("invalid engine.durable configuration: %w", err)
		return
	}
	durable := &DurableConfig{RetentionDays: defaultDurableRetentionDays}
	if err := json.Unmarshal(encoded, durable); err != nil {
		config.durableError = fmt.Errorf("engine.durable.retention-days must be an integer from 1 to 90: %w", err)
		return
	}
	if durable.RetentionDays < 1 || durable.RetentionDays > 90 {
		config.durableError = errors.New("engine.durable.retention-days must be an integer from 1 to 90")
		return
	}
	config.Durable = durable
}

func durableWorkflowID(data *WorkflowData) string {
	id := data.WorkflowID
	if id == "" {
		id = data.Name
	}
	return strings.ReplaceAll(stringutil.SanitizeForFilename(id), ".", "-")
}

func durableCheckpointName(data *WorkflowData) string {
	// Preserve the checkpoint path contract used by imported native engines.
	return "pi-durable-" + durableWorkflowID(data) + ".json"
}

func (c *Compiler) validateEngineDurable(data *WorkflowData) error {
	if data.EngineConfig == nil {
		return nil
	}
	if data.EngineConfig.durableError != nil {
		return data.EngineConfig.durableError
	}
	if data.EngineConfig.Durable == nil {
		return nil
	}
	if data.EngineConfig.Durable.RetentionDays < 1 || data.EngineConfig.Durable.RetentionDays > 90 {
		return errors.New("engine.durable.retention-days must be an integer from 1 to 90")
	}
	if data.Concurrency != "" || data.EngineConfig.Concurrency != "" {
		return errors.New("engine.durable manages workflow concurrency; remove explicit concurrency settings to serialize checkpoint publication")
	}
	if hasWorkflowCallTrigger(data.On) || data.WorkQueuePolicy != nil {
		return errors.New("engine.durable does not support workflow_call or work-queue workers; use a single workflow invocation")
	}
	engine, err := c.getAgenticEngine(data.AI)
	if err != nil {
		return err
	}
	plan, err := engine.GetDurableExecutionPlan(data, constants.AgentStdioLogPath)
	if err != nil {
		return err
	}
	if plan == nil || len(plan.ExecutionSteps) == 0 {
		return fmt.Errorf("engine %q returned an incomplete durable execution plan", engine.GetID())
	}
	return validateDurableFiles(plan.Files)
}

func validateDurableFiles(files []string) error {
	if len(files) == 0 {
		return errors.New("durable execution requires engine-declared session files or directories")
	}
	for i, file := range files {
		if !strings.HasPrefix(file, constants.TmpGhAwAgentDir) || path.Clean(file) != file ||
			strings.ContainsAny(file, "\n\r\x00*?[]") || strings.Contains(file, "${{") ||
			file == durableStateDirectory || strings.HasPrefix(file, durableStateDirectory+"/bundle") {
			return fmt.Errorf("durable file %q must be an explicit path under /tmp/gh-aw/agent/, outside the artifact bundle", file)
		}
		for _, other := range files[:i] {
			if file == other || strings.HasPrefix(file, other+"/") || strings.HasPrefix(other, file+"/") {
				return fmt.Errorf("durable files must not overlap: %q and %q", other, file)
			}
		}
	}
	return nil
}

func durableExecutionEnv(data *WorkflowData) (map[string]string, error) {
	if data.EngineConfig == nil || data.EngineConfig.Durable == nil {
		return nil, errors.New("engine.durable is required for durable execution")
	}

	return map[string]string{
		"GH_AW_DURABLE_STORAGE":       "artifact",
		"GH_AW_DURABLE_DIR":           durableStateDirectory,
		"GH_AW_DURABLE_CHECKPOINT":    path.Join(durableStateDirectory, durableCheckpointName(data)),
		"GH_AW_DURABLE_RESUMED":       "${{ steps.durable_checkpoint.outputs.restored == 'true' && 'true' || 'false' }}",
		"GH_AW_DURABLE_PARENT_RUN_ID": "${{ steps.durable_checkpoint.outputs.parent-run-id }}",
	}, nil
}

func applyDurableExecutionEnv(env map[string]string, data *WorkflowData) {
	if data.EngineConfig == nil || data.EngineConfig.Durable == nil {
		return
	}
	durableEnv, err := durableExecutionEnv(data)
	if err != nil {
		panic(fmt.Sprintf("BUG: unvalidated durable configuration: %v", err))
	}
	maps.Copy(env, durableEnv)
	applyDurableSessionEnv(env, data)
}
