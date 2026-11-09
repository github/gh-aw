package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompilerMarkdownMetadataReusesHeaderFields(t *testing.T) {
	manifest := NewGHAWManifest(nil, nil, nil, nil, "", []string{"local/reader"}, nil, nil)
	manifest.MarkdownMetadata = &GHAWMarkdownMetadata{
		Version: 2,
		Records: []GHAWMarkdownMetadataRecord{{
			SHA256: "digest", Kind: "model-pins",
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
	require.Contains(t, encoded, `"model_pins":{"digest":{`)
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
