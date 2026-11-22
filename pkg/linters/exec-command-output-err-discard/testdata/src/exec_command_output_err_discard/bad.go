package exec_command_output_err_discard

import (
	"fmt"
	"log"
	"os/exec"
)

func ignoredStmt() {
	exec.Command("git", "status").Output() // want `Output\(\) result and error are ignored`
}

func ignoredStmtParen() {
	(exec.Command("git", "status").CombinedOutput()) // want `CombinedOutput\(\) result and error are ignored`
}

func blankErr() string {
	out, _ := exec.Command("git", "rev-parse", "HEAD").Output() // want `Output\(\) error is discarded`
	return string(out)
}

func blankErrParen() {
	_, (_) = (exec.Command("gh", "auth", "status")).Output() // want `Output\(\) error is discarded`
}

func loggedOnly() {
	out, err := exec.Command("git", "log").Output() // want `Output\(\) error is handled without preserving stderr`
	if err != nil {
		log.Printf("failed: %v", err)
		return
	}
	_ = out
}

func wrappedWithoutStderr() ([]byte, error) {
	cmd := exec.Command("gh", "pr", "list")
	out, err := cmd.Output() // want `Output\(\) error is handled without preserving stderr`
	if err != nil {
		return nil, fmt.Errorf("gh failed: %w", err)
	}
	return out, nil
}

func combinedOutputDropped() error {
	_, err := exec.Command("git", "fetch").CombinedOutput() // want `CombinedOutput\(\) error is handled without preserving stderr`
	if err != nil {
		return fmt.Errorf("fetch failed: %v", err)
	}
	return nil
}

func inClosure() {
	f := func() {
		_, err := exec.Command("git", "pull").Output() // want `Output\(\) error is handled without preserving stderr`
		log.Println(err)
	}
	f()
}

func inIfInit() {
	if _, err := exec.Command("git", "gc").Output(); err != nil { // want `Output\(\) error is handled without preserving stderr`
		log.Println(err)
	}
}
