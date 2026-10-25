//go:build !js && !wasm

// Package console provides terminal UI components including spinners for
// long-running operations.
//
// # Spinner Component
//
// The spinner provides visual feedback during long-running operations with a minimal
// dot animation (⠋ ⠙ ⠹ ⠸ ⠼ ⠴ ⠦ ⠧ ⠇ ⠏). It automatically adapts to the environment:
//   - TTY Detection: Spinners only animate in terminal environments (disabled in pipes/redirects)
//   - Accessibility: Respects ACCESSIBLE environment variable to disable animations
//   - Color Adaptation: Uses lipgloss adaptive colors for light/dark terminal themes
//
// # Implementation
//
// This spinner uses idiomatic Bubble Tea patterns with tea.NewProgram() for proper
// message handling and rendering pipeline integration. It includes thread-safe
// lifecycle management:
//   - Thread-safe start/stop tracking with mutex protection
//   - Safe to call Stop/StopWithMessage before Start (no-op or message-only)
//   - Prevents multiple concurrent Start calls
//   - No deadlock when stopping before goroutine initializes
//   - Leverages Bubble Tea's message passing for updates
//
// # Usage Example
//
//	spinner := console.NewSpinner("Loading...")
//	spinner.Start()
//	// Long-running operation
//	spinner.Stop()
//
// # Accessibility
//
// Spinners respect the ACCESSIBLE environment variable. When ACCESSIBLE is set to any value,
// spinner animations are disabled to support screen readers and accessibility tools.
//
//	export ACCESSIBLE=1
//	gh aw compile workflow.md  # Spinners will be disabled
package console

import (
	"fmt"
	"io"
	"sync"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/styles"
	"github.com/github/gh-aw/pkg/tty"
)

var spinnerLog = logger.New("console:spinner")

// updateMessageMsg is a custom message for updating the spinner message
type updateMessageMsg string

// spinnerModel is the Bubble Tea model for the spinner.
// Because we use tea.WithoutRenderer(), we must manually print in Update().
type spinnerModel struct {
	spinner spinner.Model
	message string
	output  io.Writer
}

func (m spinnerModel) Init() tea.Cmd  { return m.spinner.Tick }
func (m spinnerModel) View() tea.View { return tea.View{} } // Not used with WithoutRenderer

func (m spinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case updateMessageMsg:
		m.message = string(msg)
		m.render()
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		m.render()
		return m, cmd
	}
	return m, nil
}

// render manually prints the spinner frame (required when using WithoutRenderer)
func (m spinnerModel) render() {
	if m.output != nil {
		if _, err := fmt.Fprintf(m.output, "%s%s%s %s", ansiCarriageReturn, ansiClearLine, m.spinner.View(), m.message); err != nil {
			spinnerLog.Printf("Failed to render spinner frame: %v", err)
		}
	}
}

// SpinnerWrapper wraps the spinner functionality with TTY detection and Bubble Tea program
type SpinnerWrapper struct {
	program    *tea.Program
	out        io.Writer
	enabled    bool
	running    bool
	suppressed bool
	mu         sync.Mutex
	wg         sync.WaitGroup
}

// Global spinner coordination.
//
// Only a single spinner may render to the terminal at a time. Multiple concurrent
// spinners (e.g. an outer "Sampling…" spinner plus several parallel per-run
// "Downloading…" spinners) would otherwise each write carriage-return/clear-line
// escape sequences to the same stderr line, producing visible flicker.
//
// The first spinner to Start() claims the terminal and animates; any spinner that
// Start()s while another is already active is "suppressed" — it tracks its running
// lifecycle so Stop()/UpdateMessage() stay well-behaved, but it does not render.
var (
	globalSpinnerMu     sync.Mutex
	globalSpinnerActive *SpinnerWrapper
)

// claimActiveSpinner attempts to make s the single actively-rendering spinner.
// It returns true when s becomes active, or false when another spinner already owns
// the terminal (in which case s should suppress its rendering).
func claimActiveSpinner(s *SpinnerWrapper) bool {
	globalSpinnerMu.Lock()
	defer globalSpinnerMu.Unlock()
	if globalSpinnerActive != nil {
		return false
	}
	globalSpinnerActive = s
	return true
}

// releaseActiveSpinner clears s as the active spinner. It is a no-op when s never
// claimed the terminal (i.e. it was suppressed), so it is always safe to call.
func releaseActiveSpinner(s *SpinnerWrapper) {
	globalSpinnerMu.Lock()
	defer globalSpinnerMu.Unlock()
	if globalSpinnerActive == s {
		globalSpinnerActive = nil
	}
}

// NewSpinner creates a new spinner with the given message using MiniDot style.
// Automatically disabled when not running in a TTY or when ACCESSIBLE env var is set.
func NewSpinner(message string) *SpinnerWrapper {
	isTTY := tty.IsStderrTerminal()
	isAccessible := IsAccessibleMode()
	enabled := isTTY && !isAccessible
	spinnerLog.Printf("Creating spinner: message=%q, tty=%t, accessible=%t, enabled=%t", message, isTTY, isAccessible, enabled)
	out := stderrWriter()
	s := &SpinnerWrapper{enabled: enabled, out: out}

	if enabled {
		model := spinnerModel{
			spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(styles.Info)),
			message: message,
			output:  out,
		}
		// tea.WithInput(nil) disables stdin reading so the spinner does not consume key
		// events that should be handled by subsequent interactive forms (e.g. huh.Select).
		// Ctrl+C is still handled via OS signal delivery (SIGINT), which bubbletea
		// processes independently of the input reader. This input-disabled path is
		// officially supported as of bubbletea v2.0.7 (charmbracelet/bubbletea#1680).
		s.program = tea.NewProgram(model, tea.WithOutput(out), tea.WithoutRenderer(), tea.WithInput(nil))
	}
	return s
}

func (s *SpinnerWrapper) Start() {
	if s.enabled && s.program != nil {
		shouldStart := func() bool {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.running {
				return false
			}
			// Only one spinner renders at a time. If another spinner already owns the
			// terminal, mark this one suppressed: it stays "running" for lifecycle
			// bookkeeping but does not animate, avoiding concurrent-spinner flicker.
			if !claimActiveSpinner(s) {
				s.suppressed = true
				s.running = true
				return false
			}
			s.suppressed = false
			s.running = true
			s.wg.Add(1)
			return true
		}()
		if !shouldStart {
			spinnerLog.Print("Spinner already running or suppressed by an active spinner, skipping render")
			return
		}
		spinnerLog.Print("Starting spinner")
		go func() {
			defer s.wg.Done()
			defer func() {
				s.mu.Lock()
				// Release the terminal slot before exposing the stopped state so a
				// concurrent Start() cannot observe running=false while this wrapper
				// still owns the global slot and become permanently suppressed.
				releaseActiveSpinner(s)
				s.running = false
				s.mu.Unlock()
			}()
			defer func() {
				if r := recover(); r != nil {
					spinnerLog.Printf("Panic in spinner program (recovered): %v", r)
				}
			}()
			if _, err := s.program.Run(); err != nil {
				spinnerLog.Printf("Error running spinner program: %v", err)
			}
		}()
	}
}

func (s *SpinnerWrapper) Stop() {
	if s.enabled && s.program != nil {
		var wasRendering bool
		wasRunning := func() bool {
			s.mu.Lock()
			defer s.mu.Unlock()
			if !s.running {
				return false
			}
			s.running = false
			wasRendering = !s.suppressed
			s.suppressed = false
			return true
		}()
		if !wasRunning {
			return
		}
		if wasRendering {
			spinnerLog.Print("Stopping spinner")
			s.program.Quit()
			s.wg.Wait() // Wait for the goroutine to complete (goroutine releases the slot)
			if _, err := fmt.Fprintf(s.out, "%s%s", ansiCarriageReturn, ansiClearLine); err != nil {
				spinnerLog.Printf("Failed to clear spinner line: %v", err)
			}
		} else {
			// Suppressed spinner has no goroutine; release the slot directly (no-op).
			releaseActiveSpinner(s)
		}
	}
}

func (s *SpinnerWrapper) StopWithMessage(msg string) {
	if s.enabled && s.program != nil {
		var wasRendering bool
		wasRunning := func() bool {
			s.mu.Lock()
			defer s.mu.Unlock()
			if !s.running {
				return false
			}
			s.running = false
			wasRendering = !s.suppressed
			s.suppressed = false
			return true
		}()
		if wasRunning {
			if wasRendering {
				s.program.Quit()
				s.wg.Wait() // Wait for the goroutine to complete (goroutine releases the slot)
				if _, err := fmt.Fprintf(s.out, "%s%s%s\n", ansiCarriageReturn, ansiClearLine, msg); err != nil {
					spinnerLog.Printf("Failed to write spinner completion message: %v", err)
				}
			} else {
				// Suppressed spinner never rendered; release the slot and print the message.
				// Still emit CR+ClearLine so the message is not appended to any active
				// spinner frame that may currently occupy the terminal line.
				releaseActiveSpinner(s)
				if _, err := fmt.Fprintf(s.out, "%s%s%s\n", ansiCarriageReturn, ansiClearLine, msg); err != nil {
					spinnerLog.Printf("Failed to write spinner completion message: %v", err)
				}
			}
		} else {
			// Still print the message even if spinner wasn't running
			if _, err := fmt.Fprintf(s.out, "%s\n", msg); err != nil {
				spinnerLog.Printf("Failed to write spinner completion message: %v", err)
			}
		}
	} else if msg != "" {
		// If spinner is disabled, still print the message for user feedback
		if _, err := fmt.Fprintf(s.out, "%s\n", msg); err != nil {
			spinnerLog.Printf("Failed to write spinner completion message: %v", err)
		}
	}
}

func (s *SpinnerWrapper) UpdateMessage(message string) {
	if s.enabled && s.program != nil {
		running := func() bool {
			s.mu.Lock()
			defer s.mu.Unlock()
			// Suppressed spinners have no live program to receive the update.
			return s.running && !s.suppressed
		}()
		if running {
			s.program.Send(updateMessageMsg(message))
		}
	}
}
