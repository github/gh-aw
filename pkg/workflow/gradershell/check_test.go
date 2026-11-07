package gradershell

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"mvdan.cc/sh/v3/syntax"
)

func TestHasShellCodeEvaluationInputRedirections(t *testing.T) {
	for _, test := range []struct {
		source string
		want   bool
	}{
		{`< input`, false},
		{`value=1 < input`, false},
		{`printf data | < input`, false},
		{"<<'EOF'\ndata\nEOF", false},
		{`bash < input`, true},
		{`printf data | bash`, true},
	} {
		t.Run(test.source, func(t *testing.T) {
			tree, err := syntax.NewParser().Parse(strings.NewReader(test.source), "")
			require.NoError(t, err)
			assert.Equal(t, test.want, HasShellCodeEvaluation(tree, nil))
		})
	}
}

func TestGraderShellFileWrites(t *testing.T) {
	for _, test := range []struct {
		name, source string
	}{
		{"overwrite", `printf 1 > output`},
		{"append", `echo 1 >> output`},
		{"clobber", `echo 1 >| output`},
		{"all streams", `echo 1 &> output`},
		{"append all streams", `echo 1 &>> output`},
		{"read write", `exec 3<> output`},
		{"exec fd", `exec 3> output`},
		{"exec named fd", `exec {fd}> output`},
		{"stderr", `echo 1 2> output`},
		{"legacy all streams", `echo 1 >& output`},
		{"dynamic destination", `echo 1 > "$OUTPUT"`},
		{"function", `unused() { echo 1 > output; }`},
		{"command substitution", `value=$(touch output)`},
		{"backticks", "value=`touch output`"},
		{"process substitution", `cat <(touch output)`},
		{"process sink body", `printf 1 > >(tee output)`},
		{"branch", `if false; then mkdir output; fi`},
		{"loop", `for item in 1; do rm output; done`},
		{"case", `case x in x) mv a b;; esac`},
		{"tee", `printf 1 | tee output`},
		{"tee append", `printf 1 | tee -a output`},
		{"tee variable", `printf 1 | tee "$OUTPUT"`},
		{"tee dash filename", `printf 1 | tee -- -output`},
		{"copy", `cp input output`},
		{"install", `install input output`},
		{"truncate", `truncate -s 0 output`},
		{"dd output", `dd if=input of=output`},
		{"dd variable", `dd "of=$OUTPUT"`},
		{"dd split operands", `dd if=$OPTIONS`},
		{"sed in place", `sed -i 's/a/b/' input`},
		{"sed backup", `sed --in-place=.bak 's/a/b/' input`},
		{"sed grouped", `sed -ni.bak 'p' input`},
		{"perl in place", `perl -pi -e 's/a/b/' input`},
		{"perl backup", `perl -i.bak -pe 's/a/b/' input`},
		{"quoted command", `'touch' output`},
		{"concatenated command", `t"ou"'ch' output`},
		{"escaped command", `to\uch output`},
		{"absolute command", `/bin/cp input output`},
		{"env wrapper", `env -i KEY=value touch output`},
		{"command wrapper", `command -p touch output`},
		{"exec wrapper", `exec -a test cp input output`},
		{"sudo wrapper", `sudo -n -u nobody touch output`},
		{"nice wrapper", `nice -n 5 touch output`},
		{"busybox wrapper", `busybox touch output`},
		{"static shell", `bash -c 'printf 1 > output'`},
		{"static shell grouped", `/bin/bash --noprofile --norc -lc 'touch output'`},
		{"nested static shells", `sh -c "bash -c 'touch output'"`},
		{"sort output", `sort --output=output input`},
		{"curl output", `curl -o output https://example.com`},
		{"curl headers", `curl --dump-header output https://example.com`},
		{"wget default", `wget https://example.com`},
		{"tar extract", `tar -xf input`},
		{"tar create", `tar cf output input`},
		{"find delete", `find . -type f -delete`},
		{"find execute", `find . -exec touch '{}' \;`},
		{"find print file", `find . -fprint output`},
	} {
		t.Run(test.name, func(t *testing.T) {
			findings, err := Check(t.Context(), "#!/bin/bash\n"+test.source+"\n")
			require.NoError(t, err)
			require.NotEmpty(t, findings)
			for _, finding := range findings {
				assert.GreaterOrEqual(t, finding.Line, 2)
				assert.Contains(t, []string{writeRedirection, writeCommand, writeTee, writeInPlace}, finding.Message)
				assert.NotContains(t, finding.Message, "output")
			}
		})
	}
}

func TestGraderShellReadOnly(t *testing.T) {
	for _, source := range []string{
		`jq -r '.events | length' "$GH_AW_SESSION"`,
		`cat "$GH_AW_SESSION" | jq '.events[] | select(.type == "tool_call")'`,
		`printf '%s\n' '{"value": 1, "message": "touch output >file"}'`,
		`echo 'tee output; sed -i; perl -pi; rm output; >file'`,
		"# touch output\n# echo > output\nprintf 1",
		`jq '.events[] | select(.message == ">")' "$GH_AW_SESSION"`,
		`grep -c 'touch output' "$GH_AW_SESSION"`,
		`cat < "$GH_AW_SESSION"`,
		`jq '.' "$GH_AW_SESSION" 2>/dev/null`,
		`printf 1 >/dev/stdout 2>/dev/stderr`,
		`printf 1 >/dev/fd/1 2>/proc/self/fd/2`,
		`printf 1 2>&1`,
		`printf 1 1>&2`,
		`exec 3>&1; printf 1 >&3; exec 3>&-`,
		`exec 3<>/dev/null`,
		`exec {fd}>&1`,
		`printf 1 &>/dev/null`,
		`printf 1 | tee`,
		`printf 1 | tee -a /dev/null /dev/stderr`,
		`printf 1 | tee >(jq '.')`,
		`printf 1 > >(cat)`,
		`dd if="$GH_AW_SESSION"`,
		`dd if=input of=/dev/null`,
		`sed -n -e 's/touch/x/p' "$GH_AW_SESSION"`,
		`sed -e '-i' input`,
		`sed --expression=-i input`,
		`sed -- -i input`,
		`perl -ne 'print if /touch|sed -i/' "$GH_AW_SESSION"`,
		`perl -e 'print "-i"'`,
		`perl -e-i`,
		`read -r value < "$GH_AW_SESSION"; printf '%s\n' "$value"`,
		"cat <<'EOF'\ntouch output\nEOF",
		`bash -c 'jq "." "$1"' -- "$GH_AW_SESSION"`,
		`command -v touch`,
		`env KEY=value jq '.' "$GH_AW_SESSION"`,
		`sort input`,
		`curl https://example.com`,
		`curl -o /dev/null --dump-header /dev/stderr https://example.com`,
		`curl --output=- https://example.com`,
		`sort -o /dev/stdout input`,
		`tar -tf archive`,
		`tar -tfarchive`,
		`tar tf archive`,
		`tar --file archive --list`,
		`find . -type f -print`,
		`find . -exec grep -c event '{}' \;`,
	} {
		t.Run(source, func(t *testing.T) {
			findings, err := Check(t.Context(), "#!/bin/bash\n"+source+"\n")
			require.NoError(t, err)
			assert.Empty(t, findings)
		})
	}
}

func TestGraderShellUncheckedExecution(t *testing.T) {
	for _, source := range []string{
		`"$COMMAND" output`,
		`touc? output`,
		`bash -c "$SCRIPT"`,
		`bash other.sh`,
		`./other.sh`,
		`bash`,
		`bash --rcfile file -c 'printf 1'`,
		`source other.sh`,
		`. other.sh`,
		`eval "$SCRIPT"`,
		`printf '%s' "$SCRIPT" | bash`,
		`xargs touch`,
		`env -S 'touch output'`,
		`touch_cmd=$'touch'; "$touch_cmd" output`,
	} {
		t.Run(source, func(t *testing.T) {
			findings, err := Check(t.Context(), source)
			require.NoError(t, err)
			require.NotEmpty(t, findings)
			assert.Equal(t, uncheckedShell, findings[0].Message)
		})
	}
	source := "touch output"
	for range 9 {
		source = "bash -c '" + strings.ReplaceAll(source, "'", "'\\''") + "'"
	}
	findings, err := Check(t.Context(), source)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, uncheckedShell, findings[0].Message)
}

func TestGraderShellErrorsAndLines(t *testing.T) {
	for _, source := range []string{"if PRIVATE", "bash -c 'if PRIVATE'"} {
		_, err := Check(t.Context(), source)
		require.ErrorContains(t, err, "invalid Bash syntax")
		assert.NotContains(t, err.Error(), "PRIVATE")
	}
	_, err := Check(t.Context(), strings.Repeat("#", maxSourceBytes+1))
	require.ErrorContains(t, err, "64 KiB")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = Check(ctx, "printf 1")
	require.ErrorIs(t, err, context.Canceled)
	findings, err := Check(t.Context(), "#!/bin/bash\nprintf 1\nif true; then\n  touch PRIVATE\nfi\n")
	require.NoError(t, err)
	require.Equal(t, []Finding{{Line: 4, Message: writeCommand}}, findings)
	first, err := Check(t.Context(), "rm a; rm b\n")
	require.NoError(t, err)
	assert.Equal(t, []Finding{{Line: 1, Message: writeCommand}}, first)
}

func BenchmarkGraderShellCheck(b *testing.B) {
	for _, source := range []string{
		"#!/bin/bash\njq '.events | length' \"$GH_AW_SESSION\"\n",
		"#!/bin/bash\nif false; then touch output; fi\nbash -c 'tee output'\n",
	} {
		b.Run(source, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Check(b.Context(), source); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
