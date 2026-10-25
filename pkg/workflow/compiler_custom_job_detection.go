package workflow

import (
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/workflow/aw"
)

// Run after all job augmentation and typed output-derived dependency lowering.
func (c *Compiler) guardCustomJobDetection(data *WorkflowData) error {
	detection := string(constants.DetectionJobName)
	if _, enabled := c.jobManager.GetJob(detection); !enabled {
		return nil
	}
	graph := make(aw.JobDependencyGraph)
	for id, job := range c.jobManager.GetAllJobs() {
		graph[id] = job.Needs
	}
	allJobs := c.jobManager.GetAllJobs()
	allowedOutputJobs := make(map[string]struct{})
	for id, job := range allJobs {
		if constants.IsCompilerOwnedJobName(id) && graph.DependsOn(id, detection) && aw.HasSuccessfulDetectionCondition(job.If) {
			allowedOutputJobs[id] = struct{}{}
		}
	}
	for _, id := range sliceutil.SortedKeys(data.Jobs) {
		if isBuiltinJobName(id) {
			continue
		}
		job, exists := c.jobManager.GetJob(id)
		if !exists {
			continue
		}
		if !graph.DependsOn(id, string(constants.AgentJobName)) && !graph.DependsOn(id, detection) {
			if !graph.DependsOn(string(constants.AgentJobName), id) {
				return fmt.Errorf("jobs.%s.needs: custom jobs must run before agent or after detection; make agent depend on this job or add detection to this job's needs", id)
			}
			continue
		}
		if graph.DependsOn(detection, id) {
			return fmt.Errorf("jobs.%s.needs: custom jobs must run before agent or after detection, not between them; remove this job from detection's dependencies", id)
		}
		config, ok := data.Jobs[id].(map[string]any)
		if !ok {
			return fmt.Errorf("jobs.%s: custom job configuration must be an object", id)
		}
		statusOnly := aw.IsStatusOnlyJob(config)
		if statusOnly {
			for _, dependent := range sliceutil.SortedKeys(data.Jobs) {
				if dependent != id && !isBuiltinJobName(dependent) && graph.DependsOn(dependent, id) {
					return fmt.Errorf("jobs.%s.detection-gate: status-only jobs cannot have dependent custom job %s; apply the same contract to a leaf job instead", id, dependent)
				}
			}
			violations := aw.ValidateStatusOnlyJob(id, config, allowedOutputJobs)
			if len(violations) > 0 {
				return fmt.Errorf("%s: %s", violations[0].Path, violations[0].Message)
			}
		}
		if err := addCustomJobDetectionGuard(job, statusOnly); err != nil {
			return err
		}
	}
	return nil
}

func addCustomJobDetectionGuard(job *Job, statusOnly bool) error {
	condition := strings.TrimSpace(job.If)
	if condition != "" {
		var err error
		condition, _, err = aw.ParseActionsExpression(condition)
		if err != nil {
			return fmt.Errorf("jobs.%s.if: expected one valid GitHub Actions expression; remove interpolated text and use one complete ${{ }} wrapper or omit the wrapper", job.Name)
		}
	}
	detection := string(constants.DetectionJobName)
	if !slices.Contains(job.Needs, detection) {
		job.Needs = append(job.Needs, detection)
	}
	if statusOnly {
		return nil
	}
	// Warn-mode detections can report job success while flagging tainted output.
	guard := "needs." + detection + ".result == 'success' && needs." + detection + ".outputs.detection_success == 'true'"
	if condition != "" {
		guard += " && (" + condition + ")"
	}
	job.If = "${{ " + guard + " }}"
	job.actionsCondition = true
	return nil
}
