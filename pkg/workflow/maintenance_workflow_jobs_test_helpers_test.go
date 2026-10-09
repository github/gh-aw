package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// parseMaintenanceJobs splits the generated maintenance workflow into top-level job bodies.
func parseMaintenanceJobs(t *testing.T, content string) map[string]string {
	t.Helper()
	_, jobsSection, found := strings.Cut(content, "\njobs:\n")
	require.True(t, found)
	jobs := make(map[string]string)
	current := ""
	for line := range strings.SplitSeq(jobsSection, "\n") {
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") && strings.HasSuffix(line, ":") {
			current = strings.TrimSuffix(strings.TrimSpace(line), ":")
			continue
		}
		if current != "" {
			jobs[current] += line + "\n"
		}
	}
	return jobs
}
