package main

import (
	"strings"
	"testing"
)

func TestEnsureNamePreservesSource(t *testing.T) {
	for _, test := range []struct{ source, expected string }{
		{"---\nmodel: small # keep\n---\nBody", "---\nname: \"reader\"\nmodel: small # keep\n---\nBody"},
		{"---\n{model: small}\n---\nBody", "---\n{name: \"reader\", model: small}\n---\nBody"},
		{"---\n{}\n---\nBody", "---\n{name: \"reader\"}\n---\nBody"},
		{"---\nname: existing\n---\nBody", "---\nname: existing\n---\nBody"},
		{"Body", "---\nname: \"reader\"\n---\nBody"},
	} {
		result, err := process(request{Operation: "ensure-name", Content: test.source, Value: "reader"})
		if err != nil || result.Content != test.expected {
			t.Errorf("ensure-name: got %q, %v; want %q", result.Content, err, test.expected)
		}
	}
}

func TestMetadataScalarPositionsAndChomping(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		source := strings.ReplaceAll("---\ndescription: café 😀\nmodel: \"small\" # keep\n---\nBody", "\n", newline)
		result, err := process(request{Operation: "agent", Content: source})
		if err != nil {
			t.Fatal(err)
		}
		model := result.Scalars["model"]
		wantStart := utf16Length(source[:strings.Index(source, "\"small\"")])
		if model.Value != "small" || model.Start != wantStart || model.End != wantStart+7 {
			t.Errorf("invalid UTF-16 scalar span: %+v, want start %d", model, wantStart)
		}
	}
	for _, test := range []struct{ indicator, expected string }{
		{"|", "Read files\n"},
		{"|-", "Read files"},
		{"|+", "Read files\n"},
		{">", "Read files\n"},
	} {
		result, err := process(request{Operation: "agent", Content: "---\ndescription: " + test.indicator + "\n  Read files\n---\nBody"})
		if err != nil || result.Scalars["description"].Value != test.expected || result.Value.(map[string]any)["description"] != test.expected {
			t.Errorf("chomping %s: got %+v, %v; want %q", test.indicator, result, err, test.expected)
		}
	}
}

func TestMetadataRejectsAmbiguityAndExpansion(t *testing.T) {
	for _, source := range []string{
		"model: one\nmodel: two",
		"model: [",
		"---\nmodel: one\n---\nmodel: two",
		"model: &alias [one]\nother: *alias",
		"model: !!str small",
		"model: " + strings.Repeat("[", 102) + "small" + strings.Repeat("]", 102),
	} {
		if _, err := process(request{Operation: "document", Content: source}); err == nil {
			t.Errorf("accepted invalid or unbounded metadata %q", source)
		}
	}
	if _, err := process(request{Operation: "document", Content: "model: one\nmodel: two", AllowDuplicates: true}); err != nil {
		t.Errorf("explicit duplicate-key compatibility: %v", err)
	}
	if _, err := process(request{Operation: "unknown", Content: "Body"}); err == nil {
		t.Error("accepted unknown operation")
	}
}

func TestProtectedLiteralsCannotBecomeKeys(t *testing.T) {
	replacements := map[string]string{"_VALUE_": "true\nmodel: attacker"}
	result, err := process(request{Operation: "restore", Content: "---\ndescription: _VALUE_\n---\nBody", Replacements: replacements})
	if err != nil || result.Content != "---\ndescription: \"true\\nmodel: attacker\"\n---\nBody" {
		t.Fatalf("literal restoration: %+v, %v", result, err)
	}
	if _, err := process(request{Operation: "restore", Content: "---\n_VALUE_: text\n---\n", Replacements: replacements}); err == nil {
		t.Error("accepted evaluated metadata key")
	}
	marker := "\x00VALUE_ID\x00"
	result, err = process(request{Operation: "ensure-name", Content: "---\nmodel: " + marker + "\n---\nBody", Value: "reader", Replacements: map[string]string{marker: marker}})
	if err != nil || !strings.Contains(result.Content, "model: "+marker) {
		t.Fatalf("lost protected marker: %+v, %v", result, err)
	}
}
