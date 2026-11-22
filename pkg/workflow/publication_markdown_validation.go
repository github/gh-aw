package workflow

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var (
	publicationAlertMarker = regexp.MustCompile(`^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]$`)
	publicationListMarker  = regexp.MustCompile(`^(?:[-*+]|\d+[.)])[ \t]+`)
	publicationDetailsOpen = regexp.MustCompile(`(?i)^<details(?:[ \t]+[^>]*)?>`)
)

// nestedPublicationAlertLine detects static nested alert markers in template
// sources without running Node or evaluating template placeholders.
func nestedPublicationAlertLine(source string, afterLine int) int {
	detailsDepth, listIndent := 0, 0
	lineNumber := 0
	var fence string
	for raw := range strings.Lines(source) {
		lineNumber++
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		inList := listIndent > 0 && indent >= listIndent
		if fence == "" {
			if marker := publicationListMarker.FindString(line); marker != "" && indent < 4 {
				listIndent = indent + len(marker)
				inList = true
				line = line[len(marker):]
			} else if !inList {
				listIndent = 0
			}
		}
		quotes := 0
		for strings.HasPrefix(line, ">") {
			quotes++
			line = strings.TrimSpace(line[1:])
		}
		if fence != "" {
			if isClosingCodeFence(line, fence) {
				fence = ""
			}
			continue
		}
		if indent >= 4 && !inList {
			continue
		}
		if _, ok := markdownCodeFence(line); ok {
			fence = line[:len(line)-len(strings.TrimLeft(line, line[:1]))]
			continue
		}
		if lineNumber > afterLine && quotes > 0 && (detailsDepth > 0 || inList || quotes > 1) && publicationAlertMarker.MatchString(line) {
			return lineNumber
		}
		if publicationDetailsOpen.MatchString(line) && !strings.Contains(strings.ToLower(line), "</details>") {
			detailsDepth++
		} else if detailsDepth > 0 && strings.EqualFold(line, "</details>") {
			detailsDepth--
		}
	}
	return 0
}

func (c *Compiler) warnNestedPublicationAlerts(config *SafeOutputsConfig, path string) {
	if config == nil {
		return
	}
	warn := func(field, source, prefix string) {
		afterLine := strings.Count(prefix, "\n")
		if line := nestedPublicationAlertLine(prefix+source, afterLine); line != 0 {
			message := fmt.Sprintf("%s (template line %d): GitHub alerts must be top-level blocks. Move the alert outside its container or use a labeled quote such as '> **Warning:** Check the run logs.'", field, line-afterLine)
			fmt.Fprintln(os.Stderr, formatCompilerMessage(path, "warning", message))
			c.IncrementWarningCount()
		}
	}
	var header string
	if messages := config.Messages; messages != nil {
		disclosure := messages.DisclosureHeader + "\n\n"
		header = disclosure + messages.BodyHeader
		warn("safe-outputs.messages.body-header", messages.BodyHeader, disclosure)
		for _, field := range []struct{ name, source string }{
			{"disclosure-header", messages.DisclosureHeader}, {"footer", messages.Footer},
			{"footer-install", messages.FooterInstall}, {"footer-workflow-recompile", messages.FooterWorkflowRecompile},
			{"footer-workflow-recompile-comment", messages.FooterWorkflowRecompileComment}, {"agent-failure-issue", messages.AgentFailureIssue},
			{"agent-failure-comment", messages.AgentFailureComment}, {"run-started", messages.RunStarted},
			{"run-success", messages.RunSuccess}, {"run-failure", messages.RunFailure}, {"detection-failure", messages.DetectionFailure},
			{"issue-created", messages.IssueCreated}, {"pull-request-created", messages.PullRequestCreated}, {"commit-pushed", messages.CommitPushed},
		} {
			warn("safe-outputs.messages."+field.name, field.source, "")
		}
	}
	warnFooter := func(field, footer string) {
		if footer == "" {
			return
		}
		prefix := header + "\n\n{body}\n\n"
		warn(field, footer, prefix)
	}
	warnFooter("safe-outputs.body-footer", config.BodyFooter)
	if config.CreateIssues != nil {
		warnFooter("safe-outputs.create-issue.body-footer", config.CreateIssues.BodyFooter)
	}
	if config.CreatePullRequests != nil {
		warnFooter("safe-outputs.create-pull-request.body-footer", config.CreatePullRequests.BodyFooter)
	}
}
