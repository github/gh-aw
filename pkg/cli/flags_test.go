//go:build !integration

package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMinuteTimeoutFlags(t *testing.T) {
	t.Parallel()
	commands := []struct {
		name         string
		newCommand   func() *cobra.Command
		defaultValue int
	}{
		{"logs", NewLogsCommand, 0},
		{"forecast", NewForecastCommand, 0},
		{"trial", func() *cobra.Command { return NewTrialCommand(validateEngineStub) }, 30},
	}
	for _, command := range commands {
		t.Run(command.name, func(t *testing.T) {
			t.Parallel()
			cmd := command.newCommand()
			got, err := cmd.Flags().GetInt("timeout")
			require.NoError(t, err)
			assert.Equal(t, command.defaultValue, got)

			for _, test := range []struct {
				input string
				want  int
			}{
				{"5", 5},
				{"+5", 5},
				{"0xA", 10},
				{"0b1010", 10},
				{"0o12", 10},
				{"012", 10},
				{"1_0", 10},
				{"5m", 5},
				{"1h", 60},
				{"1h30m", 90},
				{"120s", 2},
				{"1.5h", 90},
				{"0", 0},
				{"0m", 0},
				{"-1", -1},
				{"-1m", -1},
			} {
				t.Run(test.input, func(t *testing.T) {
					cmd := command.newCommand()
					require.NoError(t, cmd.ParseFlags([]string{"--timeout", test.input}))
					got, err := cmd.Flags().GetInt("timeout")
					require.NoError(t, err)
					assert.Equal(t, test.want, got)
				})
			}
			for _, input := range []string{"invalid", "5minutes", "30s", "90s", "1.5m", "999999999999999999999999", "999999999h"} {
				t.Run(input, func(t *testing.T) {
					cmd := command.newCommand()
					require.ErrorContains(t, cmd.ParseFlags([]string{"--timeout", input}), "must be an integer number of minutes or a whole-minute duration")
					got, err := cmd.Flags().GetInt("timeout")
					require.NoError(t, err)
					assert.Equal(t, command.defaultValue, got, "invalid input must not change the default")
				})
			}
		})
	}
}

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
		{
			name:         "compile command has -f for --force",
			shortFlag:    "f",
			longFlag:     "force",
			commandSetup: func() *cobra.Command { return createCompileCommandStub() },
			shouldExist:  true,
			description:  "compile should have force short flag",
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
		{
			name:         "compile command has -l for --logical-repo",
			shortFlag:    "l",
			longFlag:     "logical-repo",
			commandSetup: func() *cobra.Command { return createCompileCommandStub() },
			shouldExist:  true,
			description:  "compile should have logical-repo short flag",
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
		{
			name:         "logs command has -e for --engine",
			shortFlag:    "e",
			longFlag:     "engine",
			commandSetup: func() *cobra.Command { return NewLogsCommand() },
			shouldExist:  true,
			description:  "logs should have engine short flag",
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
	cmd.Flags().BoolP("force", "f", false, "Force overwrite")
	cmd.Flags().BoolP("watch", "w", false, "Watch for changes")
	cmd.Flags().StringP("dir", "d", "", "Workflow directory")
	cmd.Flags().StringP("logical-repo", "l", "", "Repository to simulate")
	cmd.Flags().BoolP("json", "j", false, "Output results in JSON format")
	return cmd
}

func createNewCommandStub() *cobra.Command {
	cmd := &cobra.Command{Use: "new"}
	cmd.Flags().BoolP("force", "f", false, "Overwrite existing files")
	cmd.Flags().BoolP("interactive", "i", false, "Launch interactive wizard")
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

func TestEngineFlagUsageText(t *testing.T) {
	t.Parallel()

	overrideCmd := &cobra.Command{Use: "override-test"}
	addEngineFlag(overrideCmd)

	engineFlag := overrideCmd.Flags().Lookup("engine")
	if engineFlag == nil {
		t.Fatal("Expected --engine override flag to exist")
	}

	if engineFlag.Usage != EngineFlagOverrideUsage {
		t.Errorf("Unexpected --engine override usage text: %s", engineFlag.Usage)
	}

	filterCmd := &cobra.Command{Use: "filter-test"}
	addEngineFilterFlag(filterCmd)
	filterFlag := filterCmd.Flags().Lookup("engine")
	if filterFlag == nil {
		t.Fatal("Expected --engine filter flag to exist")
	}

	if filterFlag.Usage != EngineFlagFilterUsage {
		t.Errorf("Unexpected --engine filter usage text: %s", filterFlag.Usage)
	}
}

func TestAddSecurityScannerFlag(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{Use: "test"}
	addSecurityScannerFlag(cmd)

	primary := cmd.Flags().Lookup("no-security-scanner")
	if primary == nil {
		t.Fatal("addSecurityScannerFlag should register --no-security-scanner")
	}
	if primary.Usage != "Skip security scanning of workflow markdown content" {
		t.Errorf("Unexpected --no-security-scanner usage: %s", primary.Usage)
	}

	deprecated := cmd.Flags().Lookup("disable-security-scanner")
	if deprecated == nil {
		t.Fatal("addSecurityScannerFlag should register --disable-security-scanner as a deprecated alias")
	}
	if deprecated.Deprecated != "use --no-security-scanner instead" {
		t.Errorf("Expected deprecation message 'use --no-security-scanner instead', got %q", deprecated.Deprecated)
	}
}

func TestResolveDeprecatedBoolFlag(t *testing.T) {
	t.Parallel()

	setup := func() *cobra.Command {
		cmd := &cobra.Command{Use: "test"}
		cmd.Flags().Bool("new-flag", false, "new flag")
		cmd.Flags().Bool("old-flag", false, "old flag")
		_ = cmd.Flags().MarkDeprecated("old-flag", "use --new-flag instead")
		return cmd
	}

	t.Run("both false returns false", func(t *testing.T) {
		t.Parallel()
		cmd := setup()
		if resolveDeprecatedBoolFlag(cmd, "new-flag", "old-flag") {
			t.Error("expected false when both flags are unset")
		}
	})

	t.Run("new flag true returns true", func(t *testing.T) {
		t.Parallel()
		cmd := setup()
		if err := cmd.Flags().Set("new-flag", "true"); err != nil {
			t.Fatalf("failed to set new-flag: %v", err)
		}
		if !resolveDeprecatedBoolFlag(cmd, "new-flag", "old-flag") {
			t.Error("expected true when new flag is set")
		}
	})

	t.Run("old flag true returns true", func(t *testing.T) {
		t.Parallel()
		cmd := setup()
		if err := cmd.Flags().Set("old-flag", "true"); err != nil {
			t.Fatalf("failed to set old-flag: %v", err)
		}
		if !resolveDeprecatedBoolFlag(cmd, "new-flag", "old-flag") {
			t.Error("expected true when deprecated old flag is set")
		}
	})
}
