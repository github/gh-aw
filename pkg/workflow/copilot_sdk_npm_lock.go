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
const copilotSDKNodeInstallCommand = `bash "` + constants.GhAwRootDirShell + `/actions/install_copilot_sdk.sh"`

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
		copilotSDKNodeInstallCommand + ` '` + project.newest + `' <<'GH_AW_SDK_PROJECT'`,
		`{"manifest":` + project.manifest + `,"lock":` + project.lock + `}`,
		`GH_AW_SDK_PROJECT`,
	}
}
