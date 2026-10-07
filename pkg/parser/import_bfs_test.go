//go:build !integration

package parser

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHandleAgentImportItemSubstitutesModelFromVirtualFile(t *testing.T) {
	const importedAgentPath = "/virtual/repository/.github/agents/imported.md"
	content := `---
import-schema:
  model:
    type: string
    default: claude-haiku-4.5
model: ${{ github.aw.import-inputs.model }}
---
Agent instructions.
`
	originalReadFile := readFileFunc
	readFileFunc = func(path string) ([]byte, error) {
		if path != importedAgentPath {
			return nil, fmt.Errorf("unexpected path %q", path)
		}
		return []byte(content), nil
	}
	t.Cleanup(func() {
		readFileFunc = originalReadFile
	})

	for _, test := range []struct {
		name      string
		inputs    map[string]any
		wantModel string
	}{
		{name: "input substitution", inputs: map[string]any{"model": "claude-sonnet-4.6"}, wantModel: "claude-sonnet-4.6"},
		{name: "schema default", wantModel: "claude-haiku-4.5"},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := newImportBFSState()
			handled, err := handleAgentImportItem(importQueueItem{
				fullPath:   importedAgentPath,
				importPath: ".github/agents/imported.md",
				inputs:     test.inputs,
			}, state)
			require.NoError(t, err)
			require.True(t, handled)
			require.Equal(t, []SubAgentModel{{Name: "imported.md", Model: test.wantModel}}, state.acc.subAgentModels)
		})
	}
}

func TestCollectInlineSubAgentModelsHonorsImportedSection(t *testing.T) {
	content := `---
on: issues
---
# Chosen
## agent: ` + "`selected`" + `
---
model: claude-haiku-4.5
---
Selected instructions.

# Other
## agent: ` + "`excluded`" + `
---
model: claude-opus-4.6
---
Excluded instructions.
`
	parsed, err := ExtractFrontmatterFromContent(content)
	require.NoError(t, err)

	acc := newImportAccumulator()
	err = acc.collectInlineSubAgentWarnings(
		"shared.md#Chosen", "Chosen", content, false, parsed, nil,
	)
	require.NoError(t, err)
	require.Equal(t, []SubAgentModel{{Name: "selected", Model: "claude-haiku-4.5"}}, acc.subAgentModels)
}

func TestParseImportSpecsFromArray_RejectsIfField(t *testing.T) {
	_, err := parseImportSpecsFromArray([]any{
		map[string]any{
			"uses": "shared/workflow.md",
			"if":   "experiments.variant == 'a'",
		},
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "import 'if' is no longer supported")
}

func TestParseImportSpecsFromObject(t *testing.T) {
	tests := []struct {
		name          string
		importsObject map[string]any
		wantSpecs     []ImportSpec
		wantErr       string
	}{
		{
			name:          "no aw key returns nil, nil",
			importsObject: map[string]any{},
			wantSpecs:     nil,
		},
		{
			name:          "aw key present but nil value falls to default case",
			importsObject: map[string]any{"aw": nil},
			wantErr:       "imports.aw must be an array of strings or objects",
		},
		{
			name: "aw as []any of strings",
			importsObject: map[string]any{
				"aw": []any{"shared/a.md", "shared/b.md"},
			},
			wantSpecs: []ImportSpec{{Path: "shared/a.md"}, {Path: "shared/b.md"}},
		},
		{
			name: "aw as []any of objects",
			importsObject: map[string]any{
				"aw": []any{
					map[string]any{"path": "shared/c.md"},
				},
			},
			wantSpecs: []ImportSpec{{Path: "shared/c.md"}},
		},
		{
			name: "aw as []any propagates array parse error",
			importsObject: map[string]any{
				"aw": []any{
					map[string]any{"nope": "shared/d.md"},
				},
			},
			wantErr: "imports.aw: import object must have a 'path' or 'uses' field",
		},
		{
			name: "aw as []string",
			importsObject: map[string]any{
				"aw": []string{"shared/e.md", "shared/f.md"},
			},
			wantSpecs: []ImportSpec{{Path: "shared/e.md"}, {Path: "shared/f.md"}},
		},
		{
			name: "aw as []string empty slice returns empty (non-nil) specs",
			importsObject: map[string]any{
				"aw": []string{},
			},
			wantSpecs: []ImportSpec{},
		},
		{
			name: "aw as unsupported type (string) returns error",
			importsObject: map[string]any{
				"aw": "shared/g.md",
			},
			wantErr: "imports.aw must be an array of strings or objects",
		},
		{
			name: "aw as unsupported type (map) returns error",
			importsObject: map[string]any{
				"aw": map[string]any{"path": "shared/h.md"},
			},
			wantErr: "imports.aw must be an array of strings or objects",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			specs, err := parseImportSpecsFromObject(tt.importsObject)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tt.wantErr)
				require.Nil(t, specs)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantSpecs, specs)
		})
	}
}
