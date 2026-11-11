package connectors

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStructuredBodyAndTextEncoding(t *testing.T) {
	inputs := map[string]Input{"title": {Type: "string", Required: true, MaxLength: 80}, "text": {Type: "string", Required: true, MaxLength: 4000}}
	values := map[string]Value{"fields": {Object: map[string]Value{
		"summary":      {Input: "title"},
		"description":  {Input: "text", Encoding: "jira-adf"},
		"labels":       {Array: []Value{{Literal: "reviewed"}}},
		"empty-array":  {Array: []Value{}},
		"empty-object": {Object: map[string]Value{}},
	}}}
	for _, value := range values {
		if err := validateValue(value, inputs); err != nil {
			t.Fatal(err)
		}
	}
	// Freeze/restore must preserve intentional empty arrays and objects.
	encoded, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	var restored map[string]Value
	if err := Decode(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	body, _, err := requestBody(Request{Body: restored}, map[string]any{"title": "test", "text": "<script>\n& data"})
	if err != nil {
		t.Fatal(err)
	}
	var document any
	if err := decodeJSON(body, &document); err != nil {
		t.Fatal(err)
	}
	for pointer, expected := range map[string]any{"/fields/description/type": "doc", "/fields/description/content/0/content/0/text": "<script>", "/fields/description/content/0/content/1/type": "hardBreak"} {
		if actual, ok := lookupPointer(document, pointer); !ok || actual != expected {
			t.Fatalf("unexpected encoded body at %s: %v", pointer, actual)
		}
	}
	if !strings.Contains(string(body), `"empty-array":[]`) || !strings.Contains(string(body), `"empty-object":{}`) {
		t.Fatal("structured empty values were not preserved", string(body))
	}
	storage, err := encodeText("confluence-storage", "<script>&\ntext")
	if err != nil || storage != "<p>&lt;script&gt;&amp;<br />text</p>" {
		t.Fatal("untrusted text was not escaped into storage markup", storage, err)
	}
	if _, err := encodeText("unsupported", "text"); err == nil {
		t.Fatal("unknown text encoder accepted")
	}
}

func TestStructuredValuesRejectInvalidAndUnbounded(t *testing.T) {
	inputs := map[string]Input{"text": {Type: "string", Required: true, MaxLength: 65536}}
	for _, value := range []Value{{}, {Input: "text", Literal: "ambiguous"}, {Input: "missing"}, {Literal: "x", Encoding: "jira-adf"}, {Input: "text", Encoding: "shell"}} {
		if err := validateValue(value, inputs); err == nil {
			t.Fatal("invalid structured value accepted", value)
		}
	}
	deep := Value{Literal: "leaf"}
	for range 18 {
		deep = Value{Object: map[string]Value{"child": deep}}
	}
	if err := validateValue(deep, inputs); err == nil {
		t.Fatal("excessive structure depth accepted")
	}
	if _, _, err := requestBody(Request{Body: map[string]Value{"a": {Input: "text"}, "b": {Input: "text"}, "c": {Input: "text"}, "d": {Input: "text"}}}, map[string]any{"text": strings.Repeat("x", 65536)}); err == nil {
		t.Fatal("resolved body byte limit not enforced")
	}
}

func TestJSONPatchAndFixedGraphQLVariables(t *testing.T) {
	patch := Request{Patch: []Patch{{Op: "test", Path: "/rev", Value: Value{Literal: 4}}, {Op: "add", Path: "/fields/System.Title", Value: Value{Input: "title"}}}}
	body, contentType, err := requestBody(patch, map[string]any{"title": `data " ; ${{ not-executed }}`})
	if err != nil || contentType != "application/json-patch+json" || !strings.Contains(string(body), `"path":"/fields/System.Title"`) {
		t.Fatal("JSON Patch serialization failed", string(body), contentType, err)
	}
	graphql := Request{GraphQL: "mutation Comment($input: CommentInput!) { commentCreate(input: $input) { success } }", Query: map[string]Value{"input": {Object: map[string]Value{"body": {Input: "text"}, "issueId": {Input: "issue"}}}}}
	body, _, err = requestBody(graphql, map[string]any{"text": "safe data", "issue": "ISSUE-1"})
	if err != nil || !strings.Contains(string(body), `"variables":{"input":{"body":"safe data","issueId":"ISSUE-1"}}`) {
		t.Fatal("GraphQL variables were not structured data", string(body), err)
	}
	plan := fixturePlan(t, "https://api.example.com")
	op := Operation{Request: Request{GraphQL: graphql.GraphQL}}
	if _, err := plan.validateResponse(op, []byte(`{"data":{"commentCreate":{"success":false}},"errors":[{"message":"provider failure"}]}`), nil); err == nil {
		t.Fatal("HTTP200 GraphQL errors accepted")
	}
	op.ResponseChecks = map[string]any{"/data/commentCreate/success": true}
	if _, err := plan.validateResponse(op, []byte(`{"data":{"commentCreate":{"success":false}}}`), nil); err == nil {
		t.Fatal("unsuccessful mutation accepted")
	}
}
