package lint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSafeOutputCredentialsPolicy(t *testing.T) {
	const pre = "safe-outputs:\n  steps:\n"
	for _, tt := range []struct {
		name, source string
		paths        []string
		severity     Severity
		threats      []string
	}{
		{"minted token in env", pre + "    - run: gh api /user\n      env:\n        GH_TOKEN: ${{ steps.safe-outputs-app-token.outputs.token }}\n", []string{"safe-outputs.steps[0].env"}, Error, []string{"CTR-035"}},
		{"minted token bracket in with", pre + "    - uses: local/action\n      with:\n        token: \"${{ steps['create-issue-app-token'].outputs.token }}\"\n", []string{"safe-outputs.steps[0].with"}, Error, []string{"CTR-035"}},
		{"own token step allowed", pre + "    - id: mine-app-token\n      uses: actions/create-github-app-token@v2\n    - run: echo ok\n      env:\n        T: ${{ steps.mine-app-token.outputs.token }}\n", nil, "", nil},
		{"github env", pre + "    - run: echo A=1 >> \"$GITHUB_ENV\"\n", []string{"safe-outputs.steps[0].run"}, Error, []string{"CTR-036"}},
		{"github path braces", pre + "    - run: echo /x >> ${GITHUB_PATH}\n", []string{"safe-outputs.steps[0].run"}, Error, []string{"CTR-036"}},
		{"core export in script", pre + "    - uses: actions/github-script@v8\n      with:\n        script: core.exportVariable('A', '1')\n", []string{"safe-outputs.steps[0].with.script"}, Error, []string{"CTR-036"}},
		{"checkout default persists", pre + "    - uses: actions/checkout@v5\n", []string{"safe-outputs.steps[0].uses"}, Error, []string{"CTR-037"}},
		{"checkout persist true", pre + "    - uses: actions/checkout@v5\n      with:\n        persist-credentials: true\n", []string{"safe-outputs.steps[0].with.persist-credentials"}, Error, []string{"CTR-037"}},
		{"checkout without credentials", pre + "    - uses: actions/checkout@v5\n      with:\n        persist-credentials: false\n        path: other\n", nil, "", nil},
		{"rewrite agent output", pre + "    - run: echo '{}' > \"$GH_AW_AGENT_OUTPUT\"\n", []string{"safe-outputs.steps[0].run"}, Error, []string{"CTR-038"}},
		{"sed patch", pre + "    - run: sed -i 's/a/b/' /tmp/gh-aw/aw.patch\n", []string{"safe-outputs.steps[0].run"}, Error, []string{"CTR-038"}},
		{"remove inputs", pre + "    - run: rm -rf ${RUNNER_TEMP}/gh-aw/safeoutputs\n", []string{"safe-outputs.steps[0].run"}, Error, []string{"CTR-038"}},
		{"copy over inputs", pre + "    - run: cp other.json /tmp/gh-aw/agent_output.json\n", []string{"safe-outputs.steps[0].run"}, Error, []string{"CTR-038"}},
		{"read inputs allowed", pre + "    - run: cat /tmp/gh-aw/agent_output.json | jq . > \"$RUNNER_TEMP/out.json\"\n", nil, "", nil},
		{"copy from inputs allowed", pre + "    - run: cp /tmp/gh-aw/agent_output.json ./out.json 2>&1\n", nil, "", nil},
		{"output file allowed", pre + "    - run: echo x=1 >> \"$GITHUB_OUTPUT\"\n", nil, "", nil},
		{"nonstrict warning", "strict: false\n" + pre + "    - run: echo A=1 >> $GITHUB_ENV\n", []string{"safe-outputs.steps[0].run"}, Warning, []string{"CTR-036"}},
		{"main steps unaffected", "steps:\n  - run: echo A=1 >> $GITHUB_ENV\n", nil, "", nil},
		{"multiple findings", pre + "    - run: echo A=1 >> $GITHUB_ENV\n      env:\n        T: ${{ steps.safe-outputs-app-token.outputs.token }}\n    - uses: actions/checkout@v5\n    - run: rm /tmp/gh-aw/aw.patch\n", []string{"safe-outputs.steps[0].env", "safe-outputs.steps[0].run", "safe-outputs.steps[1].uses", "safe-outputs.steps[2].run"}, Error, []string{"CTR-035", "CTR-036", "CTR-037", "CTR-038"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			diagnostics := runThreatPolicy(t, "safe-output-credentials", tt.source)
			var paths, threats []string
			for _, diagnostic := range diagnostics {
				paths = append(paths, diagnostic.Path)
				threats = append(threats, diagnostic.ThreatRuleID)
				assert.Equal(t, tt.severity, diagnostic.Severity)
			}
			assert.Equal(t, tt.paths, paths)
			assert.Equal(t, tt.threats, threats)
		})
	}
}
