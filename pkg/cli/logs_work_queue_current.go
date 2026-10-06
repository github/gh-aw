package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/workqueue"
)

// Receipts deliberately omit payloads, actor credentials, and raw evidence.
// They describe validated snapshot facts, not a live branch or API recheck.
type WorkQueueCurrentSnapshot struct {
	Role            string                      `json:"role"`
	PolicyInstalled bool                        `json:"policy_installed"`
	Tip             string                      `json:"tip"`
	PolicyEpoch     string                      `json:"policy_epoch"`
	CapturedAt      int64                       `json:"captured_at"`
	CommitCount     int                         `json:"commit_count"`
	Paused          bool                        `json:"admission_paused"`
	GrantsPaused    bool                        `json:"grants_paused"`
	Assignment      *WorkQueueAssignmentReceipt `json:"assignment,omitempty"`
}

type WorkQueueAssignmentReceipt struct {
	DispatchID string                  `json:"dispatch_id"`
	RequestID  string                  `json:"request_id"`
	CommitID   string                  `json:"commit_id"`
	State      string                  `json:"state"`
	Released   bool                    `json:"released"`
	Claims     []WorkQueueClaimReceipt `json:"claims"`
}

type WorkQueueClaimReceipt struct {
	Handle  string `json:"claim_handle"`
	ClaimID string `json:"claim_id"`
	WorkID  string `json:"work_id"`
	State   string `json:"state"`
	Barrier string `json:"delivery_barrier"`
}

type WorkQueueFinishReceipt struct {
	IntentID string `json:"intent_id"`
	Handle   string `json:"claim_handle"`
	Outcome  string `json:"outcome"`
}

func readBoundedWorkQueueArtifact(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("work queue artifact exceeds bounded input limit")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
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

func parseCurrentWorkQueueSnapshot(data []byte) (*WorkQueueSnapshot, error) {
	var envelope struct {
		Version        int                   `json:"version"`
		Role           json.RawMessage       `json:"role"`
		SHA            *string               `json:"sha"`
		Worker         *workqueue.Assignment `json:"worker"`
		TransactionLog *string               `json:"transactionLog"`
		CapturedAt     *int64                `json:"captured_at"`
		Origin         *workqueue.Actor      `json:"origin"`
		VisibleWorkIDs []string              `json:"visible_work_ids"`
	}
	if err := decodeClosedWorkQueueObject(data,
		[]string{"version", "sha", "worker", "transactionLog", "captured_at", "origin"},
		[]string{"visible_work_ids", "role"}, &envelope); err != nil {
		return nil, err
	}
	if envelope.Version != 3 || envelope.TransactionLog == nil || envelope.CapturedAt == nil ||
		*envelope.CapturedAt < 0 || *envelope.CapturedAt > workqueue.MaxTimestamp ||
		(envelope.SHA != nil && !validWorkQueueReceiptID(*envelope.SHA)) || envelope.Origin == nil ||
		!validWorkQueueNativeID(envelope.Origin.Principal) {
		return nil, errors.New("invalid current work queue snapshot")
	}
	role := "dispatcher"
	if envelope.Worker != nil {
		role = "worker"
	}
	if len(envelope.Role) != 0 {
		var declaredRole string
		if err := json.Unmarshal(envelope.Role, &declaredRole); err != nil ||
			!slices.Contains([]string{"observer", "dispatcher", "worker"}, declaredRole) {
			return nil, errors.New("invalid current work queue snapshot role")
		}
		role = declaredRole
	}
	if (role == "worker") != (envelope.Worker != nil) {
		return nil, errors.New("current snapshot role conflicts with immutable assignment")
	}
	unassignedOriginRole := "dispatcher"
	if role == "observer" {
		unassignedOriginRole = "producer"
		if !validWorkQueueReceiptID(envelope.Origin.Repository) ||
			envelope.Origin.DispatchID != "" || envelope.Origin.ClaimHandle != "" {
			return nil, errors.New("current observer snapshot requires bounded producer provenance")
		}
	}
	if envelope.Worker == nil && (envelope.Origin.Role != unassignedOriginRole ||
		envelope.Origin.RunAttempt < 1 || envelope.Origin.RunAttempt > 4096 ||
		!validWorkQueueNativeID(envelope.Origin.RunID) || !validWorkQueueReceiptID(envelope.Origin.Workflow)) {
		return nil, errors.New("current snapshot requires bounded native run and workflow provenance")
	}
	if role == "observer" && envelope.SHA == nil && *envelope.TransactionLog == "" {
		return &WorkQueueSnapshot{Version: 3, Current: &WorkQueueCurrentSnapshot{
			Role: role, CapturedAt: *envelope.CapturedAt,
		}}, nil
	}
	commits, err := workqueue.Parse([]byte(*envelope.TransactionLog))
	if err != nil {
		return nil, currentWorkQueueDiagnosticError("ledger", err)
	}
	state, err := workqueue.Replay(commits)
	if err != nil {
		return nil, currentWorkQueueDiagnosticError("chain", err)
	}
	if state.Policy == nil {
		return nil, errors.New("current snapshot requires an installed policy")
	}
	if role == "observer" && envelope.Origin.Repository != state.Repository {
		return nil, errors.New("current observer snapshot repository differs from captured queue")
	}
	current := &WorkQueueCurrentSnapshot{
		Role: role, PolicyInstalled: state.Policy != nil,
		Tip: state.Tip, PolicyEpoch: state.PolicyEpoch, CapturedAt: *envelope.CapturedAt,
		CommitCount: state.Stats.Transactions, Paused: state.AdmissionPaused, GrantsPaused: state.GrantsPaused,
	}
	if envelope.Worker != nil {
		if err := workqueue.ValidateAssignment(state, *envelope.Worker); err != nil {
			return nil, currentWorkQueueDiagnosticError("assignment", err)
		}

		dispatch := state.Dispatches[envelope.Worker.DispatchID]
		origin := envelope.Origin
		if dispatch.Run == nil || dispatch.Run.RunID != origin.RunID ||
			dispatch.Run.RunAttempt != 1 || origin.RunAttempt != 1 ||
			origin.Role != "worker" || origin.DispatchID != dispatch.DispatchID ||
			dispatch.Run.Repository != origin.Repository || dispatch.Run.Workflow != origin.Workflow ||
			dispatch.Run.Principal != origin.Principal {
			return nil, errors.New("current snapshot origin differs from durable assignment binding")
		}
		receipt := &WorkQueueAssignmentReceipt{
			DispatchID: dispatch.DispatchID, RequestID: dispatch.RequestID, CommitID: dispatch.CommitID,
			State: dispatch.State, Released: dispatch.Released,
		}
		for _, member := range dispatch.Claims {
			claim, work := state.Claims[member.ClaimID], state.Works[member.WorkID]
			receipt.Claims = append(receipt.Claims, WorkQueueClaimReceipt{
				Handle: member.Handle, ClaimID: member.ClaimID, WorkID: member.WorkID,
				State: claim.State, Barrier: work.Barrier,
			})
		}
		current.Assignment = receipt
	}
	return &WorkQueueSnapshot{Version: 3, SHA: envelope.SHA, Current: current}, nil
}

func readCurrentWorkQueueFinishIntents(path string, snapshot *WorkQueueCurrentSnapshot) ([]WorkQueueFinishReceipt, error) {
	data, err := readBoundedWorkQueueArtifact(path, 4<<20)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read current work queue intents: %w", err)
	}
	var receipts []WorkQueueFinishReceipt
	ids, outcomes := map[string]string{}, map[string]string{}
	lines := 0
	for index, line := range bytes.Split(data, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		lines++
		if lines > 256 {
			return nil, errors.New("work queue finish intent exceeds 256 entries")
		}
		var intent struct {
			Version    int    `json:"version"`
			IntentID   string `json:"intent_id"`
			Kind       string `json:"kind"`
			Handle     string `json:"claim_handle"`
			Parameters struct {
				Outcome string `json:"outcome"`
			} `json:"parameters"`
		}
		if err := decodeClosedWorkQueueObject(line, []string{"version", "intent_id", "kind", "parameters"}, []string{"claim_handle"}, &intent); err != nil {
			return nil, fmt.Errorf("finish intent line %d: %w", index+1, err)
		}
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(line, &fields)
		if _, supplied := fields["claim_handle"]; !supplied {
			if snapshot == nil || snapshot.Assignment == nil || len(snapshot.Assignment.Claims) != 1 {
				return nil, errors.New("finish intent requires a selector for the original multi-Claim assignment")
			}
			intent.Handle = snapshot.Assignment.Claims[0].Handle
		}
		if err := decodeClosedWorkQueueObject(fields["parameters"], []string{"outcome"}, nil, &intent.Parameters); err != nil {
			return nil, fmt.Errorf("finish parameters line %d: %w", index+1, err)
		}
		if intent.Version != 3 || intent.Kind != "finish" || !validWorkQueueReceiptID(intent.IntentID) ||
			!validWorkQueueReceiptID(intent.Handle) || !slices.Contains([]string{"completed", "cancelled"}, intent.Parameters.Outcome) {
			return nil, fmt.Errorf("invalid current finish intent at line %d", index+1)
		}
		if snapshot == nil || snapshot.Assignment == nil ||
			!slices.ContainsFunc(snapshot.Assignment.Claims, func(c WorkQueueClaimReceipt) bool { return c.Handle == intent.Handle }) {
			return nil, errors.New("finish intent is foreign to original immutable assignment")
		}
		canonical, _ := workqueue.Canonical(line)
		if previous, ok := ids[intent.IntentID]; ok {
			if previous != string(canonical) {
				return nil, errors.New("conflicting work queue intent identity")
			}
			continue
		}
		if previous, ok := outcomes[intent.Handle]; ok && previous != intent.Parameters.Outcome {
			return nil, errors.New("conflicting per-Claim work queue finish intents")
		}
		ids[intent.IntentID], outcomes[intent.Handle] = string(canonical), intent.Parameters.Outcome
		receipts = append(receipts, WorkQueueFinishReceipt{
			IntentID: intent.IntentID, Handle: intent.Handle, Outcome: intent.Parameters.Outcome,
		})
	}
	return receipts, nil
}

func validWorkQueueNativeID(value string) bool {
	return len(value) > 0 && len(value) <= 256 && value[0] >= '1' && value[0] <= '9' &&
		!strings.ContainsFunc(value, func(r rune) bool { return r < '0' || r > '9' })
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
