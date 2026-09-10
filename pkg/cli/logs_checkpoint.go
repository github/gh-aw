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
	stopOnce sync.Once
	stop     chan struct{}
	done     chan struct{}
}

func startLogsCheckpointWriter(opts LogsDownloadOptions, interval time.Duration) *logsCheckpointWriter {
	if opts.SummaryFile == "" || interval <= 0 {
		return nil
	}
	writer := &logsCheckpointWriter{
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	go writer.run(filepath.Join(opts.OutputDir, opts.SummaryFile), opts.OutputDir, interval, opts.Verbose)
	return writer
}

func (w *logsCheckpointWriter) Update(runs []ProcessedRun) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.latest = append(w.latest[:0], runs...)
}

func (w *logsCheckpointWriter) Stop() {
	w.stopOnce.Do(func() {
		close(w.stop)
	})
	<-w.done
}

func (w *logsCheckpointWriter) run(summaryPath, outputDir string, interval time.Duration, verbose bool) {
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
		if err := writeSummaryFile(summaryPath, buildLogsData(latest, outputDir, nil), verbose); err != nil {
			logsOrchestratorLog.Printf("Failed to write intermediate logs summary: %v", err)
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
