package workflow

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/github/gh-aw/actions/setup"
	"github.com/github/gh-aw/pkg/constants"
)

const (
	typescriptLSPDir    = constants.GhAwRootDirShell + "/engine-cli/typescript-lsp"
	typescriptLSPBinDir = typescriptLSPDir + "/node_modules/.bin"
)

var typescriptLSPVersion, typescriptLSPManifestSHA256, typescriptLSPLockSHA256, typescriptLSPPinsErr = loadTypeScriptLSPPins()

func loadTypeScriptLSPPins() (version, manifestSHA256, lockSHA256 string, err error) {
	manifest, err := setup.TypeScriptLSPProject.ReadFile("typescript-lsp/package.json")
	if err != nil {
		return "", "", "", err
	}
	lock, err := setup.TypeScriptLSPProject.ReadFile("typescript-lsp/package-lock.json")
	if err != nil {
		return "", "", "", err
	}
	var project struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(manifest, &project); err != nil {
		return "", "", "", err
	}
	version = project.Dependencies["typescript-language-server"]
	if version == "" {
		return "", "", "", errors.New("embedded TypeScript LSP manifest is missing the typescript-language-server version")
	}
	return version, fmt.Sprintf("%x", sha256.Sum256(manifest)), fmt.Sprintf("%x", sha256.Sum256(lock)), nil
}

func generateTypeScriptLSPInstallStep() GitHubActionStep {
	return GitHubActionStep{
		"      - name: Install TypeScript LSP dependencies",
		"        shell: bash",
		"        run: |",
		fmt.Sprintf(`          bash "%s/install_typescript_lsp.sh" %s %s`, SetupActionDestinationShell, typescriptLSPManifestSHA256, typescriptLSPLockSHA256),
	}
}

func prependTypeScriptLSPPath(data *WorkflowData, command string) string {
	// Restore the staged path inside sandbox and WSL shells, which may rebuild PATH.
	if _, ok := NewLSPManager(data.LSP).servers["typescript"]; ok {
		return `export PATH="` + typescriptLSPBinDir + `:$PATH" && ` + command
	}
	return command
}
