package workqueue

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"
)

type Compaction struct {
	Tip                     string `json:"tip"`
	At                      int64  `json:"at"`
	Changed                 bool   `json:"changed"`
	AcknowledgmentRecovered bool   `json:"acknowledgment_recovered"`
	Commits                 int    `json:"commits"`
	DuplicatesRemoved       int    `json:"duplicates_removed"`
	BytesBefore             int    `json:"bytes_before"`
	BytesAfter              int    `json:"bytes_after"`
}

// Compact canonicalizes representation only. Every unique commit, operation,
// logical request and charged turn remains in the original causal chain.
func (b Branch) Compact(ctx context.Context) (Compaction, error) {
	b, err := b.withClient()
	if err != nil {
		return Compaction{}, err
	}
	actor, err := b.Authenticate(ctx, "administrator")
	if err != nil {
		return Compaction{}, err
	}
	b.Remote = actor.Repository
	var previous []QueueCommit
	var pendingErr error
	for attempt := range maxRetries {
		commits, snapshot, err := b.read(ctx)
		if err != nil {
			return Compaction{}, err
		}
		if snapshot.head == "" {
			return Compaction{}, queueError("queue_missing", "compaction requires an existing current queue")
		}
		if len(previous) > 0 && !extending(previous, commits) {
			return Compaction{}, queueError("ledger_nonextending", "queue history was deleted or rewritten during compaction")
		}
		ordered, err := Compact(commits)
		if err != nil {
			return Compaction{}, err
		}
		data, err := Serialize(ordered)
		if err != nil {
			return Compaction{}, err
		}
		result := Compaction{
			Tip: ordered[len(ordered)-1].ID, At: time.Now().UnixMilli(),
			Commits: len(ordered), DuplicatesRemoved: len(commits) - len(ordered),
			BytesBefore: len(snapshot.data), BytesAfter: len(data),
		}
		if bytes.Equal(data, snapshot.data) {
			result.AcknowledgmentRecovered = pendingErr != nil &&
				!hasStatus(pendingErr, http.StatusConflict) && !hasStatus(pendingErr, http.StatusUnprocessableEntity)
			return result, nil
		}
		if pendingErr != nil && (hasStatus(pendingErr, http.StatusUnauthorized) ||
			hasStatus(pendingErr, http.StatusForbidden)) {
			return Compaction{}, pendingErr
		}
		_, err = b.publish(ctx, snapshot, ordered)
		if err == nil {
			result.Changed = true
			return result, nil
		}
		if ctx.Err() != nil {
			return Compaction{}, ctx.Err()
		}
		pendingErr, previous = err, commits
		if attempt < maxRetries-1 {
			if err := waitForPublicationRetry(ctx, attempt); err != nil {
				return Compaction{}, err
			}
		}
	}
	// A final uncertain response must still be checked before reporting failure.
	commits, snapshot, err := b.read(ctx)
	if err != nil {
		return Compaction{}, fmt.Errorf("compaction acknowledgment unresolved: %w", err)
	}
	if snapshot.head == "" || !extending(previous, commits) {
		return Compaction{}, queueError("ledger_nonextending", "queue history was deleted or rewritten during compaction")
	}
	data, err := Serialize(commits)
	if err != nil {
		return Compaction{}, err
	}
	if bytes.Equal(data, snapshot.data) {
		ordered, err := Compact(commits)
		if err != nil {
			return Compaction{}, err
		}
		return Compaction{
			Tip: ordered[len(ordered)-1].ID, At: time.Now().UnixMilli(),
			AcknowledgmentRecovered: !hasStatus(pendingErr, http.StatusConflict) &&
				!hasStatus(pendingErr, http.StatusUnprocessableEntity), Commits: len(ordered),
			BytesBefore: len(snapshot.data), BytesAfter: len(data),
		}, nil
	}
	return Compaction{}, fmt.Errorf("queue compaction unresolved after %d attempts: %w", maxRetries, pendingErr)
}
