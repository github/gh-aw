package workflow

import (
	_ "embed"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

const (
	typescriptLSPVersion = "4.3.3"
	typescriptLSPDir     = constants.GhAwRootDirShell + "/engine-cli/typescript-lsp"
	typescriptLSPBinDir  = typescriptLSPDir + "/node_modules/.bin"
)

//go:embed lsp/typescript/package.json
var typescriptLSPManifest string

//go:embed lsp/typescript/package-lock.json
var typescriptLSPLock string

func generateTypeScriptLSPInstallStep() GitHubActionStep {
	step := GitHubActionStep{
		"      - name: Install TypeScript LSP dependencies",
		"        shell: bash",
		"        run: |",
		"          set -euo pipefail",
		`          install_dir="` + typescriptLSPDir + `"`,
		`          mkdir -p "$install_dir"`,
	}
	for _, file := range []struct{ name, contents string }{
		{"package.json", typescriptLSPManifest},
		{"package-lock.json", typescriptLSPLock},
	} {
		script := `printf '%s\n' ` + shellEscapeArg(strings.TrimSuffix(file.contents, "\n")) + ` > "$install_dir/` + file.name + `"`
		for line := range strings.SplitSeq(script, "\n") {
			step = append(step, "          "+line)
		}
	}
	return append(step,
		// Keep the runner's registry and credentials; npm maps the canonical npmjs lock URLs to that registry.
		`          npm ci --prefix "$install_dir" --replace-registry-host=npmjs --no-audit --no-fund`,
		`          bin_dir="$install_dir/node_modules/.bin"`,
		`          if [[ "${RUNNER_OS:-}" == Windows ]]; then bin_dir="$(wslpath -w "$bin_dir")"; fi`,
		`          printf '%s\n' "$bin_dir" >> "$GITHUB_PATH"`,
	)
}

func prependTypeScriptLSPPath(data *WorkflowData, command string) string {
	// Restore the staged path inside sandbox and WSL shells, which may rebuild PATH.
	if _, ok := NewLSPManager(data.LSP).servers["typescript"]; ok {
		return `export PATH="` + typescriptLSPBinDir + `:$PATH" && ` + command
	}
	return command
}
