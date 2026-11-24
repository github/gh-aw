package cli

import (
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// minuteTimeoutValue preserves integer-minute flags while accepting whole-minute durations.
type minuteTimeoutValue struct {
	pflag.Value
}

func (v minuteTimeoutValue) Set(value string) error {
	if _, err := strconv.Atoi(value); err == nil {
		return v.Value.Set(value)
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration%time.Minute != 0 {
		return fmt.Errorf("must be an integer number of minutes or a whole-minute duration like \"5m\" or \"1h\"")
	}
	return v.Value.Set(strconv.FormatInt(int64(duration/time.Minute), 10))
}

func addMinuteTimeoutFlag(cmd *cobra.Command, defaultValue int, usage string) {
	cmd.Flags().Int("timeout", defaultValue, usage)
	flag := cmd.Flags().Lookup("timeout")
	flag.Value = minuteTimeoutValue{Value: flag.Value}
}

const engineFlagHelpList = "copilot, claude, codex, gemini, pi, agy (experimental)"

const (
	EngineFlagOverrideUsage = "Override AI engine (" + engineFlagHelpList + ")"
	EngineFlagFilterUsage   = "Filter logs by AI engine (" + engineFlagHelpList + ")"
)

// addEngineFlag adds the --engine/-e flag to a command.
// This flag allows overriding the AI engine type.
func addEngineFlag(cmd *cobra.Command) {
	cmd.Flags().StringP("engine", "e", "", EngineFlagOverrideUsage)
}

// addEngineFilterFlag adds the --engine/-e flag to a command for filtering.
// This flag allows filtering results by AI engine type.
func addEngineFilterFlag(cmd *cobra.Command) {
	cmd.Flags().StringP("engine", "e", "", EngineFlagFilterUsage)
}

// addRepoFlag adds the --repo/-r flag to a command.
// This flag allows specifying a target repository.
func addRepoFlag(cmd *cobra.Command) {
	cmd.Flags().StringP("repo", "r", "", "Target repository ([HOST/]owner/repo format). Defaults to current repository")
}

// addOutputFlag adds the --output/-o flag to a command.
// This flag allows specifying an output directory for generated files.
func addOutputFlag(cmd *cobra.Command, defaultValue string) {
	cmd.Flags().StringP("output", "o", defaultValue, "Output directory for generated files")
}

// addJSONFlag adds the --json/-j flag to a command.
// This flag enables JSON output format.
func addJSONFlag(cmd *cobra.Command) {
	cmd.Flags().BoolP("json", "j", false, "Output results in JSON format")
}

// addSecurityScannerFlag adds the --no-security-scanner flag and its deprecated
// --disable-security-scanner alias to a command.
func addSecurityScannerFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("no-security-scanner", false, "Skip security scanning of workflow markdown content")
	cmd.Flags().Bool("disable-security-scanner", false, "Skip security scanning of workflow markdown content")
	_ = cmd.Flags().MarkDeprecated("disable-security-scanner", "use --no-security-scanner instead")
}

// resolveDeprecatedBoolFlag returns true if either the newName flag or the
// deprecated oldName flag is set on cmd. It is intended for cases where a flag
// has been renamed: callers register both names and use this helper to collapse
// them into a single effective value.
func resolveDeprecatedBoolFlag(cmd *cobra.Command, newName, oldName string) bool {
	newVal, _ := cmd.Flags().GetBool(newName)
	oldVal, _ := cmd.Flags().GetBool(oldName)
	return newVal || oldVal
}
