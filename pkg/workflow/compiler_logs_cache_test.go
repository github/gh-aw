//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUsesSharedLogsCache(t *testing.T) {
	tests := []struct {
		name string
		data WorkflowData
		want bool
	}{
		{
			name: "scheduled custom logs command",
			data: WorkflowData{On: "schedule: daily", CustomSteps: "run: gh aw logs --json -o .github/aw/logs"},
			want: true,
		},
		{
			name: "scheduled prompt audit command",
			data: WorkflowData{On: "schedule: daily", MarkdownContent: "Run `gh aw audit 123 -o .github/aw/logs`."},
			want: true,
		},
		{
			name: "scheduled logs command with a different output directory",
			data: WorkflowData{On: "schedule: daily", CustomSteps: "run: ./gh-aw logs -o /tmp/gh-aw/aw-mcp/logs"},
			want: false,
		},
		{
			name: "non-scheduled logs command",
			data: WorkflowData{On: "workflow_dispatch:", CustomSteps: "run: gh aw logs"},
			want: false,
		},
		{
			name: "scheduled unrelated workflow",
			data: WorkflowData{On: "schedule: daily", MarkdownContent: "Review issues."},
			want: false,
		},
		{
			name: "command in YAML comment is ignored",
			data: WorkflowData{On: "schedule: daily", CustomSteps: "# run: gh aw logs --json"},
			want: false,
		},
		{
			name: "command in shell comment is ignored",
			data: WorkflowData{On: "schedule: daily", CustomSteps: "  # do not run: gh aw audit"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, usesSharedLogsCache(&tt.data))
		})
	}
}

func TestGenerateSharedLogsCacheRestoreSteps(t *testing.T) {
	cache := NewActionCache(t.TempDir())
	data := &WorkflowData{
		On:             "schedule: daily",
		CustomSteps:    "run: ./gh-aw logs -o .github/aw/logs",
		ActionCache:    cache,
		ActionResolver: NewActionResolver(cache),
	}
	var yaml strings.Builder

	generateSharedLogsCacheRestoreSteps(&yaml, data)

	output := yaml.String()
	assert.Contains(t, output, "actions/cache/restore@")
	assert.Contains(t, output, "key: "+sharedLogsCacheKey)
	assert.Contains(t, output, "path: "+sharedLogsCachePath)
	assert.NotContains(t, output, "restore-keys:")
	assert.NotContains(t, output, "actions/cache/save@")
}

func TestSharedLogsCacheRestoreFollowsCustomCheckout(t *testing.T) {
	cache := NewActionCache(t.TempDir())
	data := &WorkflowData{
		On:             "schedule: daily",
		CustomSteps:    "steps:\n  - uses: actions/checkout@v4\n  - name: Download logs\n    run: gh aw logs -o .github/aw/logs",
		ActionCache:    cache,
		ActionResolver: NewActionResolver(cache),
	}
	var yaml strings.Builder
	compiler := &Compiler{}

	compiler.addCustomStepsWithRuntimeInsertion(&yaml, data.CustomSteps, nil, sharedLogsCacheRestoreSteps(data), nil, false)

	output := yaml.String()
	checkoutIndex := strings.Index(output, "uses: actions/checkout@v4")
	restoreIndex := strings.Index(output, "Restore shared agentic logs cache")
	logsIndex := strings.Index(output, "run: gh aw logs")
	assert.Greater(t, restoreIndex, checkoutIndex)
	assert.Greater(t, logsIndex, restoreIndex)
}

// TestSharedLogsCacheRestoreFollowsLastCheckoutInMultiCheckout verifies that when custom steps
// contain multiple checkout actions (a multi-checkout scenario), the cache restore step is
// inserted after the LAST checkout, not the first. This ensures a later root checkout cannot
// wipe .github/aw/logs before the logs/audit command uses the cached data.
func TestSharedLogsCacheRestoreFollowsLastCheckoutInMultiCheckout(t *testing.T) {
	cache := NewActionCache(t.TempDir())
	data := &WorkflowData{
		On: "schedule: daily",
		CustomSteps: "steps:\n" +
			"  - uses: actions/checkout@v4\n" +
			"    with:\n" +
			"      repository: org/repo-a\n" +
			"  - name: Setup step\n" +
			"    run: echo setup\n" +
			"  - uses: actions/checkout@v4\n" +
			"    with:\n" +
			"      repository: org/repo-b\n" +
			"  - name: Download logs\n" +
			"    run: gh aw logs -o .github/aw/logs",
		ActionCache:    cache,
		ActionResolver: NewActionResolver(cache),
	}
	var yaml strings.Builder
	compiler := &Compiler{}

	compiler.addCustomStepsWithRuntimeInsertion(&yaml, data.CustomSteps, nil, sharedLogsCacheRestoreSteps(data), nil, false)

	output := yaml.String()
	firstCheckoutIndex := strings.Index(output, "uses: actions/checkout@v4")
	lastCheckoutIndex := strings.LastIndex(output, "uses: actions/checkout@v4")
	restoreIndex := strings.Index(output, "Restore shared agentic logs cache")
	logsIndex := strings.Index(output, "run: gh aw logs")

	// Cache restore must appear after the LAST checkout, not after the first.
	assert.Greater(t, restoreIndex, lastCheckoutIndex, "cache restore should follow the last checkout")
	assert.Greater(t, logsIndex, restoreIndex, "gh aw logs should follow the cache restore")
	// Verify there are indeed two separate checkouts.
	assert.NotEqual(t, firstCheckoutIndex, lastCheckoutIndex, "should have two distinct checkout positions")
}
