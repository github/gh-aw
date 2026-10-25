//go:build !integration && !js && !wasm

package console

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
)

type failingConsoleWriter struct {
	writes int
	failAt int
	err    error
}

func (w *failingConsoleWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes == w.failAt {
		return 0, w.err
	}
	return len(p), nil
}

func TestPromptFormOutputFailurePreventsInput(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name       string
		clearOnRun bool
		failAt     int
		wantWrites int
	}{
		{name: "plain spacing", failAt: 1, wantWrites: 1},
		{name: "reserve space", clearOnRun: true, failAt: 1, wantWrites: 1},
		{name: "reserved spacing", clearOnRun: true, failAt: 2, wantWrites: 3},
	} {
		t.Run(tt.name, func(t *testing.T) {
			writeErr := errors.New("output unavailable")
			out := &failingConsoleWriter{failAt: tt.failAt, err: writeErr}
			form := &PromptForm{out: out, clearOnRun: tt.clearOnRun}
			called := false

			err := form.run(func() error {
				called = true
				return nil
			})

			require.ErrorIs(t, err, writeErr)
			require.False(t, called, "failed prompts must not consume input")
			require.Equal(t, tt.wantWrites, out.writes)
		})
	}
}

func TestPromptFormCleanupFailurePreservesResult(t *testing.T) {
	t.Parallel()
	for _, formErr := range []error{nil, errors.New("form failed")} {
		out := &failingConsoleWriter{failAt: 3, err: errors.New("cleanup failed")}
		form := &PromptForm{out: out, clearOnRun: true}

		err := form.run(func() error { return formErr })

		require.ErrorIs(t, err, formErr)
		require.Equal(t, 3, out.writes)
	}
}

func useClosedStderr(t *testing.T) {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	require.NoError(t, r.Close())
	require.NoError(t, w.Close())
	old := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = old })
}

func TestShowTextConfirmOutputFailurePreventsInput(t *testing.T) {
	useClosedStderr(t)
	input := strings.NewReader("yes\n")

	confirmed, err := showTextConfirm("Continue?", "Yes", "No", input)

	require.False(t, confirmed)
	require.ErrorIs(t, err, os.ErrClosed)
	require.ErrorContains(t, err, "failed to write confirmation title")
	require.Equal(t, len("yes\n"), input.Len())
}

func TestShowTextListOutputFailurePreventsInput(t *testing.T) {
	useClosedStderr(t)
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	_, err = io.WriteString(w, "1\n")
	require.NoError(t, err)
	require.NoError(t, w.Close())
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old })

	selected, err := showTextList("Choose", []ListItem{NewListItem("One", "First", "one")})

	require.Empty(t, selected)
	require.ErrorIs(t, err, os.ErrClosed)
	require.ErrorContains(t, err, "failed to write list title")
	remaining, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, "1\n", string(remaining))
}

func TestSpinnerProgramFailureReleasesTerminal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	spinner := &SpinnerWrapper{
		enabled: true,
		out:     io.Discard,
		program: tea.NewProgram(
			spinnerModel{message: "test", output: io.Discard},
			tea.WithContext(ctx),
			tea.WithOutput(io.Discard),
			tea.WithoutRenderer(),
			tea.WithInput(nil),
		),
	}

	spinner.Start()
	spinner.wg.Wait()

	require.False(t, spinner.running)
	next := &SpinnerWrapper{}
	require.True(t, claimActiveSpinner(next))
	releaseActiveSpinner(next)
}
