package cli

import (
	"cmp"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

const logsCheckpointInterval = 30 * time.Second

type logsCheckpointWriter struct {
	mu       sync.Mutex
	latest   []ProcessedRun
	targets  map[string][]ProcessedRun
	stopOnce sync.Once
	stop     chan struct{}
	done     chan struct{}
}

func startLogsCheckpointWriter(opts LogsDownloadOptions, interval time.Duration) *logsCheckpointWriter {
	if (opts.SummaryFile == "" && opts.CachedJSON == "") || interval <= 0 {
		return nil
	}
	writer := &logsCheckpointWriter{
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	summaryPath := ""
	if opts.SummaryFile != "" {
		summaryPath = filepath.Join(opts.OutputDir, opts.SummaryFile)
	}
	go writer.run(summaryPath, opts.CachedJSON, opts.OutputDir, interval, opts.Verbose)
	return writer
}

func (w *logsCheckpointWriter) Update(runs []ProcessedRun) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.latest = append(w.latest[:0], runs...)
}

func (w *logsCheckpointWriter) UpdateTarget(target string, runs []ProcessedRun) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.targets == nil {
		w.targets = make(map[string][]ProcessedRun)
	}
	w.targets[target] = append(w.targets[target][:0], runs...)
	w.latest = w.latest[:0]
	for _, targetRuns := range w.targets {
		w.latest = append(w.latest, targetRuns...)
	}
	slices.SortStableFunc(w.latest, func(a, b ProcessedRun) int {
		if order := b.Run.CreatedAt.Compare(a.Run.CreatedAt); order != 0 {
			return order
		}
		return cmp.Compare(b.Run.DatabaseID, a.Run.DatabaseID)
	})
}

func (w *logsCheckpointWriter) Stop() {
	if w == nil {
		return
	}
	w.stopOnce.Do(func() {
		close(w.stop)
	})
	<-w.done
}

func (w *logsCheckpointWriter) run(summaryPath, cachedJSONPath, outputDir string, interval time.Duration, verbose bool) {
	defer close(w.done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	writeLatest := func() {
		w.mu.Lock()
		latest := append([]ProcessedRun(nil), w.latest...)
		w.mu.Unlock()
		if len(latest) == 0 {
			return
		}
		logsData := buildLogsData(latest, outputDir, nil)
		if summaryPath != "" {
			if err := writeSummaryFile(summaryPath, logsData, verbose); err != nil {
				logsOrchestratorLog.Printf("Failed to write intermediate logs summary: %v", err)
			}
		}
		if cachedJSONPath != "" {
			if err := writeCachedLogsJSON(cachedJSONPath, logsData, verbose); err != nil {
				logsOrchestratorLog.Printf("Failed to write intermediate cached logs JSON: %v", err)
			}
		}
	}
	for {
		select {
		case <-ticker.C:
			writeLatest()
		case <-w.stop:
			writeLatest()
			return
		}
	}
}
