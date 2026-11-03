//go:build !integration

package workflow

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/require"
)

func TestConnectorBundledNodeCLIStaging(t *testing.T) {
	node, err := exec.LookPath("node")
	require.NoError(t, err, "Node.js is required to verify credential-free CLI staging")
	runtime, err := filepath.Abs("../../actions/setup/js/connector_runtime.cjs")
	require.NoError(t, err)
	for _, test := range []struct {
		provider, operation string
		plans               func(*testing.T) (*connectors.Plan, *connectors.Plan)
		args                map[string]any
		rejectUnauthorized  bool
	}{
		{
			provider: "datadog", operation: "rename-incident", plans: datadogPlans,
			args: map[string]any{"incident-id": datadogIncidentID, "title": "Staged build evidence"},
		},
		{
			provider: "notion", operation: "add-comment", plans: notionFrozenPlans,
			args: map[string]any{"page-id": notionPageID, "text": "Release reference reviewed."},
		},
		{
			provider: "pagerduty", operation: "add-note", plans: pagerDutyPlans,
			args: pagerDutyNoteArgs(), rejectUnauthorized: true,
		},
	} {
		t.Run(test.provider, func(t *testing.T) {
			_, plan := test.plans(t)
			encoded, err := plan.Encode()
			require.NoError(t, err)
			request, err := plan.PrepareSafeOutput(test.operation, test.args)
			require.NoError(t, err)
			expected, err := json.Marshal(map[string]any{"staged": true, "request": request})
			require.NoError(t, err)
			input, err := json.Marshal(test.args)
			require.NoError(t, err)
			command := exec.CommandContext(t.Context(), node, runtime, "invoke", "safe-output", test.operation, "--stage")
			command.Env = []string{"GH_AW_CONNECTOR_PLAN=" + encoded}
			command.Stdin = bytes.NewReader(input)
			output, err := command.CombinedOutput()
			require.NoError(t, err, "%s", output)
			require.JSONEq(t, string(expected), string(output))
			if test.rejectUnauthorized {
				unauthorized := exec.CommandContext(t.Context(), node, runtime, "invoke", "safe-output", test.operation)
				unauthorized.Env = []string{"GH_AW_CONNECTOR_PLAN=" + encoded}
				unauthorized.Stdin = bytes.NewReader(input)
				output, err = unauthorized.CombinedOutput()
				require.Error(t, err)
				require.Contains(t, string(output), "requires explicit host write authorization")
			}
		})
	}
}
