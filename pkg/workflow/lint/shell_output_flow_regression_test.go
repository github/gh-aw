package lint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTemplateInjectionShellRunnerTargets(t *testing.T) {
	for _, tt := range []struct {
		name, producer, expression string
		finding                    bool
	}{
		{"stderr file duplication", `printf 'VALUE=%s\n' "$BODY" 2>> "$GITHUB_OUTPUT" >&2`, "steps.producer.outputs.VALUE", true},
		{"extra descriptor", `printf 'VALUE=%s\n' "$BODY" 3>> "$GITHUB_OUTPUT" >&3`, "steps.producer.outputs.VALUE", true},
		{"reversed duplication", `printf 'VALUE=%s\n' "$BODY" >&2 2>> "$GITHUB_OUTPUT"`, "steps.producer.outputs.VALUE", false},
		{"literal output file", `printf 'VALUE=%s\n' "$BODY" >> '$GITHUB_OUTPUT'`, "steps.producer.outputs.VALUE", false},
		{"literal output alias", `target='$GITHUB_OUTPUT'; printf 'VALUE=%s\n' "$BODY" >> "$target"`, "steps.producer.outputs.VALUE", false},
		{"expanded output alias", `target="$GITHUB_OUTPUT"; printf 'VALUE=%s\n' "$BODY" >> "$target"`, "steps.producer.outputs.VALUE", true},
		{"literal environment file", `printf 'VALUE=%s\n' "$BODY" >> '$GITHUB_ENV'`, "env.VALUE", false},
		{"literal environment alias", `target='$GITHUB_ENV'; printf 'VALUE=%s\n' "$BODY" >> "$target"`, "env.VALUE", false},
		{"expanded environment file", `printf 'VALUE=%s\n' "$BODY" >> "$GITHUB_ENV"`, "env.VALUE", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Ensure the output-flow pass runs even for the environment-file cases.
			producer := `printf 'unused=fixed\n' >> "$GITHUB_OUTPUT"` + "\n" + tt.producer
			source := contractJob + "    steps:\n      - id: producer\n        shell: bash\n        working-directory: subdir\n        env: {BODY: '${{ github.event.comment.body }}'}\n        run: |\n          " +
				strings.ReplaceAll(producer, "\n", "\n          ") + "\n      - run: |\n          echo '${{ " + tt.expression + " }}'\n"
			for _, target := range []ASTTargets{ActionsAST, AWAST} {
				diagnostics := integrityTestRun(t, TemplateInjectionRule(), ruleContractInput(t, source, target, false))
				found := false
				for _, diagnostic := range diagnostics {
					if diagnostic.Severity == Error {
						found = true
						assert.Equal(t, "jobs.check.steps[1].run", diagnostic.Path, "%+v", diagnostics)
					}
				}
				assert.Equal(t, tt.finding, found, "%+v", diagnostics)
			}
		})
	}
}
