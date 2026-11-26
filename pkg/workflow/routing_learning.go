// This file implements model-routing.learning: observe. An eligible routed workflow keeps
// its ordinary router pick, and a push_routing_history job, after the conclusion job
// has published the run's unified session, records the run in the workflow's routing
// history on memory/adaptive-routing/<workflow>
// (actions/setup/js/push_routing_history.cjs). A workflow that does not opt in, or
// that a repository default skips, compiles exactly as before.

package workflow

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var routingLearningLog = logger.New("workflow:routing_learning")

const (
	routingLearningObserve = "observe"
	// routingHistoryBranchRoot is reserved for the routing histories; no repo-memory
	// entry may write it, so the agent can never edit its own record.
	routingHistoryBranchRoot = "memory/adaptive-routing"
)

// The workflow ID names the history branch, so it must be a valid ref component.
var routingHistoryWorkflowIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*(?:\.[A-Za-z0-9_-]+)*$`)

// routingLearningSetting is the effective model-routing.learning value. explicit marks a value
// from the workflow or an import, where an unsupported workflow is an error; a
// repository default skips such a workflow silently.
type routingLearningSetting struct {
	mode     string
	explicit bool
}

// resolveRoutingLearningSetting applies the precedence: the workflow's own value
// (including false), then the first import that sets it, then aw.json, then off.
func resolveRoutingLearningSetting(frontmatter map[string]any, imported string, repoConfig *RepoConfig) (routingLearningSetting, error) {
	value, ok := ExtractMapField(frontmatter, "model-routing")["learning"]
	if !ok && imported != "" {
		if err := decodeModelRoutingJSONDocument(imported, &value); err != nil {
			return routingLearningSetting{}, fmt.Errorf("imported model-routing.learning value is not recognized: %w", err)
		}
		ok = true
	}
	if !ok {
		if repoConfig != nil && repoConfig.RoutingLearning == routingLearningObserve {
			return routingLearningSetting{mode: routingLearningObserve}, nil
		}
		return routingLearningSetting{}, nil
	}
	switch v := value.(type) {
	case string:
		if v == routingLearningObserve {
			return routingLearningSetting{mode: v, explicit: true}, nil
		}
	case bool:
		if !v {
			return routingLearningSetting{explicit: true}, nil
		}
	}
	return routingLearningSetting{}, NewValidationError("model-routing.learning", fmt.Sprint(value), "unsupported learning mode", "Use observe, or false to turn it off.")
}

// routingLearningIneligibility returns why the workflow cannot keep a routing history,
// or "" when it can: its primary model must be chosen by AWF model routing on a
// Copilot, Claude, or Codex engine, it must not be a work-queue worker or run
// experiments, and it must enable repo-memory.
func routingLearningIneligibility(data *WorkflowData) string {
	if !isModelRoutingEnabled(data) {
		return "its primary model is not chosen by model routing"
	}
	switch strings.ToLower(data.EngineConfig.ID) {
	case string(constants.CopilotEngine), string(constants.ClaudeEngine), string(constants.CodexEngine):
	default:
		return fmt.Sprintf("the %s engine is not supported; recording routing history requires Copilot, Claude, or Codex", data.EngineConfig.ID)
	}
	if isWorkQueueWorker(data) {
		return "work-queue workers are not supported"
	}
	if len(data.Experiments) > 0 {
		return "workflows that run experiments are not supported"
	}
	if hasWorkflowCallTrigger(data.On) {
		return "reusable workflows (on: workflow_call) are not supported, because each caller numbers its own runs"
	}
	if id := data.WorkflowID; len(id) > 100 || strings.HasSuffix(id, ".lock") || !routingHistoryWorkflowIDPattern.MatchString(id) {
		return fmt.Sprintf("the workflow ID %q cannot name a history branch", id)
	}
	if data.RepoMemoryConfig == nil || len(data.RepoMemoryConfig.Memories) == 0 {
		return "tools.repo-memory is required"
	}
	return ""
}

// applyRoutingLearning resolves model-routing.learning once the routing configuration and
// repo-memory are final.
func (c *Compiler) applyRoutingLearning(ctx *workflowBuildContext) error {
	repoConfig, err := c.loadRepoConfig()
	if err != nil {
		repoConfig = nil
	}
	setting, err := resolveRoutingLearningSetting(ctx.frontmatter.Frontmatter, ctx.engineSetup.importsResult.MergedRoutingLearning, repoConfig)
	if err != nil {
		return formatCompilerError(ctx.cleanPath, "error", err.Error(), err)
	}
	if setting.mode == "" {
		return nil
	}
	data := ctx.workflowData
	if reason := routingLearningIneligibility(data); reason != "" {
		if !setting.explicit {
			routingLearningLog.Printf("Repository default model-routing.learning skipped for %s: %s", data.WorkflowID, reason)
			return nil
		}
		err := NewValidationError("model-routing.learning", setting.mode, "cannot record routing history: "+reason,
			"Learning needs a router-selected primary model (for example model: router/balanced) on a Copilot, Claude, or Codex engine, and tools.repo-memory enabled. Remove learning or set it to false.")
		return formatCompilerError(ctx.cleanPath, "error", err.Error(), err)
	}
	data.RoutingLearningMode = setting.mode
	routingLearningLog.Printf("model-routing.learning set to %s for %s", setting.mode, data.WorkflowID)
	return nil
}

// validateRepoMemoryBranchesNotReserved rejects repo-memory branches inside the routing
// histories' namespace, which only push_routing_history and gh aw routing reset write.
func validateRepoMemoryBranchesNotReserved(config *RepoMemoryConfig) error {
	if config == nil {
		return nil
	}
	for _, memory := range config.Memories {
		if memory.Wiki {
			continue
		}
		if memory.BranchName == routingHistoryBranchRoot || strings.HasPrefix(memory.BranchName, routingHistoryBranchRoot+"/") {
			return NewValidationError("tools.repo-memory.branch-name", memory.BranchName, routingHistoryBranchRoot+"/ is reserved for adaptive routing histories",
				"Choose another branch name or branch-prefix for this repo-memory entry.")
		}
	}
	return nil
}

// addPushRoutingHistoryJob adds the job that records the run in its routing history.
// It runs after conclusion, which builds the unified session the verdict lives in,
// and writes through repo-memory's save loop with its own contents: write token.
// No concurrency group: the save loop converges concurrent writers itself.
func (c *Compiler) addPushRoutingHistoryJob(data *WorkflowData) error {
	if data.RoutingLearningMode == "" || data.DryRun || c.dryRun {
		return nil
	}
	if _, exists := c.jobManager.GetJob(string(constants.ConclusionJobName)); !exists {
		return errors.New("model-routing.learning requires the conclusion job, which publishes the run's unified session")
	}
	if !isSHA256Hex(data.FrontmatterHash) || !isSHA256Hex(data.BodyHash) {
		return errors.New("model-routing.learning requires the workflow's frontmatter and body hashes, which identify its definition")
	}
	permissions := NewPermissionsFromMap(map[PermissionScope]PermissionLevel{PermissionActions: PermissionRead, PermissionContents: PermissionWrite})
	agentRan := BuildNotEquals(BuildPropertyAccess(fmt.Sprintf("needs.%s.result", constants.AgentJobName)), BuildStringLiteral("skipped"))
	return c.jobManager.AddJob(&Job{
		Name:           pushRoutingHistoryJobName,
		RunsOn:         c.formatFrameworkJobRunsOn(data),
		Needs:          []string{string(constants.ActivationJobName), string(constants.AgentJobName), string(constants.ConclusionJobName)},
		If:             RenderCondition(BuildAnd(BuildFunctionCall("always"), agentRan)),
		Permissions:    permissions.RenderToYAML(),
		TimeoutMinutes: 10,
		Steps:          c.buildPushRoutingHistorySteps(data),
	})
}

func (c *Compiler) buildPushRoutingHistorySteps(data *WorkflowData) []string {
	var steps []string
	if setupRef := c.resolveActionReference("./actions/setup", data); setupRef != "" || c.actionMode.IsScript() {
		traceID := fmt.Sprintf("${{ needs.%s.outputs.setup-trace-id }}", constants.ActivationJobName)
		steps = append(steps, c.generateCheckoutActionsFolder(data)...)
		steps = append(steps, c.generateSetupStepForJob(pushRoutingHistoryJobName, data, setupRef, SetupActionDestination, false, traceID, setupParentSpanNeedsExpr(constants.ActivationJobName), "")...)
	}
	steps = append(steps, buildArtifactDownloadSteps(ArtifactDownloadConfig{
		ArtifactName:    artifactPrefixExprForDownstreamJob(data) + constants.UsageArtifactName.String(),
		DownloadPath:    path.Join(constants.TmpGhAwDir, constants.UsageArtifactName.String()),
		StepName:        "Download usage artifact",
		ContinueOnError: new(false),
	}, c.getActionPin)...)

	engineID := strings.ToLower(data.EngineConfig.ID)
	var step strings.Builder
	step.WriteString("      - name: Record the run in the routing history\n")
	step.WriteString("        id: push_routing_history\n")
	step.WriteString("        uses: " + getCachedActionPin("actions/github-script", data) + "\n")
	step.WriteString("        env:\n")
	step.WriteString("          GH_TOKEN: ${{ github.token }}\n")
	// Quoted, so a workflow ID such as null or 1e3 stays a string.
	fmt.Fprintf(&step, "          GH_AW_ROUTING_WORKFLOW: %q\n", data.WorkflowID)                          //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	fmt.Fprintf(&step, "          GH_AW_ROUTING_GENERATION: %q\n", data.FrontmatterHash+":"+data.BodyHash) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	fmt.Fprintf(&step, "          GH_AW_ROUTING_MODE: %q\n", data.RoutingLearningMode)                     //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	fmt.Fprintf(&step, "          GH_AW_ROUTING_ENGINE: %q\n", engineID)                                   //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	fmt.Fprintf(&step, "          GH_AW_ROUTING_GOAL: %q\n", data.EngineConfig.ModelRouting.Goal)          //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	// A level chosen at run time keeps its own records; otherwise levels share them.
	if isModelRoutingDynamic(data) {
		step.WriteString("          GH_AW_ROUTING_LEVEL: ${{ " + modelRoutingModeDownstream + " }}\n")
	}
	step.WriteString("          GH_AW_ROUTING_SESSION_FILE: " + path.Join(constants.TmpGhAwDir, constants.UsageArtifactName.String(), "aw_session.jsonl") + "\n")
	writeSubagentModelDeclarations(&step, data, engineID)
	step.WriteString("        with:\n")
	step.WriteString("          script: |\n")
	step.WriteString("            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n")
	step.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	step.WriteString("            const { main } = require('" + SetupActionDestination + "/push_routing_history.cjs');\n")
	step.WriteString("            await main();\n")
	steps = append(steps, step.String())
	if c.actionMode.IsDev() {
		steps = append(steps, c.generateRestoreActionsSetupStep())
	}
	return steps
}
