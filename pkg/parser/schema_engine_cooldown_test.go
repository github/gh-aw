//go:build !integration

package parser

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSchemaEngineCooldown(t *testing.T) {
	for _, inline := range []bool{false, true} {
		for _, value := range []any{0, 3, -1, true, false, "3", 1.5, nil, []any{3}} {
			t.Run(fmt.Sprintf("%t/%v", inline, value), func(t *testing.T) {
				engine := map[string]any{"id": "codex", "cooldown": value}
				if inline {
					delete(engine, "id")
					engine["runtime"] = map[string]any{"id": "codex"}
				}
				data := map[string]any{"on": "push", "engine": engine}
				err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(data, "cooldown.md")
				days, ok := value.(int)
				if ok && days >= 0 {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
				// Nested detection engines reference the same schema.
				delete(data, "engine")
				data["safe-outputs"] = map[string]any{"threat-detection": map[string]any{"engine": engine}}
				err = ValidateMainWorkflowFrontmatterWithSchemaAndLocation(data, "cooldown.md")
				if ok && days >= 0 {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			})
		}
	}
}
