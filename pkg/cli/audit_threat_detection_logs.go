package cli

import (
	"os"
	"path/filepath"
	"strings"
)

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
				case "threat_detected", "agent_failure", "parse_error", "detection_skipped":
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
	names := make(map[string]bool)
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
			names[name] = true
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
