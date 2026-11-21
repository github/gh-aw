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
}

var (
	copilotSDKNodeProject       = newCopilotSDKNPMProject("node", setup.CopilotSDKNodeManifest, setup.CopilotSDKNodeLock)
	copilotSDKTypeScriptProject = newCopilotSDKNPMProject("typescript", setup.CopilotSDKTypeScriptManifest, setup.CopilotSDKTypeScriptLock)
)

func newCopilotSDKNPMProject(variant, manifest, lock string) *copilotSDKNPMProject {
	digest := sha256.Sum256([]byte(manifest + "\x00" + lock))
	return &copilotSDKNPMProject{variant: variant, digest: hex.EncodeToString(digest[:])}
}

func (project *copilotSDKNPMProject) installCommand() string {
	return copilotSDKNodeInstallCommand + " " + project.variant + " " + project.digest
}
