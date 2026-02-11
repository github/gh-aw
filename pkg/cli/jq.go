package cli

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/logger"
)

var jqLog = logger.New("cli:jq")

// Dangerous jq patterns that should be blocked for security
var dangerousFunctions = []string{
	"input",    // Can read arbitrary files
	"debug",    // Information disclosure
	"$__loc__", // Metadata exposure
}

// Patterns that may indicate DoS or resource exhaustion
var dosPatterns = []*regexp.Regexp{
	regexp.MustCompile(`recurse\s*\(\s*[^;)]+\s*\)`), // Unbounded recurse without condition (single arg)
	regexp.MustCompile(`while\s*\(\s*true`),          // Infinite loops
	regexp.MustCompile(`until\s*\(\s*false`),         // Infinite loops
}

// Default timeout for jq execution
const defaultJqTimeout = 30 * time.Second

// validateJqFilter performs security validation on the jq filter
func validateJqFilter(filter string) error {
	jqLog.Printf("Validating jq filter for security (length: %d)", len(filter))

	// Check for dangerous functions
	filterLower := strings.ToLower(filter)
	for _, dangerous := range dangerousFunctions {
		if strings.Contains(filterLower, strings.ToLower(dangerous)) {
			jqLog.Printf("SECURITY: Blocked dangerous function: %s", dangerous)
			return fmt.Errorf("jq filter contains dangerous function '%s' which is not allowed for security reasons", dangerous)
		}
	}

	// Check for DoS patterns
	for _, pattern := range dosPatterns {
		if pattern.MatchString(filter) {
			jqLog.Printf("SECURITY: Blocked potential DoS pattern: %s", pattern.String())
			return fmt.Errorf("jq filter contains potentially dangerous pattern that may cause resource exhaustion")
		}
	}

	// Check for excessive filter length (likely malicious)
	const maxFilterLength = 10000
	if len(filter) > maxFilterLength {
		jqLog.Printf("SECURITY: Blocked excessively long filter (length: %d)", len(filter))
		return fmt.Errorf("jq filter is too long (%d characters), maximum allowed is %d", len(filter), maxFilterLength)
	}

	jqLog.Printf("Filter validation passed")
	return nil
}

// ApplyJqFilter applies a jq filter to JSON input with security validation and timeout
func ApplyJqFilter(jsonInput string, jqFilter string) (string, error) {
	jqLog.Printf("Applying jq filter: %s (input size: %d bytes)", jqFilter, len(jsonInput))

	// Validate filter is not empty
	if jqFilter == "" {
		return "", fmt.Errorf("jq filter cannot be empty")
	}

	// Security validation
	if err := validateJqFilter(jqFilter); err != nil {
		return "", err
	}

	// Check if jq is available
	jqPath, err := exec.LookPath("jq")
	if err != nil {
		jqLog.Printf("jq not found in PATH")
		return "", fmt.Errorf("jq not found in PATH")
	}
	jqLog.Printf("Found jq at: %s", jqPath)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), defaultJqTimeout)
	defer cancel()

	// Pipe through jq with timeout
	cmd := exec.CommandContext(ctx, jqPath, jqFilter)
	cmd.Stdin = strings.NewReader(jsonInput)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Check if it was a timeout
		if ctx.Err() == context.DeadlineExceeded {
			jqLog.Printf("SECURITY: jq filter execution timed out after %v", defaultJqTimeout)
			return "", fmt.Errorf("jq filter execution timed out after %v (possible resource exhaustion attack)", defaultJqTimeout)
		}
		jqLog.Printf("jq filter failed: %v, stderr: %s", err, stderr.String())
		return "", fmt.Errorf("jq filter failed: %w, stderr: %s", err, stderr.String())
	}

	jqLog.Printf("jq filter succeeded (output size: %d bytes)", stdout.Len())
	return stdout.String(), nil
}
