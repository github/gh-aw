package workqueue

import (
	"encoding/json"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const deliveryEvidenceMaxBytes = 32 * 1024

var deliveryEvidenceSchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	resource, err := contractIdentitySchema("DeliveryEvidenceOperation")
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("DeliveryEvidenceOperation.json", resource); err != nil {
		return nil, err
	}
	return compiler.Compile("DeliveryEvidenceOperation.json")
})

type deliveryEvidenceOperation struct {
	Kind         string `json:"kind"`
	WorkID       string `json:"work_id"`
	ClaimID      string `json:"claim_id"`
	DispatchID   string `json:"dispatch_id"`
	ClaimHandle  string `json:"claim_handle"`
	CompletionID string `json:"completion_id"`
	RunID        string `json:"run_id"`
	RunAttempt   int    `json:"run_attempt"`
	Outputs      []struct {
		Index    int             `json:"index"`
		Verifier string          `json:"verifier"`
		Receipt  json.RawMessage `json:"receipt"`
		Adapter  *string         `json:"adapter"`
	} `json:"outputs"`
}

// Checkpoints contain raw operations outside the QueueCommit operation schema,
// so validate the complete operation here as well as the non-schema byte bounds.
func validateDeliveryEvidence(operation Operation) (deliveryEvidenceOperation, error) {
	var evidence deliveryEvidenceOperation
	data, err := Canonical(operation)
	if err != nil {
		return evidence, err
	}
	if len(data) > deliveryEvidenceMaxBytes {
		return evidence, queueError("resource_limit", "DeliveryEvidence exceeds 32768 canonical bytes")
	}
	schema, err := deliveryEvidenceSchema()
	if err != nil {
		return evidence, err
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return evidence, err
	}
	if err := schema.Validate(value); err != nil {
		return evidence, queueError("evidence_invalid", "DeliveryEvidence requires a closed verifier receipt inventory: %v", err)
	}
	if err := validateContractIdentityBytes("DeliveryEvidenceOperation", value); err != nil {
		return evidence, err
	}
	if err := json.Unmarshal(data, &evidence); err != nil {
		return evidence, err
	}
	previousIndex := -1
	for _, output := range evidence.Outputs {
		if output.Index <= previousIndex {
			return evidence, queueError("evidence_invalid", "DeliveryEvidence output indexes require strictly increasing order")
		}
		previousIndex = output.Index
		if (output.Verifier == "none") == (len(output.Receipt) > 0) {
			return evidence, queueError("evidence_invalid", "DeliveryEvidence receipts are required exactly for write verifiers")
		}
		if output.Adapter != nil {
			if len(*output.Adapter) > 8192 {
				return evidence, queueError("evidence_invalid", "DeliveryEvidence adapter requires JSON within 8192 UTF-8 bytes")
			}
			if _, err := decodeStrict([]byte(*output.Adapter)); err != nil {
				return evidence, err
			}
		}
	}
	return evidence, nil
}

func effectiveBarrier(claim *ClaimState, work *WorkState) string {
	if claim.KeepOpen {
		return claim.Barrier
	}
	return work.Barrier
}

func (state *Projection) applyDeliveryEvidence(operation Operation, commit QueueCommit) error {
	evidence, err := validateDeliveryEvidence(operation)
	if err != nil {
		return err
	}
	// Completion uses scopedClaim too: this binds the original worker attempt,
	// principal, workflow, dispatch and handle, and rejects released dispatches.
	claim, err := state.scopedClaim(commit.Actor, commit.Actor.DispatchID, commit.Actor.ClaimHandle)
	if err != nil {
		return err
	}
	if claim == nil {
		return queueError("claim_scope_invalid", "DeliveryEvidence requires an original assignment member")
	}
	work := state.Works[claim.WorkID]
	if work == nil || claim.State != "completed" || work.ClaimID != claim.ClaimID ||
		(work.State != "completed" && !claim.KeepOpen) {
		return queueError("claim_ineffective", "DeliveryEvidence requires the current completed Claim")
	}
	if work.Barrier == "failed" {
		return queueError("claim_effects_unauthorized", "terminal DeliveryFailure cannot authorize effects again")
	}
	dispatch := state.Dispatches[claim.DispatchID]
	if evidence.WorkID != work.WorkID || evidence.ClaimID != claim.ClaimID ||
		evidence.DispatchID != dispatch.DispatchID || evidence.ClaimHandle != claim.Handle ||
		evidence.RunID != dispatch.Run.RunID || evidence.CompletionID != work.CompletionID {
		return queueError("claim_scope_invalid", "DeliveryEvidence is outside native Claim scope")
	}
	if effectiveBarrier(claim, work) != "pending" {
		return queueError("delivery_conflict", "DeliveryEvidence requires a pending delivery barrier")
	}
	if previous := state.DeliveryEvidence[evidence.ClaimID]; previous != nil {
		if !sameJSON(previous, operation) {
			return queueError("delivery_conflict", "DeliveryEvidence is immutable per Completion")
		}
		return nil
	}
	state.DeliveryEvidence[evidence.ClaimID] = operation
	return nil
}

func (state Projection) validateDeliveryEvidenceCheckpoint() error {
	for id, operation := range state.DeliveryEvidence {
		evidence, err := validateDeliveryEvidence(operation)
		if err != nil {
			return err
		}
		claim := state.Claims[id]
		work := state.Works[evidence.WorkID]
		dispatch := state.Dispatches[evidence.DispatchID]
		if evidence.ClaimID != id || claim == nil || claim.State != "completed" ||
			claim.WorkID != evidence.WorkID || claim.DispatchID != evidence.DispatchID ||
			claim.Handle != evidence.ClaimHandle || work == nil || work.ClaimID != id ||
			work.CompletionID != evidence.CompletionID || dispatch == nil || dispatch.Run == nil ||
			dispatch.Run.RunID != evidence.RunID {
			return queueError("checkpoint_invalid", "DeliveryEvidence requires the completed Claim and its bound native run")
		}
		if effectiveBarrier(claim, work) != "pending" {
			return queueError("checkpoint_invalid", "DeliveryEvidence requires a pending delivery barrier")
		}
	}
	return nil
}
