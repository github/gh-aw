//go:build !integration && !js && !wasm

package console

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"charm.land/huh/v2"
	"github.com/stretchr/testify/require"
)

func TestPromptWrappersReturnNonNilForms(t *testing.T) {
	t.Parallel()
	var inputValue string
	inputForm := NewInputForm(huh.NewInput().Value(&inputValue))
	require.NotNil(t, inputForm)

	var selectValue string
	require.NotNil(t, NewSelectForm(huh.NewSelect[string]().
		Options(huh.NewOption("Option", "option")).
		Value(&selectValue)))

	var confirmValue bool
	require.NotNil(t, NewConfirmForm(huh.NewConfirm().Value(&confirmValue)))
}

func TestPromptFormClearsCompletedQuestion(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	form := &PromptForm{out: &output, clearOnRun: true}

	err := form.run(func() error { return nil })

	require.NoError(t, err)
	require.Equal(t, strings.Repeat("\n", promptReservedRows)+cursorUp(promptReservedRows)+ansiSaveCursor+"\n"+ansiRestoreCursor+ansiClearScreenBelow, output.String())
}

func TestPromptFormDoesNotClearAccessibleOrNonTTYQuestion(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	form := &PromptForm{out: &output, clearOnRun: false}

	err := form.run(func() error { return nil })

	require.NoError(t, err)
	require.Equal(t, "\n", output.String())
}

func TestPromptFormRunRejectsNonTTYStdin(t *testing.T) {
	for _, accessible := range []string{"0", "1"} {
		t.Run("accessible="+accessible, func(t *testing.T) {
			t.Setenv("ACCESSIBLE", accessible)
			for _, method := range []string{"Run", "RunWithContext"} {
				t.Run(method, func(t *testing.T) {
					oldStdin := os.Stdin
					r, w, err := os.Pipe()
					require.NoError(t, err)
					t.Cleanup(func() { os.Stdin = oldStdin })
					t.Cleanup(func() { r.Close() })
					t.Cleanup(func() { w.Close() })
					_, err = w.WriteString("n\n")
					require.NoError(t, err)
					require.NoError(t, w.Close())
					os.Stdin = r

					var confirmed bool
					form := NewConfirmForm(huh.NewConfirm().Value(&confirmed))
					if method == "Run" {
						err = form.Run()
					} else {
						err = form.RunWithContext(context.Background())
					}
					require.ErrorContains(t, err, "stdin and stderr must be TTYs")
					var remaining bytes.Buffer
					_, err = remaining.ReadFrom(r)
					require.NoError(t, err)
					require.Equal(t, "n\n", remaining.String(), "rejected prompts must not consume piped answers")
				})
			}
		})
	}
}

func TestIsCancelled(t *testing.T) {
	t.Parallel()
	t.Run("returns true for huh.ErrUserAborted", func(t *testing.T) {
		require.True(t, IsCancelled(huh.ErrUserAborted))
	})

	t.Run("returns true for wrapped huh.ErrUserAborted", func(t *testing.T) {
		wrapped := fmt.Errorf("outer: %w", huh.ErrUserAborted)
		require.True(t, IsCancelled(wrapped))
	})

	t.Run("returns false for other errors", func(t *testing.T) {
		require.False(t, IsCancelled(errors.New("some other error")))
	})

	t.Run("returns false for nil", func(t *testing.T) {
		require.False(t, IsCancelled(nil))
	})
}
