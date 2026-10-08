package workflow

import (
	"maps"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
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
			if job.If != "false" {
				job.If = "false"
				compilerDevelopmentLog.Printf("Dry-run mutation: disable job %q", name)
			}
		}
	}
}

func (c *Compiler) dryRunWorkflowData(data *WorkflowData) *WorkflowData {
	if !c.dryRun {
		return data
	}
	result := *c.dryRunConclusionData(data)
	before := result
	defer logDryRunWorkflowChanges(&before, &result)
	result.Roles = nil
	for _, role := range data.Roles {
		if role == "admin" || role == "maintainer" || role == "maintain" {
			result.Roles = append(result.Roles, role)
		}
	}
	if len(result.Roles) == 0 {
		result.Roles = []string{"admin", "maintainer"}
	}
	result.Bots = nil
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
	defer logDryRunWorkflowChanges(data, &result)
	result.DryRun = true
	result.OTLPEndpoint = ""
	result.OTLPHeaders = ""
	result.OTLPEndpoints = ""
	result.OTLPUsesEnterpriseDefaults = false
	result.RawFrontmatter = maps.Clone(data.RawFrontmatter)
	delete(result.RawFrontmatter, "observability")
	delete(result.RawFrontmatter, maxDailyAICreditsField)
	if data.ParsedFrontmatter != nil {
		frontmatter := *data.ParsedFrontmatter
		frontmatter.Observability = nil
		frontmatter.MaxDailyAICredits = nil
		result.ParsedFrontmatter = &frontmatter
	}
	prepareDryRunCreditLimits(&result, data)
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

func prepareDryRunCreditLimits(result, original *WorkflowData) {
	result.MaxDailyAICredits = nil
	result.MaxDailyAICreditsExplicit = false
	result.MaxDailyAICBackend = ""
	result.MaxDailyAICreditsGitHubApp = nil
	result.MaxDailyAICContinueOnError = false
	if original.MaxDailyAICredits == nil {
		return
	}
	if original.EngineConfig != nil && original.EngineConfig.MaxAICredits > 0 {
		return
	}
	if value, ok := original.RawFrontmatter["max-ai-credits"].(string); ok && isExpression(value) {
		return
	}
	if !original.MaxDailyAICreditsExplicit && (original.EngineConfig == nil || original.EngineConfig.MaxAICredits == 0) {
		return
	}
	budget := *original.MaxDailyAICredits
	engine := EngineConfig{}
	if original.EngineConfig != nil {
		engine = *original.EngineConfig
	}
	if isExpression(budget) {
		engine.MaxAICredits = 0
	} else {
		engine.MaxAICredits = parseMaxAICreditsValue(budget)
	}
	if !original.MaxDailyAICreditsExplicit {
		engine.MaxAICredits = constants.DefaultMaxAICredits
		budget = strconv.FormatInt(engine.MaxAICredits, 10)
	}
	result.EngineConfig = &engine
	if result.RawFrontmatter == nil {
		result.RawFrontmatter = make(map[string]any)
	}
	result.RawFrontmatter["max-ai-credits"] = budget
	if result.ParsedFrontmatter != nil {
		value := TemplatableInt32(budget)
		result.ParsedFrontmatter.MaxAICredits = &value
	}
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
