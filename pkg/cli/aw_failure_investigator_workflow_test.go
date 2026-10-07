package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAWFailureInvestigatorPrefetchUsesRunLevelFailures(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "aw-failure-investigator.md"))
	if err != nil {
		t.Fatalf("failed to read workflow source: %v", err)
	}

	text := string(content)
	for _, fragment := range []string{
		`const FAILURE_CONCLUSIONS = new Set(['failure', 'timed_out', 'startup_failure']);`,
		`const MAX_DISCOVERY_PAGES = 20;`,
		`const FAULT_MARKER = `,
		`function captureErrorWindow(logText) {`,
		`const hasFaultMarker = capturedLines.some((line) => FAULT_MARKER.test(line));`,
		`capture_likely_missed_fault: !hasFaultMarker,`,
		`.filter((name) => name.endsWith('.lock.yml'))`,
		`falling back to workflow path suffix matching`,
		`repos/${REPO}/actions/runs`,
		"runApiJson(`repos/${REPO}/actions/runs/${runId}/jobs`, { per_page: '100' })",
		`String(jobName || '').trim().toLowerCase() === 'agent'`,
		`const jobId = job.id;`,
		`failed_job_names: [...new Set(failedJobNames)].sort(),`,
		`agent_job_conclusion: agentJobConclusion,`,
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("expected workflow prefetch to contain %q", fragment)
		}
	}
	if strings.Contains(text, `'--log-failed'`) {
		t.Fatal("expected workflow prefetch to use full job logs for error-marker capture")
	}
	if strings.Contains(text, `job.databaseId`) {
		t.Fatal("expected workflow prefetch to use the Actions API job.id")
	}
}
