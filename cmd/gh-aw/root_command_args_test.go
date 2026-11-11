package main

import (
	"strings"
	"testing"
)

func TestRootCommandArgsUsesExtensionPrefixAndSuggestions(t *testing.T) {
	if rootCmd.Args == nil {
		t.Fatal("root command should validate unknown arguments")
	}

	err := rootCmd.Args(rootCmd, []string{"not-a-command"})
	if err == nil || err.Error() != `unknown command "not-a-command" for "gh aw"` {
		t.Fatalf("unexpected unknown-command error: %v", err)
	}

	err = rootCmd.Args(rootCmd, []string{"compil"})
	if err == nil || !strings.Contains(err.Error(), "\n\nDid you mean this?\n\tcompile\n") {
		t.Fatalf("expected a suggestion for the mistyped command, got: %v", err)
	}
}
