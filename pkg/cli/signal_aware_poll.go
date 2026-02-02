package cli

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var pollLog = logger.New("cli:signal_aware_poll")

// PollResult represents the result of a polling operation
type PollResult int

const (
	// PollContinue indicates polling should continue
	PollContinue PollResult = iota
	// PollSuccess indicates polling completed successfully
	PollSuccess
	// PollFailure indicates polling failed
	PollFailure
)

// PollOptions contains configuration for signal-aware polling
type PollOptions struct {
	// Interval between poll attempts
	PollInterval time.Duration
	// Timeout for the entire polling operation
	Timeout time.Duration
	// Function to call on each poll iteration
	// Should return PollContinue to keep polling, PollSuccess to succeed, or PollFailure to fail
	PollFunc func() (PollResult, error)
	// Message to display when polling starts (optional)
	StartMessage string
	// Message to display on each poll iteration (optional)
	ProgressMessage string
	// Message to display on successful completion (optional)
	SuccessMessage string
	// Whether to show verbose progress messages
	Verbose bool
}

// PollWithSignalHandling polls with a function until it succeeds, fails, times out, or receives an interrupt signal
// This provides a reusable pattern for any operation that needs to poll with graceful Ctrl-C handling
func PollWithSignalHandling(options PollOptions) error {
	pollLog.Printf("Starting polling: interval=%v, timeout=%v", options.PollInterval, options.Timeout)

	if options.Verbose && options.StartMessage != "" {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(options.StartMessage))
	}

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	// Set up timeout
	start := time.Now()
	ticker := time.NewTicker(options.PollInterval)
	defer ticker.Stop()

	// Perform initial check immediately
	result, err := options.PollFunc()
	switch result {
	case PollSuccess:
		if options.Verbose && options.SuccessMessage != "" {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(options.SuccessMessage))
		}
		return nil
	case PollFailure:
		return err
	}

	// Continue polling
	for {
		select {
		case <-sigChan:
			pollLog.Print("Received interrupt signal")
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Received interrupt signal, stopping wait..."))
			return fmt.Errorf("interrupted by user")

		case <-ticker.C:
			// Check if timeout exceeded
			if options.Timeout > 0 && time.Since(start) > options.Timeout {
				pollLog.Printf("Timeout exceeded: %v", options.Timeout)
				return fmt.Errorf("operation timed out after %v", options.Timeout)
			}

			// Poll for status
			result, err := options.PollFunc()

			switch result {
			case PollSuccess:
				if options.Verbose && options.SuccessMessage != "" {
					fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(options.SuccessMessage))
				}
				return nil
			case PollFailure:
				return err
			}

			// Still waiting, show progress if enabled
			if options.Verbose && options.ProgressMessage != "" {
				fmt.Fprintln(os.Stderr, console.FormatProgressMessage(options.ProgressMessage))
			}
		}
	}
}
