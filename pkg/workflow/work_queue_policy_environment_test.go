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
	policy := workqueue.DefaultPolicy("1", "${{ github.repository }}")
	policy.Authorization = "aw"
	policy.Producers = map[string]workqueue.ProducerRule{}
	template := policy.Pools["default"]
	profile := template.Profiles["default"]
	profile.Principal = ""
	profile.Ref = "${{ github.sha }}"
	profile.LogicalContract = strings.Repeat("a", 64)
	template.Profiles = map[string]workqueue.WorkerProfile{}
	template.DefaultProfile = "worker-0"
	for index := range 40 {
		name := fmt.Sprintf("worker-%d", index)
		profile.Workflow = ".github/workflows/" + name + ".lock.yml"
		template.Profiles[name] = profile
	}
	for _, name := range []string{"default", "reports", "engines"} {
		policy.Pools[name] = template
	}
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
