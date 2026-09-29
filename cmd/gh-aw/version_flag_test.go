//go:build !integration

package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// TestRootVersionFlagIsStateless verifies that executing the root command with
// --version does not leave state behind that causes a later execution without
// --version to keep printing the version instead of help.
func TestRootVersionFlagIsStateless(t *testing.T) {
	captureStdout := func(fn func()) string {
		originalStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		os.Stdout = w
		defer func() { os.Stdout = originalStdout }()

		fn()

		if err := w.Close(); err != nil {
			t.Fatalf("failed to close pipe writer: %v", err)
		}
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, r); err != nil {
			t.Fatalf("failed to read pipe: %v", err)
		}
		return buf.String()
	}

	rootCmd.SetArgs([]string{"--version"})
	firstOutput := captureStdout(func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("first execute (--version) returned error: %v", err)
		}
	})
	if !strings.Contains(firstOutput, "version") {
		t.Errorf("expected first execution to print version, got: %q", firstOutput)
	}

	rootCmd.SetArgs([]string{})
	secondOutput := captureStdout(func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("second execute (no args) returned error: %v", err)
		}
	})
	if strings.Contains(secondOutput, "version") {
		t.Errorf("expected second execution to not print version (should show help), got: %q", secondOutput)
	}
}
