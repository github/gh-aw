package workqueue

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func mustOp(t testing.TB, value any) Operation {
	t.Helper()
	operation, err := Op(value)
	if err != nil {
		t.Fatal(err)
	}
	return operation
}

func mustOperationKind(t testing.TB, operation Operation) string {
	t.Helper()
	kind, err := operationKind(operation)
	if err != nil {
		t.Fatal(err)
	}
	return kind
}

func TestOperationEncodingPreservesCanonicalFailuresWithoutPartialOperations(t *testing.T) {
	for _, test := range []struct {
		name  string
		value any
	}{
		{name: "nonfinite", value: map[string]any{"kind": "Control", "value": math.NaN()}},
		{name: "unsupported-type", value: make(chan int)},
		{name: "invalid-raw-json", value: json.RawMessage(`{"kind":`)},
		{name: "duplicate-key", value: json.RawMessage(`{"kind":"Control","kind":"Policy"}`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, expected := canonicalValue(test.value)
			if expected == nil {
				t.Fatal("test requires a canonical codec error")
			}
			operation, err := Op(test.value)
			if err == nil || operation != nil || err.Error() != expected.Error() {
				t.Fatalf("operation encoding lost its exact error or returned partial data: %s %v", operation, err)
			}
			operations := []Operation{Operation(`{"kind":"Control"}`)}
			before := slices.Clone(operations)
			if err := appendOperation(&operations, test.value); err == nil || !reflect.DeepEqual(operations, before) {
				t.Fatalf("failed operation append changed the accepted prefix: %s %v", operations, err)
			}
		})
	}
	operation, err := Op(map[string]any{"value": true, "kind": "Control"})
	if err != nil || string(operation) != `{"kind":"Control","value":true}` {
		t.Fatalf("canonical operation bytes changed: %s %v", operation, err)
	}
}

func TestOperationKindPropagatesExactDecodeFailures(t *testing.T) {
	for _, raw := range []string{`{"kind":`, `{"kind":7}`, `[]`, `"Control"`} {
		t.Run(raw, func(t *testing.T) {
			var expected struct {
				Kind string `json:"kind"`
			}
			decodeErr := json.Unmarshal([]byte(raw), &expected)
			kind, err := operationKind(Operation(raw))
			if decodeErr == nil || err == nil || kind != "" || err.Error() != decodeErr.Error() {
				t.Fatalf("operation kind silently discarded or changed the decoder failure: %q %v", kind, err)
			}
		})
	}
	kind, err := operationKind(Operation(`{"kind":"Control"}`))
	if err != nil || kind != "Control" {
		t.Fatalf("valid operation kind changed: %q %v", kind, err)
	}
}

func TestGenesisPropagatesOperationCodecFailureBeforePublication(t *testing.T) {
	actor := testActor("administrator")
	policy := DefaultPolicy(actor.Principal, actor.Repository)
	for name, pool := range policy.Pools {
		pool.Retry.BackoffMS = MaxTimestamp + 1
		policy.Pools[name] = pool
		break
	}
	_, expected := canonicalValue(map[string]any{"kind": "Policy", "epoch": "epoch", "policy": policy})
	if expected == nil {
		t.Fatal("test requires a noncanonical policy number")
	}
	commit, err := Genesis(actor, policy, "invalid-policy-encoding", "epoch", 1000)
	if err == nil || err.Error() != expected.Error() || commit.ID != "" || commit.Operations != nil {
		t.Fatalf("genesis discarded codec failure or returned a partial commit: %+v %v", commit, err)
	}
}

func TestSingleOperationRequiresExactlyOne(t *testing.T) {
	policy := Operation(`{"kind":"Policy"}`)
	for _, test := range []struct {
		name       string
		operations []Operation
		unique     bool
	}{
		{name: "nil"},
		{name: "empty", operations: []Operation{}},
		{name: "one", operations: []Operation{policy}, unique: true},
		{name: "multiple", operations: []Operation{policy, policy}},
	} {
		t.Run(test.name, func(t *testing.T) {
			operation, unique := singleOperation(test.operations)
			if unique != test.unique || unique && !bytes.Equal(operation, policy) {
				t.Fatalf("singleton operation changed: %s unique=%t", operation, unique)
			}
		})
	}
}

func TestStrictSurrogateScannerPreservesEscapedBackslashes(t *testing.T) {
	for _, test := range []struct {
		name  string
		input string
		want  string
		code  string
	}{
		{name: "escaped-high", input: `"\\uD800"`, want: `\uD800`},
		{name: "escaped-low", input: `"\\uDC00"`, want: `\uDC00`},
		{name: "escaped-pair", input: `"\\uD800\\uDC00"`, want: `\uD800\uDC00`},
		{name: "pair", input: `"\uD800\uDC00"`, want: "𐀀"},
		{name: "adjacent-pairs", input: `"\uD800\uDC00\uD800\uDC00"`, want: "𐀀𐀀"},
		{name: "backslash-before-pair", input: `"\\\uD800\uDC00"`, want: `\𐀀`},
		{name: "backslash-before-high", input: `"\\\uD800"`, code: "invalid_unicode"},
		{name: "high", input: `"\uD800"`, code: "invalid_unicode"},
		{name: "low", input: `"\uDC00"`, code: "invalid_unicode"},
		{name: "two-highs", input: `"\uD800\uD800"`, code: "invalid_unicode"},
		{name: "truncated-pair", input: `"\uD800\uDC`, code: "invalid_unicode"},
	} {
		t.Run(test.name, func(t *testing.T) {
			value, err := decodeStrict([]byte(test.input))
			if test.code != "" {
				var protocol *ProtocolError
				if !errors.As(err, &protocol) || protocol.Code != test.code {
					t.Fatalf("surrogate rejection changed: %v", err)
				}
				return
			}
			text, ok := value.(string)
			if err != nil || !ok || text != test.want {
				t.Fatalf("escaped text changed: %q %v", value, err)
			}
		})
	}
}

func TestProtocolErrorPreservesCodeThroughWrapping(t *testing.T) {
	rejection := queueError("queue_missing", "queue branch %s does not exist", "work-queue")
	if rejection.Error() != "queue_missing: queue branch work-queue does not exist" {
		t.Fatalf("changed rejection message: %v", rejection)
	}
	var protocol *ProtocolError
	if !errors.As(fmt.Errorf("read failed: %w", rejection), &protocol) || protocol.Code != "queue_missing" {
		t.Fatalf("wrapped rejection lost its stable code: %v", rejection)
	}
}

func TestNativeCodecRejectionCodeParity(t *testing.T) {
	data, err := os.ReadFile("../../specs/work-queue/fixtures/canonical.json")
	if err != nil {
		t.Fatal(err)
	}

	var fixture struct {
		ErrorCodes []struct {
			Name  string `json:"name"`
			Input string `json:"input"`
			Code  string `json:"code"`
		} `json:"error_codes"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.ErrorCodes) == 0 {
		t.Fatal("independent canonical fixture must specify rejection codes")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Log("JavaScript codec parity tooling unavailable; native fixture checks still run")
		return
	}
	var input bytes.Buffer
	for _, test := range fixture.ErrorCodes {
		request, err := json.Marshal(map[string]any{"action": "canonical", "data": test.Input})
		if err != nil {
			t.Fatal(err)
		}
		input.Write(request)
		input.WriteByte('\n')
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, node, "../../specs/work-queue/native_probe.cjs")
	command.Stdin = &input
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("JavaScript codec parity: %v\n%s", err, stderr.String())
	}
	responses := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(responses) != len(fixture.ErrorCodes) {
		t.Fatalf("expected %d codec responses, received %d", len(fixture.ErrorCodes), len(responses))
	}
	for index, test := range fixture.ErrorCodes {
		t.Run(test.Name, func(t *testing.T) {
			_, nativeError := Canonical([]byte(test.Input))
			var response struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal([]byte(responses[index]), &response); err != nil {
				t.Fatal(err)
			}
			prefix := test.Code + ":"
			if nativeError == nil || !strings.HasPrefix(nativeError.Error(), prefix) ||
				!strings.HasPrefix(response.Error, prefix) {
				t.Fatalf("expected %s in both engines: Go=%v JavaScript=%s", test.Code, nativeError, response.Error)
			}
		})
	}
}

func TestParsePreservesSemanticCommitRejectionCode(t *testing.T) {
	for _, test := range []struct {
		name   string
		code   string
		update func(*QueueCommit)
	}{
		{"actor", "actor_unauthorized", func(commit *QueueCommit) { commit.Actor.Principal = "worker-login" }},
		{"fingerprint", "request_fingerprint", func(commit *QueueCommit) {
			commit.Request.Fingerprint = strings.Repeat("0", 64)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			commits := testGenesis(t, nil)
			test.update(&commits[0])
			direct := ValidateCommit(commits[0])
			if direct == nil || !strings.HasPrefix(direct.Error(), test.code+":") {
				t.Fatalf("semantic validator did not reject with %s: %v", test.code, direct)
			}
			data, err := canonicalValue(commits[0])
			if err != nil {
				t.Fatal(err)
			}
			_, parsed := Parse(append(data, '\n'))
			if parsed == nil || parsed.Error() != direct.Error()+" (line 1)" {
				t.Fatalf("parser lost original semantic rejection or line context: direct=%v parsed=%v", direct, parsed)
			}
		})
	}
}
