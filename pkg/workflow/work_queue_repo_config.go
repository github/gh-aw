package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workqueue"
)

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
	if _, local := data.RawFrontmatter["work-queue-policy"]; local {
		return fmt.Errorf("work-queue-policy conflicts with %s work_queue; move global queue configuration to aw.json and retain AW dispatch targets in the workflow", RepoConfigFileName)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(config.WorkQueue, &fields); err != nil {
		return fmt.Errorf("%s work_queue: %w", RepoConfigFileName, err)
	}
	issues, configured := fields["issues"]
	if !configured {
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

func (c *Compiler) configureAWWorkQueue(data *WorkflowData, markdownPath string) error {
	if !isWorkQueueEnabled(data) || !isWorkQueueParticipant(data) {
		return nil
	}
	if err := configureWorkQueueWorkerContract(data, markdownPath, c.getSharedImportCache()); err != nil {
		return err
	}
	config, err := c.loadRepoConfig()
	if err != nil {
		return fmt.Errorf("work-queue: cannot resolve scheduling from %s: %w", RepoConfigFileName, err)
	}
	if hasWorkQueuePolicyProposal(data) {
		if len(config.WorkQueue) > 0 {
			return fmt.Errorf("work-queue-policy conflicts with %s work_queue; move scheduling to work_queue and keep worker targets in safe-outputs.dispatch-workflow.workflows", RepoConfigFileName)
		}
		fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning", "work-queue-policy is deprecated; move scheduling to .github/workflows/aw.json work_queue and use AW dispatch targets instead of producer or worker identities"))
		c.IncrementWarningCount()
		return nil
	}
	settings, err := workqueue.ParseSettings(config.WorkQueue)
	if err != nil {
		return fmt.Errorf("%s: %w", RepoConfigFileName, err)
	}
	policy := data.WorkQueuePolicy.Policy
	policy.Authorization = "aw"
	policy.Producers = map[string]workqueue.ProducerRule{}
	profiles, err := approvedAWQueueProfiles(markdownPath, isWorkQueueWorker(data), c.getSharedImportCache())
	if err != nil {
		return err
	}
	if isWorkQueueWorker(data) {
		name := GetWorkflowIDFromPath(markdownPath)
		profile := profiles[name]
		profile.LogicalContract = data.WorkQueuePolicy.WorkerContract
		profiles[name] = profile
	}
	pool := policy.Pools["default"]
	pool.Profiles = profiles
	pool.DefaultProfile = slices.Min(slices.Collect(maps.Keys(profiles)))
	policy.Pools["default"] = pool
	policy, err = settings.Apply(policy)
	if err != nil {
		return fmt.Errorf("%s work_queue: %w", RepoConfigFileName, err)
	}
	data.WorkQueuePolicy.Policy = policy
	return workqueue.ValidatePolicy(bindWorkQueuePolicyValidationTemplates(policy))
}

func configureWorkQueueWorkerContract(data *WorkflowData, markdownPath string, cache *parser.ImportCache) error {
	if !isWorkQueueWorker(data) {
		return nil
	}
	var contract string
	var err error
	if data.RawMarkdown != "" {
		source := data.RawMarkdown
		if data.FrontmatterYAML != "" {
			source = "---\n" + data.FrontmatterYAML + "\n---\n" + source
		}
		contract, err = workQueueLogicalContractFromContentWithCache(markdownPath, source, cache)
	} else {
		contract, err = workQueueLogicalContractWithCache(markdownPath, cache)
	}
	if err != nil {
		return err
	}
	data.WorkQueuePolicy.WorkerContract = contract
	return nil
}

func approvedAWQueueProfiles(markdownPath string, worker bool, cache *parser.ImportCache) (map[string]workqueue.WorkerProfile, error) {
	paths, err := filepath.Glob(filepath.Join(filepath.Dir(markdownPath), "*.md"))
	if err != nil {
		return nil, fmt.Errorf("work-queue: discover AW workers: %w", err)
	}
	profiles := map[string]workqueue.WorkerProfile{}
	template := workqueue.DefaultPolicy("1", "${{ github.repository }}").Pools["default"].Profiles["default"]
	template.Principal = ""
	template.Ref = "${{ github.sha }}"
	for _, path := range paths {
		declared, err := mdHasWorkQueueWorker(path)
		if err != nil {
			return nil, fmt.Errorf("work-queue: inspect AW worker %s: %w", path, err)
		}
		if !declared {
			continue
		}
		name := GetWorkflowIDFromPath(path)
		profile := template
		profile.Workflow = constants.WorkflowsDirSlash + name + ".lock.yml"
		profile.LogicalContract, err = workQueueLogicalContractWithCache(path, cache)
		if err != nil {
			return nil, err
		}
		profiles[name] = profile
	}
	if worker {
		name := GetWorkflowIDFromPath(markdownPath)
		if _, found := profiles[name]; !found {
			template.Workflow = constants.WorkflowsDirSlash + name + ".lock.yml"
			profiles[name] = template
		}
	}
	if len(profiles) == 0 {
		return nil, errors.New("work-queue: declare at least one AW worker with tools.work-queue.worker: true and approve it in safe-outputs.dispatch-workflow.workflows")
	}
	return profiles, nil
}
