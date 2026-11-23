package lint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnvironmentFileInjection(t *testing.T) {
	for _, tt := range []struct {
		script string
		want   bool
	}{
		{`printf 'VALUE=%s\n' "$BODY" >> "$GITHUB_ENV"`, true},
		{`printf 'VALUE=%s\n' "$BODY" 2>> "$GITHUB_ENV" >&2`, true},
		{`printf 'VALUE=%s\n' "$BODY" 3>> "$GITHUB_ENV" 1>&3`, true},
		{`printf 'VALUE=%s\n' "$BODY" 3>> "$GITHUB_ENV" 1>&3-`, true},
		{`printf 'VALUE=%s\n' "$BODY" >& "$GITHUB_ENV"`, true},
		{`printf 'VALUE=%s\n' "$BODY" 2>> "$GITHUB_ENV"`, false},
		{`printf 'VALUE=%s\n' "$BODY" >&2 2>> "$GITHUB_ENV"`, false},
		{`printf 'VALUE=%s\n' "$BODY" >> "$GITHUB_ENV" > /dev/null`, false},
		{`echo "$BODY" >> "${GITHUB_ENV}"`, true},
		{`printf '%s\n' "$BODY" | tee -a "$GITHUB_ENV"`, true},
		{`printf '%s\n' "$BODY" >> "$GITHUB_PATH"`, true},
		{`printf '%s\n' "$BODY" > record; cat record >> "$GITHUB_ENV"`, true},
		{`{ echo 'VALUE<<EOF'; printf '%s\n' "$BODY"; echo EOF; } >> "$GITHUB_ENV"`, true},
		{`printf 'VALUE=fixed\n' >> "$GITHUB_ENV"`, false},
		{`echo /opt/trusted/bin >> "$GITHUB_PATH"`, false},
		{`printf '%s\n' "$BODY" >> '$GITHUB_ENV'`, false},
		{`printf '%s\n' "$BODY" | tee -a '$GITHUB_PATH'`, false},
		{`target='$GITHUB_ENV'; printf '%s\n' "$BODY" >> "$target"`, false},
		{`target="$GITHUB_ENV"; printf '%s\n' "$BODY" >> "$target"`, true},
		{`printf '%s\n' "$BODY" > '$GITHUB_ENV'; cat '$GITHUB_ENV' >> "$GITHUB_ENV"`, true},
		{`printf '%s\n' "$BODY"`, false},
		{`printf '%s\n' '${{ contains(github.event.comment.body, 'x') }}' >> "$GITHUB_ENV"`, false},
	} {
		t.Run(tt.script, func(t *testing.T) {
			source := contractJob + "    steps:\n      - shell: bash\n        env: {BODY: '${{ github.event.comment.body }}'}\n        run: |\n          " + tt.script + "\n"
			for _, target := range []ASTTargets{ActionsAST, AWAST} {
				diagnostics := integrityTestRun(t, EnvironmentFileInjectionRule(), ruleContractInput(t, source, target, false))
				found := false
				for _, diagnostic := range diagnostics {
					if diagnostic.Severity == Error {
						found = true
						assert.Equal(t, "jobs.check.steps[0].run", diagnostic.Path)
						assert.Equal(t, 1, diagnostic.Line)
						assert.NotContains(t, diagnostic.Message, tt.script)
					}
				}

				assert.Equal(t, tt.want, found)
			}
		})
	}
}

func TestEnvironmentFileInjectionShellCoverage(t *testing.T) {
	for _, tt := range []struct {
		name, script string
		warning      bool
		finding      bool
	}{
		{"inherited Node environment", `node -e 'require("fs").appendFileSync(process.env.GITHUB_ENV, "VALUE="+process.env.BODY)'`, true, false},
		{"inherited nodejs environment", `nodejs -e 'require("fs").appendFileSync(process.env.GITHUB_ENV, process.env.BODY)'`, true, false},
		{"cleared Node environment", `env -i node -e 'require("fs").appendFileSync(process.env.GITHUB_ENV, "VALUE=fixed")'`, false, false},
		{"nested stderr routing", `{ printf 'VALUE=%s\n' "$BODY" >&2; } 2>> "$GITHUB_ENV"`, true, false},
		{"persistent routing", `exec 2>> "$GITHUB_ENV"; printf 'VALUE=%s\n' "$BODY" >&2`, true, false},
		{"traced stderr routing", `set -x; printf '%s\n' "$BODY" 2>> "$GITHUB_ENV"`, true, false},
		{"parameter diagnostic stderr routing", `: "${BODY:?$BODY}" 2>> "$GITHUB_ENV"`, true, false},
		{"dynamic descriptor", `printf 'VALUE=%s\n' "$BODY" 1>&"$FD"`, true, false},
		{"prior finding survives unsupported routing", `printf 'VALUE=%s\n' "$BODY" >> "$GITHUB_ENV"; printf '%s\n' "$BODY" 1>&"$FD"`, true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := contractJob + "    steps:\n      - shell: bash\n        env: {BODY: '${{ github.event.comment.body }}'}\n        run: |\n          " + tt.script + "\n"
			for _, target := range []ASTTargets{ActionsAST, AWAST} {
				diagnostics := integrityTestRun(t, EnvironmentFileInjectionRule(), ruleContractInput(t, source, target, false))
				var warning, finding bool
				for _, diagnostic := range diagnostics {
					warning = warning || diagnostic.Severity == Warning && diagnostic.Path == "jobs.check.steps[0].run"
					finding = finding || diagnostic.Severity == Error
				}
				assert.Equal(t, tt.warning, warning, "%+v", diagnostics)
				assert.Equal(t, tt.finding, finding, "%+v", diagnostics)
			}
		})
	}
}
