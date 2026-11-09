package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompilerMarkdownMetadataOmitsEmptyProjections(t *testing.T) {
	agentSHA, modelSHA := strings.Repeat("a", 64), strings.Repeat("b", 64)
	referenceSHA, errorSHA := strings.Repeat("c", 64), strings.Repeat("d", 64)
	manifest := &GHAWManifest{MarkdownMetadata: &GHAWMarkdownMetadata{
		Version: 2,
		Records: []GHAWMarkdownMetadataRecord{
			{Kind: "artifact", SHA256: agentSHA, Value: json.RawMessage(`{}`)},
			{Kind: "model-pins", SHA256: modelSHA, Value: json.RawMessage(`{ }`)},
			{Kind: "artifact", SHA256: referenceSHA, Value: json.RawMessage(`null`), References: map[string]string{"": "/metadata/agent"}},
			{Kind: "artifact", SHA256: errorSHA, Value: json.RawMessage(`null`), Error: "invalid source"},
		},
	}}
	require.NoError(t, populateManifestMetadata(manifest))
	require.Empty(t, manifest.AgentMetadata)
	require.Empty(t, manifest.ModelPins)
	require.NotContains(t, manifest.AgentMetadata, agentSHA)
	require.NotContains(t, manifest.ModelPins, modelSHA)
	require.NotContains(t, manifest.AgentMetadata, referenceSHA[:20])
	require.NotContains(t, manifest.AgentMetadata, errorSHA[:20])
	require.Equal(t, map[string]string{"": "/metadata/agent"}, manifest.MetadataRefs["artifact:"+referenceSHA[:20]])
	require.Equal(t, "invalid source", manifest.MetadataErrors["artifact:"+errorSHA[:20]])
	encoded, err := manifest.ToJSON()
	require.NoError(t, err)
	require.NotContains(t, encoded, agentSHA)
	require.NotContains(t, encoded, modelSHA)
	require.NotContains(t, encoded, `"metadata_empty"`)
	require.NotContains(t, encoded, `"empty":true`)
	require.NotContains(t, encoded, `"`+referenceSHA[:20]+`":null`)
	require.NotContains(t, encoded, `"`+errorSHA[:20]+`":null`)
	require.Len(t, manifest.MarkdownMetadata.Records, 4)
}

func TestCompilerMarkdownMetadataRejectsInvalidEmptyApprovals(t *testing.T) {
	for _, source := range []struct{ kind, sha string }{
		{"artifact", "invalid"}, {"artifact", strings.Repeat("A", 64)},
		{"artifact", strings.Repeat("a", 62)}, {"unknown", strings.Repeat("a", 64)},
	} {
		manifest := &GHAWManifest{MarkdownMetadata: &GHAWMarkdownMetadata{
			Version: 2, Records: []GHAWMarkdownMetadataRecord{{Kind: source.kind, SHA256: source.sha, Value: json.RawMessage(`{}`)}},
		}}
		require.ErrorContains(t, populateManifestMetadata(manifest), "invalid Markdown metadata")
	}
}

func TestCompilerMarkdownMetadataExtendsCollidingHashPrefixes(t *testing.T) {
	prefix := strings.Repeat("a", 20)
	first, second := prefix+"b"+strings.Repeat("c", 43), prefix+"c"+strings.Repeat("b", 43)
	hashes, err := abbreviatedMetadataHashes([]GHAWMarkdownMetadataRecord{
		{SHA256: first}, {SHA256: second}, {SHA256: strings.Repeat("d", 64)},
	})
	require.NoError(t, err)
	require.Equal(t, first[:21], hashes[first])
	require.Equal(t, second[:21], hashes[second])
	require.Equal(t, strings.Repeat("d", 20), hashes[strings.Repeat("d", 64)])
}

func TestCompilerMarkdownMetadataReusesHeaderFields(t *testing.T) {
	manifest := NewGHAWManifest(nil, nil, nil, nil, "", []string{"local/reader"}, nil, nil)
	manifest.MarkdownMetadata = &GHAWMarkdownMetadata{
		Version: 2,
		Records: []GHAWMarkdownMetadataRecord{{
			SHA256: strings.Repeat("a", 64), Kind: "model-pins",
			Value: json.RawMessage(`{"engine":{"id":"copilot","model":"primary"},"skills":["local/reader"],"rate":0.25,"description":"Unique description"}`),
		}},
	}
	header := `# gh-aw-metadata: {"agent_id":"copilot","agent_model":"primary"}` + "\n"
	require.NoError(t, deduplicateMarkdownMetadata(header, manifest))
	record := manifest.MarkdownMetadata.Records[0]
	require.JSONEq(t, `{"engine":{},"rate":0.25,"description":"Unique description"}`, string(record.Value))
	require.Equal(t, map[string]string{
		"/engine/id":    "/metadata/agent_id",
		"/engine/model": "/metadata/agent_model",
		"/skills":       "/manifest/skills",
	}, record.References)
	encoded, err := manifest.ToJSON()
	require.NoError(t, err)
	require.NotContains(t, encoded, "value_json")
	require.Contains(t, encoded, `"model_pins":{"`+strings.Repeat("a", 20)+`":{`)
	require.NotContains(t, encoded, `"markdown_metadata"`)
	require.NotContains(t, encoded, `"sha256"`)
	require.NotContains(t, encoded, `"kind"`)
	require.NotContains(t, encoded, `\"engine\"`)
	require.Equal(t, 1, strings.Count(header+encoded, `"primary"`), "existing fields must occur once, not again in the catalog")
	require.Equal(t, 1, strings.Count(encoded, `"local/reader"`))
}

func TestCompilerMarkdownMetadataReferencePointers(t *testing.T) {
	index := map[string]string{}
	indexMarkdownMetadataValues(index, map[string]any{"a/b~c": []any{"model"}}, "/manifest")
	references := map[string]string{}
	value := deduplicateMarkdownMetadataValue(map[string]any{"x/y~z": []any{"model", "unique"}}, "", index, references)
	require.Equal(t, map[string]any{"x/y~z": []any{nil, "unique"}}, value)
	require.Equal(t, map[string]string{"/x~1y~0z/0": "/manifest/a~1b~0c/0"}, references)
	require.Equal(t, "/manifest/a~1b~0c", index[`["model"]`])
	require.Equal(t, "/manifest/", appendMetadataPointer("/manifest", ""))
	require.Equal(t, "/manifest/..", appendMetadataPointer("/manifest", ".."))
}

func TestCompilerMarkdownMetadataReferencesEveryJSONType(t *testing.T) {
	index := map[string]string{}
	indexMarkdownMetadataValues(index, map[string]any{
		"enabled": true, "disabled": false, "count": 1, "rate": 0.25,
		"empty": "", "object": map[string]any{}, "array": []any{}, "null": nil,
	}, "/metadata")
	references := map[string]string{}
	value := deduplicateMarkdownMetadataValue(map[string]any{
		"enabled": true, "disabled": false, "count": 1, "rate": 0.25,
		"empty": "", "object": map[string]any{}, "array": []any{}, "null": nil,
		"unique": "retained",
	}, "", index, references)
	require.Equal(t, map[string]any{"unique": "retained"}, value)
	require.Equal(t, map[string]string{
		"/enabled": "/metadata/enabled", "/disabled": "/metadata/disabled",
		"/count": "/metadata/count", "/rate": "/metadata/rate",
		"/empty": "/metadata/empty", "/object": "/metadata/object",
		"/array": "/metadata/array", "/null": "/metadata/null",
	}, references)
	rootReferences := map[string]string{}
	require.Nil(t, deduplicateMarkdownMetadataValue(nil, "", index, rootReferences))
	require.Equal(t, map[string]string{"": "/metadata/null"}, rootReferences)
}

func TestCompilerMarkdownMetadataNeverReferencesHeaderRoots(t *testing.T) {
	for _, value := range []any{map[string]any{}, map[string]any{"strict": true}} {
		index := map[string]string{}
		indexMarkdownMetadataValues(index, value, "/metadata")
		indexMarkdownMetadataValues(index, value, "/manifest")
		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		require.NotContains(t, index, string(encoded))
	}
}
