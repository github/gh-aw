package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/workqueue"
)

var workQueueControlSummaryPattern = regexp.MustCompile(`^Work queue controls: [a-z_]{1,32}; [0-9]{1,9} receipts$`)

const (
	workQueueSnapshotFile = "work-queue.snapshot.json"
	workQueueFinishFile   = "work-queue.finish.jsonl"
)

// WorkQueueReport separates activation-time queue facts and agent intent
// from the operations observed in this run's workflow job logs.
type WorkQueueReport struct {
	Snapshot      *WorkQueueSnapshot       `json:"snapshot,omitempty"`
	FinishIntents []WorkQueueFinishReceipt `json:"finish_intents,omitempty"`
	Operations    []WorkQueueOperation     `json:"operations,omitempty"`
}

// WorkQueueSnapshot is a captured activation snapshot. Snapshots of other
// protocol versions report only their version; they are not replayed.
type WorkQueueSnapshot struct {
	Version int                       `json:"version"`
	SHA     *string                   `json:"sha"`
	Current *WorkQueueCurrentSnapshot `json:"current,omitempty"`
}

type WorkQueueOperation struct {
	Timestamp string `json:"timestamp,omitempty"`
	Message   string `json:"message"`
	Source    string `json:"source"`
}

func extractWorkQueueReport(runDir string) (*WorkQueueReport, error) {
	report := &WorkQueueReport{}
	data, err := readBoundedWorkQueueArtifact(filepath.Join(runDir, workQueueSnapshotFile), 162<<20)
	if err == nil {
		report.Snapshot, err = parseWorkQueueSnapshot(data)
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("failed to read work queue snapshot: %w", err)
	}
	if report.Snapshot != nil && report.Snapshot.Current != nil {
		report.FinishIntents, err = readCurrentWorkQueueFinishIntents(filepath.Join(runDir, workQueueFinishFile), report.Snapshot.Current)
		if err != nil {
			return nil, err
		}
	}
	report.Operations, err = extractWorkQueueOperations(filepath.Join(runDir, "workflow-logs"))
	if err != nil {
		return nil, err
	}
	if report.Snapshot == nil && len(report.FinishIntents) == 0 && len(report.Operations) == 0 {
		return nil, nil
	}
	return report, nil
}

func parseWorkQueueSnapshot(data []byte) (*WorkQueueSnapshot, error) {
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return nil, err
	}
	if header.Version != workqueue.Version {
		return &WorkQueueSnapshot{Version: header.Version}, nil
	}
	return parseCurrentWorkQueueSnapshot(data)
}

func extractWorkQueueOperations(logDir string) ([]WorkQueueOperation, error) {
	if !workflowRunLogsComplete(filepath.Dir(logDir)) {
		return nil, nil
	}
	var operations []WorkQueueOperation
	seen := make(map[string]struct{})
	err := filepath.WalkDir(logDir, func(path string, entry os.DirEntry, walkErr error) error {
		if errors.Is(walkErr, fs.ErrNotExist) && path == logDir {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		source, err := filepath.Rel(logDir, path)
		if err != nil {
			return err
		}
		if entry.IsDir() || !isWorkQueueStepLog(source) {
			return nil
		}
		stepOperations, err := readWorkQueueStepLog(path, filepath.ToSlash(filepath.Join("workflow-logs", source)), seen)
		operations = append(operations, stepOperations...)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to extract work queue operations: %w", err)
	}
	slices.SortStableFunc(operations, func(a, b WorkQueueOperation) int {
		at, _ := time.Parse(time.RFC3339Nano, a.Timestamp)
		bt, _ := time.Parse(time.RFC3339Nano, b.Timestamp)
		return at.Compare(bt)
	})
	return operations, nil
}

func readWorkQueueStepLog(path, source string, seen map[string]struct{}) (operations []WorkQueueOperation, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, errors.New("could not close the work queue step log"))
		}
	}()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxScannerBufferSize)
	for scanner.Scan() {
		timestamp, message, found := strings.Cut(strings.TrimSpace(scanner.Text()), " ")
		if !found {
			continue
		}
		if _, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
			continue
		}
		message = strings.TrimSpace(message)
		if strings.HasPrefix(message, "Work queue controls:") {
			// Only the trusted controls step may report control summaries.
			if _, step, found := strings.Cut(filepath.Base(source), "_"); !found || step != "Process trusted work queue controls.txt" ||
				!workQueueControlSummaryPattern.MatchString(message) {
				continue
			}
		} else if !strings.HasPrefix(message, "Work queue activation: ") && !strings.HasPrefix(message, "Work queue finish: ") {
			continue
		}
		key := timestamp + "\n" + message
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		if len(seen) > 256 {
			return nil, errors.New("work queue operation report exceeds 256 entries")
		}
		operations = append(operations, WorkQueueOperation{
			Timestamp: timestamp, Message: boundWorkQueueDisplay(message, 512), Source: source,
		})
	}
	return operations, scanner.Err()
}

func backfillWorkQueueReport(report **WorkQueueReport, runDir string) bool {
	extracted, err := extractWorkQueueReport(runDir)
	if err != nil {
		logsOrchestratorLog.Printf("Failed to extract work queue report in %s: %v", runDir, err)
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(err.Error()))
		hadReport := *report != nil
		*report = nil
		return hadReport
	}

	// Rebuild operations from queue-owned step logs rather than trusting
	// earlier summaries that may contain agent stdout or incomplete downloads.
	changed := false
	if *report != nil && !slices.Equal((*report).Operations, operationsFromWorkQueueReport(extracted)) {
		(*report).Operations = operationsFromWorkQueueReport(extracted)
		changed = true
	}
	return mergeWorkQueueReport(report, extracted) || changed
}

func operationsFromWorkQueueReport(report *WorkQueueReport) []WorkQueueOperation {
	if report == nil {
		return nil
	}
	return report.Operations
}

func mergeWorkQueueReport(report **WorkQueueReport, extracted *WorkQueueReport) bool {
	if extracted == nil {
		return false
	}
	if *report == nil {
		*report = extracted
		return true
	}
	changed := false
	if extracted.Snapshot != nil && !equalWorkQueueSnapshots((*report).Snapshot, extracted.Snapshot) {
		(*report).Snapshot = extracted.Snapshot
		changed = true
	}
	if !slices.Equal((*report).FinishIntents, extracted.FinishIntents) {
		(*report).FinishIntents = extracted.FinishIntents
		changed = true
	}
	if len((*report).Operations) == 0 && len(extracted.Operations) > 0 {
		(*report).Operations = extracted.Operations
		changed = true
	}
	return changed
}

// equalWorkQueuePointers compares pointed-to values; nil equals only nil.
func equalWorkQueuePointers[T comparable](left, right *T) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

// equalWorkQueueSlices compares elements and keeps nil distinct from empty,
// matching what a cached report round-trips to.
func equalWorkQueueSlices[S ~[]E, E comparable](left, right S) bool {
	return (left == nil) == (right == nil) && slices.Equal(left, right)
}

func equalWorkQueueSnapshots(left, right *WorkQueueSnapshot) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Version == right.Version && equalWorkQueuePointers(left.SHA, right.SHA) &&
		equalCurrentWorkQueueSnapshots(left.Current, right.Current)
}

func equalCurrentWorkQueueSnapshots(left, right *WorkQueueCurrentSnapshot) bool {
	if left == nil || right == nil {
		return left == right
	}
	if left.Role != right.Role || left.Tip != right.Tip || left.CommitCount != right.CommitCount ||
		!equalWorkQueueDiagnostics(left.Diagnostics, right.Diagnostics) {
		return false
	}
	a, b := left.Assignment, right.Assignment
	if a == nil || b == nil {
		return a == b
	}
	return a.DispatchID == b.DispatchID && a.State == b.State && equalWorkQueuePointers(a.Run, b.Run) &&
		equalWorkQueueSlices(a.Claims, b.Claims)
}

func isWorkQueueStepLog(source string) bool {
	parts := strings.Split(filepath.ToSlash(source), "/")
	if len(parts) != 2 {
		return false
	}
	job, file := parts[0], parts[1] //nolint:uncheckedsliceindex // length checked above.
	number, name, found := strings.Cut(file, "_")
	if !found || number == "" || strings.Trim(number, "0123456789") != "" {
		return false
	}
	// Reusable workflows prefix job names with their caller job identity.
	jobMatches := func(base string) bool {
		return job == base || strings.HasSuffix(job, " _ "+base)
	}
	switch name {
	case "Snapshot work queue state.txt":
		return jobMatches("activation")
	case "Finish work queue claim.txt", "Process trusted work queue controls.txt", "Process Safe Outputs.txt":
		return jobMatches("safe_outputs")
	case "Collect work queue intents.txt", "Copy work queue claim finish intent.txt":
		return jobMatches("agent")
	default:
		return false
	}
}

func workQueueEvidenceRequested(sets []string) bool {
	return len(sets) == 0 || slices.Contains(sets, string(ArtifactSetAll)) || slices.Contains(sets, string(ArtifactSetWorkQueue))
}

func workQueueLogsMissing(requested bool, runDir string) bool {
	return requested && !workflowRunLogsComplete(runDir)
}

// Work Queue diagnostics need workflow logs even when artifacts are cached.
func ensureWorkQueueLogs(ctx context.Context, opts downloadArtifactsOptions) downloadArtifactsOptions {
	opts.includeWorkQueue = opts.includeWorkQueue || len(opts.artifactFilter) == 0
	if workQueueLogsMissing(opts.includeWorkQueue, opts.outputDir) {
		downloadWorkflowRunLogsForDiagnostics(ctx, opts)
	}
	return opts
}
