package exec_command_output_err_discard

import (
	"errors"
	"fmt"
	"os/exec"
)

func preservedErrorsAs() ([]byte, error) {
	out, err := exec.Command("git", "log").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("git failed: %w: %s", err, exitErr.Stderr)
		}
		return nil, err
	}
	return out, nil
}

func preservedTypeAssert() error {
	_, err := exec.Command("git", "log").Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("git: %s", string(ee.Stderr))
		}
		return err
	}
	return nil
}

func preservedTypeSwitch() error {
	_, err := exec.Command("git", "log").Output()
	switch e := err.(type) {
	case nil:
		return nil
	case *exec.ExitError:
		return fmt.Errorf("git: %s", e.Stderr)
	}
	return nil
}

func returnedUnchanged() ([]byte, error) {
	out, err := exec.Command("git", "log").Output()
	if err != nil {
		return nil, err
	}
	return out, nil
}

func stderrCaptured() error {
	cmd := exec.Command("git", "log")
	var buf []byte
	_ = buf
	_, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed: %v: %v", err, cmd.Stderr)
	}
	return nil
}

func combinedOutputKept() error {
	out, err := exec.Command("git", "fetch").CombinedOutput()
	if err != nil {
		return fmt.Errorf("fetch failed: %w: %s", err, out)
	}
	return nil
}

type fakeCmd struct{}

func (fakeCmd) Output() ([]byte, error) { return nil, nil }

func nonExecCommand() {
	fakeCmd{}.Output()
	out, _ := fakeCmd{}.Output()
	_ = out
}

func otherMethodsIgnored() {
	_ = exec.Command("git", "status").Run()
}

func suppressed() {
	//nolint:execcommandoutputerrdiscard
	exec.Command("git", "status").Output()
}
