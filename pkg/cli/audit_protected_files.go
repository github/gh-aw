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
	"slices"
	"strings"
	"unicode"

	"github.com/github/gh-aw/pkg/console"
)

// AuditProtectedFiles reports recorded protection decisions, not a policy inventory.
type AuditProtectedFiles struct {
	Workflow        string                        `json:"workflow"`
	WorkflowPath    string                        `json:"workflow_path,omitempty"`
	Repository      string                        `json:"repository,omitempty"`
	Files           []string                      `json:"files"`
	Detections      []AuditProtectedFileDetection `json:"detections"`
	UnavailableRuns []int64                       `json:"unavailable_runs,omitempty"`
	Partial         bool                          `json:"partial,omitempty"`
}

// AuditProtectedFileDetection retains execution identity for each protected-file decision.
type AuditProtectedFileDetection struct {
	RunID                   int64    `json:"run_id"`
	Operation               string   `json:"operation"`
	Action                  string   `json:"action"`
	Source                  string   `json:"source,omitempty"`
	Status                  string   `json:"status,omitempty"`
	Files                   []string `json:"files"`
	FallbackUsed            bool     `json:"fallback_used,omitempty"`
	TargetPullRequestNumber int64    `json:"target_pull_request_number,omitempty"`
	URL                     string   `json:"url,omitempty"`
	EvidencePath            string   `json:"evidence_path,omitempty"`
}

// resolveAuditProtectedFiles preserves replayed evidence while retrying incomplete
// diagnostics when a local unified session becomes available.
func resolveAuditProtectedFiles(run WorkflowRun, cached *AuditProtectedFiles) *AuditProtectedFiles {
	if cached == nil {
		return readAuditProtectedFiles(run)
	}
	if (!cached.Partial && len(cached.UnavailableRuns) == 0) || run.LogsPath == "" {
		return cached
	}
	sessionPath, err := findUnifiedSessionPath(run.LogsPath)
	if err != nil || sessionPath == "" {
		return cached
	}
	return mergeAuditProtectedFileEvidence(cached, readAuditProtectedFiles(run))
}

func mergeAuditProtectedFileEvidence(cached, refreshed *AuditProtectedFiles) *AuditProtectedFiles {
	merged := *cached
	if merged.Workflow == "" {
		merged.Workflow = refreshed.Workflow
	}
	if merged.WorkflowPath == "" {
		merged.WorkflowPath = refreshed.WorkflowPath
	}
	if merged.Repository == "" {
		merged.Repository = refreshed.Repository
	}
	merged.Files = append(slices.Clone(cached.Files), refreshed.Files...)
	slices.Sort(merged.Files)
	merged.Files = slices.Compact(merged.Files)
	merged.Detections = []AuditProtectedFileDetection{}
	seen := map[auditProtectedFileDetectionIdentity]struct{}{}
	refreshedKeys := map[auditProtectedFileDetectionIdentity]struct{}{}
	recordedRuns := map[int64]struct{}{}
	for _, detection := range refreshed.Detections {
		refreshedKeys[auditProtectedFileDetectionKey(detection)] = struct{}{}
	}
	// A complete parse supersedes prior partial coverage only if it includes
	// every previously recorded decision and path.
	supersedes := !refreshed.Partial
	for _, detection := range cached.Detections {
		_, exists := refreshedKeys[auditProtectedFileDetectionKey(detection)]
		supersedes = supersedes && exists
	}
	for _, file := range cached.Files {
		supersedes = supersedes && slices.Contains(refreshed.Files, file)
	}
	for _, detections := range [][]AuditProtectedFileDetection{cached.Detections, refreshed.Detections} {
		for _, detection := range detections {
			key := auditProtectedFileDetectionKey(detection)
			if _, exists := seen[key]; !exists {
				seen[key] = struct{}{}
				merged.Detections = append(merged.Detections, detection)
			}
			recordedRuns[detection.RunID] = struct{}{}
		}
	}
	merged.Partial = refreshed.Partial || (cached.Partial && !supersedes)
	merged.UnavailableRuns = nil
	for _, runs := range [][]int64{cached.UnavailableRuns, refreshed.UnavailableRuns} {
		for _, runID := range runs {
			if _, recorded := recordedRuns[runID]; !recorded {
				merged.UnavailableRuns = append(merged.UnavailableRuns, runID)
			}
		}
	}
	slices.Sort(merged.UnavailableRuns)
	merged.UnavailableRuns = slices.Compact(merged.UnavailableRuns)
	return &merged
}

type auditProtectedFileDetectionIdentity struct {
	RunID                   int64
	Operation               string
	Action                  string
	Source                  string
	Status                  string
	Files                   string
	FallbackUsed            bool
	TargetPullRequestNumber int64
	URL                     string
}

func auditProtectedFileDetectionKey(detection AuditProtectedFileDetection) auditProtectedFileDetectionIdentity {
	detection.Files = slices.Clone(detection.Files)
	slices.Sort(detection.Files)
	detection.Files = slices.Compact(detection.Files)
	return auditProtectedFileDetectionIdentity{
		RunID: detection.RunID, Operation: detection.Operation, Action: detection.Action,
		Source: detection.Source, Status: detection.Status, Files: fmt.Sprintf("%q", detection.Files),
		FallbackUsed: detection.FallbackUsed, TargetPullRequestNumber: detection.TargetPullRequestNumber,
		URL: detection.URL,
	}
}

func readAuditProtectedFiles(run WorkflowRun) *AuditProtectedFiles {
	workflow := run.WorkflowName
	if run.WorkflowPath != "" {
		workflow = filepath.Base(run.WorkflowPath)
	}
	report := &AuditProtectedFiles{
		Workflow: workflow, WorkflowPath: run.WorkflowPath, Repository: run.Repository,
		Files: []string{}, Detections: []AuditProtectedFileDetection{},
	}
	if run.LogsPath == "" {
		report.UnavailableRuns = []int64{run.DatabaseID}
		return report
	}
	sessionPath, err := findUnifiedSessionPath(run.LogsPath)
	if err == nil && sessionPath != "" {
		report.Detections, report.Partial, err = readAuditProtectedFileSession(sessionPath, run.DatabaseID)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Protected-file audit evidence is incomplete: "+err.Error()))
		report.Partial = true
	}
	if len(report.Detections) == 0 {
		report.UnavailableRuns = []int64{run.DatabaseID}
	}
	for _, detection := range report.Detections {
		report.Files = append(report.Files, detection.Files...)
	}
	slices.Sort(report.Files)
	report.Files = slices.Compact(report.Files)
	return report
}

func readAuditProtectedFileSession(path string, runID int64) (detections []AuditProtectedFileDetection, partial bool, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, true, err
	}
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			partial, err = true, closeErr
		}
	}()
	return parseAuditProtectedFiles(file, runID)
}

func parseAuditProtectedFiles(input io.Reader, runID int64) ([]AuditProtectedFileDetection, bool, error) {
	limited := &io.LimitedReader{R: input, N: maxModelResolutionSessionBytes + 1}
	reader := bufio.NewReader(limited)
	detections := []AuditProtectedFileDetection{}
	seen := map[string]struct{}{}
	headerSeen, partial := false, false
	for lineNumber := 1; ; lineNumber++ {
		line, oversized, readErr := readUnifiedSessionLine(reader)
		if limited.N == 0 {
			return detections, true, errors.New("unified session exceeds the audit input limit")
		}
		if oversized {
			if !headerSeen {
				return detections, true, errors.New("oversized unified session header")
			}
			partial = true
		} else if line = bytes.TrimSpace(line); len(line) > 0 {
			detection, err := observeAuditProtectedEvent(line, runID, headerSeen)
			if err != nil {
				return detections, true, fmt.Errorf("invalid protection evidence on line %d: %w", lineNumber, err)
			}
			headerSeen = true
			if detection != nil {
				identity := *detection
				identity.EvidencePath = ""
				key, err := json.Marshal(identity)
				if err != nil {
					return detections, true, err
				}
				if _, exists := seen[string(key)]; !exists {
					seen[string(key)] = struct{}{}
					detections = append(detections, *detection)
				}
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return detections, true, errors.New("failed to read unified session protection evidence")
			}
			break
		}
	}
	if !headerSeen {
		return detections, true, errors.New("unified session is missing its leading session.format header")
	}
	return detections, partial, nil
}

func observeAuditProtectedEvent(line []byte, runID int64, headerSeen bool) (*AuditProtectedFileDetection, error) {
	var event unifiedDetectionEvent
	if json.Unmarshal(line, &event) != nil {
		return nil, errors.New("malformed unified session record")
	}
	if !headerSeen {
		return nil, validateUnifiedDetectionHeader(event)
	}
	if event.Type == "session.format" && event.Provenance.Component == "collector" {
		return nil, errors.New("unified session contains multiple collector format headers")
	}
	if event.Provenance.Component != "safe_output" || event.Provenance.Phase != "safe_outputs" ||
		(event.Type != "safe_output.result" && event.Type != "safe_output.outcome") {
		return nil, nil
	}
	return protectedFileDetection(event, runID)
}

type auditProtectedFileData struct {
	Type         string `json:"type"`
	Status       string `json:"status"`
	Staged       bool   `json:"staged"`
	FallbackUsed bool   `json:"fallback_used"`
	Target       int64  `json:"target_pull_request_number"`
	URL          string `json:"url"`
	Protection   *struct {
		Action string   `json:"action"`
		Source string   `json:"source"`
		Files  []string `json:"files"`
	} `json:"file_protection"`
}

func protectedFileDetection(event unifiedDetectionEvent, runID int64) (*AuditProtectedFileDetection, error) {
	var data auditProtectedFileData
	if err := json.Unmarshal(event.Data, &data); err != nil {
		return nil, err
	}
	if data.Protection == nil {
		return nil, nil
	}
	if data.Protection.Action != "deny" && data.Protection.Action != "fallback" {
		return nil, errors.New("unsupported protection action")
	}
	if data.Type == "" || len(data.Type) > 200 || strings.IndexFunc(data.Type, unicode.IsControl) >= 0 {
		return nil, errors.New("invalid protected operation")
	}
	files := []string{}
	for _, file := range data.Protection.Files {
		if !validAuditProtectedPath(file) {
			return nil, errors.New("invalid protected path")
		}
		files = append(files, file)
	}
	if len(files) == 0 {
		return nil, errors.New("protection diagnostic has no paths")
	}
	slices.Sort(files)
	files = slices.Compact(files)
	source := data.Protection.Source
	if source != "" && source != "allowlist" && source != "protected" && source != "post-apply" {
		return nil, errors.New("invalid protection source")
	}
	return &AuditProtectedFileDetection{
		RunID: runID, Operation: data.Type, Action: data.Protection.Action, Source: source,
		Status: data.Status, Files: files, URL: data.URL, TargetPullRequestNumber: data.Target,
		FallbackUsed: data.FallbackUsed && !data.Staged && (data.Status == "success" || data.Status == "warning"),
		EvidencePath: event.Provenance.Path,
	}, nil
}

func validAuditProtectedPath(file string) bool {
	if file == "" || len(file) > 4096 || strings.ContainsAny(file, `\:`) || strings.IndexFunc(file, unicode.IsControl) >= 0 {
		return false
	}
	for part := range strings.SplitSeq(file, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func mergeAuditProtectedFiles(reports []*AuditProtectedFiles) []*AuditProtectedFiles {
	result := []*AuditProtectedFiles{}
	byWorkflow := map[string]*AuditProtectedFiles{}
	for _, report := range reports {
		if report == nil {
			continue
		}
		identity := report.WorkflowPath
		if identity == "" {
			identity = report.Workflow
		}
		key := report.Repository + "\x00" + identity
		merged := byWorkflow[key]
		if merged == nil {
			merged = &AuditProtectedFiles{Workflow: report.Workflow, WorkflowPath: report.WorkflowPath, Repository: report.Repository, Files: []string{}, Detections: []AuditProtectedFileDetection{}}
			byWorkflow[key] = merged
			result = append(result, merged)
		}
		merged.Files = append(merged.Files, report.Files...)
		merged.Detections = append(merged.Detections, report.Detections...)
		merged.UnavailableRuns = append(merged.UnavailableRuns, report.UnavailableRuns...)
		merged.Partial = merged.Partial || report.Partial
	}
	for _, report := range result {
		slices.Sort(report.Files)
		report.Files = slices.Compact(report.Files)
		slices.Sort(report.UnavailableRuns)
		report.UnavailableRuns = slices.Compact(report.UnavailableRuns)
	}
	slices.SortFunc(result, func(a, b *AuditProtectedFiles) int {
		return strings.Compare(a.Repository+"\x00"+a.WorkflowPath+"\x00"+a.Workflow, b.Repository+"\x00"+b.WorkflowPath+"\x00"+b.Workflow)
	})
	return result
}

func renderAuditProtectedFiles(w io.Writer, reports []*AuditProtectedFiles, markdown bool) {
	for _, report := range reports {
		if report == nil {
			continue
		}
		if markdown {
			writeModelRoutingReportLine(w, "## Protected Files: %s\n\n", modelRoutingMarkdownCell(report.Workflow))
		} else {
			writeModelRoutingReportLine(w, "  protected_files: workflow=%s repository=%s files=%d detections=%d partial=%t\n", safeModelRoutingText(report.Workflow), safeModelRoutingText(report.Repository), len(report.Files), len(report.Detections), report.Partial)
		}
		for _, file := range report.Files {
			writeModelRoutingReportLine(w, "    - %s\n", modelRoutingMarkdownCell(file))
		}
		for _, detection := range report.Detections {
			writeModelRoutingReportLine(w, "    run=%d action=%s source=%s status=%s fallback_published=%t target_pr=%d\n", detection.RunID, safeModelRoutingText(detection.Action), safeModelRoutingText(detection.Source), safeModelRoutingText(detection.Status), detection.FallbackUsed, detection.TargetPullRequestNumber)
		}
		if len(report.UnavailableRuns) > 0 {
			writeModelRoutingReportLine(w, "Protection paths not recorded for run(s): %s; absence is not proof that no files were blocked.\n", formatRunIDList(report.UnavailableRuns))
		}
		if report.Partial {
			writeModelRoutingReportLine(w, "Protected-file audit evidence is incomplete; additional protection decisions may not be shown.\n")
		}
		if markdown {
			writeModelRoutingReportLine(w, "\n")
		}
	}
}
