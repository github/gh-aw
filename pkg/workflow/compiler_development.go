package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var compilerDevelopmentLog = logger.New("workflow:compiler_development")

func (c *Compiler) disableDevelopmentPushJobs() {
	if !c.developmentMode {
		return
	}
	for name, job := range c.jobManager.GetAllJobs() {
		if strings.HasPrefix(name, "push_") {
			job.If = "false"
			compilerDevelopmentLog.Printf("Disabled push job for development testing: %s", name)
		}
	}
}

func (c *Compiler) developmentConclusionData(data *WorkflowData) *WorkflowData {
	if !c.developmentMode || data.SafeOutputs == nil {
		return data
	}
	result := *data
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
	value := false
	result.StatusComment = &value
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
