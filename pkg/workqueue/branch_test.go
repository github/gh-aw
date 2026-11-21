package workqueue

import (
	"context"
	"crypto/sha1" //nolint:gosec // test-only object identities
	"encoding/hex"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeLedgerStore is an in-memory queue branch. beforeWrite runs before each
// write (a concurrent writer); ambiguousOnce loses one successful write response.
type fakeLedgerStore struct {
	commits       map[string]fakeCommit
	headSHA       string
	serial        int
	writes        int
	beforeWrite   func()
	ambiguousOnce bool
}

type fakeCommit struct {
	parent string
	data   []byte
}

func newFakeLedgerStore() *fakeLedgerStore {
	return &fakeLedgerStore{commits: map[string]fakeCommit{}}
}

func (s *fakeLedgerStore) install(data []byte) string {
	s.serial++
	sum := sha1.Sum(fmt.Appendf(nil, "commit-%d", s.serial)) //nolint:gosec // test-only object identities
	sha := hex.EncodeToString(sum[:])
	s.commits[sha] = fakeCommit{parent: s.headSHA, data: data}
	s.headSHA = sha
	return sha
}

func (s *fakeLedgerStore) head(context.Context) (string, error) { return s.headSHA, nil }

func (s *fakeLedgerStore) read(_ context.Context, head string) ([]byte, string, error) {
	commit, ok := s.commits[head]
	if !ok {
		return nil, "", errors.New("missing commit")
	}
	return commit.data, "tree-" + head, nil
}

func (s *fakeLedgerStore) write(_ context.Context, parent, _ string, content []byte, _ string) (string, error) {
	if s.beforeWrite != nil {
		hook := s.beforeWrite
		s.beforeWrite = nil
		hook()
	}
	if parent != s.headSHA {
		return "", errLedgerConflict
	}
	s.writes++
	sha := s.install(content)
	if s.ambiguousOnce {
		s.ambiguousOnce = false
		return "", errors.New("connection reset")
	}
	return sha, nil
}

func (s *fakeLedgerStore) close() {}

func testBranch(store *fakeLedgerStore) Branch {
	return Branch{Remote: testRepository, Name: DefaultBranch, store: store}
}

func ledgerBytes(t *testing.T, log []QueueCommit) []byte {
	t.Helper()
	data, err := Serialize(log)
	require.NoError(t, err)
	return data
}

func submitOperations(t *testing.T, works ...WorkDefinition) (WorkIDsParameters, OperationsFor) {
	t.Helper()
	ids := []string{}
	values := []any{}
	for _, work := range works {
		ids = append(ids, work.WorkID)
		values = append(values, work)
	}
	operations := mustOps(t, values...)
	return WorkIDsParameters{WorkIDs: ids}, func(Projection, Request) ([]Operation, error) { return operations, nil }
}

func TestBranchReadsAbsentAndEmptyLedgerAsEmptyQueue(t *testing.T) {
	store := newFakeLedgerStore()
	snapshot, err := testBranch(store).Read(context.Background())
	require.NoError(t, err)
	require.Empty(t, snapshot.Head)
	require.Empty(t, snapshot.State.Works)
	store.install(nil)
	snapshot, err = testBranch(store).Read(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, snapshot.Head)
	require.Empty(t, snapshot.State.Transactions)
}

func TestBranchRejectsV3HeadWithConversionGuidance(t *testing.T) {
	store := newFakeLedgerStore()
	store.install([]byte(`{"actor":{},"operations":[],"version":3}` + "\n"))
	_, err := testBranch(store).Read(context.Background())
	requireCode(t, err, "unsupported_protocol")
	require.Contains(t, err.Error(), "agentics-maintenance")
	require.Equal(t, 3, LedgerProtocol([]byte(`{"x":1,"version":3}`+"\n")))
}

func TestBranchPublishCreatesAndExtendsTheLog(t *testing.T) {
	store := newFakeLedgerStore()
	branch := testBranch(store)
	parameters, operationsFor := submitOperations(t, testWork(t, "a", nil))
	published, err := branch.Publish(context.Background(), testAdministrator, "submit", parameters, operationsFor)
	require.NoError(t, err)
	require.True(t, published.Committed)
	require.False(t, published.Recovered)
	require.Nil(t, published.Commit.Previous)
	parameters, operationsFor = submitOperations(t, testWork(t, "b", nil))
	published, err = branch.Publish(context.Background(), testAdministrator, "submit", parameters, operationsFor)
	require.NoError(t, err)
	require.True(t, published.Committed)
	snapshot, err := branch.Read(context.Background())
	require.NoError(t, err)
	require.Len(t, snapshot.State.Transactions, 2)
	require.Len(t, snapshot.State.Works, 2)
	// Republishing a committed request recovers it without a write.
	writes := store.writes
	again, err := branch.Publish(context.Background(), testAdministrator, "submit", parameters, operationsFor)
	require.NoError(t, err)
	require.True(t, again.Recovered)
	require.Equal(t, writes, store.writes)
}

func TestBranchPublishReplansAfterLostCompareAndSwap(t *testing.T) {
	store := newFakeLedgerStore()
	store.install(ledgerBytes(t, testSubmit(t, nil, testWork(t, "a", nil), testWork(t, "b", nil))))
	branch := testBranch(store)
	concurrent := testDispatchParameters(func(parameters *DispatchParameters) { parameters.Concurrency = 1 })
	store.beforeWrite = func() {
		current, err := branch.Read(context.Background())
		require.NoError(t, err)
		log, _ := testDispatchNext(t, current.State.Transactions, testDispatchParameters(func(parameters *DispatchParameters) { parameters.Concurrency, parameters.MaxClaims = 1, 15 }))
		store.install(ledgerBytes(t, log))
	}
	plans := 0
	published, err := branch.Publish(context.Background(), testDispatcher, "dispatch_next", concurrent, func(state Projection, request Request) ([]Operation, error) {
		plans++
		decision, err := PlanDispatch(state, concurrent, request.ID)
		if err != nil {
			return nil, err
		}
		values := []any{}
		for _, operation := range decision.Operations {
			values = append(values, operation)
		}
		return mustOps(t, values...), nil
	})
	require.NoError(t, err)
	require.Equal(t, 2, plans)
	require.False(t, published.Committed, "the re-read head is at capacity")
}

func TestBranchPublishRecoversAmbiguousWriteAndExhaustsCompareAndSwap(t *testing.T) {
	store := newFakeLedgerStore()
	store.ambiguousOnce = true
	branch := testBranch(store)
	parameters, operationsFor := submitOperations(t, testWork(t, "a", nil))
	published, err := branch.Publish(context.Background(), testAdministrator, "submit", parameters, operationsFor)
	require.NoError(t, err)
	require.True(t, published.Recovered)

	conflicts := 0
	parameters, operationsFor = submitOperations(t, testWork(t, "b", nil))
	_, err = branch.Publish(context.Background(), testAdministrator, "submit", parameters, func(state Projection, request Request) ([]Operation, error) {
		conflicts++
		store.beforeWrite = func() { store.install(store.commits[store.headSHA].data) }
		return operationsFor(state, request)
	})
	requireCode(t, err, "cas_exhausted")
	require.Equal(t, CASAttempts, conflicts)
}

func TestBranchPublishRejectsForeignActor(t *testing.T) {
	parameters, operationsFor := submitOperations(t, testWork(t, "a", nil))
	foreign := Actor{Role: "administrator", Repository: "other/repo"}
	_, err := testBranch(newFakeLedgerStore()).Publish(context.Background(), foreign, "submit", parameters, operationsFor)
	requireCode(t, err, "actor_unauthorized")
}

func TestBranchCheckpointReplacesTheLog(t *testing.T) {
	store := newFakeLedgerStore()
	log := testSubmit(t, nil, testWork(t, "a", nil))
	log, _ = testDispatchNext(t, log, testDispatchParameters(nil))
	store.install(ledgerBytes(t, log))
	branch := testBranch(store)
	skipped, err := branch.Checkpoint(context.Background(), testReconciler, 2)
	require.NoError(t, err)
	require.False(t, skipped.Checkpointed)
	result, err := branch.Checkpoint(context.Background(), testReconciler, 1)
	require.NoError(t, err)
	require.True(t, result.Checkpointed)
	require.Equal(t, 2, result.Lines)
	snapshot, err := branch.Read(context.Background())
	require.NoError(t, err)
	require.Len(t, snapshot.State.Transactions, 1)
	require.Equal(t, 3, snapshot.State.Commits)
	require.Len(t, snapshot.State.Works, 1)
	require.Equal(t, store.commits[result.Head].parent, store.commits[store.headSHA].parent)
}
