package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
)

const (
	workQueueSnapshotFile       = "work-queue.snapshot.json"
	workQueueFinishFile         = "work-queue.finish.jsonl"
	legacyWorkQueueSnapshotFile = "dispatch-work-coordinator.snapshot.json"
	legacyWorkQueueFinishFile   = "dispatch-work-coordinator.finish.jsonl"
)

// WorkQueueReport separates activation-time queue facts and agent intent
// from the operations observed in this run's workflow job logs.
type WorkQueueReport struct {
	Snapshot     *WorkQueueSnapshot   `json:"snapshot,omitempty"`
	FinishIntent string               `json:"finish_intent,omitempty"`
	Operations   []WorkQueueOperation `json:"operations,omitempty"`
}

type WorkQueueSnapshot struct {
	Version      int                    `json:"version"`
	SHA          *string                `json:"sha"`
	Worker       *WorkQueueWorker       `json:"worker"`
	Transactions []WorkQueueTransaction `json:"transactions"`
}

type WorkQueueWorker struct {
	WorkID  string `json:"work_id"`
	ClaimID string `json:"claim_id"`
}

// These fields follow the runtime queue protocol, not pkg/workqueue's
// independent CLI queue protocol.
type WorkQueueTransaction struct {
	Version int     `json:"version"`
	Kind    string  `json:"kind"`
	Work    string  `json:"work"`
	Claim   *string `json:"claim"`
	Attempt *string `json:"attempt"`
}

type WorkQueueOperation struct {
	Timestamp string `json:"timestamp,omitempty"`
	Message   string `json:"message"`
	Source    string `json:"source"`
}

func extractWorkQueueReport(runDir string) (*WorkQueueReport, error) {
	report := &WorkQueueReport{}
	snapshotPath, err := workQueueArtifactPath(runDir, workQueueSnapshotFile, legacyWorkQueueSnapshotFile)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(snapshotPath)
	if err == nil {
		report.Snapshot, err = parseWorkQueueSnapshot(data)
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read work queue snapshot: %w", err)
	}

	finishPath, err := workQueueArtifactPath(runDir, workQueueFinishFile, legacyWorkQueueFinishFile)
	if err != nil {
		return nil, err
	}
	report.FinishIntent, err = readWorkQueueFinishIntent(finishPath)
	if err != nil {
		return nil, err
	}
	report.Operations, err = extractWorkQueueOperations(filepath.Join(runDir, "workflow-logs"))
	if err != nil {
		return nil, err
	}
	if report.Snapshot == nil && report.FinishIntent == "" && len(report.Operations) == 0 {
		return nil, nil
	}
	return report, nil
}

func workQueueArtifactPath(runDir, current, legacy string) (string, error) {
	path := filepath.Join(runDir, current)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return filepath.Join(runDir, legacy), nil
		}
		return "", fmt.Errorf("failed to inspect work queue artifact: %w", err)
	}
	return path, nil
}

func parseWorkQueueSnapshot(data []byte) (*WorkQueueSnapshot, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for _, field := range []string{"version", "sha", "worker", "transactionLog"} {
		if _, present := fields[field]; !present {
			return nil, fmt.Errorf("work queue snapshot is missing %q", field)
		}
	}
	var snapshot struct {
		Version        int              `json:"version"`
		SHA            *string          `json:"sha"`
		Worker         *WorkQueueWorker `json:"worker"`
		TransactionLog *string          `json:"transactionLog"`
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, err
	}
	if snapshot.Version != 2 || snapshot.TransactionLog == nil ||
		(snapshot.SHA != nil && *snapshot.SHA == "") ||
		(snapshot.Worker != nil && (snapshot.Worker.WorkID == "" || snapshot.Worker.ClaimID == "")) {
		return nil, errors.New("invalid work queue snapshot")
	}
	transactions := make([]WorkQueueTransaction, 0)
	for i, line := range strings.Split(*snapshot.TransactionLog, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		transaction, err := parseWorkQueueTransaction([]byte(line))
		if err != nil {
			return nil, fmt.Errorf("transaction line %d: %w", i+1, err)
		}
		transactions = append(transactions, transaction)
	}
	return &WorkQueueSnapshot{
		Version: snapshot.Version, SHA: snapshot.SHA, Worker: snapshot.Worker, Transactions: transactions,
	}, nil
}

func parseWorkQueueTransaction(data []byte) (WorkQueueTransaction, error) {
	var tx WorkQueueTransaction
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return tx, err
	}
	if fields == nil {
		return tx, errors.New("transaction must be a JSON object")
	}
	// The runtime upgrades unversioned historical records to version 1.
	_, hasVersion := fields["version"]
	legacy := !hasVersion || strings.TrimSpace(string(fields["version"])) == "0"
	if !hasVersion {
		fields["version"] = json.RawMessage("1")
	}
	if len(fields) != 5 || fields["kind"] == nil || fields["work"] == nil || fields["claim"] == nil || fields["attempt"] == nil {
		return tx, errors.New("transaction must contain version, kind, work, claim, and attempt")
	}
	if err := json.Unmarshal(data, &tx); err != nil {
		return tx, err
	}
	if legacy {
		tx.Version = 1
	}
	if tx.Version != 1 || tx.Work == "" ||
		(tx.Claim != nil && *tx.Claim == "") || (tx.Attempt != nil && *tx.Attempt == "") {
		return tx, errors.New("invalid transaction version or identifiers")
	}
	valid := false
	switch tx.Kind {
	case "Work", "WorkCancellation":
		valid = tx.Claim == nil && tx.Attempt == nil
	case "Claim", "ClaimCancellation":
		valid = tx.Claim != nil && tx.Attempt == nil
	case "Completion":
		valid = tx.Claim != nil && tx.Attempt != nil
	}
	if !valid {
		return tx, fmt.Errorf("invalid %q transaction", tx.Kind)
	}
	return tx, nil
}

func readWorkQueueFinishIntent(path string) (string, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to read work queue finish intent: %w", err)
	}
	defer file.Close()

	outcome := ""
	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var intent map[string]string
		if err := json.Unmarshal(scanner.Bytes(), &intent); err != nil {
			return "", fmt.Errorf("finish intent line %d: %w", line, err)
		}
		value := intent["outcome"]
		if len(intent) != 1 || !slices.Contains([]string{"completed", "cancelled"}, value) {
			return "", fmt.Errorf("invalid finish intent at line %d", line)
		}
		if outcome != "" && outcome != value {
			return "", errors.New("conflicting work queue finish intents")
		}
		outcome = value
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to scan work queue finish intents: %w", err)
	}
	return outcome, nil
}

func extractWorkQueueOperations(logDir string) ([]WorkQueueOperation, error) {
	if !workflowRunLogsComplete(filepath.Dir(logDir)) {
		return nil, nil
	}
	var operations []WorkQueueOperation
	seen := make(map[string]struct{})
	err := filepath.WalkDir(logDir, func(path string, entry os.DirEntry, walkErr error) error {
		if os.IsNotExist(walkErr) && path == logDir {
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

func readWorkQueueStepLog(path, source string, seen map[string]struct{}) ([]WorkQueueOperation, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var operations []WorkQueueOperation
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
		if !strings.HasPrefix(message, "Work queue: ") &&
			!strings.HasPrefix(message, "Work queue claim reconciliation") &&
			!strings.HasPrefix(message, "Captured work queue snapshot (") &&
			!strings.HasPrefix(message, "Dispatch coordinator: ") &&
			!strings.HasPrefix(message, "Dispatch work claim reconciliation") &&
			!strings.HasPrefix(message, "Captured dispatch coordinator snapshot (") {
			continue
		}
		key := timestamp + "\n" + message
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		operations = append(operations, WorkQueueOperation{
			Timestamp: timestamp, Message: message, Source: source,
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
	if (*report).Snapshot == nil && extracted.Snapshot != nil {
		(*report).Snapshot = extracted.Snapshot
		changed = true
	}
	if (*report).FinishIntent == "" && extracted.FinishIntent != "" {
		(*report).FinishIntent = extracted.FinishIntent
		changed = true
	}
	if len((*report).Operations) == 0 && len(extracted.Operations) > 0 {
		(*report).Operations = extracted.Operations
		changed = true
	}
	return changed
}

func isWorkQueueStepLog(source string) bool {
	parts := strings.Split(filepath.ToSlash(source), "/")
	if len(parts) != 2 {
		return false
	}
	var job, file string
	for _, part := range parts {
		if job == "" {
			job = part
		} else {
			file = part
		}
	}
	number, name, found := strings.Cut(file, "_")
	if !found || number == "" || strings.Trim(number, "0123456789") != "" {
		return false
	}
	// Reusable workflows prefix job names with their caller job identity.
	jobMatches := func(base string) bool {
		return job == base || strings.HasSuffix(job, " _ "+base)
	}
	switch name {
	case "Snapshot work queue state.txt", "Snapshot dispatch coordinator state.txt":
		return jobMatches("activation")
	case "Reconcile work queue claim.txt", "Reconcile dispatch work claim.txt":
		return jobMatches("safe_outputs")
	case "Copy work queue claim finish intent.txt", "Copy dispatch claim finish intent.txt":
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
