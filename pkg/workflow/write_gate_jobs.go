package workflow

import (
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"
)

// publicationJobNames is shared by the write gate and require-agent-success.
// Activation-owned experiment persistence is independent of agent publication.
func publicationJobNames(safeOutputs *SafeOutputsConfig) map[string]struct{} {
	names := map[string]struct{}{
		string(constants.SafeOutputsJobName):        {},
		string(constants.UploadAssetsJobName):       {},
		string(constants.UploadCodeScanningJobName): {},
		string(constants.UploadCodeCoverageJobName): {},
		pushRepoMemoryJobName:                       {},
		updateCacheMemoryJobName:                    {},
		updateDriveMemoryJobName:                    {},
		pushLedgerChangesJobName:                    {},
		pushEvalsStateJobName:                       {},
	}
	if safeOutputs != nil {
		for name := range safeOutputs.Jobs {
			names[stringutil.NormalizeSafeOutputIdentifier(name)] = struct{}{}
		}
		if safeOutputs.CallWorkflow != nil {
			for _, name := range safeOutputs.CallWorkflow.Workflows {
				names["call-"+sanitizeJobName(name)] = struct{}{}
			}
		}
	}
	return names
}

func hasWritePublicationPath(data *WorkflowData) bool {
	if data == nil {
		return false
	}
	for name := range publicationJobNames(data.SafeOutputs) {
		switch name {
		case "safe_outputs":
			if HasSafeOutputsEnabled(data.SafeOutputs) || data.CommentMemoryConfig != nil {
				return true
			}
		case pushRepoMemoryJobName:
			if data.RepoMemoryConfig != nil && len(data.RepoMemoryConfig.Memories) > 0 {
				return true
			}
		case updateCacheMemoryJobName:
			if data.CacheMemoryConfig != nil && slices.ContainsFunc(data.CacheMemoryConfig.Caches, func(cache CacheMemoryEntry) bool { return !cache.RestoreOnly }) {
				return true
			}
		case updateDriveMemoryJobName:
			if data.DriveMemoryConfig != nil && slices.ContainsFunc(data.DriveMemoryConfig.Drives, func(drive DriveMemoryEntry) bool { return !drive.RestoreOnly }) {
				return true
			}
		case pushLedgerChangesJobName:
			if data.LedgerConfig.Enabled() {
				return true
			}
		case pushEvalsStateJobName:
			if data.Evals != nil && data.Evals.HasEvals() {
				return true
			}
		}
	}
	return false
}

func requirePublicationAgentSuccess(condition ConditionNode, safeOutputs *SafeOutputsConfig, jobName string) ConditionNode {
	if _, publication := publicationJobNames(safeOutputs)[jobName]; !publication {
		return condition
	}
	return requireSafeOutputsAgentSuccess(condition, safeOutputs)
}

func buildWriteGateAllowedCondition() ConditionNode {
	verdict := BuildPropertyAccess("needs.safe_outputs.outputs.write_gate")
	return BuildAnd(
		&NotNode{Child: BuildFunctionCall("cancelled")},
		BuildAnd(
			BuildEquals(BuildPropertyAccess("needs.safe_outputs.result"), BuildStringLiteral("success")),
			BuildOr(
				BuildEquals(verdict, BuildStringLiteral("allow")),
				BuildEquals(verdict, BuildStringLiteral("partial")),
			),
		),
	)
}

func (c *Compiler) guardPublicationWriteGate(data *WorkflowData) error {
	if !hasWriteGateSteps(data) {
		return nil
	}
	if _, exists := c.jobManager.GetJob(string(constants.SafeOutputsJobName)); !exists {
		return fmt.Errorf("jobs.safe_outputs.gate-steps requires a generated safe_outputs job")
	}
	for name := range publicationJobNames(data.SafeOutputs) {
		if name == string(constants.SafeOutputsJobName) {
			continue
		}
		job, exists := c.jobManager.GetJob(name)
		if !exists {
			continue
		}
		if !slices.Contains(job.Needs, string(constants.SafeOutputsJobName)) {
			job.Needs = append(job.Needs, string(constants.SafeOutputsJobName))
		}
		condition := buildWriteGateAllowedCondition()
		if job.If != "" {
			condition = BuildAnd(condition, &ExpressionNode{Expression: c.extractExpressionFromIfString(job.If)})
		}
		// Keep the existing require-agent-success rendering outside gated mode.
		if !strings.Contains(job.If, "needs.agent.result == 'success'") {
			condition = requirePublicationAgentSuccess(condition, data.SafeOutputs, name)
			if data.SafeOutputs != nil && data.SafeOutputs.RequireAgentSuccess &&
				!slices.Contains(job.Needs, string(constants.AgentJobName)) {
				job.Needs = append(job.Needs, string(constants.AgentJobName))
			}
		}
		job.If = RenderCondition(condition)
	}
	return c.jobManager.ValidateDependencies()
}
