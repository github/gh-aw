package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workqueue"
)

var workQueueRepoConfigLog = logger.New("workflow:work_queue_repo_config")

// workQueueRepositoryTemplate is bound by Actions at run time; worker effects stay in the run's repository.
const workQueueRepositoryTemplate = "${{ github.repository }}"

func (c *Compiler) applyRepositoryWorkQueueOptions(data *WorkflowData) error {
	if !isWorkQueueEnabled(data) || data.workQueueRepoIssuesConfigured {
		return nil
	}
	config, err := c.loadRepoConfig()
	if err != nil {
		return fmt.Errorf("work-queue: cannot resolve %s: %w", RepoConfigFileName, err)
	}
	if _, err := workqueue.ParseSettings(config.WorkQueue); err != nil {
		return fmt.Errorf("%s: %w", RepoConfigFileName, err)
	}
	if len(config.WorkQueue) == 0 {
		c.warnLegacyWorkQueueIssues(data)
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(config.WorkQueue, &fields); err != nil {
		return fmt.Errorf("%s work_queue: %w", RepoConfigFileName, err)
	}
	issues, configured := fields["issues"]
	if !configured {
		workQueueRepoConfigLog.Printf("%s work_queue has no issues section; keeping workflow-level settings", RepoConfigFileName)
		c.warnLegacyWorkQueueIssues(data)
		return nil
	}
	tool, ok := data.Tools["work-queue"].(map[string]any)
	if !ok {
		tool = map[string]any{}
	}
	if _, local := tool["issues"]; local {
		return fmt.Errorf("tools.work-queue.issues conflicts with %s work_queue.issues; configure Issues and labels only in aw.json", RepoConfigFileName)
	}
	tool = maps.Clone(tool)
	if tool == nil {
		tool = map[string]any{}
	}
	var value any
	if err := json.Unmarshal(issues, &value); err != nil {
		return fmt.Errorf("%s work_queue.issues: %w", RepoConfigFileName, err)
	}
	workQueueRepoConfigLog.Printf("Applying work_queue.issues from %s to tools.work-queue", RepoConfigFileName)
	tool["issues"] = value
	data.Tools["work-queue"] = tool
	data.workQueueRepoIssuesConfigured = true
	return nil
}

func (c *Compiler) warnLegacyWorkQueueIssues(data *WorkflowData) {
	tool, ok := data.Tools["work-queue"].(map[string]any)
	if !ok {
		return
	}
	if _, configured := tool["issues"]; configured {
		fmt.Fprintln(os.Stderr, formatCompilerMessage(RepoConfigFileName, "warning", "tools.work-queue.issues is deprecated; move global Issue projection and label settings to aw.json work_queue.issues"))
		c.IncrementWarningCount()
	}
}

// configureAWWorkQueue compiles the v4 policy environment for queue participants
// from aw.json scheduling settings and the worker workflows in the same directory.
func (c *Compiler) configureAWWorkQueue(data *WorkflowData, markdownPath string) error {
	if !isWorkQueueEnabled(data) || !isWorkQueueParticipant(data) || data.WorkQueuePolicy == nil {
		return nil
	}
	config, err := c.loadRepoConfig()
	if err != nil {
		return fmt.Errorf("work-queue: cannot resolve scheduling from %s: %w", RepoConfigFileName, err)
	}
	settings, err := workqueue.ParseSettings(config.WorkQueue)
	if err != nil {
		return fmt.Errorf("%s: %w", RepoConfigFileName, err)
	}
	workers, err := discoverWorkQueueWorkers(markdownPath, isWorkQueueWorker(data))
	if err != nil {
		return err
	}
	workQueueRepoConfigLog.Printf("Configuring work queue policy: path=%s, workers=%d", markdownPath, len(workers))
	env, err := workqueue.BuildPolicyEnv(settings, workQueueRepositoryTemplate, workers)
	if err != nil {
		return fmt.Errorf("%s %w", RepoConfigFileName, err)
	}
	env.Issues = nil
	if issues := workQueueIssuesConfig(data); issues != nil {
		env.Issues = &workqueue.IssuesEnv{Label: issues.Label, TitlePrefix: issues.TitlePrefix, OrphanedAfterDays: issues.OrphanedAfterDays}
	}
	if err := workqueue.ValidatePolicyEnv(bindWorkQueuePolicyValidationRepository(env)); err != nil {
		return fmt.Errorf("work-queue: invalid compiled policy: %w", err)
	}
	encoded, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("work-queue: encode compiled policy: %w", err)
	}
	data.WorkQueuePolicy.PolicyJSON = string(encoded)
	return nil
}

// bindWorkQueuePolicyValidationRepository binds the run-time repository
// template on a copy so the closed policy shape can be validated at compile time.
func bindWorkQueuePolicyValidationRepository(env workqueue.PolicyEnv) workqueue.PolicyEnv {
	pools := make(map[string]workqueue.PoolEnv, len(env.Pools))
	for name, pool := range env.Pools {
		profiles := make(map[string]workqueue.ProfileEnv, len(pool.Profiles))
		for profileName, profile := range pool.Profiles {
			if profile.EffectScope == workQueueRepositoryTemplate {
				profile.EffectScope = "owner/repo"
			}
			profiles[profileName] = profile
		}
		pool.Profiles = profiles
		pools[name] = pool
	}
	env.Pools = pools
	return env
}

// discoverWorkQueueWorkers returns the workflow IDs in the workflow's directory
// that declare tools.work-queue.worker: true, including the compiled workflow itself.
func discoverWorkQueueWorkers(markdownPath string, worker bool) ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(filepath.Dir(markdownPath), "*.md"))
	if err != nil {
		return nil, fmt.Errorf("work-queue: discover workers: %w", err)
	}
	var workers []string
	for _, path := range paths {
		declared, err := mdHasWorkQueueWorker(path)
		if err != nil {
			return nil, fmt.Errorf("work-queue: inspect worker %s: %w", path, err)
		}
		if declared {
			workers = append(workers, GetWorkflowIDFromPath(path))
		}
	}
	if worker {
		workers = append(workers, GetWorkflowIDFromPath(markdownPath))
	}
	workQueueRepoConfigLog.Printf("Discovered %d worker(s) from %d markdown file(s)", len(workers), len(paths))
	if len(workers) == 0 {
		return nil, errors.New("work-queue: declare at least one worker with tools.work-queue.worker: true and approve it in safe-outputs.dispatch-workflow.workflows")
	}
	return workers, nil
}
