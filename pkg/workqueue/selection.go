package workqueue

import (
	"cmp"
	"slices"
	"strings"
	"time"
)

// MaxEnqueued is the largest integer represented exactly by JavaScript.
const MaxEnqueued int64 = 9007199254740991

// NewWork captures immutable enqueue time before publication retries.
func NewWork(payload []byte) (Transaction, error) {
	id, canonical, err := WorkID(payload)
	if err != nil {
		return Transaction{}, err
	}
	return Transaction{Kind: "Work", WorkID: id, Work: canonical, Enqueued: time.Now().UnixMilli()}, nil
}

func compareQueueWork(a, b WorkState) int {
	if order := cmp.Compare(a.Enqueued, b.Enqueued); order != 0 {
		return order
	}
	return strings.Compare(a.WorkID, b.WorkID)
}

func availableWorkIDs(works []WorkState) []string {
	available := make([]WorkState, 0)
	for _, work := range works {
		if work.State == "available" {
			available = append(available, work)
		}
	}
	slices.SortFunc(available, compareQueueWork)
	ids := make([]string, 0, len(available))
	for _, work := range available {
		ids = append(ids, work.WorkID)
	}
	return ids
}

// OldestAvailable selects by enqueue time, then UTF-8 Work identity.
// Legacy Work has age zero; an empty identity means no Work is available.
func OldestAvailable(transactions []Transaction) (string, error) {
	projection, err := Replay(transactions)
	if err != nil {
		return "", err
	}
	for _, work := range projection.Available {
		return work, nil
	}
	return "", nil
}
