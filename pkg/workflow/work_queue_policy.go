package workflow

import (
	"errors"
	"fmt"
	"strings"
)

const workQueueWorkerRunName = `run-name: "${{ github.event_name == 'workflow_dispatch' && inputs.work_queue_assignment != '' && format('gh-aw work-queue {0}', fromJSON(inputs.work_queue_assignment).dispatch_id) || 'gh-aw work-queue unassigned' }}"`

// WorkQueuePolicyConfig holds the compiled per-invocation queue policy. PolicyJSON
// is the v4 GH_AW_WORK_QUEUE_POLICY value for participants and empty for observers.
type WorkQueuePolicyConfig struct {
	PolicyJSON string
}

func validateWorkQueueConfiguration(data *WorkflowData) error {
	if data == nil {
		return nil
	}
	if err := validateWorkQueueIssuesConfig(data); err != nil {
		return err
	}
	if err := configureWorkQueueMemory(data); err != nil {
		return err
	}
	if data.SafeOutputs != nil && data.SafeOutputs.claimAdaptersParseError != nil {
		return data.SafeOutputs.claimAdaptersParseError
	}
	if data.SafeOutputs != nil && len(data.SafeOutputs.ClaimAdapters) > 0 && !isWorkQueueWorker(data) {
		return errors.New("safe-outputs.claim-adapters require a declared work-queue worker; unassigned dispatchers cannot delegate ordinary effects")
	}
	value, configured := data.Tools["work-queue"]
	if configured && value == false {
		return errors.New("tools.work-queue: scheduling cannot be disabled; remove the tool to use an ordinary workflow")
	}
	if !isWorkQueueEnabled(data) {
		return nil
	}
	if isWorkQueueWorker(data) {
		if _, configured := data.RawFrontmatter["run-name"]; configured {
			return errors.New("work-queue: worker run-name is reserved for dispatch correlation; remove run-name from frontmatter")
		}
		data.RunName = workQueueWorkerRunName
	}
	if isWorkQueueParticipant(data) && data.StatusComment != nil && *data.StatusComment {
		return errors.New("work-queue: status-comment writes are unscoped; use a Claim-scoped add-comment output")
	}
	if config, ok := value.(map[string]any); ok {
		if required, exists := config["require-assignment"]; exists && required == false && isWorkQueueWorker(data) {
			return errors.New("tools.work-queue.require-assignment: queue workers cannot disable assignment validation")
		}
		if isWorkQueueWorker(data) && data.RawFrontmatter != nil {
			if on, exists := data.RawFrontmatter["on"]; exists && !containsWorkflowDispatch(on) {
				return errors.New("work-queue: workers require workflow_dispatch to receive their dispatch assignment")
			}
		}
	}
	data.WorkQueuePolicy = &WorkQueuePolicyConfig{}
	if workQueueRuntimeRole(data) == "observer" {
		return nil
	}
	return validateWorkQueueSafeOutputs(data)
}

func validateWorkQueueSafeOutputs(data *WorkflowData) error {
	// Delegated effects require explicit trusted per-Claim delivery adapters.
	if data.SafeOutputs != nil {
		if err := validateWorkQueueClaimAdapters(data); err != nil {
			return err
		}
		if data.SafeOutputs.UploadCodeCoverage != nil {
			target := data.SafeOutputs.UploadCodeCoverage.TargetRef
			if target == "" || !strings.HasPrefix(target, "refs/heads/") && !strings.HasPrefix(target, "refs/tags/") || strings.Contains(target, "${{") {
				return errors.New("work-queue: upload-code-coverage requires a trusted per-Claim delivery adapter with a literal refs/heads/ or refs/tags/ target-ref")
			}
		}
		if data.SafeOutputs.CreateCodeScanningAlerts != nil {
			target := data.SafeOutputs.CreateCodeScanningAlerts.TargetRef
			if target == "" || !strings.HasPrefix(target, "refs/heads/") && !strings.HasPrefix(target, "refs/tags/") || strings.Contains(target, "${{") {
				return errors.New("work-queue: create-code-scanning-alert requires a trusted per-Claim delivery adapter with a literal refs/heads/ or refs/tags/ target-ref")
			}
		}
		if data.SafeOutputs.CallWorkflow != nil || data.SafeOutputs.DispatchRepository != nil {
			return errors.New("work-queue: delegated workflow/repository dispatch requires a trusted per-Claim delivery adapter; use work_queue_dispatch_next")
		}
	}
	if data.LedgerConfig != nil || data.RepoMemoryConfig != nil || data.DriveMemoryConfig != nil {
		return errors.New("work-queue: standalone persistent ledger/repository/drive writes require a trusted per-Claim delivery adapter")
	}
	return nil
}

func (c *Compiler) validateWorkQueueTargets(data *WorkflowData, markdownPath string) error {
	if data.WorkQueuePolicy == nil || !isWorkQueueParticipant(data) || data.SafeOutputs == nil || data.SafeOutputs.DispatchWorkflow == nil {
		return nil
	}
	for _, name := range data.SafeOutputs.DispatchWorkflow.Workflows {
		sourcePath, err := resolveWorkQueueWorkerSource(name, markdownPath)
		if errors.Is(err, errNotCompilerManagedWorkQueueTarget) {
			return fmt.Errorf("work-queue: approved worker %q requires compiler-managed workflow source", name)
		}
		if err != nil {
			return fmt.Errorf("work-queue: approved worker %q: %w", name, err)
		}
		worker, err := mdHasWorkQueueWorker(sourcePath)
		if err != nil {
			return err
		}
		if !worker {
			return fmt.Errorf("work-queue: approved target %q must declare tools.work-queue.worker: true; ordinary delegated workflows have no Claim-scoped delivery adapter", name)
		}
	}
	return nil
}

var errNotCompilerManagedWorkQueueTarget = errors.New("workflow source is not compiler-managed")

func resolveWorkQueueWorkerSource(name, markdownPath string) (string, error) {
	target, err := findWorkflowFile(name, markdownPath)
	if err != nil {
		return "", err
	}
	if !target.mdExists {
		return "", errNotCompilerManagedWorkQueueTarget
	}
	return target.mdPath, nil
}

// workQueuePolicyEnvironment renders the env block shared by every queue step:
// the runtime role and, for participants, the compiled v4 policy.
func workQueuePolicyEnvironment(data *WorkflowData) []string {
	if data == nil || data.WorkQueuePolicy == nil {
		return nil
	}
	lines := []string{
		"        env:\n",
		"          GH_AW_WORK_QUEUE_ENABLED: \"true\"\n",
		"          GH_AW_WORK_QUEUE_ROLE: " + fmt.Sprintf("%q", workQueueRuntimeRole(data)) + "\n",
	}
	if data.WorkQueuePolicy.PolicyJSON != "" {
		lines = append(lines, workQueuePolicyJSONEnvironment(data.WorkQueuePolicy.PolicyJSON)...)
	}
	if isWorkQueueParticipant(data) && data.SafeOutputs != nil && data.SafeOutputs.DispatchWorkflow != nil {
		budget := "1"
		if data.SafeOutputs.DispatchWorkflow.Max != nil {
			budget = *data.SafeOutputs.DispatchWorkflow.Max
		}
		lines = append(lines, "          GH_AW_WORK_QUEUE_DISPATCH_BUDGET: "+fmt.Sprintf("%q", budget)+"\n")
	}
	return lines
}
