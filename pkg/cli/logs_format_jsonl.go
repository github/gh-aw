package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

type logsJSONLContextKey struct{}

type logsJSONLEvent struct {
	Type       string    `json:"type"`
	Stage      string    `json:"stage,omitempty"`
	Repository string    `json:"repository,omitempty"`
	Workflow   string    `json:"workflow,omitempty"`
	RunID      int64     `json:"run_id,omitempty"`
	Count      int       `json:"count,omitempty"`
	Cached     bool      `json:"cached,omitempty"`
	Message    string    `json:"message,omitempty"`
	Run        *RunData  `json:"run,omitempty"`
	Data       *LogsData `json:"data,omitempty"`
}

// logsJSONLStream serializes concurrent producers without buffering output.
// The first write failure cancels collection and is returned by finish.
type logsJSONLStream struct {
	mu               sync.Mutex
	encoder          *json.Encoder
	verbose          bool
	includeWorkQueue bool
	err              error
	cancel           context.CancelFunc
}

func newLogsJSONLStream(w io.Writer, verbose, includeWorkQueue bool) *logsJSONLStream {
	return &logsJSONLStream{encoder: json.NewEncoder(w), verbose: verbose, includeWorkQueue: includeWorkQueue}
}

func startLogsJSONL(ctx context.Context, format string, verbose, includeWorkQueue bool) (context.Context, *logsJSONLStream, func(*error)) {
	if format != "jsonl" {
		return ctx, nil, func(*error) {}
	}
	ctx, cancel := context.WithCancel(ctx)
	stream := newLogsJSONLStream(os.Stdout, verbose, includeWorkQueue)
	stream.cancel = cancel
	ctx = context.WithValue(ctx, logsJSONLContextKey{}, stream)
	stream.emit(logsJSONLEvent{Type: "progress", Stage: "started"})
	return ctx, stream, stream.finish
}

func logsJSONLFromContext(ctx context.Context) *logsJSONLStream {
	if stream, ok := ctx.Value(logsJSONLContextKey{}).(*logsJSONLStream); ok {
		return stream
	}
	return nil
}

func (s *logsJSONLStream) emit(event logsJSONLEvent) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return
	}
	if err := s.encoder.Encode(event); err != nil {
		s.err = fmt.Errorf("failed to write logs JSONL output: %w", err)
		if s.cancel != nil {
			s.cancel()
		}
	}
}

func (s *logsJSONLStream) finish(err *error) {
	event := logsJSONLEvent{Type: "complete"}
	if *err != nil {
		event.Type = "error"
		event.Message = (*err).Error()
	}
	s.emit(event)
	s.mu.Lock()
	defer s.mu.Unlock()
	*err = errors.Join(*err, s.err)
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *logsJSONLStream) run(run ProcessedRun) {
	if s == nil {
		return
	}
	updateLogsRunMessageCounts(&run)
	data := buildLogsData([]ProcessedRun{run}, "", nil)
	data.Runs = workQueueReportRuns(data.Runs, s.includeWorkQueue)
	if !s.verbose {
		data = compactLogsData(data)
	}
	for _, record := range data.Runs {
		s.emit(logsJSONLEvent{Type: "run", Run: &record})
	}
}

func (s *logsJSONLStream) summary(data LogsData) error {
	if s == nil {
		return errors.New("logs JSONL output stream is not initialized")
	}
	if !s.verbose {
		data = compactLogsData(data)
	}
	s.emit(logsJSONLEvent{Type: "summary", Data: &data})
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func emitLogsDownloadProgress(ctx context.Context, result DownloadResult) {
	event := logsJSONLEvent{
		Type: "progress", Stage: "downloaded", RunID: result.Run.DatabaseID,
		Repository: result.Run.Repository, Cached: result.Cached,
	}
	switch {
	// Panic recovery marks the run skipped to exclude it from analysis, not
	// because downloading was intentionally skipped.
	case errors.Is(result.Error, errRunDownloadPanicked):
		event.Stage = "failed"
	case result.Skipped:
		event.Stage = "skipped"
	case result.Error != nil:
		event.Stage = "failed"
	}
	if result.Error != nil {
		event.Message = result.Error.Error()
	}
	logsJSONLFromContext(ctx).emit(event)
}
