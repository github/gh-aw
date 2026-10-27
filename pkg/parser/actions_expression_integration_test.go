package parser

import (
	"reflect"
	"testing"
)

func TestImportInputActionsSyntax(t *testing.T) {
	inputs := map[string]any{"value": "resolved", "empty": "", "nested": map[string]any{"key": "nested"}}
	tests := []struct {
		expression string
		want       string
	}{
		{`${{ github['aw']['import-inputs']['value'] }}`, "resolved"},
		{`${{ (github.aw.import-inputs.value) }}`, "resolved"},
		{`${{ github.aw.import-inputs.nested['key'] }}`, "nested"},
		{`${{ github.aw.import-inputs.empty || 'a }} || b' }}`, "a }} || b"},
		{`${{ github.aw.import-inputs.empty || (github.aw.import-inputs.value || 'unused') }}`, "resolved"},
		{`${{ (github.aw.import-inputs.empty || '') || 'fallback' }}`, "fallback"},
		{`${{ github.aw.import-inputs.empty || ('x' || 'y') }}`, "x"},
		{`${{ github.aw.import-inputs.empty || (false && 'unsafe') }}`, ""},
		{`${{ 'github.aw.import-inputs.value' || 'literal' }}`, ""},
		{`${{ github.aw.import-inputs.empty || 'bad' 'literal' }}`, ""},
		{`${{ github.aw.import-inputs.empty || "invalid" }}`, ""},
		{`${{ github.aw.import-inputs.value || (github.event.name || 'runtime') }}`, ""},
		{`${{ github.aw.import-inputs.value || 'unclosed }}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.expression, func(t *testing.T) {
			want := tt.want
			if want == "" {
				want = tt.expression
			}
			if got := substituteImportInputsInContent(tt.expression, inputs); got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
}

func TestHashTemplateActionsSyntax(t *testing.T) {
	tests := []struct {
		text string
		want []string
	}{
		{`${{ env['KEY'] }}`, nil},
		{`${{ ENV['KEY'] }}`, nil},
		{`${{ env[github.event.name] }}`, nil},
		{`${{ toJSON(env) }}`, nil},
		{`${{ format('}} {0}', vars.KEY) }}`, []string{`${{ format('}} {0}', vars.KEY) }}`}},
		{`${{ format('env.KEY and vars.KEY') }}`, []string{`${{ format('env.KEY and vars.KEY') }}`}},
		{`${{ github.event.env.KEY }}`, []string{`${{ github.event.env.KEY }}`}},
		{`${{ 'env.KEY' }}`, []string{`${{ 'env.KEY' }}`}},
		{`${{ env.KEY || }}`, []string{`${{ env.KEY || }}`}},
		{`${{ }}`, nil},
		{`${{ env['KEY' }}`, nil},
		{`${{ env.KEY`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			if got := extractRelevantTemplateExpressions(tt.text); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %#v, want %#v", got, tt.want)
			}
		})
	}
	text := `${{ format('a }} b', github.actor) }}`
	if got := extractAllTemplateExpressions(text); !reflect.DeepEqual(got, []string{text}) {
		t.Fatalf("quoted braces changed extraction: %#v", got)
	}
}

func TestSimpleSecretActionsSyntax(t *testing.T) {
	for _, value := range []string{`${{ secrets.TOKEN }}`, `${{ secrets['TOKEN'] }}`} {
		if !IsSimpleSecretExpression(value) {
			t.Errorf("valid direct secret rejected: %s", value)
		}
	}
	for _, value := range []string{
		`${{ 'secrets.TOKEN' }}`, `${{ secrets['lowercase'] }}`, `${{ secrets['123'] }}`,
		`${{ secrets['TOKEN-WITH-DASH'] }}`, `${{ secrets['TOKEN'] || 'fallback' }}`,
		`${{ secrets["TOKEN"] }}`, `${{ secrets.TOKEN || }}`, `${{ secrets[env.NAME] }}`,
	} {
		if IsSimpleSecretExpression(value) {
			t.Errorf("unsafe secret accepted: %s", value)
		}
	}
}
