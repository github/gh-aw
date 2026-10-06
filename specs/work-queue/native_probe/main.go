// Command native_probe is a local verification adapter, not a workflow helper.
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/github/gh-aw/pkg/workqueue"
)

type request struct {
	Action           string                       `json:"action"`
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

func execute(input request) (map[string]any, error) {
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
		file, err := os.Open(input.LedgerFile)
		if err != nil {
			return nil, err
		}
		info, err := file.Stat()
		file.Close()
		if err != nil {
			return nil, err
		}
		if info.Size() > 80<<20 {
			return nil, fmt.Errorf("resource_limit: adapter input exceeds 80 MiB")
		}
		data, err = os.ReadFile(input.LedgerFile)
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
	result := map[string]any{
		"selection": next, "packing": decision,
		"metrics": map[string]any{
			"parse_ms": parseMS, "cold_replay_ms": replayMS, "selection_ms": selectMS,
			"packing_ms": packMS, "serialization_ms": serializeMS,
			"input_bytes": len(data), "canonical_bytes": len(serialized),
			"canonical_sha256":  fmt.Sprintf("%x", sha256.Sum256(serialized)),
			"allocated_bytes":   after.TotalAlloc - before.TotalAlloc,
			"heap_in_use_bytes": after.HeapInuse, "stats": state.Stats,
			"rss_peak_bytes": processRSSPeak(),
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
		writer.Flush()
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
