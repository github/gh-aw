package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/workqueue"
)

// WorkQueueCurrentSnapshot describes validated activation-time snapshot facts,
// not a live branch read. Receipts omit payloads and effect resources.
type WorkQueueCurrentSnapshot struct {
	Role        string                      `json:"role"`
	Tip         string                      `json:"tip"`
	CommitCount int                         `json:"commit_count"`
	Assignment  *WorkQueueAssignmentReceipt `json:"assignment,omitempty"`
	Diagnostics *WorkQueueDiagnostics       `json:"diagnostics,omitempty"`
}

// WorkQueueAssignmentReceipt is the worker's Dispatch as captured after binding.
type WorkQueueAssignmentReceipt struct {
	DispatchID string                  `json:"dispatch_id"`
	State      string                  `json:"state"`
	Run        *workqueue.RunBinding   `json:"run"`
	Claims     []WorkQueueClaimReceipt `json:"claims"`
}

type WorkQueueClaimReceipt struct {
	Handle  string `json:"claim_handle"`
	ClaimID string `json:"claim_id"`
	WorkID  string `json:"work_id"`
	State   string `json:"state"`
}

// WorkQueueFinishReceipt is one staged finish intent: "completed" or "failed".
type WorkQueueFinishReceipt struct {
	Handle  string `json:"claim_handle"`
	Outcome string `json:"outcome"`
}

func readBoundedWorkQueueArtifact(path string, limit int64) (data []byte, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, errors.New("could not close the work queue artifact; check filesystem access before retrying"))
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("work queue artifact exceeds bounded input limit")
	}
	data, err = io.ReadAll(io.LimitReader(file, limit+1))
	if int64(len(data)) > limit {
		return nil, errors.New("work queue artifact exceeds bounded input limit")
	}
	return data, err
}

func decodeClosedWorkQueueObject(data []byte, required, optional []string, target any) error {
	canonical, err := workqueue.Canonical(data)
	if err != nil {
		return currentWorkQueueDiagnosticError("artifact", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(canonical, &fields); err != nil {
		return err
	}
	if fields == nil {
		return errors.New("work queue artifact must be an object")
	}
	for _, key := range required {
		if _, present := fields[key]; !present {
			return fmt.Errorf("work queue artifact missing %q", key)
		}
	}
	for key := range fields {
		if !slices.Contains(required, key) && !slices.Contains(optional, key) {
			return errors.New("work queue artifact has an unknown field")
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid typed work queue artifact")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("invalid typed work queue artifact")
	}
	return nil
}

func currentWorkQueueDiagnosticError(stage string, err error) error {
	code, _, _ := strings.Cut(err.Error(), ":")
	if code == "" || len(code) > 64 || strings.ContainsFunc(code, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_'
	}) {
		code = "invalid_protocol"
	}
	// Schema/codec error details may include untrusted payload values.
	return fmt.Errorf("current snapshot %s rejected (%s)", stage, code)
}

type currentWorkQueueEnvelope struct {
	Version        int                   `json:"version"`
	SHA            *string               `json:"sha"`
	TransactionLog string                `json:"transactionLog"`
	Origin         workqueue.Actor       `json:"origin"`
	Role           string                `json:"role"`
	Worker         *workqueue.Assignment `json:"worker"`
	VisibleWorkIDs []string              `json:"visible_work_ids,omitempty"`
}

func parseCurrentWorkQueueSnapshot(data []byte) (*WorkQueueSnapshot, error) {
	envelope, state, err := replayCurrentWorkQueueSnapshot(data)
	if err != nil {
		return nil, err
	}
	current := &WorkQueueCurrentSnapshot{Role: envelope.Role, Tip: state.Tip, CommitCount: state.Stats.Transactions}
	if envelope.Worker != nil {
		if current.Assignment, err = currentWorkQueueAssignmentReceipt(state, *envelope.Worker, envelope.Origin); err != nil {
			return nil, err
		}
	}
	current.Diagnostics = capturedWorkQueueDiagnostics(state, envelope)
	return &WorkQueueSnapshot{Version: workqueue.Version, SHA: envelope.SHA, Current: current}, nil
}

// replayCurrentWorkQueueSnapshot validates a v4 activation snapshot and replays its captured ledger.
func replayCurrentWorkQueueSnapshot(data []byte) (currentWorkQueueEnvelope, workqueue.Projection, error) {
	var envelope currentWorkQueueEnvelope
	state := workqueue.NewProjection()
	if err := decodeClosedWorkQueueObject(data,
		[]string{"version", "sha", "transactionLog", "origin", "role", "worker"},
		[]string{"visible_work_ids"}, &envelope); err != nil {
		return envelope, state, err
	}
	if envelope.Version != workqueue.Version || envelope.SHA != nil && !validWorkQueueReceiptID(*envelope.SHA) {
		return envelope, state, errors.New("invalid current work queue snapshot")
	}
	if !slices.Contains([]string{"observer", "dispatcher", "worker"}, envelope.Role) ||
		envelope.Origin.Role != envelope.Role || workqueue.ValidateActor(envelope.Origin) != nil {
		return envelope, state, errors.New("invalid current work queue snapshot origin")
	}
	if envelope.Worker != nil && envelope.Role != "worker" {
		return envelope, state, errors.New("current snapshot role conflicts with its assignment")
	}
	if envelope.TransactionLog == "" {
		state.Repository = envelope.Origin.Repository
		return envelope, state, nil
	}
	state, err := workqueue.ReplayLog([]byte(envelope.TransactionLog))
	if err != nil {
		return envelope, state, currentWorkQueueDiagnosticError("ledger", err)
	}
	if !strings.EqualFold(state.Repository, envelope.Origin.Repository) {
		return envelope, state, errors.New("current snapshot origin repository differs from its captured queue")
	}
	return envelope, state, nil
}

// currentWorkQueueAssignmentReceipt checks a worker snapshot against its captured
// ledger: the Dispatch exists and is bound to the snapshot's own run.
func currentWorkQueueAssignmentReceipt(state workqueue.Projection, assignment workqueue.Assignment, origin workqueue.Actor) (*WorkQueueAssignmentReceipt, error) {
	dispatch := state.Dispatches[assignment.DispatchID]
	if dispatch == nil {
		return nil, errors.New("current snapshot assignment is not in its captured ledger")
	}
	if dispatch.Run == nil || dispatch.Run.RunID != origin.RunID || dispatch.Run.RunAttempt != origin.RunAttempt {
		return nil, errors.New("current snapshot origin differs from the Dispatch's bound run")
	}
	receipt := &WorkQueueAssignmentReceipt{DispatchID: dispatch.DispatchID, State: dispatch.State, Run: dispatch.Run, Claims: []WorkQueueClaimReceipt{}}
	for _, member := range assignment.Claims {
		claim := state.Claims[member.ClaimID]
		if claim == nil || claim.DispatchID != dispatch.DispatchID || claim.WorkID != member.WorkID || claim.Handle != member.Handle {
			return nil, errors.New("current snapshot assignment differs from its captured ledger")
		}
		receipt.Claims = append(receipt.Claims, WorkQueueClaimReceipt{Handle: claim.Handle, ClaimID: claim.ClaimID, WorkID: claim.WorkID, State: claim.State})
	}
	return receipt, nil
}

// readCurrentWorkQueueFinishIntents reads staged `{"version":4,"claim_handle","outcome"}` lines.
func readCurrentWorkQueueFinishIntents(path string, snapshot *WorkQueueCurrentSnapshot) ([]WorkQueueFinishReceipt, error) {
	data, err := readBoundedWorkQueueArtifact(path, 4<<20)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read current work queue intents: %w", err)
	}
	var receipts []WorkQueueFinishReceipt
	outcomes := map[string]string{}
	lines := 0
	for index, line := range bytes.Split(data, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if lines++; lines > 256 {
			return nil, errors.New("work queue finish intent exceeds 256 entries")
		}
		var intent struct {
			Version int    `json:"version"`
			Handle  string `json:"claim_handle"`
			Outcome string `json:"outcome"`
		}
		if err := decodeClosedWorkQueueObject(line, []string{"version", "claim_handle", "outcome"}, nil, &intent); err != nil {
			return nil, fmt.Errorf("finish intent line %d: %w", index+1, err)
		}
		if intent.Version != workqueue.Version || !validWorkQueueReceiptID(intent.Handle) || !slices.Contains([]string{"completed", "failed"}, intent.Outcome) {
			return nil, fmt.Errorf("invalid current finish intent at line %d", index+1)
		}
		if snapshot == nil || snapshot.Assignment == nil ||
			!slices.ContainsFunc(snapshot.Assignment.Claims, func(c WorkQueueClaimReceipt) bool { return c.Handle == intent.Handle }) {
			return nil, errors.New("finish intent is foreign to the captured assignment")
		}
		if previous, ok := outcomes[intent.Handle]; ok {
			if previous != intent.Outcome {
				return nil, errors.New("conflicting per-Claim work queue finish intents")
			}
			continue
		}
		outcomes[intent.Handle] = intent.Outcome
		receipts = append(receipts, WorkQueueFinishReceipt{Handle: intent.Handle, Outcome: intent.Outcome})
	}
	return receipts, nil
}

func validWorkQueueReceiptID(value string) bool {
	return value != "" && len(value) <= 256 && !strings.ContainsFunc(value, func(r rune) bool { return r < 32 || r == 127 })
}

func boundWorkQueueDisplay(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	runes := []rune(value)
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes) + "…"
}
