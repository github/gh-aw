//go:build !integration

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthWorkQueueSanitizesAPIWarnings(t *testing.T) {
	for _, stage := range []string{"repository", "detection"} {
		for _, limited := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/rate-limit=%t", stage, limited), func(t *testing.T) {
				collection := healthWorkQueueFixture(t, "env:\n  GH_AW_WORK_QUEUE_ENABLED: true")
				original := healthRunGH
				healthRunGH = func(ctx context.Context, message string, args ...string) ([]byte, error) {
					if stage == "repository" || args[3] != "repos/owner/repo" {
						detail := "SENTINEL-CREDENTIAL"
						if limited {
							detail += " API rate limit exceeded"
						}
						return []byte("SENTINEL-RESPONSE"), errors.New(detail)
					}
					return original(ctx, message, args...)
				}
				collectHealthWorkQueue(collection)
				require.NotNil(t, collection.WorkQueue)
				require.NotEmpty(t, collection.WorkQueue.Warnings)
				data, err := json.Marshal(collection)
				require.NoError(t, err)
				assert.NotContains(t, string(data), "SENTINEL")
				var text bytes.Buffer
				require.NoError(t, renderHealthCollection(&text, collection))
				assert.NotContains(t, text.String(), "SENTINEL")
				if limited {
					assert.Contains(t, text.String(), "rate_limit:")
					assert.True(t, collection.Partial)
					assert.Equal(t, "rate_limit", collection.Reason)
					assert.NotEmpty(t, collection.Warning)
				} else {
					assert.Contains(t, text.String(), "verify GitHub authentication")
				}
			})
		}
	}
}

func TestHealthWorkQueueSanitizesQuotaCheckWarning(t *testing.T) {
	collection := healthWorkQueueFixture(t, "env:\n  GH_AW_WORK_QUEUE_ENABLED: true")
	collection.APIRateLimit = nil
	original := fetchRateLimitForHostFunc
	t.Cleanup(func() { fetchRateLimitForHostFunc = original })
	fetchRateLimitForHostFunc = func(context.Context, string) (rateLimitResource, error) {
		return rateLimitResource{}, errors.New("SENTINEL-CREDENTIAL")
	}
	collectHealthWorkQueue(collection)
	assert.True(t, collection.Partial)
	data, err := json.Marshal(collection)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "SENTINEL")
	var text bytes.Buffer
	require.NoError(t, renderHealthCollection(&text, collection))
	assert.NotContains(t, text.String(), "SENTINEL")
}

func TestHealthWorkQueueDetectionWarningDoesNotChangeLedgerStatus(t *testing.T) {
	collection := healthWorkQueueFixture(t, "jobs: [", "env:\n  GH_AW_WORK_QUEUE_ENABLED: true")
	snapshot := healthWQSnapshot(func() *workQueueTestLog { log, _ := workQueueMixedFixture(t); return log }())
	healthReadWorkQueue = func(context.Context, workqueue.Branch) (workqueue.Snapshot, error) {
		return snapshot, nil
	}
	collectHealthWorkQueue(collection)
	require.NotNil(t, collection.WorkQueue)
	require.NotNil(t, collection.WorkQueue.Stats)
	assert.Equal(t, "observed", collection.WorkQueue.Status)
	require.Len(t, collection.WorkQueue.Warnings, 1)
	assert.Contains(t, collection.WorkQueue.Warnings[0], "invalid workflow YAML")
}

func TestHealthWorkQueueReadFailureGuidance(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"timeout", fmt.Errorf("SENTINEL: %w", context.DeadlineExceeded), "timeout: request timed out"},
		{"canceled", context.Canceled, "canceled: request canceled"},
		{"authentication", &api.HTTPError{StatusCode: http.StatusUnauthorized, Message: "SENTINEL"}, "access_unavailable: verify GitHub authentication"},
		{"permission", &api.HTTPError{StatusCode: http.StatusForbidden, Message: "SENTINEL"}, "access_unavailable: verify GitHub authentication"},
		{"missing or inaccessible", &api.HTTPError{StatusCode: http.StatusNotFound, Message: "SENTINEL"}, "access_unavailable: verify GitHub authentication"},
		{"transport", errors.New("SENTINEL"), "read_unavailable: verify GitHub authentication, repository read access, and host connectivity"},
		{"protocol", fmt.Errorf("wrapped: %w", &workqueue.ProtocolError{Code: "checkpoint_invalid", Message: "SENTINEL"}), "rejected (checkpoint_invalid)"},
		{"untrusted code", &workqueue.ProtocolError{Code: "SENTINEL\ninvalid", Message: "SENTINEL"}, "rejected (invalid_protocol)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			collection := healthWorkQueueFixture(t, "env:\n  GH_AW_WORK_QUEUE_ENABLED: true")
			healthReadWorkQueue = func(context.Context, workqueue.Branch) (workqueue.Snapshot, error) {
				return workqueue.Snapshot{}, tc.err
			}
			collectHealthWorkQueue(collection)
			require.NotNil(t, collection.WorkQueue)
			assert.Equal(t, "unavailable", collection.WorkQueue.Status)
			require.Len(t, collection.WorkQueue.Warnings, 1)
			assert.Contains(t, collection.WorkQueue.Warnings[0], tc.want)
			data, err := json.Marshal(collection)
			require.NoError(t, err)
			assert.NotContains(t, string(data), "SENTINEL")
		})
	}
}

func TestHealthWorkQueueDetectsCompiledEnvironmentMarker(t *testing.T) {
	for _, engine := range []string{"copilot", "claude", "codex"} {
		for _, worker := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/worker=%t", engine, worker), func(t *testing.T) {
				dir := t.TempDir()
				source := filepath.Join(dir, "worker.md")
				tool := "true"
				if worker {
					tool = "\n    worker: true"
				}
				require.NoError(t, os.WriteFile(source, []byte("---\non: workflow_dispatch\nengine: "+engine+"\ntools:\n  work-queue: "+tool+"\n---\nProcess queue work.\n"), 0o600))
				compiler := workflow.NewCompiler()
				compiler.SetApprove(true)
				require.NoError(t, compiler.CompileWorkflow(source))
				content, err := os.ReadFile(filepath.Join(dir, "worker.lock.yml"))
				require.NoError(t, err)
				enabled, err := healthWorkflowUsesWorkQueue(content)
				require.NoError(t, err)
				assert.True(t, enabled, "compiled workflows must expose the structured environment marker")
			})
		}
	}
}
