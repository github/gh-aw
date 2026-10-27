package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveOTLPMetadataReferences(t *testing.T) {
	tests := []struct {
		name         string
		metadata     any
		attributes   map[string]any
		resource     map[string]any
		want         map[string]any
		wantResource map[string]any
		wantErr      string
	}{
		{
			name:     "resolves both attribute maps and preserves unrelated expressions",
			metadata: map[string]any{"version": "0.1.8", "category": "automation"},
			attributes: map[string]any{
				"catalogue.version":  "${{ github.aw.metadata.version }}",
				"catalogue.category": "${{ github.aw.metadata.category }}",
				"actor":              "${{ github.actor }}",
			},
			resource: map[string]any{
				"catalogue.category": "${{ github.aw.metadata.category }}",
			},
			want: map[string]any{
				"catalogue.version":  "0.1.8",
				"catalogue.category": "automation",
				"actor":              "${{ github.actor }}",
			},
			wantResource: map[string]any{"catalogue.category": "automation"},
		},
		{
			name:     "allows an empty metadata value",
			metadata: map[string]any{"owner": ""},
			attributes: map[string]any{
				"catalogue.owner": "${{ github.aw.metadata.owner }}",
			},
			want: map[string]any{"catalogue.owner": ""},
		},
		{
			name: "requires a root metadata map",
			attributes: map[string]any{
				"catalogue.version": "${{ github.aw.metadata.version }}",
			},
			wantErr: `observability.otlp.attributes.catalogue.version references metadata key "version", but the root workflow has no metadata map`,
		},
		{
			name:     "requires referenced keys to exist",
			metadata: map[string]any{"category": "automation"},
			attributes: map[string]any{
				"catalogue.version": "${{ github.aw.metadata.version }}",
			},
			wantErr: `observability.otlp.attributes.catalogue.version references missing metadata key "version"`,
		},
		{
			name:     "requires string metadata values",
			metadata: map[string]any{"version": 1},
			attributes: map[string]any{
				"catalogue.version": "${{ github.aw.metadata.version }}",
			},
			wantErr: `observability.otlp.attributes.catalogue.version references metadata key "version", which must be a string`,
		},
		{
			name:     "rejects embedded references",
			metadata: map[string]any{"version": "1.0"},
			attributes: map[string]any{
				"catalogue.version": "release-${{ github.aw.metadata.version }}",
			},
			wantErr: "observability.otlp.attributes.catalogue.version must be a whole-value reference",
		},
		{
			name:     "rejects nested selectors",
			metadata: map[string]any{"catalogue": map[string]any{"version": "1.0"}},
			attributes: map[string]any{
				"catalogue.version": "${{ github.aw.metadata.catalogue.version }}",
			},
			wantErr: "observability.otlp.attributes.catalogue.version must be a whole-value reference",
		},
		{
			name:     "preserves literal and quoted namespace mentions",
			metadata: map[string]any{"version": "1.0"},
			attributes: map[string]any{
				"literal": "github.aw.metadata.version",
				"quoted":  "${{ 'github.aw.metadata.version' }}",
			},
			resource: map[string]any{
				"literal": "github.aw.metadata.version",
				"quoted":  `${{ "github.aw.metadata.version" }}`,
			},
			want: map[string]any{
				"literal": "github.aw.metadata.version",
				"quoted":  "${{ 'github.aw.metadata.version' }}",
			},
			wantResource: map[string]any{
				"literal": "github.aw.metadata.version",
				"quoted":  `${{ "github.aw.metadata.version" }}`,
			},
		},
		{
			name:     "resolves bracket-selected metadata in attributes",
			metadata: map[string]any{"version": "1.0"},
			attributes: map[string]any{
				"catalogue.version": "${{ github.aw['metadata'].version }}",
			},
			want: map[string]any{"catalogue.version": "1.0"},
		},
		{
			name:     "resolves bracket-selected metadata in resource attributes",
			metadata: map[string]any{"version": "1.0"},
			resource: map[string]any{
				"catalogue.version": `${{ github['aw']['metadata']['version'] }}`,
			},
			wantResource: map[string]any{"catalogue.version": "1.0"},
		},
		{
			name:     "rejects nested metadata access in expressions",
			metadata: map[string]any{"version": "1.0"},
			attributes: map[string]any{
				"catalogue.version": "${{ contains(github.aw.metadata.version, 'x') }}",
			},
			resource: map[string]any{
				"catalogue.version": "${{ contains(github.aw.metadata.version, 'x') }}",
			},
			wantErr: "observability.otlp.attributes.catalogue.version must be a whole-value reference",
		},
		{
			name:     "reports invalid attributes deterministically",
			metadata: map[string]any{},
			attributes: map[string]any{
				"zeta":  "${{ github.aw['metadata'].version }}",
				"alpha": "${{ github.aw.metadata.missing }}",
			},
			wantErr: `observability.otlp.attributes.alpha references missing metadata key "missing"`,
		},
		{
			name:     "rejects expression-looking metadata",
			metadata: map[string]any{"version": "${{ github.actor }}"},
			attributes: map[string]any{
				"catalogue.version": "${{ github.aw.metadata.version }}",
			},
			wantErr: "contains GitHub Actions expression delimiters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frontmatter := map[string]any{
				"observability": map[string]any{
					"otlp": map[string]any{"attributes": tt.attributes},
				},
			}
			otlp := frontmatter["observability"].(map[string]any)["otlp"].(map[string]any)
			if tt.resource != nil {
				otlp["resource-attributes"] = tt.resource
			}
			if tt.metadata != nil {
				frontmatter["metadata"] = tt.metadata
			}
			workflowData := &WorkflowData{RawFrontmatter: frontmatter}

			err := resolveOTLPMetadataReferences(workflowData)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, otlp["attributes"])
			if tt.wantResource != nil {
				assert.Equal(t, tt.wantResource, otlp["resource-attributes"])
			}
		})
	}
}

func TestResolveOTLPMetadataReferencesNormalizesMergedStringMaps(t *testing.T) {
	workflowData := &WorkflowData{RawFrontmatter: map[string]any{
		"metadata": map[string]any{"version": "1.0", "category": "automation"},
		"observability": map[string]any{
			"otlp": map[string]any{
				"attributes": map[string]string{
					"catalogue.version": "${{ github.aw.metadata.version }}",
				},
				"resource-attributes": map[string]string{
					"catalogue.category": "${{ github.aw.metadata.category }}",
				},
			},
		},
	}}

	err := resolveOTLPMetadataReferences(workflowData)

	require.NoError(t, err)
	otlp := workflowData.RawFrontmatter["observability"].(map[string]any)["otlp"].(map[string]any)
	assert.Equal(t, map[string]any{"catalogue.version": "1.0"}, otlp["attributes"])
	assert.Equal(t, map[string]any{"catalogue.category": "automation"}, otlp["resource-attributes"])
}

func TestResolveOTLPMetadataReferencesRejectsUnsafeResourceAttribute(t *testing.T) {
	workflowData := &WorkflowData{RawFrontmatter: map[string]any{
		"metadata": map[string]any{"category": "${{ secrets.TOKEN }}"},
		"observability": map[string]any{
			"otlp": map[string]any{
				"resource-attributes": map[string]any{
					"catalogue.category": "${{ github.aw.metadata.category }}",
				},
			},
		},
	}}

	err := resolveOTLPMetadataReferences(workflowData)

	require.ErrorContains(t, err, "observability.otlp.resource-attributes.catalogue.category")
	require.ErrorContains(t, err, "contains GitHub Actions expression delimiters")
}
