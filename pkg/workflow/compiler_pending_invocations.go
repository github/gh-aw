package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
)

type pendingWriteInvocation struct {
	Module          string            `json:"module"`
	Env             map[string]string `json:"env"`
	InputBindingIDs []string          `json:"input_binding_ids"`
	PathEnv         map[string]string `json:"path_env"`
}

func pendingNativeModule(job string) string {
	switch job {
	case pushRepoMemoryJobName:
		return "push_repo_memory.cjs"
	case pushLedgerChangesJobName:
		return "push_ledger_changes.cjs"
	case pushEvalsStateJobName:
		return "push_evals_state.cjs"
	case "upload_assets":
		return "upload_assets.cjs"
	default:
		return ""
	}
}

func pendingCredentialEnv(name string) bool {
	upper := strings.ToUpper(name)
	return strings.Contains(upper, "TOKEN") || strings.Contains(upper, "SECRET") || strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "PRIVATE_KEY")
}

func pendingStepString(step map[string]any, key string) string {
	value, ok := step[key].(string)
	if !ok {
		return ""
	}
	return value
}

func pendingStepEnv(step map[string]any) map[string]any {
	env, ok := step["env"].(map[string]any)
	if !ok {
		return nil
	}
	return env
}

// Reuse the emitted native step environment rather than reassembling defaults.
func (c *Compiler) freezePendingInvocations(spec *pendingWriteSpec) error { //nolint:largefunc // Capture and rewrite use the same emitted-step view to avoid widening native invocation defaults.
	for index := range spec.Publications {
		publication := &spec.Publications[index]
		module := pendingNativeModule(publication.Job)
		job, exists := c.jobManager.GetJob(publication.Job)
		if module == "" || !exists {
			continue
		}
		var parsed struct {
			Steps []map[string]any `yaml:"steps"`
		}
		if err := yaml.Unmarshal([]byte("steps:\n"+strings.Join(job.Steps, "")), &parsed); err != nil {
			return fmt.Errorf("reading native invocation %s: %w", publication.Job, err)
		}
		for _, step := range parsed.Steps {
			candidate, writes := pendingPublicationForStep(spec.Publications, step, publication.Job)
			if !writes || candidate.ID != publication.ID {
				continue
			}
			env := map[string]string{}
			for key, value := range job.Env {
				if !pendingCredentialEnv(key) && !strings.HasPrefix(key, "GH_AW_PENDING_") && !strings.HasPrefix(key, "GH_AW_WRITE_GATE_") {
					env[key] = value
				}
			}
			if values, ok := step["env"].(map[string]any); ok {
				for key, value := range values {
					if !pendingCredentialEnv(key) {
						env[key] = fmt.Sprint(value)
					}
				}
			}
			inputs := []string{}
			if publication.BindingID != "" {
				inputs = append(inputs, publication.BindingID)
			} else if publication.Kind == "ledger" {
				inputs = append(inputs, "agent_output")
			}
			paths := map[string]string{}
			switch module {
			case "push_repo_memory.cjs":
				paths["ARTIFACT_DIR"] = publication.BindingID
			case "push_evals_state.cjs":
				paths["GH_AW_STATE_DIR"] = publication.BindingID
			case "upload_assets.cjs":
				paths["GH_AW_ASSETS_DIR"] = publication.BindingID
			case "push_ledger_changes.cjs":
				paths["GH_AW_LEDGER_TRANSACTIONS"] = "generated:" + publication.Job + ":" + publication.ID
				inputs = append(inputs, paths["GH_AW_LEDGER_TRANSACTIONS"])
			}
			publication.Invocation = &pendingWriteInvocation{Module: module, Env: env, InputBindingIDs: inputs, PathEnv: paths}
			break
		}
	}
	encoded, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("encoding frozen pending invocations: %w", err)
	}
	safeJob, exists := c.jobManager.GetJob("safe_outputs")
	if !exists {
		return nil
	}
	var parsed struct {
		Steps []map[string]any `yaml:"steps"`
	}
	if err := yaml.Unmarshal([]byte("steps:\n"+strings.Join(safeJob.Steps, "")), &parsed); err != nil {
		return fmt.Errorf("reading pending-write preparation steps: %w", err)
	}
	var steps []string
	for _, step := range parsed.Steps {
		env := pendingStepEnv(step)
		if env != nil && (env["GH_AW_PENDING_WRITES_SPEC"] != nil || step["id"] == "process_safe_outputs") {
			env["GH_AW_PENDING_WRITES_SPEC"] = string(encoded)
		}
		rendered, err := ConvertStepToYAML(step)
		if err != nil {
			return fmt.Errorf("binding frozen invocation specification: %w", err)
		}
		steps = append(steps, rendered)
	}
	safeJob.Steps = steps
	return nil
}
