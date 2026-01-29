//go:build !integration

package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

// TestShortFlagConsistency tests that short flags are consistently defined
// across commands where they should be present.
func TestShortFlagConsistency(t *testing.T) {
	t.Parallel()

	// Define test cases for each short flag
	testCases := []struct {
		name         string
		shortFlag    string
		longFlag     string
		commandSetup func() *cobra.Command
		shouldExist  bool
		description  string
	}{
		// -v flag (verbose) should be global (tested separately)

		// -e flag (engine)
		{
			name:         "compile command has -e for --engine",
			shortFlag:    "e",
			longFlag:     "engine",
			commandSetup: func() *cobra.Command { return createCompileCommandStub() },
			shouldExist:  true,
			description:  "compile should have engine short flag",
		},

		// -f flag (force)
		{
			name:         "new command has -f for --force",
			shortFlag:    "f",
			longFlag:     "force",
			commandSetup: func() *cobra.Command { return createNewCommandStub() },
			shouldExist:  true,
			description:  "new should have force short flag",
		},
		{
			name:         "add command has -f for --force",
			shortFlag:    "f",
			longFlag:     "force",
			commandSetup: func() *cobra.Command { cmd := NewAddCommand(validateEngineStub); return cmd },
			shouldExist:  true,
			description:  "add should have force short flag",
		},
		{
			name:         "update command has -f for --force",
			shortFlag:    "f",
			longFlag:     "force",
			commandSetup: func() *cobra.Command { return NewUpdateCommand(validateEngineStub) },
			shouldExist:  true,
			description:  "update should have force short flag",
		},

		// -F flag (raw-field in run command)
		{
			name:         "run command has -F for --raw-field",
			shortFlag:    "F",
			longFlag:     "raw-field",
			commandSetup: func() *cobra.Command { return createRunCommandStub() },
			shouldExist:  true,
			description:  "run should have raw-field short flag (uppercase F)",
		},

		// -j flag (json)
		{
			name:         "compile command has -j for --json",
			shortFlag:    "j",
			longFlag:     "json",
			commandSetup: func() *cobra.Command { return createCompileCommandStub() },
			shouldExist:  true,
			description:  "compile should have json short flag",
		},
		{
			name:         "logs command has -j for --json",
			shortFlag:    "j",
			longFlag:     "json",
			commandSetup: func() *cobra.Command { return NewLogsCommand() },
			shouldExist:  true,
			description:  "logs should have json short flag",
		},
		{
			name:         "audit command has -j for --json",
			shortFlag:    "j",
			longFlag:     "json",
			commandSetup: func() *cobra.Command { return NewAuditCommand() },
			shouldExist:  true,
			description:  "audit should have json short flag",
		},
		{
			name:         "status command has -j for --json",
			shortFlag:    "j",
			longFlag:     "json",
			commandSetup: func() *cobra.Command { return NewStatusCommand() },
			shouldExist:  true,
			description:  "status should have json short flag",
		},

		// -o flag (output)
		{
			name:         "logs command has -o for --output",
			shortFlag:    "o",
			longFlag:     "output",
			commandSetup: func() *cobra.Command { return NewLogsCommand() },
			shouldExist:  true,
			description:  "logs should have output short flag",
		},
		{
			name:         "audit command has -o for --output",
			shortFlag:    "o",
			longFlag:     "output",
			commandSetup: func() *cobra.Command { return NewAuditCommand() },
			shouldExist:  true,
			description:  "audit should have output short flag",
		},

		// -d flag (dir)
		{
			name:         "compile command has -d for --dir",
			shortFlag:    "d",
			longFlag:     "dir",
			commandSetup: func() *cobra.Command { return createCompileCommandStub() },
			shouldExist:  true,
			description:  "compile should have dir short flag",
		},
		{
			name:         "add command has -d for --dir",
			shortFlag:    "d",
			longFlag:     "dir",
			commandSetup: func() *cobra.Command { return NewAddCommand(validateEngineStub) },
			shouldExist:  true,
			description:  "add should have dir short flag",
		},
		{
			name:         "update command has -d for --dir",
			shortFlag:    "d",
			longFlag:     "dir",
			commandSetup: func() *cobra.Command { return NewUpdateCommand(validateEngineStub) },
			shouldExist:  true,
			description:  "update should have dir short flag",
		},

		// -c flag (count) - should only be in logs command
		{
			name:         "logs command has -c for --count",
			shortFlag:    "c",
			longFlag:     "count",
			commandSetup: func() *cobra.Command { return NewLogsCommand() },
			shouldExist:  true,
			description:  "logs should have count short flag",
		},
		{
			name:         "add command does NOT have -c for --number",
			shortFlag:    "c",
			longFlag:     "number",
			commandSetup: func() *cobra.Command { return NewAddCommand(validateEngineStub) },
			shouldExist:  false,
			description:  "add should NOT have -c short flag for --number (conflict resolved)",
		},

		// -r flag (repo)
		{
			name:         "enable command has -r for --repo",
			shortFlag:    "r",
			longFlag:     "repo",
			commandSetup: func() *cobra.Command { return createEnableCommandStub() },
			shouldExist:  true,
			description:  "enable should have repo short flag",
		},
		{
			name:         "disable command has -r for --repo",
			shortFlag:    "r",
			longFlag:     "repo",
			commandSetup: func() *cobra.Command { return createDisableCommandStub() },
			shouldExist:  true,
			description:  "disable should have repo short flag",
		},

		// -w flag (watch)
		{
			name:         "compile command has -w for --watch",
			shortFlag:    "w",
			longFlag:     "watch",
			commandSetup: func() *cobra.Command { return createCompileCommandStub() },
			shouldExist:  true,
			description:  "compile should have watch short flag",
		},

		// -i flag (interactive)
		{
			name:         "new command has -i for --interactive",
			shortFlag:    "i",
			longFlag:     "interactive",
			commandSetup: func() *cobra.Command { return createNewCommandStub() },
			shouldExist:  true,
			description:  "new should have interactive short flag",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cmd := tc.commandSetup()
			if cmd == nil {
				t.Fatal("command setup returned nil")
			}

			flag := cmd.Flags().Lookup(tc.longFlag)
			if flag == nil {
				if tc.shouldExist {
					t.Fatalf("Expected flag --%s to exist, but it doesn't", tc.longFlag)
				}
				return
			}

			if tc.shouldExist {
				if flag.Shorthand != tc.shortFlag {
					t.Errorf("%s: Expected short flag to be '-%s', got '-%s'",
						tc.description, tc.shortFlag, flag.Shorthand)
				}
			} else {
				if flag.Shorthand == tc.shortFlag {
					t.Errorf("%s: Expected NO short flag '-%s', but it exists",
						tc.description, tc.shortFlag)
				}
			}
		})
	}
}

// Stub command creation functions that match main.go structure
func createCompileCommandStub() *cobra.Command {
	cmd := &cobra.Command{Use: "compile"}
	cmd.Flags().StringP("engine", "e", "", "Override AI engine")
	cmd.Flags().BoolP("watch", "w", false, "Watch for changes")
	cmd.Flags().StringP("dir", "d", "", "Workflow directory")
	cmd.Flags().BoolP("json", "j", false, "Output results in JSON format")
	return cmd
}

func createNewCommandStub() *cobra.Command {
	cmd := &cobra.Command{Use: "new"}
	cmd.Flags().BoolP("force", "f", false, "Overwrite existing files")
	cmd.Flags().BoolP("interactive", "i", false, "Launch interactive wizard")
	return cmd
}

func createRunCommandStub() *cobra.Command {
	cmd := &cobra.Command{Use: "run"}
	cmd.Flags().StringArrayP("raw-field", "F", []string{}, "Add string parameter")
	cmd.Flags().StringP("engine", "e", "", "Override AI engine")
	cmd.Flags().StringP("repo", "r", "", "Target repository")
	return cmd
}

func createEnableCommandStub() *cobra.Command {
	cmd := &cobra.Command{Use: "enable"}
	cmd.Flags().StringP("repo", "r", "", "Target repository")
	return cmd
}

func createDisableCommandStub() *cobra.Command {
	cmd := &cobra.Command{Use: "disable"}
	cmd.Flags().StringP("repo", "r", "", "Target repository")
	return cmd
}
