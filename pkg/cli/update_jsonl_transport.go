package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// Re-encode relaunched CLI events to retain one command lifecycle and aggregate
// changed-file counts without redirecting process-global stdout.
type updateJSONLForwarder struct {
	ctx     context.Context
	pending []byte
	err     error
}

func (w *updateJSONLForwarder) Write(data []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	w.pending = append(w.pending, data...)
	if len(w.pending) > 8*1024*1024 {
		w.err = errors.New("upgrade JSON Lines event exceeds 8 MiB")
		return 0, w.err
	}
	for {
		line, rest, found := bytes.Cut(w.pending, []byte{'\n'})
		if !found {
			break
		}
		var event updateJSONLEvent
		if err := json.Unmarshal(line, &event); err != nil {
			w.err = fmt.Errorf("invalid upgrade JSON Lines event: %w", err)
			return 0, w.err
		}
		if event.SchemaVersion != 1 || event.Command != "upgrade" || event.Event == "" ||
			event.Event == "command_started" || event.Event == "command_completed" {
			w.err = errors.New("invalid upgrade JSON Lines event contract")
			return 0, w.err
		}
		if !validUpdateForwardEvent(event.Event) {
			w.pending = rest
			continue
		}
		if err := emitUpdateEvent(w.ctx, event); err != nil {
			w.err = err
			return 0, err
		}
		w.pending = rest
	}
	return len(data), nil
}

func (w *updateJSONLForwarder) finish() error {
	if w.err != nil {
		return w.err
	}
	if len(w.pending) != 0 {
		return errors.New("incomplete upgrade JSON Lines event")
	}
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
