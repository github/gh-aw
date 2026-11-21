package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/github/gh-aw/actions/setup"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/jsonutil"
)

const copilotSDKNodeTargetDir = constants.GhAwRootDirShell + "/engine-cli/copilot-sdk"
const copilotSDKNodeInstallCommand = `bash "${RUNNER_TEMP}/gh-aw/actions/install_copilot_sdk.sh"`

type copilotSDKNPMProject struct {
	variant string
	digest  string
	newest  string
	err     error
}

var (
	copilotSDKNodeProject       = loadCopilotSDKNPMProject("node", setup.CopilotSDKNodeManifest, setup.CopilotSDKNodeLock, setup.CopilotSDKNodeCooldown)
	copilotSDKTypeScriptProject = loadCopilotSDKNPMProject("typescript", setup.CopilotSDKTypeScriptManifest, setup.CopilotSDKTypeScriptLock, setup.CopilotSDKTypeScriptCooldown)
)

func loadCopilotSDKNPMProject(variant, manifest, lock, cooldown string) *copilotSDKNPMProject {
	project, err := newCopilotSDKNPMProject(variant, manifest, lock, cooldown)
	if err != nil {
		return &copilotSDKNPMProject{variant: variant, err: err}
	}
	return project
}

func newCopilotSDKNPMProject(variant, manifest, lock, cooldown string) (*copilotSDKNPMProject, error) {
	digest := sha256.Sum256([]byte(manifest + "\x00" + lock))
	var metadata struct {
		Digest string `json:"digest"`
		Newest string `json:"newest"`
	}
	if err := jsonutil.UnmarshalString(cooldown, &metadata); err != nil {
		return nil, fmt.Errorf("invalid %s Copilot SDK cooldown metadata: %w", variant, err)
	}
	if metadata.Digest != hex.EncodeToString(digest[:]) {
		return nil, fmt.Errorf("stale %s Copilot SDK cooldown metadata: run node scripts/refresh-copilot-sdk-npm-metadata.mjs", variant)
	}
	published, err := time.Parse(time.RFC3339, metadata.Newest)
	if err != nil {
		return nil, fmt.Errorf("invalid %s Copilot SDK publication time: %w", variant, err)
	}
	return &copilotSDKNPMProject{
		variant: variant, digest: metadata.Digest, newest: published.UTC().Format(time.RFC3339Nano),
	}, nil
}

func (project *copilotSDKNPMProject) installCommand() string {
	if project.err != nil {
		return `echo "::error::Invalid embedded Copilot SDK cooldown metadata; rebuild gh-aw with refreshed metadata" >&2; exit 1`
	}
	return copilotSDKNodeInstallCommand + " " + project.variant + " " + project.digest + " " + project.newest
}

func validateCopilotSDKCooldownMetadata(data *WorkflowData) error {
	if project := selectCopilotSDKInstallSpec(data).npm; project != nil {
		return project.err
	}
	return nil
}
