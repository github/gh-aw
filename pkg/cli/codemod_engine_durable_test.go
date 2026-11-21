//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEngineDurableCodemod_Metadata(t *testing.T) {
	t.Parallel()
	codemod := getEngineDurableToRequestContinuationCodemod()

	assert.Equal(t, "engine-durable-to-request-continuation", codemod.ID)
	assert.NotEmpty(t, codemod.Name)
	assert.NotEmpty(t, codemod.Description)
	assert.Equal(t, "1.5.0", codemod.IntroducedIn)
	require.NotNil(t, codemod.Apply)
}

func TestEngineDurableCodemod_Apply(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		content  string
		expected string
		applied  bool
	}{
		{
			name: "no durable is a no-op",
			content: `---
on: workflow_dispatch
engine:
  id: copilot
---
# Body
`,
			applied: false,
		},
		{
			name: "engine.durable becomes request-continuation",
			content: `---
on: workflow_dispatch # manual
engine:
  id: copilot
  durable:
    retention-days: 3
safe-outputs:
  noop: {}
---

# Body
`,
			expected: `---
on: workflow_dispatch # manual
engine:
  id: copilot
safe-outputs:
  request-continuation: {}
  noop: {}
---

# Body
`,
			applied: true,
		},
		{
			name: "engine.pi.durable is removed and safe-outputs is created",
			content: `---
on: workflow_dispatch
engine:
  id: pi
  pi:
    durable: {}
    model: openai/gpt-5
---
# Body
`,
			expected: `---
safe-outputs:
  request-continuation: {}
on: workflow_dispatch
engine:
  id: pi
  pi:
    model: openai/gpt-5
---
# Body
`,
			applied: true,
		},
		{
			name: "existing request-continuation is preserved",
			content: `---
on: workflow_dispatch
engine:
  id: claude
  durable: {}
safe-outputs:
  request-continuation:
    max: 3
---
# Body
`,
			expected: `---
on: workflow_dispatch
engine:
  id: claude
safe-outputs:
  request-continuation:
    max: 3
---
# Body
`,
			applied: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			codemod := getEngineDurableToRequestContinuationCodemod()
			result, applied, err := codemod.Apply(tt.content, nil)
			require.NoError(t, err)
			assert.Equal(t, tt.applied, applied)
			if !tt.applied {
				assert.Equal(t, tt.content, result)
				return
			}
			assert.Equal(t, tt.expected, result)

			again, appliedAgain, err := codemod.Apply(result, nil)
			require.NoError(t, err, "second run should succeed")
			assert.False(t, appliedAgain, "codemod should be idempotent")
			assert.Equal(t, result, again)
		})
	}
}
