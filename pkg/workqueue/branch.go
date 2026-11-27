package workqueue

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/logger"
)

var branchLog = logger.New("workqueue:branch")

var (
	branchPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
	repoPattern     = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}(?:[0-9a-f]{24})?$`)
	protocolPattern = regexp.MustCompile(`,"version":[0-9]+}\r?$`)
)

// errLedgerConflict reports a lost compare-and-swap: the queue head moved.
var errLedgerConflict = errors.New("work queue head changed")

// ledgerStore reads and writes the single queue log file of the queue branch.
type ledgerStore interface {
	// head returns the branch head commit, or "" when the branch does not exist.
	head(ctx context.Context) (string, error)
	// read returns the log bytes and tree of a head commit.
	read(ctx context.Context, head string) ([]byte, string, error)
	// write commits content as the whole log with parent (or as a root when
	// parent is "") and moves the branch without force; errLedgerConflict when
	// the head moved.
	write(ctx context.Context, parent, baseTree string, content []byte, message string) (string, error)
	close()
}

// Branch is the Git-backed queue ledger of one repository branch.
type Branch struct {
	Remote string
	Name   string
	Host   string
	store  ledgerStore
}

// Snapshot is one read of the queue branch.
type Snapshot struct {
	Head  string
	Tree  string
	Data  []byte
	State Projection
}

// Publication is the outcome of one request publication.
type Publication struct {
	Committed bool
	Recovered bool
	Commit    *QueueCommit
	State     Projection
	Head      string
	Request   Request
}

// OperationsFor plans a request's operations against the current projection; it
// is re-evaluated after every lost compare-and-swap. No operations commits nothing.
type OperationsFor func(state Projection, request Request) ([]Operation, error)

func (b Branch) validate() error {
	if !repoPattern.MatchString(b.Remote) {
		return errors.New("repo must be owner/repo")
	}
	for part := range strings.SplitSeq(b.Remote, "/") {
		if part == "." || part == ".." {
			return errors.New("repo must be owner/repo")
		}
	}
	if !branchPattern.MatchString(b.Name) || strings.Contains(b.Name, "..") || strings.Contains(b.Name, "//") ||
		strings.HasSuffix(b.Name, ".") || strings.HasSuffix(b.Name, "/") {
		return errors.New("invalid queue branch")
	}
	for part := range strings.SplitSeq(b.Name, "/") {
		if strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return errors.New("invalid queue branch")
		}
	}
	return nil
}

// open returns a branch bound to a ledger store and its cleanup.
func (b Branch) open(ctx context.Context) (Branch, func(), error) {
	if err := b.validate(); err != nil {
		return b, nil, err
	}
	if b.store != nil {
		return b, func() {}, nil
	}
	store, err := newGitLedgerStore(ctx, b.Host, b.Remote, b.Name)
	if err != nil {
		return b, nil, err
	}
	b.store = store
	return b, store.close, nil
}

// LedgerProtocol returns the protocol version of a canonical log, read from the end
// of its first line (canonical JSON sorts version last), or 0 when unknown.
func LedgerProtocol(data []byte) int {
	line, _, _ := bytes.Cut(data, []byte{'\n'})
	if len(line) > 32 {
		line = line[len(line)-32:]
	}
	match := protocolPattern.Find(line)
	if match == nil {
		return 0
	}
	digits := bytes.TrimRight(bytes.TrimPrefix(match, []byte(`,"version":`)), "}\r")
	version, err := strconv.Atoi(string(digits))
	if err != nil {
		return 0
	}
	return version
}

func (b Branch) snapshot(ctx context.Context) (Snapshot, error) {
	head, err := b.store.head(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	if head == "" {
		return Snapshot{State: NewProjection()}, nil
	}
	data, tree, err := b.store.read(ctx, head)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot := Snapshot{Head: head, Tree: tree, Data: data}
	if len(data) == 0 {
		snapshot.State = NewProjection()
		return snapshot, nil
	}
	if version := LedgerProtocol(data); version != 0 && version < Version {
		return Snapshot{}, queueError("unsupported_protocol", "the %s branch holds a v%d ledger; the next dispatcher or agentics-maintenance run converts it to v4", b.Name, version)
	}
	snapshot.State, err = ReplayLog(data)
	if err != nil {
		return Snapshot{}, err
	}
	branchLog.Printf("read %s at %s: %d commits", b.Name, head, len(snapshot.State.Transactions))
	return snapshot, nil
}

// Read returns the replayed queue; an absent branch or empty log is an empty queue.
func (b Branch) Read(ctx context.Context) (Snapshot, error) {
	b, cleanup, err := b.open(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	defer cleanup()
	return b.snapshot(ctx)
}

func (b Branch) checkActor(actor Actor) error {
	if err := ValidateActor(actor); err != nil {
		return err
	}
	if !strings.EqualFold(actor.Repository, b.Remote) {
		return queueError("actor_unauthorized", "actor belongs to another repository")
	}
	return nil
}

// Publish commits one request with up to CASAttempts compare-and-swap attempts.
// Each attempt re-reads the ledger (the re-read is the only backoff) and first
// recovers a request that is already committed.
func (b Branch) Publish(ctx context.Context, actor Actor, kind string, parameters any, operationsFor OperationsFor) (Publication, error) {
	if err := b.checkActor(actor); err != nil {
		return Publication{}, err
	}
	request, err := NewRequest(kind, actor, parameters)
	if err != nil {
		return Publication{}, err
	}
	b, cleanup, err := b.open(ctx)
	if err != nil {
		return Publication{}, err
	}
	defer cleanup()
	for attempt := 1; attempt <= CASAttempts; attempt++ {
		current, err := b.snapshot(ctx)
		if err != nil {
			return Publication{}, err
		}
		if prior, committed := current.State.CommittedRequest(request.ID); committed {
			return Publication{Committed: true, Recovered: true, Commit: prior, State: current.State, Head: current.Head, Request: request}, nil
		}
		operations, err := operationsFor(current.State, request)
		if err != nil {
			return Publication{}, err
		}
		if len(operations) == 0 {
			return Publication{State: current.State, Head: current.Head, Request: request}, nil
		}
		commit := BuildCommit(current.State, actor, request, operations, time.Now().UnixMilli())
		next, err := AppendCommit(current.State, commit)
		if err != nil {
			return Publication{}, err
		}
		line, err := CanonicalValue(commit)
		if err != nil {
			return Publication{}, err
		}
		content := append(append(bytes.Clone(current.Data), line...), '\n')
		if len(content) > LedgerBytes {
			return Publication{}, queueError("ledger_limit", "the ledger would exceed its byte bound; a checkpoint is required")
		}
		head, err := b.store.write(ctx, current.Head, current.Tree, content, "work queue: "+kind)
		if err == nil {
			return Publication{Committed: true, Commit: &commit, State: next, Head: head, Request: request}, nil
		}
		if !errors.Is(err, errLedgerConflict) {
			return b.recoverPublication(ctx, request, err)
		}
		branchLog.Printf("publish %s attempt %d lost the compare-and-swap", kind, attempt)
	}
	return b.recoverPublication(ctx, request, queueError("cas_exhausted", "work queue head changed on all %d publication attempts", CASAttempts))
}

// recoverPublication reports an ambiguous or exhausted write as committed when
// the re-read ledger already carries the request.
func (b Branch) recoverPublication(ctx context.Context, request Request, failure error) (Publication, error) {
	latest, err := b.snapshot(ctx)
	if err != nil {
		return Publication{}, errors.Join(failure, err)
	}
	if prior, committed := latest.State.CommittedRequest(request.ID); committed {
		return Publication{Committed: true, Recovered: true, Commit: prior, State: latest.State, Head: latest.Head, Request: request}, nil
	}
	return Publication{}, failure
}

// CheckpointResult is the outcome of a checkpoint publication.
type CheckpointResult struct {
	Checkpointed bool
	Head         string
	Lines        int
	State        Projection
}

// Checkpoint replaces the whole log with one Checkpoint commit (the Git parent
// keeps the prior content) when the log has more than minimumLines lines.
func (b Branch) Checkpoint(ctx context.Context, actor Actor, minimumLines int) (CheckpointResult, error) {
	if err := b.checkActor(actor); err != nil {
		return CheckpointResult{}, err
	}
	b, cleanup, err := b.open(ctx)
	if err != nil {
		return CheckpointResult{}, err
	}
	defer cleanup()
	for range CASAttempts {
		current, err := b.snapshot(ctx)
		if err != nil {
			return CheckpointResult{}, err
		}
		lines := len(current.State.Transactions)
		if current.Head == "" || lines <= max(1, minimumLines) {
			return CheckpointResult{Head: current.Head, Lines: lines, State: current.State}, nil
		}
		commit, err := CheckpointCommit(current.State, actor, time.Now().UnixMilli())
		if err != nil {
			return CheckpointResult{}, err
		}
		line, err := CanonicalValue(commit)
		if err != nil {
			return CheckpointResult{}, err
		}
		head, err := b.store.write(ctx, current.Head, current.Tree, append(line, '\n'), "work queue: checkpoint")
		if err == nil {
			state, err := ReplayLog(append(line, '\n'))
			if err != nil {
				return CheckpointResult{}, err
			}
			return CheckpointResult{Checkpointed: true, Head: head, Lines: lines, State: state}, nil
		}
		if !errors.Is(err, errLedgerConflict) {
			return CheckpointResult{}, err
		}
	}
	return CheckpointResult{}, queueError("cas_exhausted", "work queue head changed on all %d checkpoint attempts", CASAttempts)
}
