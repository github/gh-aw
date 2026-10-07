// Command native_probe is a local verification adapter, not a workflow helper.
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/github/gh-aw/pkg/workqueue"
)

type request struct {
	Action           string                       `json:"action"`
	Literal          string                       `json:"literal"`
	Container        string                       `json:"container"`
	Data             string                       `json:"data"`
	LedgerFile       string                       `json:"ledger_file"`
	Pool             string                       `json:"pool"`
	At               int64                        `json:"at"`
	Parameters       workqueue.DispatchParameters `json:"parameters"`
	RequestID        string                       `json:"request_id"`
	CommitID         string                       `json:"commit_id"`
	IncludeCanonical bool                         `json:"include_canonical"`
}

func millis(start time.Time) float64 {
	return float64(time.Since(start).Nanoseconds()) / 1e6
}

func readLedgerFile(filename string) (data []byte, err error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	const maxBytes = 80 << 20
	if info.Size() > maxBytes {
		return nil, errors.New("resource_limit: adapter input exceeds 80 MiB")
	}
	data, err = io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBytes {
		return nil, errors.New("resource_limit: adapter input exceeds 80 MiB")
	}
	return data, nil
}

func execute(input request) (map[string]any, error) {
	if input.Action == "typed_number_literal" {
		if input.Literal != "NaN" && input.Literal != "+Infinity" && input.Literal != "-Infinity" &&
			!json.Valid([]byte(input.Literal)) {
			return nil, fmt.Errorf("adapter_invalid: expected a typed number literal")
		}
		number, err := strconv.ParseFloat(input.Literal, 64)
		if err != nil && !errors.Is(err, strconv.ErrRange) {
			return nil, fmt.Errorf("adapter_invalid: expected a typed number literal")
		}
		var value any = number
		switch input.Container {
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
			return nil, fmt.Errorf("adapter_invalid: expected root, object, array, or nested container")
		}
		data, err := workqueue.CanonicalValue(value)
		return map[string]any{"canonical": string(data)}, err
	}
	if input.Action == "typed_canonical" {
		var value any
		if err := json.Unmarshal([]byte(input.Data), &value); err != nil {
			return nil, fmt.Errorf("adapter_invalid: invalid typed JSON input: %w", err)
		}
		// JSON syntax is valid, but encoding/json repairs invalid string Unicode.
		// Check original string tokens without imposing raw numeric spelling.
		for offset := 0; offset < len(input.Data); offset++ {
			if input.Data[offset] != '"' {
				continue
			}
			start := offset
			offset++
			for input.Data[offset] != '"' {
				if input.Data[offset] == '\\' {
					offset++
				}
				offset++
			}
			if _, err := workqueue.Canonical([]byte(input.Data[start : offset+1])); err != nil {
				return nil, err
			}
		}
		record, err := workqueue.NewRequest("typed-probe", "typed-probe", workqueue.Actor{}, value)
		return map[string]any{"canonical": string(record.Parameters)}, err
	}
	if input.Action == "canonical" {
		value, err := workqueue.Canonical([]byte(input.Data))
		return map[string]any{"canonical": string(value)}, err
	}
	if input.Action == "validate_commit" {
		commits, err := workqueue.Parse([]byte(input.Data + "\n"))
		if err != nil {
			return nil, err
		}
		if len(commits) != 1 {
			return nil, fmt.Errorf("adapter_invalid: expected exactly one commit")
		}
		value, err := workqueue.Canonical([]byte(input.Data))
		return map[string]any{"canonical": string(value)}, err
	}
	if input.Action == "validate_policy" {
		data, err := workqueue.Canonical([]byte(input.Data))
		if err != nil {
			return nil, err
		}
		var policy workqueue.Policy
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&policy); err != nil {
			return nil, fmt.Errorf("policy_invalid: invalid typed policy: %w", err)
		}
		if err := workqueue.ValidatePolicy(policy); err != nil {
			return nil, err
		}
		return map[string]any{"valid": true}, nil
	}
	data := []byte(input.Data)
	if input.LedgerFile != "" {
		var err error
		data, err = readLedgerFile(input.LedgerFile)
		if err != nil {
			return nil, err
		}
	}
	start := time.Now()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	commits, err := workqueue.Parse(data)
	if err != nil {
		return nil, err
	}
	parseMS := millis(start)
	start = time.Now()
	state, err := workqueue.Replay(commits)
	if err != nil {
		return nil, err
	}
	replayMS := millis(start)
	if input.Action == "recovery_headroom" {
		return map[string]any{"headroom_bytes": workqueue.RecoveryHeadroom(state)}, nil
	}
	start = time.Now()
	next, err := workqueue.PlanNext(state, input.Pool, input.At)
	if err != nil {
		return nil, err
	}
	selectMS := millis(start)
	start = time.Now()
	decision, err := workqueue.PlanDispatch(state, input.Parameters, input.RequestID, input.CommitID, input.At)
	if err != nil {
		return nil, err
	}
	packMS := millis(start)
	start = time.Now()
	serialized, err := workqueue.Serialize(commits)
	if err != nil {
		return nil, err
	}
	serializeMS := millis(start)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	rssPeak, err := processRSSPeak()
	if err != nil {
		return nil, fmt.Errorf("adapter_resource_error: failed to measure peak RSS: %w", err)
	}
	result := map[string]any{
		"selection": next, "packing": decision,
		"metrics": map[string]any{
			"parse_ms": parseMS, "cold_replay_ms": replayMS, "selection_ms": selectMS,
			"packing_ms": packMS, "serialization_ms": serializeMS,
			"input_bytes": len(data), "canonical_bytes": len(serialized),
			"canonical_sha256":  fmt.Sprintf("%x", sha256.Sum256(serialized)),
			"allocated_bytes":   after.TotalAlloc - before.TotalAlloc,
			"heap_in_use_bytes": after.HeapInuse, "stats": state.Stats,
			"rss_peak_bytes": rssPeak,
		},
	}
	if input.Action != "benchmark" {
		result["projection"] = state
	}
	if input.IncludeCanonical {
		result["canonical_ledger"] = string(serialized)
	}
	return result, nil
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64<<10), 96<<20)
	writer := bufio.NewWriter(os.Stdout)
	for scanner.Scan() {
		var input request
		err := json.Unmarshal(scanner.Bytes(), &input)
		var result map[string]any
		if err == nil {
			result, err = execute(input)
		}
		if err != nil {
			result = map[string]any{"error": err.Error()}
		}
		if err := json.NewEncoder(writer).Encode(result); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if err := writer.Flush(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
