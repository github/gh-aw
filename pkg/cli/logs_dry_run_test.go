//go:build !integration

package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDryRunMetadataAndFilters(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, metadata  string
		dryRun, missing bool
	}{
		{"dry-run", `{"engine_id":"copilot","dry_run":true}`, true, false},
		{"normal", `{"engine_id":"copilot","dry_run":false}`, false, false},
		{"legacy", `{"engine_id":"copilot"}`, false, false},
		{"invalid", `{"dry_run":"true"}`, false, true},
		{"missing", "", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.metadata != "" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "aw_info.json"), []byte(tc.metadata), 0600))
			}
			result := DownloadResult{LogsPath: dir}
			assert.False(t, applyRunFilters(context.Background(), result, runFilterOpts{}, false))
			assert.Equal(t, tc.missing || !tc.dryRun, applyRunFilters(context.Background(), result, runFilterOpts{dryRun: true}, false))
			assert.Equal(t, tc.missing || tc.dryRun, applyRunFilters(context.Background(), result, runFilterOpts{noDryRun: true}, false))
			if !tc.missing {
				info, err := parseAwInfo(filepath.Join(dir, "aw_info.json"), false)
				require.NoError(t, err)
				assert.Equal(t, tc.dryRun, info.DryRun)
				config := extractEngineConfigWithInferredEngine(dir, "")
				require.NotNil(t, config)
				assert.Equal(t, tc.dryRun, config.DryRun)
			}
		})
	}
}

func TestLogsDryRunFlags(t *testing.T) {
	t.Parallel()
	for _, flag := range []string{"dry-run", "no-dry-run"} {
		t.Run(flag, func(t *testing.T) {
			cmd := NewLogsCommand()
			require.NoError(t, cmd.Flags().Set(flag, "true"))
			opts, err := loadCommonLogsOptions(cmd)
			require.NoError(t, err)
			assert.Equal(t, flag == "dry-run", opts.DryRun)
			assert.Equal(t, flag == "no-dry-run", opts.NoDryRun)
			stdinOpts, err := loadStdinLogsOptions(cmd)
			require.NoError(t, err)
			assert.Equal(t, opts.DryRun, stdinOpts.DryRun)
			assert.Equal(t, opts.NoDryRun, stdinOpts.NoDryRun)
			continuation := buildContinuationIfNeeded(nil, false, true, false, false, logsTargetContinuationOptions(opts))
			require.NotNil(t, continuation)
			assert.Equal(t, opts.DryRun, continuation.DryRun)
			assert.Equal(t, opts.NoDryRun, continuation.NoDryRun)
		})
	}
	cmd := NewLogsCommand()
	require.NoError(t, cmd.Flags().Set("dry-run", "true"))
	require.NoError(t, cmd.Flags().Set("no-dry-run", "true"))
	require.ErrorContains(t, cmd.ValidateFlagGroups(), "none of the others")
}
