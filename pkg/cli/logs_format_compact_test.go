//go:build !integration

package cli

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func compactTestData() LogsData {
	return LogsData{
		Summary: LogsSummary{TotalRuns: 1},
		Runs: []RunData{{
			RunID:        1234567,
			WorkflowName: "Logs",
			WorkflowPath: ".github/workflows/logs.lock.yml",
			EngineID:     "copilot",
			Status:       "completed",
			Conclusion:   "success",
			Duration:     "1m2s",
			TokenUsage:   1200,
			Turns:        4,
			Event:        "push",
			Actor:        "octocat",
			Branch:       "main",
			CreatedAt:    time.Now(),
			WSRF:         "3.90",
		}},
	}
}

func TestRenderLogsCompactRendersRunsTableWithBorders(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	renderLogsCompactToWriter(&buf, compactTestData())
	out := buf.String()

	assert.Contains(t, out, "[runs]")
	assert.Contains(t, out, "╭")
	assert.Contains(t, out, "RUNID")
	assert.Contains(t, out, "1234567")
	assert.Contains(t, out, "logs")
	assert.Contains(t, out, "WSRF")
	assert.Contains(t, out, "3.90")
	assert.NotContains(t, out, "\x1b[", "non-TTY output should degrade to plain text")
}

func TestRenderLogsCompactVerboseRendersRunsTableWithBorders(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	renderLogsCompactVerboseToWriter(&buf, compactTestData())
	out := buf.String()

	assert.Contains(t, out, "[runs]")
	assert.Contains(t, out, "╭")
	assert.Contains(t, out, "CLASS")
	assert.Contains(t, out, "1234567")
	assert.Contains(t, out, "WSRF")
	assert.Contains(t, out, "3.90")
	assert.NotContains(t, out, "\x1b[", "non-TTY output should degrade to plain text")
}

func TestRenderLogsCompactSkipsSkippedAndCancelledRuns(t *testing.T) {
	t.Parallel()
	data := compactTestData()
	data.Runs = append(data.Runs,
		RunData{RunID: 222, WorkflowName: "skipped-wf", Status: "skipped", CreatedAt: time.Now()},
		RunData{RunID: 333, WorkflowName: "cancelled-wf", Status: "cancelled", CreatedAt: time.Now()},
		RunData{RunID: 444, WorkflowName: "skipped-c", Conclusion: "skipped", CreatedAt: time.Now()},
		RunData{RunID: 555, WorkflowName: "cancelled-c", Conclusion: "cancelled", CreatedAt: time.Now()},
	)

	var buf bytes.Buffer
	renderLogsCompactToWriter(&buf, data)
	out := buf.String()

	assert.NotContains(t, out, "222")
	assert.NotContains(t, out, "333")
	assert.NotContains(t, out, "444")
	assert.NotContains(t, out, "555")
	assert.Equal(t, 1, strings.Count(out, "1234567"))
}

type failingCompactLogsWriter struct {
	calls   int
	failAt  int
	failure error
}

func (w *failingCompactLogsWriter) Write(data []byte) (int, error) {
	w.calls++
	if w.calls == w.failAt {
		return len(data) / 2, w.failure
	}
	return len(data), nil
}

func TestRenderLogsCompactPropagatesEveryWriterFailure(t *testing.T) {
	t.Parallel()
	data := compactTestData()
	data.Runs[0].WorkQueue = &WorkQueueReport{FinishIntent: "completed"}
	data.LogsLocation = "/logs"
	failure := errors.New("diagnostic destination unavailable")
	for name, render := range map[string]func(io.Writer, LogsData) error{
		"compact": renderLogsCompactToWriter, "verbose": renderLogsCompactVerboseToWriter,
	} {
		t.Run(name, func(t *testing.T) {
			success := &failingCompactLogsWriter{}
			require.NoError(t, render(success, data))
			for failAt := 1; failAt <= success.calls; failAt++ {
				writer := &failingCompactLogsWriter{failAt: failAt, failure: failure}
				require.ErrorIs(t, render(writer, data), failure)
				assert.Equal(t, failAt, writer.calls, "no writes may follow the first failure")
			}
			short := &failingCompactLogsWriter{failAt: 1}
			require.ErrorIs(t, render(short, data), io.ErrShortWrite)
			assert.Equal(t, 1, short.calls)
			empty := &failingCompactLogsWriter{failAt: 1, failure: failure}
			require.ErrorIs(t, render(empty, LogsData{}), failure)
			assert.Equal(t, 1, empty.calls)
		})
	}
}
