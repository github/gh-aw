package execcommandoutputerrdiscard

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
)

type outputBytes []byte

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
	output, err := cmd.Output()
	if err != nil {
		_ = output
		return fmt.Errorf("command failed: %w", err)
	}
	return nil
}

func wrapsAnUnrelatedError() error {
	cmd := exec.Command("test")
	output, err := cmd.Output() // want "exec.Cmd.Output error handling"
	if err != nil {
		_ = output
		otherErr := errors.New("unrelated")
		return fmt.Errorf("%w: %v", otherErr, err)
	}
	return nil
}

func wrapsTransformedOutputError() error {
	cmd := exec.Command("test")
	output, err := cmd.Output() // want "exec.Cmd.Output error handling"
	if err != nil {
		_ = output
		return fmt.Errorf("%w", errors.New(err.Error()))
	}
	return nil
}

func wrappedIndexedOutputError() error {
	cmd := exec.Command("test")
	output, err := cmd.Output()
	if err != nil {
		_ = output
		return fmt.Errorf("command failed: %[1]w", err)
	}
	return nil
}

func joinedFormattedOutputError() error {
	cmd := exec.Command("test")
	output, err := cmd.Output() // want "exec.Cmd.Output error handling"
	if err != nil {
		_ = output
		return errors.Join(fmt.Errorf("%v", err))
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

func earlyReturnBeforeErrorCheck(ignore bool) error {
	cmd := exec.Command("test")
	_, err := cmd.Output() // want "exec.Cmd.Output error handling"
	if ignore {
		return nil
	}
	if err != nil {
		return err
	}
	return nil
}

func optionalErrorCheck(check bool) error {
	cmd := exec.Command("test")
	_, err := cmd.Output() // want "exec.Cmd.Output error handling"
	if check {
		if err != nil {
			return err
		}
	}
	return nil
}

func sequentialOutputChecks() error {
	first := exec.Command("first")
	_, err := first.Output()
	if err != nil {
		return err
	}
	second := exec.Command("second")
	_, err = second.Output()
	if err != nil {
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

func namedCombinedOutputCheckCanBeBypassed(ignore bool) (output []byte, err error) {
	cmd := exec.Command("test")
	output, err = cmd.CombinedOutput() // want "exec.Cmd.CombinedOutput result"
	if ignore {
		output = nil
		return
	}
	if err != nil {
		return
	}
	return
}

func reassignedNamedCombinedOutputError() (output []byte, err error) {
	cmd := exec.Command("test")
	output, err = cmd.CombinedOutput()
	err = errors.New("unrelated")
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

func errorsAsReplacementDoesNotHandleCommandError() {
	cmd := exec.Command("test")
	output, err := cmd.Output() // want "exec.Cmd.Output error handling"
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		otherExitErr := &exec.ExitError{}
		errors.As(otherExitErr, &exitErr)
		slog.Error("other command failed", "stderr", exitErr.Stderr)
	}
	_ = output
}

func clearingExitErrorStderrDoesNotHandleCommandError() {
	cmd := exec.Command("test")
	output, err := cmd.Output() // want "exec.Cmd.Output error handling"
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		exitErr.Stderr = nil
	}
	_ = output
}

func retainedCombinedOutput() []byte {
	cmd := exec.Command("test")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output
	}
	return output
}

func combinedOutputDroppedOnFailure() ([]byte, error) {
	cmd := exec.Command("test")
	output, err := cmd.CombinedOutput() // want "exec.Cmd.CombinedOutput result"
	if err != nil {
		return nil, err
	}
	return output, nil
}

func combinedOutputRetainedByAppend() []byte {
	cmd := exec.Command("test")
	output, _ := cmd.CombinedOutput()
	output = append([]byte("command failed: "), output...)
	return output
}

func combinedOutputClearedBeforeReturn() []byte {
	cmd := exec.Command("test")
	output, _ := cmd.CombinedOutput() // want "exec.Cmd.CombinedOutput result"
	clear(output)
	return output
}

func combinedOutputClearedByAlias() []byte {
	cmd := exec.Command("test")
	output, _ := cmd.CombinedOutput() // want "exec.Cmd.CombinedOutput result"
	alias := append(output[:0], output...)
	alias2 := alias
	clear(alias2)
	return output
}

func combinedOutputClearedByNamedSliceConversion() []byte {
	cmd := exec.Command("test")
	output, _ := cmd.CombinedOutput() // want "exec.Cmd.CombinedOutput result"
	alias := outputBytes(output)
	clear(alias)
	return output
}

func combinedOutputWithShadowedAppend() []byte {
	append := func([]byte, ...byte) []byte { return nil }
	cmd := exec.Command("test")
	output, _ := cmd.CombinedOutput() // want "exec.Cmd.CombinedOutput result"
	output = append([]byte("command failed: "), output...)
	return output
}

func combinedOutputExtendedThroughAlias() []byte {
	cmd := exec.Command("test")
	output, _ := cmd.CombinedOutput()
	alias := output
	alias = append(alias, []byte("suffix")...)
	return output
}

func combinedOutputOverlappingAppendThenFullAlias() []byte {
	cmd := exec.Command("test")
	output, _ := cmd.CombinedOutput() // want "exec.Cmd.CombinedOutput result"
	alias := output[:0]
	alias = append(alias, 'x')
	alias = output
	return output
}

func combinedOutputConditionalFullAliasThenAppend() []byte {
	cmd := exec.Command("test")
	output, _ := cmd.CombinedOutput() // want "exec.Cmd.CombinedOutput result"
	alias := output[:0]
	if os.Getenv("APPEND") == "true" {
		alias = output
	}
	alias = append(alias, 'x')
	return output
}

func combinedOutputWithUnrelatedAliasReassignment() []byte {
	cmd := exec.Command("test")
	output, _ := cmd.CombinedOutput()
	alias := output
	alias = []byte("unrelated")
	clear(alias)
	return output
}

func combinedOutputDerivedAfterUnrelatedAliasReassignment() []byte {
	cmd := exec.Command("test")
	output, _ := cmd.CombinedOutput()
	alias := output
	alias = []byte("unrelated")
	derived := alias
	clear(derived)
	return output
}

func combinedOutputWithEarlierAliasedAppendThenReassignment() []byte {
	cmd := exec.Command("test")
	output, _ := cmd.CombinedOutput() // want "exec.Cmd.CombinedOutput result"
	alias := output[:0]
	alias = append(alias, 'x')
	alias = []byte("unrelated")
	return output
}

func combinedOutputLoggedOnFailure() error {
	cmd := exec.Command("test")
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error("command failed", "output", string(output))
		return err
	}
	return nil
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

func outputWithStreamedStderr() {
	cmd := exec.Command("test")
	cmd.Stderr = os.Stderr
	_, _ = cmd.Output()
}

func outputWithConditionalStderrDiscard(discard bool) {
	cmd := exec.Command("test")
	cmd.Stderr = os.Stderr
	if discard {
		cmd.Stderr = io.Discard
	}
	_, err := cmd.Output() // want "exec.Cmd.Output error handling"
	if err != nil {
		slog.Error("command failed", "error", err)
	}
}

func outputWithReassignedCommand() {
	cmd := exec.Command("first")
	cmd.Stderr = os.Stderr
	cmd = exec.Command("second")
	_, err := cmd.Output() // want "exec.Cmd.Output error handling"
	if err != nil {
		slog.Error("command failed", "error", err)
	}
}

func outputWithCapturedStderr() []byte {
	var stderr bytes.Buffer
	cmd := exec.Command("test")
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		slog.Error("command failed", "stderr", stderr.String())
		return nil
	}
	return output
}

func outputWithReassignedCapturedBuffer() {
	stderr := &bytes.Buffer{}
	cmd := exec.Command("test")
	cmd.Stderr = stderr
	stderr = &bytes.Buffer{}
	_, err := cmd.Output() // want "exec.Cmd.Output error handling"
	if err != nil {
		slog.Error("command failed", "stderr", stderr.String())
	}
}

func outputWithReturnedCapturedStderrError() error {
	var stderr bytes.Buffer
	cmd := exec.Command("test")
	cmd.Stderr = &stderr
	_, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("command failed: %s", stderr.String())
	}
	return nil
}

func outputWithDiscardedCapturedStderrError() error {
	var stderr bytes.Buffer
	cmd := exec.Command("test")
	cmd.Stderr = &stderr
	_, err := cmd.Output() // want "exec.Cmd.Output error handling"
	if err != nil {
		fmt.Errorf("command failed: %s", stderr.String())
		return nil
	}
	return nil
}

func outputWithResetCapturedStderr() {
	var stderr bytes.Buffer
	cmd := exec.Command("test")
	cmd.Stderr = &stderr
	_, err := cmd.Output() // want "exec.Cmd.Output error handling"
	if err != nil {
		stderr.Reset()
		slog.Error("command failed", "stderr", stderr.String())
	}
}

func outputWithResetCapturedStderrAlias() {
	stderr := &bytes.Buffer{}
	cmd := exec.Command("test")
	cmd.Stderr = stderr
	_, err := cmd.Output() // want "exec.Cmd.Output error handling"
	if err != nil {
		alias := stderr
		alias.Reset()
		slog.Error("command failed", "stderr", stderr.String())
	}
}

func outputWithTruncatedCapturedStderr() {
	var stderr bytes.Buffer
	cmd := exec.Command("test")
	cmd.Stderr = &stderr
	_, err := cmd.Output() // want "exec.Cmd.Output error handling"
	if err != nil {
		stderr.Truncate(0)
		slog.Error("command failed", "stderr", stderr.String())
	}
}

type discardingStderrWriter struct{}

func (*discardingStderrWriter) Write([]byte) (int, error) {
	return 0, nil
}

func (*discardingStderrWriter) String() string {
	return ""
}

func outputWithDiscardingStderrWriter() {
	var stderr discardingStderrWriter
	cmd := exec.Command("test")
	cmd.Stderr = &stderr
	_, err := cmd.Output() // want "exec.Cmd.Output error handling"
	if err != nil {
		slog.Error("command failed", "stderr", stderr.String())
	}
}

func outputWithClearedCapturedStderrBytes() {
	var stderr bytes.Buffer
	cmd := exec.Command("test")
	cmd.Stderr = &stderr
	_, err := cmd.Output() // want "exec.Cmd.Output error handling"
	if err != nil {
		clear(stderr.Bytes())
		slog.Error("command failed", "stderr", stderr.String())
	}
}

func outputWithClearedCapturedStderrBytesAlias() {
	var stderr bytes.Buffer
	cmd := exec.Command("test")
	cmd.Stderr = &stderr
	_, err := cmd.Output() // want "exec.Cmd.Output error handling"
	if err != nil {
		output := stderr.Bytes()
		clear(output)
		slog.Error("command failed", "stderr", stderr.String())
	}
}

func outputWithAppendedCapturedStderrBytes() {
	var stderr bytes.Buffer
	cmd := exec.Command("test")
	cmd.Stderr = &stderr
	_, err := cmd.Output() // want "exec.Cmd.Output error handling"
	if err != nil {
		_ = append(stderr.Bytes()[:0], []byte("replacement")...)
		slog.Error("command failed", "stderr", stderr.String())
	}
}

func outputWithUnretainedCapturedStderr() {
	var stderr bytes.Buffer
	cmd := exec.Command("test")
	cmd.Stderr = &stderr
	output, err := cmd.Output() // want "exec.Cmd.Output error handling"
	if err != nil {
		slog.Error("command failed", "error", err)
	}
	_ = output
}

type unrelatedCommand struct{}

func (unrelatedCommand) Output() ([]byte, error) {
	return nil, nil
}

func unrelatedOutputMethod() {
	var cmd unrelatedCommand
	cmd.Output()
}
