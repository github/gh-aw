package workflow

import (
	"crypto/sha256"
	_ "embed"
	"fmt"

	"github.com/github/gh-aw/pkg/constants"
)

//go:embed data/copilot-sdk/node/package.json
var copilotSDKNodeManifest string

//go:embed data/copilot-sdk/node/package-lock.json
var copilotSDKNodeLock string

//go:embed data/copilot-sdk/typescript/package.json
var copilotSDKTypeScriptManifest string

//go:embed data/copilot-sdk/typescript/package-lock.json
var copilotSDKTypeScriptLock string

const copilotSDKNodeTargetDir = constants.GhAwRootDirShell + "/engine-cli/copilot-sdk"
const copilotSDKNodeInstallCommand = `bash "${RUNNER_TEMP}/gh-aw/actions/install_copilot_sdk.sh"`

type copilotSDKNPMProject struct {
	manifest string
	lock     string
	newest   string
	sdkSpec  string
	variant  string
}

// npm ci does not apply the release-age resolver to an already locked graph.
// When refreshing either lock, update its newest registry publication timestamp
// across all locked versions, including optional platform packages. Keep npmjs
// URLs portable for registry overrides and verify SHA-512 against approved feed
// tarballs when the registry metadata provides only SHA-1. The setup action's
// corresponding JSON assets must match these embedded files.
var (
	copilotSDKNodeProject = &copilotSDKNPMProject{
		manifest: copilotSDKNodeManifest, lock: copilotSDKNodeLock, variant: "node",
		newest: "2026-09-30T21:58:46Z", sdkSpec: "@github/copilot-sdk@" + string(constants.DefaultCopilotSDKVersion),
	}
	copilotSDKTypeScriptProject = &copilotSDKNPMProject{
		manifest: copilotSDKTypeScriptManifest, lock: copilotSDKTypeScriptLock, variant: "typescript",
		newest: "2026-11-13T04:00:07Z", sdkSpec: "@github/copilot-sdk@" + string(constants.DefaultCopilotSDKVersion),
	}
)

func (project *copilotSDKNPMProject) installLines() []string {
	return []string{
		fmt.Sprintf("%s %s %s %x %x", copilotSDKNodeInstallCommand, project.variant, project.newest,
			sha256.Sum256([]byte(project.manifest)), sha256.Sum256([]byte(project.lock))),
	}
}
