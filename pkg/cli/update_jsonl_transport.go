package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
)

// Re-encode relaunched CLI events to retain one command lifecycle and aggregate
// changed-file counts without redirecting process-global stdout.
type updateJSONLForwarder struct {
	ctx         context.Context
	pending     []byte
	discardLine bool
	err         error
}

func (w *updateJSONLForwarder) Write(data []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	written := len(data)
	if w.discardLine {
		newline := bytes.IndexByte(data, '\n')
		if newline < 0 {
			return written, nil
		}
		data = data[newline+1:]
		w.discardLine = false
	}
	w.pending = append(w.pending, data...)
	for {
		line, rest, found := bytes.Cut(w.pending, []byte{'\n'})
		if !found {
			break
		}
		if err := w.forwardLine(line); err != nil {
			w.err = err
			return 0, err
		}
		w.pending = rest
	}
	if len(w.pending) > 8*1024*1024 {
		w.pending = nil
		w.discardLine = true
	}
	return written, nil
}

func (w *updateJSONLForwarder) forwardLine(line []byte) error {
	var event updateJSONLEvent
	if err := json.Unmarshal(line, &event); err != nil {
		return nil
	}
	if event.SchemaVersion != 1 || event.Command != "upgrade" || !validUpdateForwardEvent(event.Event) {
		return nil
	}
	return emitUpdateEvent(w.ctx, event)
}

func (w *updateJSONLForwarder) finish() error {
	if w.err != nil {
		return w.err
	}
	if !w.discardLine && len(w.pending) > 0 {
		if err := w.forwardLine(w.pending); err != nil {
			return err
		}
	}
	w.pending = nil
	return nil
}

func validUpdateForwardEvent(event string) bool {
	return slices.Contains([]string{"change", "warning", "codemod", "pull_request", "issue"}, event)
}

func reportUpdateRolloutResult(ctx context.Context, result orgRolloutResult) error {
	if updateReporter(ctx) == nil {
		return nil
	}
	for _, event := range result.Events {
		if event.SchemaVersion != 1 || !validUpdateForwardEvent(event.Event) {
			return fmt.Errorf("invalid JSON Lines event in worker result for %s", result.Repo)
		}
		event.Repository = result.Repo
		if err := emitUpdateEvent(ctx, event); err != nil {
			return err
		}
	}
	if result.Preview != nil {
		preview := *result.Preview
		preview.Repo = result.Repo
		if err := reportUpdatePreview(ctx, preview, result.Status); err != nil {
			return err
		}
	}
	return emitUpdateEvent(ctx, updateJSONLEvent{
		Event: "repository_completed", Repository: result.Repo, Status: result.Status, URL: result.URL, Error: result.Error,
	})
}
