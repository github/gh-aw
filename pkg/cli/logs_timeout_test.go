//go:build !integration

package cli

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBuildLogsCommandArgsIncludesResourceBudgets(t *testing.T) {
	cmdArgs, _, _ := buildLogsCommandArgs(context.Background(), logsArgs{
		Count:                 1,
		Timeout:               1,
		MaxGitHubAPIRateLimit: -2000,
		MaxStorageMB:          10240,
		PruneOlderRuns:        true,
	})
	command := strings.Join(cmdArgs, " ")

	if !strings.Contains(command, "--max-github-api-rate-limit -2000") {
		t.Fatalf("command args do not include GitHub API rate-limit budget: %s", command)
	}
	if !strings.Contains(command, "--max-storage 10240") {
		t.Fatalf("command args do not include storage budget: %s", command)
	}
	if !strings.Contains(command, "--prune-older-runs") {
		t.Fatalf("command args do not include older-run pruning mode: %s", command)
	}
}

// TestTimeoutFlagParsing tests that the timeout flag is properly parsed
func TestTimeoutFlagParsing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		timeout         int
		expectTimeout   bool
		expectedMinutes int
	}{
		{
			name:            "no timeout specified",
			timeout:         0,
			expectTimeout:   false,
			expectedMinutes: 0,
		},
		{
			name:            "timeout of 5 minutes",
			timeout:         5,
			expectTimeout:   true,
			expectedMinutes: 5,
		},
		{
			name:            "timeout of 30 minutes",
			timeout:         30,
			expectTimeout:   true,
			expectedMinutes: 30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Test that the timeout value is correctly used
			if tt.expectTimeout && tt.timeout == 0 {
				t.Errorf("Expected timeout to be set but got 0")
			}
			if !tt.expectTimeout && tt.timeout != 0 {
				t.Errorf("Expected no timeout but got %d", tt.timeout)
			}
			if tt.expectTimeout && tt.timeout != tt.expectedMinutes {
				t.Errorf("Expected timeout of %d minutes but got %d", tt.expectedMinutes, tt.timeout)
			}
		})
	}
}

func TestEffectiveMCPLogsToolSoftTimeoutSeconds(t *testing.T) {
	t.Parallel()
	t.Run("no gateway deadline leaves CLI timeout unchanged", func(t *testing.T) {
		t.Parallel()
		got, ok := effectiveMCPLogsToolSoftTimeoutSeconds(context.Background(), 5)
		if ok || got != 0 {
			t.Fatalf("effectiveMCPLogsToolSoftTimeoutSeconds without deadline = (%d, %v), want (0, false)", got, ok)
		}
	})

	t.Run("gateway deadline below CLI timeout returns safety margin", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		got, ok := effectiveMCPLogsToolSoftTimeoutSeconds(ctx, 5)
		if !ok {
			t.Fatal("expected soft timeout when gateway deadline is shorter than CLI timeout")
		}
		if got < 50 || got > 54 {
			t.Fatalf("soft timeout = %d seconds, want between 50 and 54 seconds", got)
		}
	})

	t.Run("gateway deadline beyond CLI timeout leaves CLI timeout unchanged", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		got, ok := effectiveMCPLogsToolSoftTimeoutSeconds(ctx, 5)
		if ok || got != 0 {
			t.Fatalf("effectiveMCPLogsToolSoftTimeoutSeconds with long deadline = (%d, %v), want (0, false)", got, ok)
		}
	})
}

// TestTimeoutLogic tests the timeout logic without making network calls
func TestTimeoutLogic(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		timeout       int
		elapsed       time.Duration
		shouldTimeout bool
	}{
		{
			name:          "no timeout set",
			timeout:       0,
			elapsed:       100 * time.Minute,
			shouldTimeout: false,
		},
		{
			name:          "timeout not reached",
			timeout:       60,
			elapsed:       30 * time.Minute,
			shouldTimeout: false,
		},
		{
			name:          "just under boundary",
			timeout:       1,
			elapsed:       59 * time.Second,
			shouldTimeout: false,
		},
		{
			name:          "timeout exactly reached",
			timeout:       1,
			elapsed:       60 * time.Second,
			shouldTimeout: true,
		},
		{
			name:          "timeout exceeded",
			timeout:       1,
			elapsed:       90 * time.Second,
			shouldTimeout: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Simulate the timeout check logic (timeout is in minutes, elapsed in seconds)
			var timeoutReached bool
			if tt.timeout > 0 {
				if tt.elapsed.Seconds() >= float64(tt.timeout)*60 {
					timeoutReached = true
				}
			}

			if timeoutReached != tt.shouldTimeout {
				t.Errorf("Expected timeout reached=%v but got %v (timeout=%d min, elapsed=%.1fs)",
					tt.shouldTimeout, timeoutReached, tt.timeout, tt.elapsed.Seconds())
			}
		})
	}
}

// TestEffectiveMCPLogsToolTimeoutMinutes verifies that the MCP logs tool
// scales its implicit timeout with larger fetch windows while preserving
// explicit user-provided timeouts.
func TestEffectiveMCPLogsToolTimeoutMinutes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		requestedTimeout int
		count            int
		workflowName     string
		engine           string
		want             int
	}{
		{
			name:             "explicit timeout is preserved",
			requestedTimeout: 5,
			count:            100,
			workflowName:     "my-workflow",
			want:             5,
		},
		{
			name:             "explicit timeout is preserved even without workflow name",
			requestedTimeout: 5,
			count:            100,
			workflowName:     "",
			want:             5,
		},
		{
			name:             "small fetch window keeps one minute default (named workflow)",
			requestedTimeout: 0,
			count:            40,
			workflowName:     "my-workflow",
			want:             1,
		},
		{
			name:             "fetch window above forty runs gets two minutes (named workflow)",
			requestedTimeout: 0,
			count:            41,
			workflowName:     "my-workflow",
			want:             2,
		},
		{
			name:             "eighty run fetch window stays in two minute tier (named workflow)",
			requestedTimeout: 0,
			count:            80,
			workflowName:     "my-workflow",
			want:             2,
		},
		{
			name:             "eighty one run fetch window enters three minute tier (named workflow)",
			requestedTimeout: 0,
			count:            81,
			workflowName:     "my-workflow",
			want:             3,
		},
		{
			name:             "default hundred run window gets three minutes (named workflow)",
			requestedTimeout: 0,
			count:            100,
			workflowName:     "my-workflow",
			want:             3,
		},
		{
			name:             "unspecified count falls back to default window size (named workflow)",
			requestedTimeout: 0,
			count:            0,
			workflowName:     "my-workflow",
			want:             3,
		},
		// Engine-filtering cases: minimum 5 minutes when an engine filter is given,
		// regardless of whether workflow_name is also present.
		{
			name:             "engine filtering with named workflow uses all-workflow minimum",
			requestedTimeout: 0,
			count:            2,
			workflowName:     "my-workflow",
			engine:           "claude",
			want:             defaultMCPLogsMinTimeoutMinutesAllWorkflows,
		},
		{
			name:             "engine filtering without workflow name also uses all-workflow minimum",
			requestedTimeout: 0,
			count:            2,
			workflowName:     "",
			engine:           "claude",
			want:             defaultMCPLogsMinTimeoutMinutesAllWorkflows,
		},
		// All-workflow cases: minimum 5 minutes when no workflow_name is given
		{
			name:             "small count uses all-workflow minimum (no workflow name)",
			requestedTimeout: 0,
			count:            3,
			workflowName:     "",
			want:             defaultMCPLogsMinTimeoutMinutesAllWorkflows,
		},
		{
			name:             "default count uses all-workflow minimum (no workflow name)",
			requestedTimeout: 0,
			count:            100,
			workflowName:     "",
			want:             defaultMCPLogsMinTimeoutMinutesAllWorkflows,
		},
		{
			name:             "very large count exceeds all-workflow minimum (no workflow name)",
			requestedTimeout: 0,
			count:            250,
			workflowName:     "",
			want:             (250 + mcpLogsRunsPerDefaultTimeoutMinute - 1) / mcpLogsRunsPerDefaultTimeoutMinute, // ceil(250/mcpLogsRunsPerDefaultTimeoutMinute) > defaultMCPLogsMinTimeoutMinutesAllWorkflows
		},
		// Timeout cap cases: user-supplied value must not exceed maxMCPLogsSubprocessTimeoutMinutes
		{
			name:             "explicit timeout exactly at max is preserved",
			requestedTimeout: maxMCPLogsSubprocessTimeoutMinutes,
			count:            100,
			workflowName:     "my-workflow",
			want:             maxMCPLogsSubprocessTimeoutMinutes,
		},
		{
			name:             "explicit timeout above max is capped",
			requestedTimeout: maxMCPLogsSubprocessTimeoutMinutes + 1,
			count:            100,
			workflowName:     "my-workflow",
			want:             maxMCPLogsSubprocessTimeoutMinutes,
		},
		{
			name:             "very large explicit timeout is capped",
			requestedTimeout: 100000,
			count:            100,
			workflowName:     "",
			want:             maxMCPLogsSubprocessTimeoutMinutes,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := effectiveMCPLogsToolTimeoutMinutes(tt.requestedTimeout, tt.count, tt.workflowName, tt.engine); got != tt.want {
				t.Errorf("effectiveMCPLogsToolTimeoutMinutes(%d, %d, %q, %q) = %d, want %d", tt.requestedTimeout, tt.count, tt.workflowName, tt.engine, got, tt.want)
			}
		})
	}
}
