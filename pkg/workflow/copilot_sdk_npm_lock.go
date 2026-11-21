package workflow

import (
	"bytes"
	"embed"
	"encoding/json"

	"github.com/github/gh-aw/pkg/constants"
)

//go:embed data/copilot-sdk/*/package*.json
var copilotSDKNPMLocks embed.FS

const copilotSDKNodeTargetDir = constants.GhAwRootDirShell + "/engine-cli/copilot-sdk"
const copilotSDKNodeInstallCommand = `npm ci --prefix "` + copilotSDKNodeTargetDir + `" --ignore-scripts --no-audit --no-fund`

type copilotSDKNPMProject struct {
	manifest string
	lock     string
	newest   string
	sdkSpec  string
}

// npm ci does not apply the release-age resolver to an already locked graph.
// When refreshing either lock, update its newest registry publication timestamp
// across all locked versions, including optional platform packages. Keep npmjs
// URLs portable for registry overrides and verify SHA-512 against approved feed
// tarballs when the registry metadata provides only SHA-1.
var (
	copilotSDKNodeProject       = loadCopilotSDKNPMProject("node", "2026-09-30T21:58:46Z")
	copilotSDKTypeScriptProject = loadCopilotSDKNPMProject("typescript", "2026-11-13T04:00:07Z")
)

func loadCopilotSDKNPMProject(variant, newest string) *copilotSDKNPMProject {
	read := func(name string) string {
		content, err := copilotSDKNPMLocks.ReadFile("data/copilot-sdk/" + variant + "/" + name)
		if err != nil {
			panic(err)
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, content); err != nil {
			panic(err)
		}
		return compact.String()
	}
	manifest := read("package.json")
	var parsed struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(manifest), &parsed); err != nil {
		panic(err)
	}
	const name = "@github/copilot-sdk"
	return &copilotSDKNPMProject{
		manifest: manifest, lock: read("package-lock.json"), newest: newest,
		sdkSpec: name + "@" + parsed.Dependencies[name],
	}
}

func (project *copilotSDKNPMProject) installLines() []string {
	return []string{
		`set -euo pipefail`,
		`node -e 'const days = Number(process.env.NPM_CONFIG_MIN_RELEASE_AGE); const published = Date.parse(process.argv[1]); if (!Number.isInteger(days) || days < 0 || !Number.isFinite(published) || (days > 0 && Date.now() - published < days * 86400000)) { console.error("Locked Copilot SDK dependencies do not satisfy engine.cooldown"); process.exit(1); }' '` + project.newest + `'`,
		`mkdir -p "` + copilotSDKNodeTargetDir + `"`,
		`cd "` + copilotSDKNodeTargetDir + `"`,
		`if [[ -e npm-shrinkwrap.json || -L npm-shrinkwrap.json ]]; then echo "Unexpected npm-shrinkwrap.json in the Copilot SDK install directory" >&2; exit 1; fi`,
		`if [[ -L package.json || -L package-lock.json ]]; then echo "Unexpected symlink in the Copilot SDK install directory" >&2; exit 1; fi`,
		`cat > package.json <<'GH_AW_SDK_PACKAGE'`,
		project.manifest,
		`GH_AW_SDK_PACKAGE`,
		`cat > package-lock.json <<'GH_AW_SDK_LOCK'`,
		project.lock,
		`GH_AW_SDK_LOCK`,
		copilotSDKNodeInstallCommand,
	}
}
