package workqueue

// Protocol v4 bounds. They are constants of the wire format, never Policy: the
// compiled policy environment carries only pool scheduling preferences.
const (
	PayloadBytes         = 16 * 1024
	OperationsPerCommit  = 256
	GraphNodes           = 4096
	Predecessors         = 64
	AssignmentBytes      = 48 * 1024
	EffectsBytes         = 4096
	LedgerBytes          = 64 * 1024 * 1024
	CheckpointAfterLines = 512
	// MaxPendingWork bounds available plus claimed Work per pool during replay.
	MaxPendingWork = 4096
	// RetainedRequests bounds the request IDs a Checkpoint keeps: the most recent of
	// the log it folds, so request_reused deduplicates through at least the next epoch.
	RetainedRequests = 4096
	// CASAttempts bounds publication; each failed compare-and-swap re-reads the
	// ledger before retrying and the re-read is the only backoff.
	CASAttempts = 8
)
