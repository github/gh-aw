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
	"regexp"
	"strconv"
)

// DurableSessionReport separates validated restoration from observed native startup.
type DurableSessionReport struct {
	Status      string `json:"status"`
	Engine      string `json:"engine,omitempty"`
	Storage     string `json:"storage,omitempty"`
	ParentRunID string `json:"parent_run_id,omitempty"`
	ArtifactID  string `json:"artifact_id,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
	Started     bool   `json:"started"`
	Evidence    string `json:"evidence,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

type durableSessionRecord struct {
	Engine      string `json:"engine"`
	Storage     string `json:"storage"`
	RunID       string `json:"runId"`
	RunAttempt  string `json:"runAttempt"`
	Status      string `json:"status"`
	ParentRunID string `json:"parentRunId"`
	ArtifactID  string `json:"artifactId"`
	SessionID   string `json:"sessionId"`
}

var durableSessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_-]{0,127}$`)
var durableNumericIDPattern = regexp.MustCompile(`^[1-9][0-9]*$`)

func hasDurableRestoreStep(jobs []JobInfoWithDuration) bool {
	for _, job := range jobs {
		for _, step := range job.Steps {
			if step.Name == "Restore durable workflow state" {
				return true
			}
		}
	}
	return false
}

func readDurableSessionReport(runDir string, run WorkflowRun, configured bool) *DurableSessionReport {
	unavailable := func(reason string) *DurableSessionReport {
		return &DurableSessionReport{Status: "unavailable", Reason: reason}
	}
	for _, relative := range []string{"usage/aw_session.jsonl", "aw_session.jsonl"} {
		file := filepath.Join(runDir, filepath.FromSlash(relative))
		info, err := os.Lstat(file)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err == nil {
			err = validateSubagentSessionSource(file)
		}
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxModelResolutionSessionBytes {
			return unavailable("invalid_session_source")
		}
		input, err := os.Open(file)
		if err != nil {
			return unavailable("unreadable_session")
		}
		report, err := parseDurableSessionReport(input, run)
		err = errors.Join(err, input.Close())
		if err != nil {
			healthLog.Printf("Durable session evidence unavailable for run %d: %v", run.DatabaseID, err)
			return unavailable("invalid_session")
		}
		if report != nil {
			report.Evidence = relative
			return report
		}
		if configured {
			return unavailable("durable_events_not_recorded")
		}
		return nil
	}
	if configured {
		return unavailable("session_not_cached")
	}
	return nil
}

func parseDurableSessionReport(input io.Reader, run WorkflowRun) (*DurableSessionReport, error) {
	limited := &io.LimitedReader{R: input, N: maxModelResolutionSessionBytes + 1}
	reader := bufio.NewReader(limited)
	var restore, startup *durableSessionRecord
	headerSeen := false
	for lineNumber := 1; ; lineNumber++ {
		line, oversized, readErr := readUnifiedSessionLine(reader)
		if limited.N == 0 {
			return nil, errors.New("unified session exceeds durable evidence input limit")
		}
		if oversized {
			return nil, errors.New("oversized unified session record")
		}
		if line = bytes.TrimSpace(line); len(line) > 0 {
			var event unifiedDetectionEvent
			if err := json.Unmarshal(line, &event); err != nil {
				return nil, fmt.Errorf("invalid unified session record on line %d", lineNumber)
			}
			if !headerSeen {
				if err := validateUnifiedDetectionHeader(event); err != nil {
					return nil, err
				}
				headerSeen = true
			} else if (event.Type == "durable.restore" || event.Type == "durable.session") &&
				event.Provenance.Component == "durable" && event.Provenance.Phase == "agent" &&
				(event.Provenance.Path == "agent/durable-session.jsonl" || event.Provenance.Path == "usage/agent/durable-session.jsonl") {
				var record durableSessionRecord
				if err := json.Unmarshal(event.Data, &record); err != nil || !validDurableSessionRecord(record, event.Type) {
					return nil, errors.New("invalid durable session metadata")
				}
				attempt := max(run.Attempt, 1)
				if record.RunID != strconv.FormatInt(run.DatabaseID, 10) || record.RunAttempt != strconv.Itoa(attempt) {
					return nil, errors.New("durable session metadata belongs to another run or attempt")
				}
				target := &restore
				if event.Type == "durable.session" {
					target = &startup
				}
				if *target != nil && **target != record {
					return nil, errors.New("conflicting durable session metadata")
				}
				*target = &record
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return nil, readErr
		}
	}
	if !headerSeen {
		return nil, errors.New("missing unified session header")
	}
	return durableSessionReportFromRecords(restore, startup)
}

func durableSessionReportFromRecords(restore, startup *durableSessionRecord) (*DurableSessionReport, error) {
	if restore == nil {
		if startup != nil {
			return nil, errors.New("native durable startup has no restoration evidence")
		}
		return nil, nil
	}
	report := &DurableSessionReport{Status: restore.Status, Engine: restore.Engine, Storage: restore.Storage,
		ParentRunID: restore.ParentRunID, ArtifactID: restore.ArtifactID}
	if restore.Status == "failed" {
		report.Status = "restore_failed"
	}
	if startup != nil {
		if restore.Status == "failed" || startup.Engine != restore.Engine || startup.ParentRunID != restore.ParentRunID ||
			(restore.Status == "restored") != (startup.Status == "resumed") {
			return nil, errors.New("native startup does not match restored durable state")
		}
		report.Status, report.Started, report.SessionID = startup.Status, true, startup.SessionID
	} else if restore.Status != "failed" {
		report.Reason = "native_start_not_observed"
	}
	return report, nil
}

func validDurableSessionRecord(record durableSessionRecord, eventType string) bool {
	if !modelResolutionEnginePattern.MatchString(record.Engine) || record.Storage != "artifact" ||
		!durableNumericIDPattern.MatchString(record.RunID) || !durableNumericIDPattern.MatchString(record.RunAttempt) {
		return false
	}
	for _, id := range []string{record.ParentRunID, record.ArtifactID} {
		if id != "" && !durableNumericIDPattern.MatchString(id) {
			return false
		}
	}
	if eventType == "durable.session" {
		return (record.Status == "resumed" || record.Status == "fresh") && durableSessionIDPattern.MatchString(record.SessionID)
	}
	return record.Status == "fresh" || record.Status == "failed" ||
		(record.Status == "restored" && record.ParentRunID != "" && record.ArtifactID != "")
}

func renderDurableSession(w io.Writer, report *DurableSessionReport) error {
	if report == nil {
		return nil
	}
	_, err := fmt.Fprintf(w, "[durable_session] status=%s started=%t engine=%q parent_run_id=%q artifact_id=%q session_id=%q evidence=%q reason=%q\n",
		report.Status, report.Started, report.Engine, report.ParentRunID, report.ArtifactID, report.SessionID, report.Evidence, report.Reason)
	return err
}
