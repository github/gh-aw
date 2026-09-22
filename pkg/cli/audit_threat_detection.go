package cli

import (
	"bufio"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

const threatDetectionResultPrefix = "THREAT_DETECTION_RESULT:"

type threatDetectionVerdict struct {
	PromptInjection bool `json:"prompt_injection"`
	SecretLeak      bool `json:"secret_leak"`
	MaliciousPatch  bool `json:"malicious_patch"`
}

func generateThreatDetectionFindings(processedRun ProcessedRun) []AuditFinding {
	detectionJobFailed := false
	for _, job := range processedRun.JobDetails {
		if normalizeJobName(job.Name) == string(constants.DetectionJobName) &&
			strings.EqualFold(strings.TrimSpace(job.Conclusion), "failure") {
			detectionJobFailed = true
			break
		}
	}

	verdict, found := findThreatDetectionVerdict(processedRun.Run.LogsPath)
	threatKinds := verdict.threatKinds()
	findings := make([]AuditFinding, 0, 2)
	if detectionJobFailed {
		description := "The threat-detection job failed before producing a threat verdict"
		impact := "Safe outputs may have been blocked because the security control did not complete"
		if found {
			description = "The threat-detection job concluded with failure"
			impact = "Safe outputs were blocked by the failed security gate"
		}
		findings = append(findings, AuditFinding{
			Code:        AuditFindingDetectionJobFailed,
			Category:    "security",
			Severity:    "high",
			Title:       "Threat Detection Job Failed",
			Description: description,
			Impact:      impact,
		})
	}
	if found && len(threatKinds) > 0 {
		findings = append(findings, AuditFinding{
			Code:        AuditFindingThreatDetected,
			Category:    "security",
			Severity:    "critical",
			Title:       "Security Threat Detected",
			Description: "Threat detection identified: " + strings.Join(threatKinds, ", "),
			Impact:      "Agent output may be unsafe and should not be applied without review",
		})
	}
	return findings
}

func (v threatDetectionVerdict) threatKinds() []string {
	kinds := make([]string, 0, 3)
	if v.PromptInjection {
		kinds = append(kinds, "prompt injection")
	}
	if v.SecretLeak {
		kinds = append(kinds, "secret leak")
	}
	if v.MaliciousPatch {
		kinds = append(kinds, "malicious patch")
	}
	return kinds
}

func findThreatDetectionVerdict(runDir string) (threatDetectionVerdict, bool) {
	if runDir == "" {
		return threatDetectionVerdict{}, false
	}
	var resultFiles, logFiles []string
	_ = filepath.WalkDir(runDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		switch entry.Name() {
		case "detection_result.json":
			resultFiles = append(resultFiles, path)
		case "detection.log":
			logFiles = append(logFiles, path)
		}
		return nil
	})
	for _, path := range resultFiles {
		if verdict, ok := readThreatDetectionResult(path); ok {
			return verdict, true
		}
	}
	for _, path := range logFiles {
		if verdict, ok := scanThreatDetectionLog(path); ok {
			return verdict, true
		}
	}
	return threatDetectionVerdict{}, false
}

func readThreatDetectionResult(path string) (threatDetectionVerdict, bool) {
	file, err := os.Open(path)
	if err != nil {
		return threatDetectionVerdict{}, false
	}
	defer file.Close()

	var verdict threatDetectionVerdict
	if err := json.NewDecoder(io.LimitReader(file, 1024*1024)).Decode(&verdict); err != nil {
		return threatDetectionVerdict{}, false
	}
	return verdict, true
}

func scanThreatDetectionLog(path string) (threatDetectionVerdict, bool) {
	file, err := os.Open(path)
	if err != nil {
		return threatDetectionVerdict{}, false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		prefixIndex := strings.Index(line, threatDetectionResultPrefix)
		if prefixIndex < 0 {
			continue
		}
		var verdict threatDetectionVerdict
		if err := json.Unmarshal([]byte(line[prefixIndex+len(threatDetectionResultPrefix):]), &verdict); err == nil {
			return verdict, true
		}
	}
	if scanner.Err() != nil {
		return threatDetectionVerdict{}, false
	}
	return threatDetectionVerdict{}, false
}
