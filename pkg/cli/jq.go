package cli

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var jqLog = logger.New("cli:jq")

// ApplyJqFilter applies a jq filter to JSON input
func ApplyJqFilter(jsonInput string, jqFilter string) (string, error) {
	jqLog.Printf("Applying jq filter: %s (input size: %d bytes)", jqFilter, len(jsonInput))

	// Validate filter is not empty
	if jqFilter == "" {
		return "", fmt.Errorf("jq filter cannot be empty")
	}

	// Check if jq is available
	jqPath, err := exec.LookPath("jq")
	if err != nil {
		jqLog.Printf("jq not found in PATH")
		return "", fmt.Errorf("jq not found in PATH")
	}
	jqLog.Printf("Found jq at: %s", jqPath)

	// Pipe through jq
	cmd := exec.Command(jqPath, jqFilter)
	cmd.Stdin = strings.NewReader(jsonInput)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		jqLog.Printf("jq filter failed: %v, stderr: %s", err, stderr.String())
		return "", fmt.Errorf("jq filter failed: %w, stderr: %s", err, stderr.String())
	}

	jqLog.Printf("jq filter succeeded (output size: %d bytes)", stdout.Len())
	return stdout.String(), nil
}
