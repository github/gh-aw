package workflow

import (
	"maps"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var compilerDevelopmentLog = logger.New("workflow:compiler_development")

func (c *Compiler) disableDryRunPushJobs() {
	if !c.dryRun {
		return
	}
	for name, job := range c.jobManager.GetAllJobs() {
		if strings.HasPrefix(name, "push_") || name == "update_cache_memory" || name == "update_drive_memory" {
			job.If = "false"
			compilerDevelopmentLog.Printf("Disabled mutation job for dry-run: %s", name)
		}
	}
}

func (c *Compiler) dryRunWorkflowData(data *WorkflowData) *WorkflowData {
	if !c.dryRun {
		return data
	}
	result := *c.dryRunConclusionData(data)
	result.DryRun = true
	result.AIReaction = "none"
	result.LockForAgent = false
	result.LabelCommandRemoveLabel = false
	result.ReportBlockedVersionDisabled = true
	result.Tools = maps.Clone(data.Tools)
	if _, exists := result.Tools["work-queue"]; exists {
		result.Tools["work-queue"] = false
	}
	if github, configured := result.Tools["github"]; configured && github != false {
		config, ok := github.(map[string]any)
		if !ok || config == nil {
			config = make(map[string]any)
		} else {
			config = maps.Clone(config)
		}
		config["read-only"] = true
		result.Tools["github"] = config
	}
	if data.ParsedTools != nil {
		tools := *data.ParsedTools
		if tools.GitHub != nil {
			github := *tools.GitHub
			github.ReadOnly = true
			tools.GitHub = &github
		}
		result.ParsedTools = &tools
	}
	if data.CacheMemoryConfig != nil {
		config := *data.CacheMemoryConfig
		config.Caches = sliceutil.Map(config.Caches, func(cache CacheMemoryEntry) CacheMemoryEntry {
			cache.RestoreOnly = true
			return cache
		})
		result.CacheMemoryConfig = &config
	}
	if data.DriveMemoryConfig != nil {
		config := *data.DriveMemoryConfig
		config.Drives = sliceutil.Map(config.Drives, func(drive DriveMemoryEntry) DriveMemoryEntry {
			drive.RestoreOnly = true
			return drive
		})
		result.DriveMemoryConfig = &config
	}
	return &result
}

func (c *Compiler) dryRunConclusionData(data *WorkflowData) *WorkflowData {
	if !c.dryRun {
		return data
	}
	result := *data
	value := false
	result.StatusComment = &value
	if data.SafeOutputs == nil {
		return &result
	}
	outputs := *data.SafeOutputs
	disabled := TemplatableBool("false")
	staged := TemplatableBool("true")
	outputs.Staged = &staged
	outputs.ReportFailureAsIssue = &disabled
	outputs.ReportFailedJobs = &disabled
	outputs.MissingTool = disableConclusionIssueCreation(outputs.MissingTool)
	outputs.ReportIncomplete = disableConclusionIssueCreation(outputs.ReportIncomplete)
	if outputs.NoOp != nil {
		noop := *outputs.NoOp
		value := "false"
		noop.ReportAsIssue = &value
		outputs.NoOp = &noop
	}
	if outputs.ThreatDetection != nil {
		detection := *outputs.ThreatDetection
		value := false
		detection.ReportAsIssue = &value
		outputs.ThreatDetection = &detection
	}
	result.SafeOutputs = &outputs
	return &result
}

func disableConclusionIssueCreation(config *IssueReportingConfig) *IssueReportingConfig {
	if config == nil {
		return nil
	}
	result := *config
	value := "false"
	result.CreateIssue = &value
	return &result
}
