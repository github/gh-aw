package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractResourceEntriesDoesNotInferGraderFiles(t *testing.T) {
	entries, err := extractResourceEntries(`---
on: workflow_dispatch
resources:
  - data/config.json
graders:
  operational-value:
    script: |
      return [{id: "effect", value: 1}]
---
# Test
`)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "data/config.json", entries[0].path)
}
