package actionslanguageservice

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func boundaryCases() []conformanceCase {
	context := []string{"github", "env", "inputs", "needs", "matrix", "secrets", "vars", "steps",
		"always(0,0)", "failure(0,MAX)", "success(0,MAX)", "cancelled(0,0)", "hashFiles(1,255)"}
	var cases []conformanceCase
	add := func(kind, name, source string) {
		cases = append(cases, conformanceCase{Name: "boundaries/" + name, Kind: kind, Source: source, Contexts: context})
	}
	for i, source := range []string{
		"", " ", "true", "false", "null", "TRUE", "NaN", "Infinity", "-Infinity", "+Infinity",
		"0x10", "0b10", "0o10", "-0x10", "0x1p2", "1_000", "01", ".5", "1.", "+1", "-0", "1e9999", "1e-9999",
		"0x", "0b", "0o", ".", "+", "-", "-NaN", "1foo", "nil", "github.true", "github.false", "github.null",
		"'a''b''c'", "'a''''b'", "'a\\'b'", "'a\\b'", "'}}'", "'${{'", "'('", "'😊'", "''", "\"a\"", "`a`",
		"github.*.name", "github[*].name", "github['a''b''c']", "(github).ref", "('a')[0]", "'a'[0]",
		"contains('a', 'b',)", "contains()", "format('a')", "join('a')", "case(true, 'a', 'b')", "case(true, 'a')",
		"hashFiles('**')", "always()", "success('job')", "failure('job')", "github.ref ==\u00a0'foo'",
		"true && false || true", "true || false && false", "true == false < 1", "!!true", "true ? 1 : 2",
		"true + false", "true & false", "true | false", "true = false", "github.ref.", "github[", "github[0",
	} {
		add("expression", fmt.Sprintf("expression-%d", i), source)
		add("expression", fmt.Sprintf("group-%d", i), "("+source+")")
		add("expression", fmt.Sprintf("logical-%d", i), "true && "+source+" || false")
	}
	for depth := range 56 {
		add("expression", fmt.Sprintf("group-depth-%d", depth), strings.Repeat("(", depth)+"true"+strings.Repeat(")", depth))
		add("expression", fmt.Sprintf("unary-depth-%d", depth), strings.Repeat("!", depth)+"true")
		add("expression", fmt.Sprintf("index-depth-%d", depth), "github"+strings.Repeat(".ref", depth))
	}
	for _, length := range []int{20999, 21000, 21001} {
		add("expression", fmt.Sprintf("length-%d", length), "'"+strings.Repeat("x", length-2)+"'")
		add("expression", fmt.Sprintf("unicode-length-%d", length), "'"+strings.Repeat("😊", (length-2)/2)+"'")
	}
	base := "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: echo hi}]\n"
	for i, field := range []string{
		"name: ${{ 'literal' }}", "name: ${{ github.ref }}", "run-name: ${{ github.ref }}",
		"env: {FOO: '${{ secrets.FOO }}'}", "env: {FOO: '${{ env.FOO }}'}",
		"env: {FOO: '${{ unknown.FOO }}'}", "env: {FOO: '${{ ''}}'' }}'}",
		"env: {$'{{ insert }}': {FOO: bar}}", "env: {'${{ insert }}': {FOO: bar}}",
		"env: {FOO: '${{ ''a''''b''''c'' }}'}", "env: {FOO: '${{ 1 }}-${{ 2 }}'}",
		"env: {FOO: '${{ }}'}", "env: {FOO: '${{ true '}", "env: {FOO: '${{ true }}'}",
		"env: {FOO: !!bool xyz}", "env: {FOO: !!int nope}", "env: {FOO: !!float nope}",
		"env: {FOO: !!timestamp 2020-01-01}", "env: {FOO: 2020-01-01}",
		"env: {FOO: TRUE}", "env: {FOO: 0x10}", "env: {FOO: null}",
		"env: {FOO: true, foo: false}", "env: {FOO: 'x'}#bad comment",
		"env: {FOO: 'x'} # good comment", "env: &env {FOO: x}\nconcurrency: *env",
		"concurrency: ${{ secrets.FOO }}", "concurrency: ${{ github.ref }}",
		"env: {\"ééé\": 'first\n  second'}", "env: {\"ééé\": 'first\nsecond'}",
		"env: {\"😊😊😊\": 'value'} # comment", "env: {\"😊😊😊\": 'value'}#comment",
	} {
		add("workflow", fmt.Sprintf("workflow-%d", i), field+"\n"+base)
	}
	return append(cases, flowScalarQuoteCases()...)
}

func flowScalarQuoteCases() []conformanceCase {
	var cases []conformanceCase
	for _, test := range []struct {
		name     string
		field    string
		accepted bool
	}{
		{"plain-apostrophe", "env: {DESCRIPTION: Why it's needed}", true},
		{"plain-double-quote", `env: {DESCRIPTION: Say "hello}`, true},
		{"plain-colon-apostrophe", "env: {DESCRIPTION: Why:it's needed}", true},
		{"plain-colon-double-quote", `env: {DESCRIPTION: Say:"hello}`, true},
		{"plain-then-quoted", `env: {FIRST: it's fine, SECOND: 'quoted'}`, true},
		{"quoted-then-plain", `env: {FIRST: "quoted", SECOND: it's fine}`, true},
		{"quoted-key-and-value", `env: {"DESCRIPTION":'quoted'}`, true},
		{"multiline-quoted", "env: {DESCRIPTION: 'first\n  second'}", true},
		{"unindented-multiline-quoted", "env: {DESCRIPTION: 'first\nsecond'}", false},
		{"quoted-key-unindented-multiline-value", "env: {\"DESCRIPTION\":'first\nsecond'}", false},
		{"plain-unseparated-comment", "env: {DESCRIPTION: Why it's needed}#comment", false},
	} {
		cases = append(cases, conformanceCase{
			Name:     "boundaries/flow-quotes/" + test.name,
			Kind:     "workflow",
			Source:   test.field + "\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps: [{run: echo it's}]\n",
			Accepted: test.accepted,
		})
	}
	return cases
}

func TestWorkflowFlowScalarQuotes(t *testing.T) {
	for _, test := range flowScalarQuoteCases() {
		t.Run(test.Name, func(t *testing.T) {
			_, err := ParseWorkflow(test.Name, test.Source)
			require.Equal(t, test.Accepted, err == nil, "native parser error: %v", err)
		})
	}
}
