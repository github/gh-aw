package workflow

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/stretchr/testify/require"
)

func TestWorkQueuePolicyEnvironmentSharedRegistryWithinActionsLimit(t *testing.T) {
	settings, err := workqueue.ParseSettings([]byte(`{"pools":{"reports":{"concurrency":3},"engines":{"concurrency":3,"per_account_limit":1},"smoke":{},"scanning":{}}}`))
	require.NoError(t, err)
	// Workers are listed once, so only a large approved set exceeds one Actions value.
	workers := make([]string, 0, 256)
	for index := range 256 {
		workers = append(workers, fmt.Sprintf("worker-%03d-%s", index, strings.Repeat("x", 80)))
	}
	policy, err := workqueue.BuildPolicyEnv(settings, "${{ github.repository }}", workers)
	require.NoError(t, err)
	encoded, err := json.Marshal(policy)
	require.NoError(t, err)
	require.Greater(t, len(encoded), 21*1024)
	chunks := splitWorkQueuePolicyJSON(string(encoded))
	require.Equal(t, string(encoded), strings.Join(chunks, ""))
	for _, chunk := range chunks {
		require.Less(t, len(chunk), 21*1024)
		require.Len(t, regexp.MustCompile(`\$\{\{[^}]*\}\}`).FindAllString(chunk, -1), strings.Count(chunk, "${{"))
	}
	lines := strings.Join(workQueuePolicyJSONEnvironment(string(encoded)), "")
	require.Contains(t, lines, "GH_AW_WORK_QUEUE_POLICY_PARTS:")
	require.NotContains(t, lines, "GH_AW_WORK_QUEUE_POLICY:")
}

func TestWorkQueuePolicyEnvironmentPreservesUTF8AndQuotedDelimiters(t *testing.T) {
	encoded, err := json.Marshal(map[string]any{
		"values": strings.Repeat("quoted \", value λ ${{ github.sha }} ", 700),
		"tail":   strings.Repeat("value,", 400),
	})
	require.NoError(t, err)
	chunks := splitWorkQueuePolicyJSON(string(encoded))
	require.Equal(t, string(encoded), strings.Join(chunks, ""))
	for _, chunk := range chunks {
		require.True(t, utf8.ValidString(chunk))
	}
	require.Contains(t, strings.Join(workQueuePolicyJSONEnvironment(`{"authorization":"aw"}`), ""), "GH_AW_WORK_QUEUE_POLICY:")
}
