package logger

import (
	"context"
	"log/slog"
	"strings"
)

// SlogHandler implements slog.Handler by wrapping a gh-aw Logger
// This allows integration with libraries that expect slog.Logger
type SlogHandler struct {
	logger *Logger
}

// NewSlogHandler creates a new slog.Handler that wraps a gh-aw Logger
func NewSlogHandler(logger *Logger) *SlogHandler {
	return &SlogHandler{logger: logger}
}

// Enabled reports whether the handler handles records at the given level.
// We enable all levels when our logger is enabled.
func (h *SlogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return h.logger.Enabled()
}

// Handle handles the Record.
// It will only be called when Enabled returns true.
func (h *SlogHandler) Handle(_ context.Context, r slog.Record) error {
	if !h.logger.Enabled() {
		return nil
	}

	// Format the message with attributes
	var msg strings.Builder
	msg.WriteString(r.Message)
	if r.NumAttrs() > 0 {
		// Format attributes as key=value pairs
		r.Attrs(func(a slog.Attr) bool {
			msg.WriteString(" " + a.Key + "=" + formatSlogValue(a.Value))
			return true
		})
	}

	// Log with appropriate level prefix
	levelPrefix := ""
	switch r.Level {
	case slog.LevelDebug:
		levelPrefix = "· "
	case slog.LevelInfo:
		levelPrefix = "· "
	case slog.LevelWarn:
		levelPrefix = "⚠ "
	case slog.LevelError:
		levelPrefix = "✗ "
	}

	h.logger.Print(levelPrefix + msg.String())
	return nil
}

// WithAttrs returns a new Handler whose attributes consist of
// both the receiver's attributes and the arguments.
// This implementation does not persist attributes.
func (h *SlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

// WithGroup returns a new Handler with the given group appended to
// the receiver's existing groups.
// This implementation does not persist groups.
func (h *SlogHandler) WithGroup(name string) slog.Handler {
	return h
}

// formatSlogValue formats an slog.Value for display
func formatSlogValue(v any) string {
	if val, ok := v.(slog.Value); ok {
		return val.String()
	}
	return slog.AnyValue(v).String()
}

// NewSlogLoggerWithHandler creates a new slog.Logger using an existing Logger instance
func NewSlogLoggerWithHandler(logger *Logger) *slog.Logger {
	handler := NewSlogHandler(logger)
	return slog.New(handler)
}
