package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"

	"github.com/github/gh-aw/actions/setup"
	"github.com/github/gh-aw/pkg/constants"
)

const copilotSDKNodeTargetDir = constants.GhAwRootDirShell + "/engine-cli/copilot-sdk"
const copilotSDKNodeInstallCommand = `bash "${RUNNER_TEMP}/gh-aw/actions/install_copilot_sdk.sh"`

type copilotSDKNPMProject struct {
	variant string
	digest  string
	version string
}

type copilotSDKNPMProjectPin struct {
	Version string
	SHA256  string
}

var copilotSDKPinVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
var copilotSDKPinSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (pin *copilotSDKNPMProjectPin) valid() bool {
	return copilotSDKPinVersionPattern.MatchString(pin.Version) && copilotSDKPinSHA256Pattern.MatchString(pin.SHA256)
}

func validateCopilotSDKPin(data *WorkflowData) error {
	if data == nil || data.EngineConfig == nil || data.EngineConfig.CopilotSDKPin == nil {
		return nil
	}
	if !data.EngineConfig.CopilotSDKPin.valid() {
		return errors.New("engine.copilot-sdk requires an exact stable version and a lowercase 64-character sha256")
	}
	if ResolveEngineID(data) != "copilot" || selectDefaultCopilotSDKInstallSpec(data).npm == nil {
		return errors.New("engine.copilot-sdk version and sha256 are supported only for npm-backed Copilot SDK drivers")
	}
	if data.CheckoutDisabled {
		return errors.New("engine.copilot-sdk version and sha256 require a checkout containing .github/aw/copilot-sdk")
	}
	return nil
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
	command := copilotSDKNodeInstallCommand + " " + project.variant + " " + project.digest
	if project.version != "" {
		command += " " + project.version
	}
	return command
}
