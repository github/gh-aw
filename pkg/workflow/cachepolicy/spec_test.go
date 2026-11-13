//go:build !integration

package cachepolicy_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/github/gh-aw/pkg/workflow/cachepolicy"
)

// TestSpec_PublicAPI_WriteEvents checks README.md / Public API: pull_request is
// allowed for caches isolated to a pull-request merge ref.
func TestSpec_PublicAPI_WriteEvents(t *testing.T) {
	t.Parallel()

	assert.Contains(t, cachepolicy.WriteEvents(), "pull_request",
		"the documented cache-write allowlist should include pull_request")
}

// TestSpec_PublicAPI_ReadOnlyEvents checks README.md / Public API: every
// read-only event lies outside the write allowlist.
func TestSpec_PublicAPI_ReadOnlyEvents(t *testing.T) {
	t.Parallel()

	writers := cachepolicy.WriteEvents()
	for _, event := range cachepolicy.ReadOnlyEvents() {
		t.Run(event, func(t *testing.T) {
			assert.NotContains(t, writers, event,
				"read-only event %q should be outside the cache-write allowlist", event)
		})
	}
}

// SPEC_AMBIGUITY: The README does not enumerate either complete event set or
// specify ordering, collection ownership, or which set contains workflow_call.
// Its caller-policy review requirement cannot be decided by these lists alone.
