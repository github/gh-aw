package gradershell

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInterpreterOptionEvaluatesCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		interpreter string
		option      string
		want        bool
	}{
		{name: "python -c evaluates code", interpreter: "python", option: "-c", want: true},
		{name: "python --command evaluates code", interpreter: "python", option: "--command", want: true},
		{name: "python -m evaluates code", interpreter: "python", option: "-m", want: true},
		{name: "python --module evaluates code", interpreter: "python", option: "--module", want: true},
		{name: "python2 evaluates code", interpreter: "python2", option: "-c", want: true},
		{name: "python3 evaluates code", interpreter: "python3", option: "-c", want: true},
		{name: "pypy evaluates code", interpreter: "pypy", option: "-c", want: true},
		{name: "pypy3 evaluates code", interpreter: "pypy3", option: "-c", want: true},
		{name: "ruby -e evaluates code", interpreter: "ruby", option: "-e", want: true},
		{name: "ruby --eval evaluates code", interpreter: "ruby", option: "--eval", want: true},
		{name: "perl -e evaluates code", interpreter: "perl", option: "-e", want: true},
		{name: "perl -E evaluates code", interpreter: "perl", option: "-E", want: true},
		{name: "php -r evaluates code", interpreter: "php", option: "-r", want: true},
		{name: "node -e evaluates code", interpreter: "node", option: "-e", want: true},
		{name: "node --eval evaluates code", interpreter: "node", option: "--eval", want: true},
		{name: "node -p evaluates code", interpreter: "node", option: "-p", want: true},
		{name: "node --print evaluates code", interpreter: "node", option: "--print", want: true},
		{name: "bun evaluates code", interpreter: "bun", option: "-e", want: true},
		{name: "deno eval evaluates code", interpreter: "deno", option: "eval", want: true},
		{name: "deno --eval evaluates code", interpreter: "deno", option: "--eval", want: true},
		{name: "lua evaluates code", interpreter: "lua", option: "-e", want: true},
		{name: "qjs evaluates code", interpreter: "qjs", option: "--eval", want: true},
		{name: "js evaluates code", interpreter: "js", option: "-e", want: true},
		{name: "R -e evaluates code", interpreter: "R", option: "-e", want: true},
		{name: "Rscript --expression evaluates code", interpreter: "Rscript", option: "--expression", want: true},
		{name: "python rejects a non-evaluating option", interpreter: "python", option: "--unrelated", want: false},
		{name: "ruby rejects a non-evaluating option", interpreter: "ruby", option: "--unrelated", want: false},
		{name: "perl rejects a non-evaluating option", interpreter: "perl", option: "--unrelated", want: false},
		{name: "php rejects a non-evaluating option", interpreter: "php", option: "--unrelated", want: false},
		{name: "node rejects a non-evaluating option", interpreter: "node", option: "--unrelated", want: false},
		{name: "deno rejects a non-evaluating option", interpreter: "deno", option: "--unrelated", want: false},
		{name: "lua rejects a non-evaluating option", interpreter: "lua", option: "--unrelated", want: false},
		{name: "R rejects a non-evaluating option", interpreter: "R", option: "--unrelated", want: false},
		{name: "unknown interpreter is rejected", interpreter: "unknown", option: "-e", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, interpreterOptionEvaluatesCode(tt.interpreter, tt.option))
		})
	}
}
