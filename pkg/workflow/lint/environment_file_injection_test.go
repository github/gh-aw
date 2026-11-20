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
		{`echo "$BODY" >> "${GITHUB_ENV}"`, true},
		{`printf '%s\n' "$BODY" | tee -a "$GITHUB_ENV"`, true},
		{`printf '%s\n' "$BODY" >> "$GITHUB_PATH"`, true},
		{`printf '%s\n' "$BODY" > record; cat record >> "$GITHUB_ENV"`, true},
		{`{ echo 'VALUE<<EOF'; printf '%s\n' "$BODY"; echo EOF; } >> "$GITHUB_ENV"`, true},
		{`printf 'VALUE=fixed\n' >> "$GITHUB_ENV"`, false},
		{`echo /opt/trusted/bin >> "$GITHUB_PATH"`, false},
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
