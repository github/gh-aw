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
		{"own token step allowed", pre + "    - id: mine-app-token-v2\n      uses: actions/create-github-app-token@v2\n    - run: echo ok\n      env:\n        T: ${{ steps.mine-app-token-v2.outputs.token }}\n", nil, "", nil},
		{"own bracket token step allowed", pre + "    - id: mine-app-token-release\n      uses: actions/create-github-app-token@v2\n    - run: echo ok\n      env:\n        T: ${{ steps['mine-app-token-release'].outputs.token }}\n", nil, "", nil},
		{"compiler indexed token", pre + "    - run: echo ${{ steps.checkout-safe-output-app-token-0.outputs.token }}\n", []string{"safe-outputs.steps[0].run"}, Error, []string{"CTR-035"}},
		{"status metadata allowed", pre + "    - if: steps.safe-outputs-app-token.outcome == 'success'\n      run: echo ${{ steps['safe-outputs-app-token']['conclusion'] }}\n", nil, "", nil},
		{"github env", pre + "    - run: echo A=1 >> \"$GITHUB_ENV\"\n", []string{"safe-outputs.steps[0].run"}, Error, []string{"CTR-036"}},
		{"github path braces", pre + "    - run: echo /x >> ${GITHUB_PATH}\n", []string{"safe-outputs.steps[0].run"}, Error, []string{"CTR-036"}},
		{"github context env", pre + "    - run: echo A=1 >> \"${{ github.env }}\"\n", []string{"safe-outputs.steps[0].run"}, Error, []string{"CTR-036"}},
		{"powershell env path", pre + "    - shell: pwsh\n      run: Add-Content $env:GITHUB_ENV 'A=1'\n", []string{"safe-outputs.steps[0].run"}, Error, []string{"CTR-036"}},
		{"read github env allowed", pre + "    - run: cat \"$GITHUB_ENV\"\n", nil, "", nil},
		{"write github env value elsewhere allowed", pre + "    - run: echo \"$GITHUB_ENV\" > \"$RUNNER_TEMP/out\"\n", nil, "", nil},
		{"core export in script", pre + "    - uses: actions/github-script@v8\n      with:\n        script: core.exportVariable('A', '1')\n", []string{"safe-outputs.steps[0].with.script"}, Error, []string{"CTR-036"}},
		{"checkout default persists", pre + "    - uses: actions/checkout@v5\n", []string{"safe-outputs.steps[0].uses"}, Error, []string{"CTR-037"}},
		{"checkout persist true", pre + "    - uses: actions/checkout@v5\n      with:\n        persist-credentials: true\n", []string{"safe-outputs.steps[0].with.persist-credentials"}, Error, []string{"CTR-037"}},
		{"checkout without credentials", pre + "    - uses: actions/checkout@v5\n      with:\n        persist-credentials: false\n        path: other\n", nil, "", nil},
		{"checkout case-insensitive false", pre + "    - uses: Actions/Checkout@v5\n      with:\n        persist-credentials: 'FALSE'\n", nil, "", nil},
		{"checkout expression false", pre + "    - uses: actions/checkout@v5\n      with:\n        persist-credentials: \"${{ false }}\"\n", nil, "", nil},
		{"rewrite agent output", pre + "    - run: echo '{}' > \"$GH_AW_AGENT_OUTPUT\"\n", []string{"safe-outputs.steps[0].run"}, Error, []string{"CTR-038"}},
		{"sed patch", pre + "    - run: sed -i 's/a/b/' /tmp/gh-aw/aw.patch\n", []string{"safe-outputs.steps[0].run"}, Error, []string{"CTR-038"}},
		{"remove inputs", pre + "    - run: rm -rf ${RUNNER_TEMP}/gh-aw/safeoutputs\n", []string{"safe-outputs.steps[0].run"}, Error, []string{"CTR-038"}},
		{"remove input directory", pre + "    - run: rm -rf /tmp/gh-aw\n", []string{"safe-outputs.steps[0].run"}, Error, []string{"CTR-038"}},
		{"remove quoted runner temp directory", pre + "    - run: rm -rf \"${RUNNER_TEMP}/gh-aw\"\n", []string{"safe-outputs.steps[0].run"}, Error, []string{"CTR-038"}},
		{"remove protected file after scratch operand", pre + "    - run: rm scratch /tmp/gh-aw/aw.patch\n", []string{"safe-outputs.steps[0].run"}, Error, []string{"CTR-038"}},
		{"similarly named scratch directory allowed", pre + "    - run: rm -rf /tmp/gh-aw-backup\n", nil, "", nil},
		{"copy over inputs", pre + "    - run: cp other.json /tmp/gh-aw/agent_output.json\n", []string{"safe-outputs.steps[0].run"}, Error, []string{"CTR-038"}},
		{"copy redirection noise", pre + "    - run: cp file /tmp/gh-aw/agent_output.json 2>/dev/null\n", []string{"safe-outputs.steps[0].run"}, Error, []string{"CTR-038"}},
		{"read inputs allowed", pre + "    - run: cat /tmp/gh-aw/agent_output.json | jq . > \"$RUNNER_TEMP/out.json\"\n", nil, "", nil},
		{"copy from inputs allowed", pre + "    - run: cp /tmp/gh-aw/agent_output.json ./out.json 2>&1\n", nil, "", nil},
		{"copy target option from input allowed", pre + "    - run: cp -t ./archive /tmp/gh-aw/agent_output.json\n", nil, "", nil},
		{"output file allowed", pre + "    - run: echo x=1 >> \"$GITHUB_OUTPUT\"\n", nil, "", nil},
		{"safe output pre and setup token IDs", "safe-outputs:\n  steps:\n    - run: echo ${{ steps.release-app-token.outputs.token }}\n  pre-steps:\n    - id: release-app-token\n      run: echo token\n  setup-steps:\n    - id: setup-app-token\n      run: echo token\njobs:\n  safe_outputs:\n    pre-steps:\n      - id: job-pre-app-token\n        run: echo token\n    setup-steps:\n      - id: job-setup-app-token\n        run: echo token\n", nil, "", nil},
		{"safe outputs jobs alias token ID", pre + "    - run: echo ${{ steps.job-setup-app-token.outputs.token }}\njobs:\n  safe-outputs:\n    setup-steps:\n      - id: job-setup-app-token\n        run: echo token\n", nil, "", nil},
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
