package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/lifecycle"
)

const maxLifecycleCacheBytes = 16 * 1024 * 1024
const maxLifecycleCacheItems = 20000

type lifecycleCacheKey struct{}

var lifecycleCacheGates sync.Map

func lockLifecycleCache(ctx context.Context, runDir string) (func(), error) {
	return lockLifecycleStore(ctx, filepath.Dir(lifecycleStorePath(runDir)))
}

func lockLifecycleStore(ctx context.Context, dir string) (func(), error) {
	value, _ := lifecycleCacheGates.LoadOrStore(dir, make(chan struct{}, 1))
	gate, ok := value.(chan struct{})
	if !ok {
		return nil, errors.New("invalid lifecycle cache lock")
	}
	select {
	case gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := os.MkdirAll(dir, constants.DirPermSensitive); err != nil {
		<-gate
		return nil, err
	}
	lock := filepath.Join(dir, ".lifecycle-cache-lock")
	if err := os.Mkdir(lock, constants.DirPermSensitive); err != nil {
		<-gate
		return nil, fmt.Errorf("lock lifecycle cache %s: %w; another collector may be using this output directory", dir, err)
	}
	return func() {
		defer func() { <-gate }()
		if err := os.Remove(lock); err != nil {
			outcomeEvalLog.Printf("Release lifecycle cache lock %s: %v", lock, err)
		}
	}, nil
}

func loadLifecycleCache(runDir string) (map[string]lifecycle.Record, error) {
	return loadLifecycleRecords(lifecycleStorePath(runDir))
}

func loadLifecycleRecords(path string) (map[string]lifecycle.Record, error) {
	records := make(map[string]lifecycle.Record)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return records, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxLifecycleCacheBytes {
		return nil, errors.New("lifecycle cache must be a regular file no larger than 16 MiB")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(content) > maxLifecycleCacheBytes {
		return nil, errors.New("lifecycle cache exceeds the 16 MiB limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	for {
		var record cachedLogsJSONLRecord
		err := decoder.Decode(&record) //nolint:jsondecodertrailingdataunchecked // This JSONL stream consumes every record through EOF; trailing malformed data fails below.
		if errors.Is(err, io.EOF) {
			return records, nil
		}
		if err != nil {
			return nil, fmt.Errorf("decode retained lifecycle evidence: %w", err)
		}
		if record.SchemaVersion != cachedLogsJSONLLifecycleSchemaVersion || record.Kind != cachedLogsJSONLKindLifecycle || record.Lifecycle == nil {
			return nil, errors.New("incompatible lifecycle cache record; recollect delivery evidence into a new output directory")
		}
		if len(records) >= maxLifecycleCacheItems {
			return nil, errors.New("lifecycle cache exceeds the 20000-item limit")
		}
		incoming := *record.Lifecycle
		merged, err := lifecycle.Merge(records[incoming.ID], incoming)
		if err != nil {
			return nil, err
		}
		records[incoming.ID] = merged
	}
}

func writeLifecycleCache(runDir string, records map[string]lifecycle.Record) error {
	return writeLifecycleRecords(lifecycleStorePath(runDir), records)
}

func writeLifecycleRecords(path string, records map[string]lifecycle.Record) error {
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	cutoff := time.Now().Add(-365 * 24 * time.Hour)
	retained := 0
	for _, key := range keys {
		record := records[key]
		if record.Delivery.At.Before(cutoff) {
			continue
		}
		retained++
		if retained > maxLifecycleCacheItems {
			return errors.New("lifecycle cache exceeds the 20000-item limit; use a separate output directory")
		}
		if err := encoder.Encode(cachedLogsJSONLRecord{
			SchemaVersion: cachedLogsJSONLLifecycleSchemaVersion, Kind: cachedLogsJSONLKindLifecycle, Lifecycle: &record,
		}); err != nil {
			return err
		}
		if buffer.Len() > maxLifecycleCacheBytes {
			return errors.New("lifecycle cache exceeds the 16 MiB limit; use a separate output directory")
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), constants.DirPermSensitive); err != nil {
		return err
	}
	if err := writeSessionFileAtomically(path, buffer.Bytes()); err != nil {
		return fmt.Errorf("persist lifecycle observations: %w", err)
	}
	return nil
}

func (w *cachedLogsJSONLWriter) AppendLifecycle(record lifecycle.Record) error {
	if w == nil {
		return nil
	}
	data, err := json.Marshal(cachedLogsJSONLRecord{
		SchemaVersion: cachedLogsJSONLLifecycleSchemaVersion, Kind: cachedLogsJSONLKindLifecycle, Lifecycle: &record,
	})
	if err != nil {
		return err
	}
	return w.appendRecord(data)
}

func cacheLogsLifecycles(writer *cachedLogsJSONLWriter, reports []OutcomesData) error {
	for _, report := range reports {
		for _, record := range report.Records {
			if err := writer.AppendLifecycle(record); err != nil {
				return err
			}
		}
	}
	return nil
}
