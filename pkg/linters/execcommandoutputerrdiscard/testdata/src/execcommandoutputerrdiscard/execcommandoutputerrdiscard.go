package execcommandoutputerrdiscard

import (
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
)

func ignoredOutput() {
	cmd := exec.Command("test")
	cmd.Output() // want "exec.Cmd.Output error handling"
}

func blankOutputError() {
	cmd := exec.Command("test")
	output, _ := cmd.Output() // want "exec.Cmd.Output error handling"
	_ = output
}

func loggedOutputError() {
	cmd := exec.Command("test")
	output, err := cmd.Output() // want "exec.Cmd.Output error handling"
	if err != nil {
		slog.Error("command failed", "error", err)
		_ = output
	}
}

func wrappedOutputError() error {
	cmd := exec.Command("test")
	output, err := cmd.Output() // want "exec.Cmd.Output error handling"
	if err != nil {
		_ = output
		return fmt.Errorf("command failed: %w", err)
	}
	return nil
}

func handledExitError() []byte {
	cmd := exec.Command("test")
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.Stderr
		}
		return nil
	}
	return output
}

func handledExitErrorWithErrorsAs() []byte {
	cmd := exec.Command("test")
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.Stderr
		}
		return nil
	}
	return output
}

func returnedOutput() ([]byte, error) {
	cmd := exec.Command("test")
	return cmd.Output()
}

func returnedCombinedOutput() ([]byte, error) {
	cmd := exec.Command("test")
	return cmd.CombinedOutput()
}

func returnedErrorOnFailurePath() error {
	cmd := exec.Command("test")
	_, err := cmd.Output()
	if err != nil {
		return err
	}
	return nil
}

func nestedFailurePathCanDiscardError(ignore bool) error {
	cmd := exec.Command("test")
	_, err := cmd.Output() // want "exec.Cmd.Output error handling"
	if err != nil {
		if ignore {
			return nil
		}
		return err
	}
	return nil
}

func returnedNamedErrorOnFailurePath() (output []byte, err error) {
	cmd := exec.Command("test")
	output, err = cmd.Output()
	if err != nil {
		return
	}
	return
}

func returnedErrorOnlyOnSuccessPath() error {
	cmd := exec.Command("test")
	output, err := cmd.Output() // want "exec.Cmd.Output error handling"
	if err != nil {
		_ = output
		return nil
	}
	return err
}

func returnedNamedCombinedOutputOnFailurePath() (output []byte, err error) {
	cmd := exec.Command("test")
	output, err = cmd.CombinedOutput()
	if err != nil {
		return
	}
	return
}

func discardedExitErrorStderr() {
	cmd := exec.Command("test")
	output, err := cmd.Output() // want "exec.Cmd.Output error handling"
	if exitErr, ok := err.(*exec.ExitError); ok {
		_ = exitErr.Stderr
	}
	_ = output
}

func unrelatedExitErrorDoesNotHandleCommandError() {
	cmd := exec.Command("test")
	output, err := cmd.Output() // want "exec.Cmd.Output error handling"
	otherErr := errors.New("unrelated")
	if exitErr, ok := otherErr.(*exec.ExitError); ok {
		_ = exitErr.Stderr
	}
	if err != nil {
		_ = output
	}
}

func retainedCombinedOutput() []byte {
	cmd := exec.Command("test")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output
	}
	return output
}

func discardedCombinedOutput() {
	cmd := exec.Command("test")
	output, _ := cmd.CombinedOutput() // want "exec.Cmd.CombinedOutput result"
	_ = output
}

func ignoredCombinedOutput() {
	cmd := exec.Command("test")
	cmd.CombinedOutput() // want "exec.Cmd.CombinedOutput result"
}

func overwrittenCombinedOutput() {
	cmd := exec.Command("test")
	output, err := cmd.CombinedOutput() // want "exec.Cmd.CombinedOutput result"
	output = nil
	if err != nil {
		slog.Error("command failed", "output", output)
	}
}

func reassignedOutputError() {
	cmd := exec.Command("test")
	output, err := cmd.Output() // want "exec.Cmd.Output error handling"
	err = errors.New("unrelated")
	if exitErr, ok := err.(*exec.ExitError); ok {
		slog.Error("command failed", "stderr", exitErr.Stderr)
	}
	_ = output
}

func parenthesizedOutput() {
	cmd := exec.Command("test")
	output, err := (cmd.Output)() // want "exec.Cmd.Output error handling"
	if err != nil {
		_ = output
	}
}

func outputInClosure() {
	cmd := exec.Command("test")
	outerErr := errors.New("unrelated")
	if exitErr, ok := outerErr.(*exec.ExitError); ok {
		_ = exitErr.Stderr
	}
	func() {
		output, err := cmd.Output() // want "exec.Cmd.Output error handling"
		if err != nil {
			_ = output
		}
	}()
}

func outputInIfInitializer() []byte {
	cmd := exec.Command("test")
	if output, err := cmd.Output(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.Stderr
		}
		return nil
	} else {
		return output
	}
}

func outputWithNolint() {
	cmd := exec.Command("test")
	output, err := cmd.Output() //nolint:execcommandoutputerrdiscard
	if err != nil {
		_ = output
	}
}

type unrelatedCommand struct{}

func (unrelatedCommand) Output() ([]byte, error) {
	return nil, nil
}

func unrelatedOutputMethod() {
	var cmd unrelatedCommand
	cmd.Output()
}
