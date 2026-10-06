package workqueue

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// These cases measure cold validation, not Git transfer/API latency or an SLO.
func BenchmarkQueueColdReplay(b *testing.B) {
	for _, size := range []int{1, 16, 64, 80} {
		b.Run(strconv.Itoa(size)+"MiB", func(b *testing.B) {
			actor := testActor("administrator")
			genesis, err := Genesis(actor, DefaultPolicy(testPrincipal, testRepository), "genesis", "epoch", 1000)
			if err != nil {
				b.Fatal(err)
			}
			commits := []QueueCommit{genesis}
			encoded, _ := canonicalValue(genesis)
			total := len(encoded) + 1
			target := min(size<<20, maxParseBytes-1024)
			for index := 0; total < target; index++ {
				operations := []Operation{Op(map[string]any{
					"kind": "Control", "control": "grants_paused", "value": index%2 == 0,
					"reason": strings.Repeat("b", 128),
				})}
				request, _ := NewRequest("request_"+strconv.Itoa(index), "control", actor, OperationsParameters{Operations: operations})
				previous := commits[len(commits)-1].ID
				commit := QueueCommit{
					Version: Version, ID: "bench_" + strconv.Itoa(index), Previous: &previous,
					Request: request, Actor: actor, PolicyEpoch: "epoch", At: int64(2000 + index),
					Operations: operations,
				}
				encoded, _ := canonicalValue(commit)
				total += len(encoded) + 1
				commits = append(commits, commit)
			}
			var buffer strings.Builder
			for _, commit := range commits {
				line, _ := canonicalValue(commit)
				buffer.Write(line)
				buffer.WriteByte('\n')
			}
			ledger := []byte(buffer.String())
			b.SetBytes(int64(len(ledger)))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				parsed, err := Parse(ledger)
				if err != nil {
					b.Fatal(err)
				}
				projection, err := Replay(parsed)
				if err != nil {
					b.Fatal(err)
				}
				if len(projection.Requests) != len(commits) {
					b.Fatal("cold load dropped request history")
				}
				if _, err := json.Marshal(projection.Stats); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
