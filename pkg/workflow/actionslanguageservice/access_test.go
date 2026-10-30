package actionslanguageservice

import (
	"reflect"
	"strings"
	"testing"
)

func TestExpressionSourceRanges(t *testing.T) {
	source := "  format('héllo ''}}''', github['event'].issue.number) || (inputs.branch && !needs['build'].result) "
	tree, err := ParseSyntax(source)
	if err != nil {
		t.Fatal(err)
	}
	Walk(tree, func(node *Expression) bool {
		if node.Start < 0 || node.End > len(source) || node.Start >= node.End {
			t.Errorf("invalid %s source range: [%d,%d)", node.Kind, node.Start, node.End)
			return false
		}
		if node.Kind != "star" {
			// A dotted property token is an AST literal, not a standalone
			// quoted literal, so validate its spelling through Tokenize.
			if _, err := Tokenize(source[node.Start:node.End]); err != nil {
				t.Errorf("%s range %q cannot be tokenized: %v", node.Kind, source[node.Start:node.End], err)
			}
		}
		for _, child := range node.Children {
			if child.Start < node.Start || child.End > node.End {
				t.Errorf("%s child range escapes %s", child.Kind, node.Kind)
			}
		}
		return true
	})
}

func TestReferenceWalkingAndRewriting(t *testing.T) {
	source := "format('needs.build.outputs.version }}', needs['build'].outputs.version) || inputs[vars.branch]"
	tree, err := ParseSyntax(source)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"needs", "build", "outputs", "version"}, {"inputs"}, {"vars", "branch"}}
	if got := References(tree); !reflect.DeepEqual(got, want) {
		t.Fatalf("references = %#v; want %#v", got, want)
	}
	got := RewriteReferences(source, func(path []string) (string, bool) {
		if reflect.DeepEqual(path, want[0]) {
			return "steps.version.outputs.version", true
		}
		return "", false
	})
	wantSource := "format('needs.build.outputs.version }}', steps.version.outputs.version) || inputs[vars.branch]"
	if got != wantSource {
		t.Fatalf("rewritten source = %q; want %q", got, wantSource)
	}
	if got := RewriteReferences("'unterminated", func([]string) (string, bool) { t.Fatal("rewrote invalid source"); return "", false }); got != "'unterminated" {
		t.Fatal("invalid source was changed")
	}
}

func TestScannerPatterns(t *testing.T) {
	source := "before ${{ format('}}{0}', inputs.name) }}\nafter ${{ secrets['TOKEN'] }} ${{ missing"
	pattern := Pattern{}
	matches := pattern.FindAllStringSubmatch(source, -1)
	if len(matches) != 2 || matches[0][1] != " format('}}{0}', inputs.name) " {
		t.Fatalf("scanner matches = %#v", matches)
	}
	if got := pattern.ReplaceAllString(source, "x"); got != "before x\nafter x ${{ missing" {
		t.Fatalf("replacement = %q", got)
	}
	reference := ReferencePattern(false, false, func(path []string) []string {
		if len(path) == 2 && path[0] == "secrets" {
			return []string{path[1]}
		}
		return nil
	})
	if got := reference.FindStringSubmatch(source); len(got) != 2 || got[1] != "TOKEN" {
		t.Fatalf("reference captures = %#v", got)
	}
	decoded := ReferencePattern(false, false, func(path []string) []string {
		return []string{strings.Join(path, ".")}
	})
	if got := decoded.FindStringSubmatch("${{ secrets['TOKEN'] }}"); len(got) != 2 || got[1] != "secrets.TOKEN" {
		t.Fatalf("decoded captures = %#v", got)
	}
}

func TestSyntaxParsingDoesNotChangeSchemaContexts(t *testing.T) {
	if _, err := ParseSyntax("gateway.events && experiments.variant"); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseExpression("gateway.events", []string{"github"}); err == nil {
		t.Fatal("schema context restriction was bypassed")
	}
}
