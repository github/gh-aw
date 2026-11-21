package workflow

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/github/gh-aw/actions/setup"
	"github.com/github/gh-aw/pkg/constants"
)

const copilotSDKNodeTargetDir = constants.GhAwRootDirShell + "/engine-cli/copilot-sdk"
const copilotSDKNodeInstallCommand = `bash "${RUNNER_TEMP}/gh-aw/actions/install_copilot_sdk.sh"`

type copilotSDKNPMProject struct {
	variant string
	digest  string
	newest  string
}

// When refreshing a lock, update the newest publication timestamp across all
// locked versions, including optional platform packages.
var (
	copilotSDKNodeProject       = newCopilotSDKNPMProject("node", "2026-09-30T21:58:46Z", setup.CopilotSDKNodeManifest, setup.CopilotSDKNodeLock)
	copilotSDKTypeScriptProject = newCopilotSDKNPMProject("typescript", "2026-11-13T04:00:07Z", setup.CopilotSDKTypeScriptManifest, setup.CopilotSDKTypeScriptLock)
)

func newCopilotSDKNPMProject(variant, newest, manifest, lock string) *copilotSDKNPMProject {
	digest := sha256.Sum256([]byte(manifest + "\x00" + lock))
	return &copilotSDKNPMProject{
		variant: variant, digest: hex.EncodeToString(digest[:]), newest: newest,
	}
}

func (project *copilotSDKNPMProject) installCommand() string {
	return copilotSDKNodeInstallCommand + " " + project.variant + " " + project.digest + " " + project.newest
}
