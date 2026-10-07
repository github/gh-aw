//go:build !js && !wasm

package console

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"charm.land/huh/v2"
	"github.com/github/gh-aw/pkg/styles"
	"github.com/github/gh-aw/pkg/tty"
)

const (
	ansiSaveCursor       = "\0337"
	ansiRestoreCursor    = "\0338"
	ansiClearScreenBelow = "\033[J"
	promptReservedRows   = 12
)

// PromptForm wraps a huh form so completed questions are removed before the
// caller prints the decision result.
type PromptForm struct {
	*huh.Form
	out        io.Writer
	clearOnRun bool
}

// NewForm creates a huh form with gh-aw's default theme and accessibility mode.
func NewForm(groups ...*huh.Group) *PromptForm {
	accessible := IsAccessibleMode()
	clearOnRun := tty.IsStdinTerminal() && tty.IsStderrTerminal() && !accessible
	form := huh.NewForm(groups...).WithTheme(styles.HuhTheme).WithAccessible(accessible)
	if clearOnRun {
		form = form.WithHeight(promptReservedRows)
	}
	return &PromptForm{
		Form:       form,
		out:        stderrWriter(),
		clearOnRun: clearOnRun,
	}
}

// NewInputForm creates a themed, accessibility-aware single-input form.
func NewInputForm(input *huh.Input) *PromptForm {
	return NewForm(huh.NewGroup(input))
}

// NewSelectForm creates a themed, accessibility-aware single-select form.
func NewSelectForm[T comparable](selectField *huh.Select[T]) *PromptForm {
	return NewForm(huh.NewGroup(selectField))
}

// NewConfirmForm creates a themed, accessibility-aware single-confirm form.
func NewConfirmForm(confirm *huh.Confirm) *PromptForm {
	return NewForm(huh.NewGroup(confirm))
}

// Run runs the form and removes its rendered question when it exits.
func (f *PromptForm) Run() error {
	if !tty.IsStdinTerminal() || !tty.IsStderrTerminal() {
		return errors.New("interactive form not available (stdin and stderr must be TTYs)")
	}
	return f.run(func() error { return f.Form.Run() })
}

// RunWithContext runs the form with a context and removes its rendered question when it exits.
func (f *PromptForm) RunWithContext(ctx context.Context) error {
	if !tty.IsStdinTerminal() || !tty.IsStderrTerminal() {
		return errors.New("interactive form not available (stdin and stderr must be TTYs)")
	}
	return f.run(func() error { return f.Form.RunWithContext(ctx) })
}

func (f *PromptForm) run(runForm func() error) error {
	if !f.clearOnRun {
		fmt.Fprintln(f.out)
		return runForm()
	}
	// Reserve enough inline space before saving the cursor. Without this, a form
	// rendered near the bottom of the terminal can scroll its saved origin upward,
	// leaving completed question lines behind when the cursor is restored.
	fmt.Fprint(f.out, strings.Repeat("\n", promptReservedRows), cursorUp(promptReservedRows), ansiSaveCursor)
	fmt.Fprintln(f.out)
	defer fmt.Fprint(f.out, ansiRestoreCursor, ansiClearScreenBelow)
	return runForm()
}

func cursorUp(rows int) string {
	return fmt.Sprintf("\033[%dA", rows)
}

// IsCancelled reports whether err represents a deliberate user cancellation
// (Ctrl-C / Esc before form submission, i.e. huh.ErrUserAborted).
// Use this to distinguish graceful cancellation from genuine failures.
func IsCancelled(err error) bool {
	return errors.Is(err, huh.ErrUserAborted)
}
