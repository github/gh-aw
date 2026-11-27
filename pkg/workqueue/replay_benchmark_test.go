package workqueue

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// BenchmarkQueueColdReplay measures cold parsing and replay of a v4 log, not Git
// transfer or API latency. Each size is a full ledger of submitted Work with
// near-maximal payloads, spread over graphs and pools to stay within the
// per-graph and per-pool pending bounds.
func BenchmarkQueueColdReplay(b *testing.B) {
	for _, size := range []int{1, 16, 64} {
		b.Run(strconv.Itoa(size)+"MiB", func(b *testing.B) {
			ledger, works := benchmarkLedger(b, min(size<<20, LedgerBytes-(1<<20)))
			b.SetBytes(int64(len(ledger)))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				state, err := ReplayLog(ledger)
				if err != nil {
					b.Fatal(err)
				}
				if len(state.Works) != works {
					b.Fatal("cold replay dropped Work")
				}
			}
		})
	}
}

// benchmarkLedger builds a canonical log of at least target bytes incrementally,
// appending each commit to one projection instead of replaying the prefix.
func benchmarkLedger(b *testing.B, target int) ([]byte, int) {
	b.Helper()
	const perGraph = 1024
	const perCommit = 64
	padding := strings.Repeat("p", PayloadBytes-1024)
	state := NewProjection()
	var ledger strings.Builder
	works := 0
	for ledger.Len() < target {
		operations := make([]Operation, 0, perCommit)
		ids := make([]string, 0, perCommit)
		for range perCommit {
			graph := "g" + strconv.Itoa(works/perGraph)
			pool := "p" + strconv.Itoa(works/perGraph)
			key := "n" + strconv.Itoa(works%perGraph)
			payload, err := json.Marshal(map[string]string{"task": key, "padding": padding})
			if err != nil {
				b.Fatal(err)
			}
			work, err := NewWork(graph, key, pool, "worker", "", 3, payload, nil)
			if err != nil {
				b.Fatal(err)
			}
			operation, err := Op(work)
			if err != nil {
				b.Fatal(err)
			}
			operations = append(operations, operation)
			ids = append(ids, work.WorkID)
			works++
		}
		request, err := NewRequest("submit", testProducer, WorkIDsParameters{WorkIDs: ids})
		if err != nil {
			b.Fatal(err)
		}
		commit := BuildCommit(state, testProducer, request, operations, 1)
		if state, err = AppendCommit(state, commit); err != nil {
			b.Fatal(err)
		}
		line, err := CanonicalValue(commit)
		if err != nil {
			b.Fatal(err)
		}
		ledger.Write(line)
		ledger.WriteByte('\n')
	}
	return []byte(ledger.String()), works
}
