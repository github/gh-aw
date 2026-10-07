package workqueue

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestIndependentTypedCanonicalFixtures(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("strict typed canonical parity requires Node test tooling")
	}
	data, err := os.ReadFile("../../specs/work-queue/fixtures/canonical-typed.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Version int `json:"version"`
		Cases   []struct {
			Name      string `json:"name"`
			Literal   string `json:"literal"`
			Container string `json:"container"`
			Expected  string `json:"expected"`
			Code      string `json:"code"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Version != 1 || len(fixture.Cases) != 25 {
		t.Fatal("all independent typed canonical descriptors are required")
	}
	const script = `
const fs = require("node:fs");
const { canonical } = require("../../actions/setup/js/work_queue_codec.cjs");
const fixture = JSON.parse(fs.readFileSync(0, "utf8"));
process.stdout.write(JSON.stringify(fixture.cases.map(test => {
  const number = test.literal === "NaN" ? NaN :
    test.literal === "+Infinity" ? Infinity :
    test.literal === "-Infinity" ? -Infinity : Number(test.literal);
  if (test.literal === "-0" && !Object.is(number, -0) ||
      test.literal === "NaN" && !Number.isNaN(number) ||
      test.literal === "+Infinity" && number !== Infinity ||
      test.literal === "-Infinity" && number !== -Infinity) {
    throw new Error("typed literal transport erased the numeric value");
  }
  const containers = {
    root: number, object: { value: number }, array: [number],
    nested: { payload: { numbers: [number], sentinel: "9007199254740993" } }
  };
  if (!Object.hasOwn(containers, test.container)) throw new Error("unknown typed container");
  try { return { canonical: canonical(containers[test.container]) }; }
  catch (error) {
    if (typeof error.code !== "string") throw error;
    return { code: error.code };
  }
})));
`
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, node, "-e", script)
	command.Stdin = bytes.NewReader(data)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("JavaScript typed canonical parity: %v\n%s", err, stderr.String())
	}
	var results []struct {
		Canonical string `json:"canonical"`
		Code      string `json:"code"`
	}
	if err := json.Unmarshal(output, &results); err != nil || len(results) != len(fixture.Cases) {
		t.Fatalf("expected all %d typed results, got %d / %v", len(fixture.Cases), len(results), err)
	}
	names := map[string]bool{}
	for index, test := range fixture.Cases {
		if names[test.Name] {
			t.Fatalf("duplicate independent descriptor %q", test.Name)
		}
		names[test.Name] = true
		t.Run(test.Name, func(t *testing.T) {
			result := results[index]
			if result.Code != test.Code || result.Canonical != test.Expected {
				t.Fatalf("JavaScript expected literal %q / %s, got %q / %s",
					test.Expected, test.Code, result.Canonical, result.Code)
			}
			number, err := strconv.ParseFloat(test.Literal, 64)
			if err != nil {
				t.Fatal(err)
			}
			if test.Literal == "-0" && !math.Signbit(number) ||
				test.Literal == "NaN" && !math.IsNaN(number) ||
				test.Literal == "+Infinity" && !math.IsInf(number, 1) ||
				test.Literal == "-Infinity" && !math.IsInf(number, -1) {
				t.Fatal("descriptor transport erased the actual typed numeric value")
			}
			var value any = number
			switch test.Container {
			case "root":
			case "object":
				value = map[string]any{"value": number}
			case "array":
				value = []any{number}
			case "nested":
				value = map[string]any{"payload": map[string]any{
					"numbers": []any{number}, "sentinel": "9007199254740993",
				}}
			default:
				t.Fatalf("unsupported independent descriptor container %q", test.Container)
			}
			canonical, err := CanonicalValue(value)
			if test.Code != "" {
				if err == nil || !strings.HasPrefix(err.Error(), test.Code+":") || canonical != nil {
					t.Fatalf("expected exact %s rejection, got %q / %v", test.Code, canonical, err)
				}
				return
			}
			if err != nil || string(canonical) != test.Expected {
				t.Fatalf("expected literal canonical %s, got %s / %v", test.Expected, canonical, err)
			}
		})
	}
}

func TestCanonicalValueNonNumericMarshalFailuresRemainExplicit(t *testing.T) {
	if _, err := CanonicalValue(make(chan int)); err == nil || strings.HasPrefix(err.Error(), "noncanonical_number:") {
		t.Fatalf("unsupported type lost its own explicit failure: %v", err)
	}
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	if _, err := CanonicalValue(cyclic); err == nil || strings.HasPrefix(err.Error(), "noncanonical_number:") {
		t.Fatalf("cyclic data was misclassified as a number: %v", err)
	}
}

func TestTypedCanonicalValidationPrecedesLossyStructEncoding(t *testing.T) {
	type payload struct {
		Value   float64 `json:"value,omitempty"`
		Ignored float64 `json:"-"`
	}
	negativeZero := math.Copysign(0, -1)
	for _, value := range []any{
		payload{Value: negativeZero},
		&negativeZero,
		float32(math.Inf(1)),
		[]float32{float32(math.NaN())},
	} {
		if _, err := CanonicalValue(value); err == nil || !strings.HasPrefix(err.Error(), "noncanonical_number:") {
			t.Fatalf("typed invalid number disappeared before validation: %T / %v", value, err)
		}
	}
	data, err := CanonicalValue(payload{Ignored: math.NaN()})
	if err != nil || string(data) != "{}" {
		t.Fatalf("explicitly excluded non-wire field entered the canonical contract: %s / %v", data, err)
	}
}

func TestTypedCanonicalNestingUsesJSONDepthNotInterfaceWrappers(t *testing.T) {
	var value any = float64(0)
	for range 64 {
		value = []any{value}
	}
	expected := strings.Repeat("[", 64) + "0" + strings.Repeat("]", 64)
	data, err := CanonicalValue(value)
	if err != nil || string(data) != expected {
		t.Fatalf("valid depth64 typed JSON differed from raw canonical profile: %v", err)
	}
	if _, err := CanonicalValue([]any{value}); err == nil || !strings.HasPrefix(err.Error(), "resource_limit:") {
		t.Fatalf("typed depth65 did not reject with its exact bound: %v", err)
	}
}
