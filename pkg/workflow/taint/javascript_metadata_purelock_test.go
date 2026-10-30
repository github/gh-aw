package taint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestJsReplacementLines(t *testing.T) {
	t.Parallel()

	literal := func(text string, lines jsLineSafety) jsValue {
		return jsValue{kind: jsString, literal: true, text: text, lines: lines}
	}
	regexp := func(text string) jsValue {
		return jsValue{kind: jsRegExp, text: text}
	}
	safeReplacement := literal("✓", jsSingleLine)
	tests := []struct {
		name   string
		base   jsValue
		args   []jsValue
		method string
		want   jsLineSafety
	}{
		{name: "missing arguments preserve every base flag", base: jsValue{lines: jsSingleLine | jsTerminatedLine}, want: jsSingleLine | jsTerminatedLine},
		{name: "one argument preserves base flags", base: jsValue{lines: jsNoLF | jsTerminatedLine}, args: []jsValue{literal("\r", jsNoLF)}, method: "replaceAll", want: jsNoLF | jsTerminatedLine},
		{name: "zero base and replacement remain unknown", args: []jsValue{{}, {}}, method: "replaceAll"},
		{name: "dynamic replacement retains only common safety", base: jsValue{lines: jsSingleLine | jsTerminatedLine}, args: []jsValue{regexp(`/[\r\n]/g`), {kind: jsString, lines: jsNoLF | jsTerminatedLine}}, method: "replace", want: jsNoLF},
		{name: "nonstring replacement cannot prove removals", base: jsValue{lines: jsSingleLine}, args: []jsValue{regexp(`/[\r\n]/g`), {kind: jsNumber, literal: true, lines: jsNoCR}}, method: "replace", want: jsNoCR},
		{name: "dollar substitution cannot prove removals", args: []jsValue{regexp(`/[\r\n]/g`), literal("$&", jsSingleLine)}, method: "replace"},
		{name: "dollar substitution retains existing safety", base: jsValue{lines: jsSingleLine}, args: []jsValue{regexp(`/[\r\n]/g`), literal("値$$", jsNoLF)}, method: "replace", want: jsNoLF},
		{name: "replaceAll removes literal carriage returns", args: []jsValue{literal("\r", jsNoLF), safeReplacement}, method: "replaceAll", want: jsNoCR},
		{name: "replaceAll removes literal newlines", args: []jsValue{literal("\n", jsNoCR), safeReplacement}, method: "replaceAll", want: jsNoLF},
		{name: "literal removal combines with existing safety", base: jsValue{lines: jsNoLF}, args: []jsValue{literal("\r", jsNoLF), safeReplacement}, method: "replaceAll", want: jsSingleLine},
		{name: "replacement can reintroduce removed carriage returns", args: []jsValue{literal("\r", jsNoLF), literal("\r", jsNoLF)}, method: "replaceAll"},
		{name: "replacement can reintroduce removed newlines", args: []jsValue{literal("\n", jsNoCR), literal("\n", jsNoCR)}, method: "replaceAll"},
		{name: "literal CRLF does not prove individual removals", args: []jsValue{literal("\r\n", 0), safeReplacement}, method: "replaceAll"},
		{name: "empty literal pattern proves no removals", args: []jsValue{literal("", jsSingleLine), safeReplacement}, method: "replaceAll"},
		{name: "Unicode literal pattern retains common safety", base: jsValue{lines: jsSingleLine | jsTerminatedLine}, args: []jsValue{literal("世", jsSingleLine), safeReplacement}, method: "replaceAll", want: jsSingleLine},
		{name: "single literal replacement proves no removals", args: []jsValue{literal("\r", jsNoLF), safeReplacement}, method: "replace"},
		{name: "dynamic string pattern proves no removals", args: []jsValue{{kind: jsString, text: "\n"}, safeReplacement}, method: "replaceAll"},
		{name: "unknown pattern retains common safety", base: jsValue{lines: jsNoCR}, args: []jsValue{{kind: jsUnknown}, safeReplacement}, method: "replaceAll", want: jsNoCR},
		{name: "global regex removes carriage returns", args: []jsValue{regexp(`/\r/g`), safeReplacement}, method: "replace", want: jsNoCR},
		{name: "global regex removes newlines", args: []jsValue{regexp(`/\n/g`), safeReplacement}, method: "replaceAll", want: jsNoLF},
		{name: "global regex character class removes both separators", args: []jsValue{regexp(`/[\r\n]/g`), safeReplacement}, method: "replace", want: jsSingleLine},
		{name: "global regex alternation removes both separators", args: []jsValue{regexp(`/\r|\n/igm`), safeReplacement}, method: "replace", want: jsSingleLine},
		{name: "regex removal respects replacement safety", args: []jsValue{regexp(`/[\r\n]/g`), literal("\n", jsNoCR|jsTerminatedLine)}, method: "replace", want: jsNoCR},
		{name: "nonglobal regex cannot prove complete removal", args: []jsValue{regexp(`/[\r\n]/im`), safeReplacement}, method: "replace"},
		{name: "sticky global regex cannot prove complete removal", args: []jsValue{regexp(`/[\r\n]/gy`), safeReplacement}, method: "replace"},
		{name: "unrecognized global regex proves no removals", args: []jsValue{regexp(`/\s/g`), safeReplacement}, method: "replace"},
		{name: "regex without delimiter proves no removals", args: []jsValue{regexp(`\r`), safeReplacement}, method: "replace"},
		{name: "regex with only leading delimiter proves no removals", args: []jsValue{regexp(`/g`), safeReplacement}, method: "replace"},
		{name: "regex without flags proves no removals", args: []jsValue{regexp(`/\r/`), safeReplacement}, method: "replace"},
		{name: "unused arguments cannot change inferred safety", args: []jsValue{regexp(`/[\r\n]/g`), safeReplacement, literal("\r\n", 0)}, method: "replace", want: jsSingleLine},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			baseBefore := tt.base
			var argsBefore []jsValue
			if tt.args != nil {
				argsBefore = append([]jsValue{}, tt.args...)
			}

			got := jsReplacementLines(tt.base, tt.args, tt.method)

			assert.Equal(t, tt.want, got)
			assert.Equal(t, baseBefore, tt.base, "base must remain unchanged")
			assert.Equal(t, argsBefore, tt.args, "arguments must remain unchanged")
		})
	}
}
