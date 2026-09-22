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
	"github.com/github/gh-aw/pkg/logger"
)

const threatDetectionResultPrefix = "THREAT_DETECTION_RESULT:"

var auditThreatDetectionLog = logger.New("cli:audit_threat_detection")

type threatDetectionVerdict struct {
	PromptInjection bool `json:"prompt_injection"`
	SecretLeak      bool `json:"secret_leak"`
	MaliciousPatch  bool `json:"malicious_patch"`
}

type rawThreatDetectionVerdict struct {
	PromptInjection *bool `json:"prompt_injection"`
	SecretLeak      *bool `json:"secret_leak"`
	MaliciousPatch  *bool `json:"malicious_patch"`
}

type detectionExecutionEvidence struct {
	Version    int    `json:"version"`
	Component  string `json:"component"`
	RunID      int64  `json:"run_id"`
	RunAttempt int    `json:"run_attempt"`
	State      string `json:"state"`
}

func generateThreatDetectionFindings(processedRun ProcessedRun) []AuditFinding {
	detectionJobFailed := false
	detectionJobSkipped := false
	for _, job := range processedRun.JobDetails {
		if normalizeJobName(job.Name) == string(constants.DetectionJobName) {
			switch strings.ToLower(strings.TrimSpace(job.Conclusion)) {
			case "failure":
				detectionJobFailed = true
			case "skipped":
				detectionJobSkipped = true
			}
		}
	}

	verdict, found := findThreatDetectionVerdict(processedRun.Run.LogsPath)
	detectionExecutionNotStarted := !detectionJobSkipped && threatDetectionExecutionNotStarted(processedRun.Run)
	threatKinds := verdict.threatKinds()
	findings := make([]AuditFinding, 0, 2)
	if detectionJobFailed || detectionExecutionNotStarted {
		description := "The threat-detection job failed before producing a threat verdict"
		impact := "Safe outputs may have been blocked because the security control did not complete"
		if detectionExecutionNotStarted && !detectionJobFailed {
			description = "Usage artifact evidence indicates the threat-detection job did not start"
			impact = "Safe outputs may have been blocked because the security control did not run"
		} else if found {
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
		if err != nil || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			if entry.Name() == "base" || strings.HasPrefix(entry.Name(), "baseline-") {
				return filepath.SkipDir
			}
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
	var foundVerdict threatDetectionVerdict
	found := false
	for _, path := range append(resultFiles, logFiles...) {
		var verdict threatDetectionVerdict
		var ok bool
		if filepath.Base(path) == "detection_result.json" {
			verdict, ok = readThreatDetectionResult(path)
		} else {
			verdict, ok = scanThreatDetectionLog(path)
		}
		if !ok {
			continue
		}
		if found && verdict != foundVerdict {
			auditThreatDetectionLog.Printf("Conflicting threat-detection verdicts: path=%s", path)
			return threatDetectionVerdict{}, false
		}
		foundVerdict = verdict
		found = true
	}
	return foundVerdict, found
}

func threatDetectionExecutionNotStarted(run WorkflowRun) bool {
	if run.LogsPath == "" {
		return false
	}
	path := filepath.Join(run.LogsPath, constants.UsageArtifactName.String(), string(constants.DetectionJobName), "execution.json")
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	var evidence detectionExecutionEvidence
	if err := json.NewDecoder(io.LimitReader(file, 1024*1024)).Decode(&evidence); err != nil {
		return false
	}
	if evidence.Version != 1 ||
		evidence.Component != string(constants.DetectionJobName) ||
		evidence.State != "not_started" {
		return false
	}
	if run.DatabaseID != 0 && evidence.RunID != 0 && evidence.RunID != run.DatabaseID {
		return false
	}
	if run.Attempt != 0 && evidence.RunAttempt != 0 && evidence.RunAttempt != run.Attempt {
		return false
	}
	return true
}

func hasThreatDetectionArtifact(runDir string) bool {
	if runDir == "" {
		return false
	}
	found := false
	_ = filepath.WalkDir(runDir, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			if entry.Name() == "base" || strings.HasPrefix(entry.Name(), "baseline-") {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() == "detection_result.json" || entry.Name() == "detection.log" {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}

func readThreatDetectionResult(path string) (threatDetectionVerdict, bool) {
	file, err := os.Open(path)
	if err != nil {
		return threatDetectionVerdict{}, false
	}
	defer file.Close()

	var raw rawThreatDetectionVerdict
	if err := json.NewDecoder(io.LimitReader(file, 1024*1024)).Decode(&raw); err != nil {
		return threatDetectionVerdict{}, false
	}
	return validateThreatDetectionVerdict(raw)
}

func validateThreatDetectionVerdict(raw rawThreatDetectionVerdict) (threatDetectionVerdict, bool) {
	if raw.PromptInjection == nil || raw.SecretLeak == nil || raw.MaliciousPatch == nil {
		return threatDetectionVerdict{}, false
	}
	return threatDetectionVerdict{
		PromptInjection: *raw.PromptInjection,
		SecretLeak:      *raw.SecretLeak,
		MaliciousPatch:  *raw.MaliciousPatch,
	}, true
}

func scanThreatDetectionLog(path string) (threatDetectionVerdict, bool) {
	file, err := os.Open(path)
	if err != nil {
		return threatDetectionVerdict{}, false
	}
	defer file.Close()

	var foundVerdict threatDetectionVerdict
	found := false
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		prefixIndex := strings.Index(line, threatDetectionResultPrefix)
		if prefixIndex < 0 {
			continue
		}
		var raw rawThreatDetectionVerdict
		if err := json.Unmarshal([]byte(line[prefixIndex+len(threatDetectionResultPrefix):]), &raw); err != nil {
			continue
		}
		verdict, ok := validateThreatDetectionVerdict(raw)
		if !ok {
			continue
		}
		if found && verdict != foundVerdict {
			auditThreatDetectionLog.Printf("Conflicting threat-detection verdicts in detection log: path=%s", path)
			return threatDetectionVerdict{}, false
		}
		foundVerdict = verdict
		found = true
	}
	if scanner.Err() != nil {
		return threatDetectionVerdict{}, false
	}
	return foundVerdict, found
}
