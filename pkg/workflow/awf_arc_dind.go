// This file contains ARC/DinD path rewriting, chroot patch, and image digest helpers.

package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var arcDindPathLog = logger.New("workflow:awf_arc_dind")

func rewriteArcDindPath(path string) string {
	return strings.ReplaceAll(path, constants.TmpGhAwDir, awfArcDindRootPathExpr)
}

func rewriteArcDindEngineCommand(command string) string {
	arcDindPathLog.Print("Rewriting engine command for arc-dind topology")
	rewritten := rewriteArcDindPath(command)
	return fmt.Sprintf("export HOME=%s\nexport GH_AW_HARNESS_TELEMETRY_DIR=\"%s/sandbox/agent\"\n%s", awfArcDindHomePathExpr, awfArcDindRootPathExpr, rewritten)
}

// buildAWFImageTagWithDigests returns an image tag value for AWF's --image-tag flag.
// When known firewall container digests are available, it appends AWF's digest
// metadata format:
//
//	<tag>,squid=sha256:...,agent=sha256:...,api-proxy=sha256:...,cli-proxy=sha256:...
//
// For arc-dind topology, build-tools is also included:
//
//	<tag>,squid=sha256:...,agent=sha256:...,api-proxy=sha256:...,cli-proxy=sha256:...,build-tools=sha256:...
//
// This keeps AWF sidecar configuration aligned with digest-pinned pre-download images.
func buildAWFImageTagWithDigests(imageTag string, workflowData *WorkflowData) string {
	if imageTag == "" {
		return imageTag
	}

	type digestSpec struct {
		name  string
		image string
	}
	specs := []digestSpec{
		{name: "squid", image: constants.DefaultFirewallRegistry + "/squid:" + imageTag},
		{name: "agent", image: constants.DefaultFirewallRegistry + "/agent:" + imageTag},
		{name: "agent-act", image: constants.DefaultFirewallRegistry + "/agent-act:" + imageTag},
		{name: "api-proxy", image: constants.DefaultFirewallRegistry + "/api-proxy:" + imageTag},
		{name: "cli-proxy", image: constants.DefaultFirewallRegistry + "/cli-proxy:" + imageTag},
	}
	if isArcDindTopology(workflowData) {
		specs = append(specs, digestSpec{name: "build-tools", image: constants.DefaultFirewallRegistry + "/build-tools:" + imageTag})
	}

	parts := []string{imageTag}
	var missing []string
	for _, spec := range specs {
		digest := lookupContainerDigest(spec.image, workflowData)
		if digest == "" {
			missing = append(missing, spec.name)
			continue
		}
		parts = append(parts, spec.name+"="+digest)
	}
	if len(missing) > 0 {
		arcDindPathLog.Printf("No cached digest found for images: %s", strings.Join(missing, ", "))
	}

	if len(parts) == 1 {
		return imageTag
	}
	arcDindPathLog.Printf("Built AWF image tag with %d digest(s) for tag %s", len(parts)-1, imageTag)
	return strings.Join(parts, ",")
}

// lookupContainerDigest resolves a container image digest from cache first, then
// falls back to embedded container pins.
func lookupContainerDigest(image string, workflowData *WorkflowData) string {
	var cache *ActionCache
	if workflowData != nil {
		cache = workflowData.ActionCache
	}
	if pin, ok := lookupContainerPin(image, cache); ok && pin.Digest != "" {
		return pin.Digest
	}
	return ""
}

// buildArcDindChrootConfigPatchBody returns the Node.js command that patches the AWF
// config file with chroot.binariesSourcePath and chroot.identity.*. It is designed to be
// embedded inside a bash if-block that already guards on DOCKER_HOST=tcp://...
//
// Using the repository JavaScript helper avoids a runtime Python dependency and keeps the
// patch logic aligned with the rest of the actions/setup/js helpers.
// The config path under ${RUNNER_TEMP}/gh-aw is updated in place.
func buildArcDindChrootConfigPatchBody() string {
	return fmt.Sprintf(
		`  GH_AW_CHROOT_BINARIES_SOURCE_PATH="%s" GH_AW_CHROOT_IDENTITY_HOME="%s" node "${RUNNER_TEMP}/gh-aw/actions/patch_awf_chroot_config.cjs"`,
		awfArcDindChrootBinariesSourcePath,
		awfArcDindChrootIdentityHome,
	)
}

// buildArcDindChrootConfigPatchBodyBash returns bash commands (using jq) that patch the AWF
// config file with chroot.binariesSourcePath and chroot.identity.*. This is the bash
// equivalent of buildArcDindChrootConfigPatchBody, used for detection runs where Python
// must not be injected.
// The config path under ${RUNNER_TEMP}/gh-aw is updated in place.
func buildArcDindChrootConfigPatchBodyBash() string {
	return fmt.Sprintf(
		`  _GH_AW_CHROOT_JSON=$(jq -c --arg src "%s" --arg user "$(id -un)" --argjson uid "$(id -u)" --argjson gid "$(id -g)" --arg home "%s" '.chroot={"binariesSourcePath":$src,"identity":{"user":$user,"uid":$uid,"gid":$gid,"home":$home}}' "${RUNNER_TEMP}/gh-aw/awf-config.json") || { echo "chroot config patch failed" >&2; exit 1; }
  printf '%%s\n' "$_GH_AW_CHROOT_JSON" > "${RUNNER_TEMP}/gh-aw/awf-config.json"`,
		awfArcDindChrootBinariesSourcePath,
		awfArcDindChrootIdentityHome,
	)
}
