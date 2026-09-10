package cli

import (
	"path/filepath"
	"time"
)

const logsCheckpointInterval = 30 * time.Second

type logsCheckpointWriter struct {
	updates chan []ProcessedRun
	stop    chan struct{}
	done    chan struct{}
}

func startLogsCheckpointWriter(opts LogsDownloadOptions, interval time.Duration) *logsCheckpointWriter {
	if opts.SummaryFile == "" || interval <= 0 {
		return nil
	}
	writer := &logsCheckpointWriter{
		updates: make(chan []ProcessedRun, 1),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	go writer.run(filepath.Join(opts.OutputDir, opts.SummaryFile), opts.OutputDir, interval, opts.Verbose)
	return writer
}

func (w *logsCheckpointWriter) Update(runs []ProcessedRun) {
	snapshot := append([]ProcessedRun(nil), runs...)
	select {
	case w.updates <- snapshot:
	default:
		select {
		case <-w.updates:
		default:
		}
		w.updates <- snapshot
	}
}

func (w *logsCheckpointWriter) Stop() {
	close(w.stop)
	<-w.done
}

func (w *logsCheckpointWriter) run(summaryPath, outputDir string, interval time.Duration, verbose bool) {
	defer close(w.done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var latest []ProcessedRun
	writeLatest := func() {
		if len(latest) == 0 {
			return
		}
		if err := writeSummaryFile(summaryPath, buildLogsData(latest, outputDir, nil), verbose); err != nil {
			logsOrchestratorLog.Printf("Failed to write intermediate logs summary: %v", err)
		}
	}
	for {
		select {
		case latest = <-w.updates:
		case <-ticker.C:
			writeLatest()
		case <-w.stop:
			select {
			case latest = <-w.updates:
			default:
			}
			writeLatest()
			return
		}
	}
}
