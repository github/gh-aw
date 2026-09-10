package cli

import (
	"path/filepath"
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
	if (opts.SummaryFile == "" && opts.CachedLogs == "") || interval <= 0 {
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
	go writer.run(summaryPath, opts.CachedLogs, opts.OutputDir, interval, opts.Verbose)
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

func stopLogsCheckpointWriter(writer *logsCheckpointWriter) {
	if writer != nil {
		writer.Stop()
	}
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
