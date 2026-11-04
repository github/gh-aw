package taint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestJavascriptInputHasSources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input JavaScriptInput
		want  bool
	}{
		{
			name: "returns false when all fields are zero",
		},
		{
			name: "returns false for initialized empty source collections",
			input: JavaScriptInput{
				Stdin:    []Source{},
				Env:      map[string][]Source{},
				Literals: map[string][]Source{},
			},
		},
		{
			name: "returns false when every mapped source list is nil or empty",
			input: JavaScriptInput{
				Env: map[string][]Source{
					"UNSET": nil,
					"EMPTY": {},
				},
				Literals: map[string][]Source{
					"__GH_AW_TAINT_0__": nil,
					"__GH_AW_TAINT_1__": {},
				},
			},
		},
		{
			name: "detects stdin sources",
			input: JavaScriptInput{
				Stdin: []Source{
					{Reference: "secrets.token", Path: "stdin"},
					{Reference: "secrets.password", Path: "stdin"},
				},
			},
			want: true,
		},
		{
			name: "counts a zero valued stdin source",
			input: JavaScriptInput{
				Stdin: []Source{{}},
			},
			want: true,
		},
		{
			name: "detects environment sources alongside nil and empty lists",
			input: JavaScriptInput{
				Stdin: []Source{},
				Env: map[string][]Source{
					"UNSET": nil,
					"EMPTY": {},
					"TOKEN": {{Reference: "secrets.token", Path: "env.TOKEN"}},
				},
			},
			want: true,
		},
		{
			name: "counts a zero valued environment source under an empty key",
			input: JavaScriptInput{
				Env: map[string][]Source{"": {{}}},
			},
			want: true,
		},
		{
			name: "detects literal sources after empty stdin and environment lists",
			input: JavaScriptInput{
				Stdin: []Source{},
				Env: map[string][]Source{
					"UNSET": nil,
					"EMPTY": {},
				},
				Literals: map[string][]Source{
					"__GH_AW_TAINT_0__": nil,
					"__GH_AW_TAINT_1__": {},
					"__GH_AW_TAINT_2__": {{Reference: "secrets.token", Path: "jobs.build.steps[0].run"}},
				},
			},
			want: true,
		},
		{
			name: "counts a zero valued literal source under an empty key",
			input: JavaScriptInput{
				Literals: map[string][]Source{"": {{}}},
			},
			want: true,
		},
		{
			name: "detects environment sources with Unicode keys and metadata",
			input: JavaScriptInput{
				Env: map[string][]Source{
					"令牌🔐": {{Reference: "secrets.令牌", Path: "env.令牌🔐"}},
				},
			},
			want: true,
		},
		{
			name: "detects literal sources with Unicode keys and metadata",
			input: JavaScriptInput{
				Literals: map[string][]Source{
					"🔐令牌": {{Reference: "secrets.令牌", Path: "jobs.构建.steps[0].run"}},
				},
			},
			want: true,
		},
		{
			name: "ignores metadata and sources in arguments and files",
			input: JavaScriptInput{
				Name:          "构建.js",
				Source:        "console.log('令牌')",
				Args:          [][]Source{{{Reference: "secrets.argument", Path: "args[0]"}}},
				Files:         map[string][]Source{"秘密.txt": {{Reference: "secrets.file", Path: "files.秘密.txt"}}},
				CaptureStdout: true,
				ArgvOffset:    -1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, javascriptInputHasSources(tt.input))
		})
	}
}
