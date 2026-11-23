//go:build !integration

package lint

import (
	"context"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoteScriptExecution(t *testing.T) {
	const url = "https://example.com/private-installer.sh"
	for _, tc := range []struct {
		name, script, shell string
		want                int
	}{
		{"curl pipe bash", "curl -fsSL " + url + " | bash", "", 1},
		{"wget pipe sh", "wget -qO- " + url + " | sh", "bash", 1},
		{"wget separate stdout flag", "wget -q -O - " + url + " | sh -s -- --yes", "", 1},
		{"sudo wrapper", "curl -sSL " + url + " | sudo -E bash -", "", 1},
		{"pipe through filter", "curl -sSL " + url + " | tr -d '\\r' | bash", "", 1},
		{"pipe all", "curl -sSL " + url + " |& /bin/bash", "", 1},
		{"python stdin", "curl -sSL " + url + " | python3 -", "", 1},
		{"process substitution", "bash <(curl -fsSL " + url + ")", "", 1},
		{"source process substitution", "source <(curl -fsSL " + url + ")", "", 1},
		{"command substitution", `/bin/sh -c "$(curl -fsSL ` + url + `)"`, "", 1},
		{"command substitution cluster", `bash -ec "$(wget -qO- ` + url + `)"`, "", 1},
		{"eval", `eval "$(curl -fsSL ` + url + `)"`, "", 1},
		{"here string", `bash <<< "$(curl -fsSL ` + url + `)"`, "", 1},
		{"stdin redirect", "bash < <(curl -fsSL " + url + ")", "", 1},
		{"curl stdout dash", "curl -fsSLo - " + url + " | bash", "", 1},
		{"sh shell", "curl -fsSL " + url + " | sh", "sh -e {0}", 1},
		{"download to file then verify", "curl -fsSLo install.sh " + url + "\nsha256sum -c install.sh.sha256\nbash install.sh", "", 0},
		{"remote name", "curl -fsSLO " + url + " && bash private-installer.sh", "", 0},
		{"wget default file", "wget " + url + " && sh private-installer.sh", "", 0},
		{"redirect to file", "curl -fsSL " + url + " > install.sh | bash", "", 0},
		{"pipe to jq", "curl -fsSL " + url + " | jq -r .version", "", 0},
		{"pipe to tee", "curl -fsSL " + url + " | tee install.sh", "", 0},
		{"python inline data", `curl -fsSL ` + url + ` | python3 -c 'import json,sys; print(json.load(sys.stdin))'`, "", 0},
		{"bash inline literal", "curl -fsSL " + url + ` | bash -c 'wc -l'`, "", 0},
		{"bash script reads stdin data", "curl -fsSL " + url + " | bash ./parse.sh", "", 0},
		{"data substitution", `VERSION="$(curl -fsSL ` + url + `)"`, "", 0},
		{"quoted text", `echo 'curl -fsSL ` + url + ` | bash'`, "", 0},
		{"comment", "# curl -fsSL " + url + " | bash", "", 0},
		{"pwsh", "curl -fsSL " + url + " | bash", "pwsh", 0},
		{"python shell", "curl -fsSL " + url + " | bash", "python", 0},
		{"invalid syntax", "if", "", 0},
		{"absolute custom shell", "curl -fsSL " + url + " | bash", "/bin/bash -e {0}", 1},
		{"curl next transfer to stdout", "curl -o saved.txt https://example.com/data --next " + url + " | bash", "", 1},
		{"curl short next transfer to stdout", "curl -o saved.txt https://example.com/data -: " + url + " | bash", "", 1},
		{"curl all transfers to files", "curl -o saved.txt https://example.com/data --next -o install.sh " + url + " | bash", "", 0},
		{"curl attached method", "curl -XPOST " + url + " | bash", "", 1},
		{"curl attached header", "curl -HOrigin:x " + url + " | bash", "", 1},
		{"curl attached user agent", "curl -sAOops " + url + " | bash", "", 1},
		{"wget attached user agent", "wget -UFOO -qO- " + url + " | sh", "", 1},
		{"wget no verbose", "wget -nv -O- " + url + " | sh", "", 1},
		{"stdin redirect overrides pipe", "curl -fsSL " + url + " | bash < verified-local.sh", "", 0},
		{"intervening stdin redirect", "curl -fsSL " + url + " | tr -d x < local.sh | bash", "", 0},
		{"intervening stdout redirect", "curl -fsSL " + url + " | tee copy > saved.txt | bash", "", 0},
		{"stdin redirect from download", "curl -fsSL https://example.com/data | bash < <(curl -fsSL " + url + ")", "", 1},
		{"dynamic interpreter", "curl -fsSL " + url + ` | "$TOOL"`, "", 0},
		{"numeric command", "curl -fsSL " + url + " | 3", "", 0},
		{"node attached eval", "curl -fsSL " + url + " | node --eval='console.log(1)'", "", 0},
		{"node attached print", "curl -fsSL " + url + " | node --print='1'", "", 0},
		{"node attached eval substitution", `node --eval="$(curl -fsSL ` + url + `)"`, "", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields := map[string]any{"run": tc.script}
			if tc.shell != "" {
				fields["shell"] = tc.shell
			}
			workflow := &actions.Workflow{Jobs: []*actions.Job{{
				ID: "check", RunsOn: "ubuntu-latest",
				Steps: []actions.Step{actions.RawStep{Fields: fields}},
			}}}
			diagnostics := integrityTestRun(t, RemoteScriptExecutionRule(), Input{Workflow: workflow})
			require.Len(t, diagnostics, tc.want)
			for _, diagnostic := range diagnostics {
				assert.Equal(t, Warning, diagnostic.Severity)
				assert.Equal(t, "jobs.check.steps[0].run", diagnostic.Path)
				assert.Positive(t, diagnostic.Line)
				assert.NotContains(t, diagnostic.Message, "private-installer")
				assert.Contains(t, diagnostic.Message, "checksum or signature")
			}
		})
	}
}

func TestRemoteScriptExecutionLineAndCancellation(t *testing.T) {
	workflow := &actions.Workflow{Jobs: []*actions.Job{{
		ID: "check", RunsOn: "ubuntu-latest",
		Steps: []actions.Step{actions.RawStep{Fields: map[string]any{
			"run": "echo '${{ github.event.issue.title }}'\nset -e\ncurl -fsSL https://example.com/x.sh | bash",
		}}},
	}}}
	diagnostics := integrityTestRun(t, RemoteScriptExecutionRule(), Input{Workflow: workflow})
	require.Len(t, diagnostics, 1)
	assert.Equal(t, 3, diagnostics[0].Line)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := checkRemoteScriptExecution(ctx, Input{Workflow: workflow})
	require.ErrorIs(t, err, context.Canceled)
}
