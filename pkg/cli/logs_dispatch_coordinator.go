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
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"
)

const (
	dispatchCoordinatorSnapshotFile = "dispatch-work-coordinator.snapshot.json"
	dispatchCoordinatorFinishFile   = "dispatch-work-coordinator.finish.jsonl"
)

// DispatchCoordinatorReport separates activation-time queue facts and agent intent
// from the operations observed in this run's workflow job logs.
type DispatchCoordinatorReport struct {
	Snapshot     *DispatchCoordinatorSnapshot   `json:"snapshot,omitempty"`
	FinishIntent string                         `json:"finish_intent,omitempty"`
	Operations   []DispatchCoordinatorOperation `json:"operations,omitempty"`
}

type DispatchCoordinatorSnapshot struct {
	Version      int                              `json:"version"`
	SHA          *string                          `json:"sha"`
	Worker       *DispatchCoordinatorWorker       `json:"worker"`
	Transactions []DispatchCoordinatorTransaction `json:"transactions"`
}

type DispatchCoordinatorWorker struct {
	WorkID  string `json:"work_id"`
	ClaimID string `json:"claim_id"`
}

// These fields follow the runtime coordinator protocol, not pkg/workqueue's
// independent CLI queue protocol.
type DispatchCoordinatorTransaction struct {
	Version int     `json:"version"`
	Kind    string  `json:"kind"`
	Work    string  `json:"work"`
	Claim   *string `json:"claim"`
	Attempt *string `json:"attempt"`
}

type DispatchCoordinatorOperation struct {
	Timestamp string `json:"timestamp,omitempty"`
	Message   string `json:"message"`
	Source    string `json:"source"`
}

func extractDispatchCoordinatorReport(runDir string) (*DispatchCoordinatorReport, error) {
	report := &DispatchCoordinatorReport{}
	snapshotPath := filepath.Join(runDir, dispatchCoordinatorSnapshotFile)
	data, err := os.ReadFile(snapshotPath)
	if err == nil {
		report.Snapshot, err = parseDispatchCoordinatorSnapshot(data)
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read dispatch coordinator snapshot: %w", err)
	}

	report.FinishIntent, err = readDispatchCoordinatorFinishIntent(filepath.Join(runDir, dispatchCoordinatorFinishFile))
	if err != nil {
		return nil, err
	}
	report.Operations, err = extractDispatchCoordinatorOperations(filepath.Join(runDir, "workflow-logs"))
	if err != nil {
		return nil, err
	}
	if report.Snapshot == nil && report.FinishIntent == "" && len(report.Operations) == 0 {
		return nil, nil
	}
	return report, nil
}

func parseDispatchCoordinatorSnapshot(data []byte) (*DispatchCoordinatorSnapshot, error) {
	var snapshot struct {
		Version        int                        `json:"version"`
		SHA            *string                    `json:"sha"`
		Worker         *DispatchCoordinatorWorker `json:"worker"`
		TransactionLog *string                    `json:"transactionLog"`
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, err
	}
	if snapshot.Version != 2 || snapshot.TransactionLog == nil ||
		(snapshot.SHA != nil && *snapshot.SHA == "") ||
		(snapshot.Worker != nil && (snapshot.Worker.WorkID == "" || snapshot.Worker.ClaimID == "")) {
		return nil, errors.New("invalid dispatch coordinator snapshot")
	}
	transactions := make([]DispatchCoordinatorTransaction, 0)
	for i, line := range strings.Split(*snapshot.TransactionLog, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		transaction, err := parseDispatchCoordinatorTransaction([]byte(line))
		if err != nil {
			return nil, fmt.Errorf("transaction line %d: %w", i+1, err)
		}
		transactions = append(transactions, transaction)
	}
	return &DispatchCoordinatorSnapshot{
		Version: snapshot.Version, SHA: snapshot.SHA, Worker: snapshot.Worker, Transactions: transactions,
	}, nil
}

func parseDispatchCoordinatorTransaction(data []byte) (DispatchCoordinatorTransaction, error) {
	var tx DispatchCoordinatorTransaction
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

func readDispatchCoordinatorFinishIntent(path string) (string, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to read dispatch coordinator finish intent: %w", err)
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
			return "", errors.New("conflicting dispatch coordinator finish intents")
		}
		outcome = value
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to scan dispatch coordinator finish intents: %w", err)
	}
	return outcome, nil
}

func extractDispatchCoordinatorOperations(logDir string) ([]DispatchCoordinatorOperation, error) {
	var operations []DispatchCoordinatorOperation
	seen := make(map[string]struct{})
	err := filepath.WalkDir(logDir, func(path string, entry os.DirEntry, walkErr error) error {
		if os.IsNotExist(walkErr) && path == logDir {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".txt" {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64*1024), maxScannerBufferSize)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			timestamp, message, found := strings.Cut(line, " ")
			if !found {
				continue
			}
			if _, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
				continue
			}
			message = strings.TrimSpace(message)
			if !strings.HasPrefix(message, "Dispatch coordinator: ") &&
				!strings.HasPrefix(message, "Dispatch work claim reconciliation") &&
				!strings.HasPrefix(message, "Captured dispatch coordinator snapshot (") {
				continue
			}
			// GitHub's archive contains both whole-job and per-step copies.
			key := timestamp + "\n" + message
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			source, err := filepath.Rel(filepath.Dir(logDir), path)
			if err != nil {
				return err
			}
			operations = append(operations, DispatchCoordinatorOperation{
				Timestamp: timestamp, Message: message, Source: filepath.ToSlash(source),
			})
		}
		return scanner.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("failed to extract dispatch coordinator operations: %w", err)
	}
	slices.SortStableFunc(operations, func(a, b DispatchCoordinatorOperation) int {
		at, _ := time.Parse(time.RFC3339Nano, a.Timestamp)
		bt, _ := time.Parse(time.RFC3339Nano, b.Timestamp)
		return at.Compare(bt)
	})
	return operations, nil
}

func backfillDispatchCoordinatorReport(report **DispatchCoordinatorReport, runDir string) bool {
	if *report != nil && (*report).Snapshot != nil && (*report).FinishIntent != "" && len((*report).Operations) > 0 {
		return false
	}
	extracted, err := extractDispatchCoordinatorReport(runDir)
	if err != nil {
		logsOrchestratorLog.Printf("Failed to extract dispatch coordinator report in %s: %v", runDir, err)
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(err.Error()))
		return false
	}

	return mergeDispatchCoordinatorReport(report, extracted)
}

func mergeDispatchCoordinatorReport(report **DispatchCoordinatorReport, extracted *DispatchCoordinatorReport) bool {
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

func dispatchCoordinatorArtifactsRequested(filter []string) bool {
	return slices.Contains(filter, constants.ActivationArtifactName.String()) &&
		slices.Contains(filter, constants.AgentArtifactName.String())
}

func dispatchCoordinatorLogsMissing(filter []string, runDir string) bool {
	return dispatchCoordinatorArtifactsRequested(filter) &&
		!fileutil.DirExists(filepath.Join(runDir, "workflow-logs"))
}

// Coordinator diagnostics need workflow logs even when artifacts are cached.
func ensureDispatchCoordinatorLogs(ctx context.Context, opts downloadArtifactsOptions) {
	if dispatchCoordinatorLogsMissing(opts.artifactFilter, opts.outputDir) {
		downloadWorkflowRunLogsForDiagnostics(ctx, opts)
	}
}
