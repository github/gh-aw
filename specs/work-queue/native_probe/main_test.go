package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workqueue"
)

func TestReadLedgerFileIsBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	if err := os.WriteFile(path, []byte("ledger\n"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := readLedgerFile(path)
	if err != nil || string(data) != "ledger\n" {
		t.Fatalf("unexpected bounded read: %q %v", data, err)
	}
	if err := os.Truncate(path, (80<<20)+1); err != nil {
		t.Fatal(err)
	}
	if _, err := readLedgerFile(path); err == nil ||
		!strings.HasPrefix(err.Error(), "resource_limit:") {
		t.Fatalf("oversized ledger was not refused: %v", err)
	}
	if _, err := readLedgerFile(filepath.Join(t.TempDir(), "absent")); !os.IsNotExist(err) {
		t.Fatalf("missing file error was not preserved: %v", err)
	}
}

func TestTypedCanonicalRejectsOriginalInvalidUnicode(t *testing.T) {
	tests := []struct {
		name, data, code string
	}{
		{"high-value", `{"x":"\ud800"}`, "invalid_unicode"},
		{"low-value", `{"x":"\udc00"}`, "invalid_unicode"},
		{"high-key", `{"\ud800":1}`, "invalid_unicode"},
		{"low-key", `{"\udc00":1}`, "invalid_unicode"},
		{"high-followed-by-BMP", `{"x":"\ud800\u0041"}`, "invalid_unicode"},
		{"nested-value", `{"x":[{"y":"\ud800"}]}`, "invalid_unicode"},
		{"nested-key", `{"x":[{"\udc00":1}]}`, "invalid_unicode"},
		{"overwritten-key-value", `{"x":"\ud800","x":"valid"}`, "invalid_unicode"},
		{"invalid-UTF8-value", "{\"x\":\"\xff\"}", "invalid_utf8"},
		{"invalid-UTF8-key", "{\"\xff\":1}", "invalid_utf8"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := execute(request{Action: "typed_canonical", Data: test.data})
			if err == nil || !strings.HasPrefix(err.Error(), test.code+":") {
				t.Fatalf("expected original string rejection %s, got %v", test.code, err)
			}
		})
	}
}

func TestTypedCanonicalPublicAPIRejectsInvalidUTF8WithoutTransportGuard(t *testing.T) {
	invalid := string([]byte{0xff})
	for name, value := range map[string]any{
		"value":  invalid,
		"object": map[string]any{"value": invalid},
		"key":    map[string]any{invalid: "value"},
		"nested": []any{map[string]any{"value": invalid}},
		"struct": struct{ Value string }{invalid},
	} {
		t.Run(name, func(t *testing.T) {
			data, err := workqueue.CanonicalValue(value)
			if err == nil || !strings.HasPrefix(err.Error(), "invalid_utf8:") || data != nil {
				t.Fatalf("public API repaired invalid typed UTF8: %q %v", data, err)
			}
		})
	}
}

func TestTypedCanonicalPreservesTypedParsingAndValidStrings(t *testing.T) {
	tests := []struct {
		name, data, expected string
	}{
		{"exponent-integer", `1e0`, `1`},
		{"fraction-spelling-of-integer", `1.0`, `1`},
		{"duplicate-key-typed-object", `{"x":1,"x":2}`, `{"x":2}`},
		{"paired-surrogates", `{"x":"\ud83d\ude00"}`, "{\"x\":\"\U0001f600\"}"},
		{"literal-surrogate-escape", `{"\\ud800":"\\udc00"}`, `{"\\ud800":"\\udc00"}`},
		{"escaped-quote", `{"x":"a\"b"}`, `{"x":"a\"b"}`},
		{"escaped-backslash-and-quote", `{"x":"\\\"tail"}`, `{"x":"\\\"tail"}`},
		{"literal-Unicode-byte-offsets", "{\"\U0001f600\":\"\u00e9\\\"\U0001f600\\\\\"}", "{\"\U0001f600\":\"\u00e9\\\"\U0001f600\\\\\"}"},
		{"replacement-scalar", `{"x":"\ufffd"}`, "{\"x\":\"\ufffd\"}"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output, err := execute(request{Action: "typed_canonical", Data: test.data})
			if err != nil || len(output) != 1 || output["canonical"] != test.expected {
				t.Fatalf("expected exact typed canonical %s, got %#v %v", test.expected, output, err)
			}
		})
	}
}

func TestTypedCanonicalRejectsSyntaxBeforeStringScanning(t *testing.T) {
	for _, data := range []string{`"unterminated`, `{"x":"\q"}`, `{"x":`, `{} {}`} {
		t.Run(data, func(t *testing.T) {
			_, err := execute(request{Action: "typed_canonical", Data: data})
			if err == nil || !strings.HasPrefix(err.Error(), "adapter_invalid:") {
				t.Fatalf("expected explicit typed syntax error, got %v", err)
			}
		})
	}
}

func TestConstructedTypedNumbersUseIndependentFixture(t *testing.T) {
	data, err := os.ReadFile("../fixtures/canonical-typed.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Version int `json:"version"`
		Cases   []struct {
			Name, Literal, Container, Expected, Code string
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Version != 1 || len(fixture.Cases) == 0 {
		t.Fatal("missing constructed typed number fixtures")
	}
	for _, test := range fixture.Cases {
		t.Run(test.Name, func(t *testing.T) {
			output, err := execute(request{Action: "typed_number_literal", Literal: test.Literal, Container: test.Container})
			if test.Code != "" {
				if err == nil || !strings.HasPrefix(err.Error(), test.Code+":") {
					t.Fatalf("expected %s, got %#v %v", test.Code, output, err)
				}
			} else if err != nil || len(output) != 1 || output["canonical"] != test.Expected {
				t.Fatalf("expected exact canonical %s, got %#v %v", test.Expected, output, err)
			}
		})
	}
}

func TestConstructedTypedNumbersRejectInvalidAdapterInput(t *testing.T) {
	for _, literal := range []string{"", "unknown", "true", "null", "0x10", "Infinity", "01", "+1"} {
		t.Run(literal, func(t *testing.T) {
			_, err := execute(request{Action: "typed_number_literal", Literal: literal, Container: "root"})
			if err == nil || !strings.HasPrefix(err.Error(), "adapter_invalid:") {
				t.Fatalf("invalid literal acquired typed value authority: %v", err)
			}
		})
	}
	if _, err := execute(request{Action: "typed_number_literal", Literal: "1", Container: "unknown"}); err == nil ||
		!strings.HasPrefix(err.Error(), "adapter_invalid:") {
		t.Fatalf("invalid typed container was not refused: %v", err)
	}
	if _, err := execute(request{Action: "typed_number_literal", Literal: "1e309", Container: "root"}); err == nil ||
		!strings.HasPrefix(err.Error(), "noncanonical_number:") {
		t.Fatalf("overflow did not reach the actual typed number guard: %v", err)
	}
}

func TestRecoveryHeadroomActionUsesReplayWithoutPlanning(t *testing.T) {
	data, err := os.ReadFile("../fixtures/canonical-prefix.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Commits []json.RawMessage `json:"commits"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Commits) == 0 {
		t.Fatal("missing genesis fixture")
	}
	genesis, err := workqueue.Canonical(fixture.Commits[0])
	if err != nil {
		t.Fatal(err)
	}
	output, err := execute(request{Action: "recovery_headroom", Data: string(genesis) + "\n"})
	if err != nil {
		t.Fatal(err)
	}
	if len(output) != 1 || output["headroom_bytes"] != int64(0) {
		t.Fatalf("unexpected empty projection headroom: %#v", output)
	}
}

func TestRecoveryHeadroomActionRejectsMalformedLedger(t *testing.T) {
	if _, err := execute(request{Action: "recovery_headroom", Data: "{\n"}); err == nil {
		t.Fatal("accepted malformed ledger")
	}
}
