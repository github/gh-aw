package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
)

type unifiedDetectionResult struct {
	JobResult       *string `json:"jobResult"`
	Conclusion      *string `json:"conclusion"`
	Reason          *string `json:"reason"`
	PromptInjection *bool   `json:"promptInjection"`
	SecretLeak      *bool   `json:"secretLeak"`
	MaliciousPatch  *bool   `json:"maliciousPatch"`
}

// maxUnifiedSessionLineSize bounds JSONL record allocations while allowing large agent records.
const maxUnifiedSessionLineSize = 10 * maxScannerBufferSize

type unifiedDetectionEvent struct {
	Type       string          `json:"type"`
	Data       json.RawMessage `json:"data"`
	Provenance struct {
		Component string `json:"component"`
		Phase     string `json:"phase"`
	} `json:"provenance"`
}

func readUnifiedDetectionResult(runDir string) (*unifiedDetectionResult, error) {
	if runDir == "" {
		return nil, nil
	}
	usageDir := filepath.Join(runDir, constants.UsageArtifactName.String())
	sessionPath := filepath.Join(usageDir, "aw_session.jsonl")
	for _, path := range []string{usageDir, sessionPath} {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("failed to stat unified session source: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("unified session source is a symbolic link: %s", path)
		}
	}
	file, err := os.Open(sessionPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open unified session: %w", err)
	}
	defer file.Close()
	return parseUnifiedDetectionResult(file)
}

func parseUnifiedDetectionResult(input io.Reader) (*unifiedDetectionResult, error) {
	reader := bufio.NewReader(input)
	headerSeen := false
	var result *unifiedDetectionResult
	for lineNumber := 1; ; lineNumber++ {
		line, oversized, readErr := readUnifiedSessionLine(reader)
		if oversized {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Skipping oversized unified session record on line %d", lineNumber)))
		} else if line = bytes.TrimSpace(line); len(line) > 0 {
			var event unifiedDetectionEvent
			if err := json.Unmarshal(line, &event); err != nil {
				if !headerSeen {
					return nil, fmt.Errorf("invalid unified session header on line %d", lineNumber)
				}
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Skipping malformed unified session event on line %d", lineNumber)))
			} else if !headerSeen {
				if err := validateUnifiedDetectionHeader(event); err != nil {
					return nil, err
				}
				headerSeen = true
			} else {
				next, err := unifiedDetectionEventResult(event, lineNumber)
				if err != nil {
					return nil, err
				}
				if next != nil {
					if result != nil && !reflect.DeepEqual(result, next) {
						return nil, errors.New("conflicting unified session detection results")
					}
					result = next
				}
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return nil, fmt.Errorf("failed to read unified session: %w", readErr)
			}
			break
		}
	}
	if !headerSeen {
		return nil, errors.New("unified session is missing its leading session.format header")
	}
	return result, nil
}

func readUnifiedSessionLine(reader *bufio.Reader) ([]byte, bool, error) {
	var line []byte
	oversized := false
	for {
		fragment, err := reader.ReadSlice('\n')
		if !oversized {
			if len(line)+len(fragment) > maxUnifiedSessionLineSize {
				oversized = true
				line = nil
			} else {
				line = append(line, fragment...)
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return line, oversized, err
	}
}

func validateUnifiedDetectionHeader(event unifiedDetectionEvent) error {
	if event.Type != "session.format" || event.Provenance.Component != "collector" {
		return errors.New("unified session is missing its leading session.format header")
	}
	var format struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(event.Data, &format); err != nil || format.Version != 1 {
		return errors.New("unsupported unified session file-format version")
	}
	return nil
}

func unifiedDetectionEventResult(event unifiedDetectionEvent, lineNumber int) (*unifiedDetectionResult, error) {
	if event.Type == "session.format" && event.Provenance.Component == "collector" {
		return nil, errors.New("unified session contains multiple collector format headers")
	}
	if event.Type != "detection.result" || event.Provenance.Component != "detection" || event.Provenance.Phase != "detection" {
		return nil, nil
	}
	var result unifiedDetectionResult
	if !bytes.HasPrefix(bytes.TrimSpace(event.Data), []byte("{")) || json.Unmarshal(event.Data, &result) != nil {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Skipping malformed unified detection result on line %d", lineNumber)))
		return nil, nil
	}
	return &result, nil
}

type threatDetectionEvidence struct {
	Verdict    threatDetectionVerdict
	HasVerdict bool
	Result     detectionUsageResult
	HasResult  bool
}

func readThreatDetectionEvidence(runDir string) threatDetectionEvidence {
	unified, err := readUnifiedDetectionResult(runDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to parse unified threat detection results: %v", err)))
	}
	var evidence threatDetectionEvidence
	if unified != nil {
		auditThreatDetectionLog.Printf("Using unified-session detection evidence")
		// Older unified files contain verdict flags only; retain separately recorded job outcomes.
		legacyResult, _ := readLegacyDetectionUsageResult(runDir)
		evidence.Result = mergeUnifiedDetectionResult(unified, legacyResult)
		evidence.HasResult = validDetectionJobResult(evidence.Result.JobResult)
		evidence.Verdict, evidence.HasVerdict = validateThreatDetectionVerdict(rawThreatDetectionVerdict{
			PromptInjection: unified.PromptInjection,
			SecretLeak:      unified.SecretLeak,
			MaliciousPatch:  unified.MaliciousPatch,
		})
		return evidence
	}

	evidence.Verdict, evidence.HasVerdict = findLegacyThreatDetectionVerdict(runDir)
	evidence.Result, evidence.HasResult = readLegacyDetectionUsageResult(runDir)
	return evidence
}

func mergeUnifiedDetectionResult(unified *unifiedDetectionResult, legacy detectionUsageResult) detectionUsageResult {
	if unified.JobResult != nil {
		legacy.JobResult = *unified.JobResult
	}
	if unified.Conclusion != nil {
		legacy.Conclusion = *unified.Conclusion
	}
	if unified.Reason != nil {
		legacy.Reason = *unified.Reason
	}
	return legacy
}
