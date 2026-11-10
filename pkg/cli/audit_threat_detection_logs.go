package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Matches only the validated quota details recorded by release_integrity.sh.
var detectionQuotaDetailsPattern = regexp.MustCompile(`resource=[a-z_]{1,64}; limit=(?:[0-9]{1,16}|unknown); remaining=(?:[0-9]{1,16}|unknown); used=(?:[0-9]{1,16}|unknown); reset=(?:[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z|[0-9]{1,12}|unknown); reason=(?:reset_exceeds_budget|unknown_reset|renewed_exhaustion)`)

// readDetectionQuotaDetails returns the GitHub API quota details recorded by the
// detection job when AWF attestation verification stopped on quota exhaustion.
func readDetectionQuotaDetails(runDir string) string {
	if runDir == "" {
		return ""
	}
	for _, raw := range readJobLogLines(filepath.Join(runDir, "workflow-logs"), JobInfo{Name: "detection"}) {
		line := stripGHALogTimestamps(raw)
		if !strings.Contains(line, "GH_AW_GITHUB_API_QUOTA_BLOCKED") && !strings.Contains(line, "GitHub API quota exhausted") {
			continue
		}
		if details := detectionQuotaDetailsPattern.FindString(line); details != "" {
			return details
		}
	}
	return ""
}

// Older usage artifacts omit detection.result. Recover only complete, enumerated
// outcomes from the conclusion job's runner-emitted environment blocks.
func readConclusionDetectionUsageResult(runDir string) (detectionUsageResult, bool) {
	if runDir == "" {
		return detectionUsageResult{}, false
	}
	workflowLogsDir := filepath.Join(runDir, "workflow-logs")
	job := JobInfo{Name: "conclusion"}
	location := locateJobLogs(workflowLogsDir, job)
	if location.file == "" && location.dir == "" {
		name, ok := uniqueQualifiedConclusionLog(workflowLogsDir)
		if !ok {
			return detectionUsageResult{}, false
		}
		job.Name = name
		location = locateJobLogs(workflowLogsDir, job)
	}
	var lines []string
	if location.file != "" {
		// A nested directory may contain only system.txt, not the step headers.
		lines = readLogLines(location.file)
	} else {
		lines = readJobLogLines(workflowLogsDir, job)
	}
	var result, block detectionUsageResult
	found, inHeader, inEnv := false, false, false
	for _, raw := range lines {
		line := stripGHALogTimestamps(raw)
		switch {
		case strings.HasPrefix(line, "##[group]Run "):
			inHeader, inEnv = true, false
			block = detectionUsageResult{}
		case inHeader && strings.TrimSpace(line) == "env:":
			inEnv = true
		case line == "##[endgroup]":
			if inEnv && validDetectionJobResult(block.JobResult) && validDetectionConclusion(block.Conclusion) {
				if found && block != result {
					return detectionUsageResult{}, false
				}
				result, found = block, true
			}
			inHeader, inEnv = false, false
		case inEnv && strings.HasPrefix(line, "  GH_AW_DETECTION_"):
			key, value, ok := strings.Cut(strings.TrimSpace(line), ": ")
			if !ok {
				continue
			}
			switch key {
			case "GH_AW_DETECTION_JOB_RESULT":
				block.JobResult = value
			case "GH_AW_DETECTION_CONCLUSION":
				block.Conclusion = value
			case "GH_AW_DETECTION_REASON":
				switch value {
				case "threat_detected", "agent_failure", "parse_error", "detection_skipped", "github_api_quota_exhausted":
					block.Reason = value
				}
			}
		}
	}
	return result, found
}

// Never combine outcomes from different reusable-workflow invocations.
func uniqueQualifiedConclusionLog(workflowLogsDir string) (string, bool) {
	entries, err := os.ReadDir(workflowLogsDir)
	if err != nil {
		return "", false
	}
	names := make(map[string]struct{})
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		name := entry.Name()
		if !entry.IsDir() {
			number, jobName := parseStepFilename(name)
			if number <= 0 || !strings.HasSuffix(name, ".txt") {
				continue
			}
			name = jobName
		}
		if strings.HasSuffix(name, " _ conclusion") {
			names[name] = struct{}{}
		}
	}
	if len(names) != 1 {
		return "", false
	}
	for name := range names {
		return name, true
	}
	return "", false
}

func validDetectionConclusion(conclusion string) bool {
	return conclusion == "success" || conclusion == "failure" ||
		conclusion == "warning" || conclusion == "skipped"
}
