package workflow

import (
	"fmt"
	"regexp"

	"github.com/github/gh-aw/pkg/constants"
)

var releaseChecksumsSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var pinnedReleaseVersionPattern = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+$`)

func (c *Compiler) validateReleaseIntegrityPins(data *WorkflowData) error {
	if data == nil {
		return nil
	}
	if err := validateReleaseManifestPin("compiler.default-copilot", string(constants.DefaultCopilotVersion), constants.DefaultCopilotChecksumsSHA256); err != nil {
		return fmt.Errorf("invalid compiler default Copilot release pin; add an independently authenticated release pin, then run 'bash scripts/update-copilot-version.sh %s' to update the version and SHA256 together in pkg/constants/version_constants.go: %w", constants.DefaultCopilotVersion, err)
	}
	if err := validateEngineReleaseIntegrityPin(data.EngineConfig, ResolveEngineID(data), "engine"); err != nil {
		return err
	}
	if data.SafeOutputs != nil && data.SafeOutputs.ThreatDetection != nil {
		config := data.SafeOutputs.ThreatDetection.EngineConfig
		if config != nil {
			engineID := config.ID
			if engineID == "" {
				engineID = c.getThreatDetectionEngineID(data)
			}
			if err := validateEngineReleaseIntegrityPin(config, engineID, "safe-outputs.threat-detection.engine"); err != nil {
				return err
			}
		}
	}
	agent := getAgentConfig(data)
	if agent == nil || agent.ChecksumsSHA256 == "" {
		return nil
	}
	if agent.Disabled || agent.Command != "" {
		return NewValidationError("sandbox.agent.checksums-sha256", agent.ChecksumsSHA256,
			"checksum pins require the built-in AWF installer",
			"Remove the pin for a disabled/custom sandbox, or omit sandbox.agent.command and use sandbox.agent.version: v0.28.62.")
	}
	return validateReleaseManifestPin("sandbox.agent", agent.Version, agent.ChecksumsSHA256)
}

func validateEngineReleaseIntegrityPin(config *EngineConfig, engineID, field string) error {
	if config == nil || config.ChecksumsSHA256 == "" {
		return nil
	}
	if engineID != "copilot" || config.Command != "" || config.IsInlineDefinition {
		return NewValidationError(field+".checksums-sha256", config.ChecksumsSHA256,
			"checksum pins require the built-in Copilot CLI installer",
			"Use id: copilot with an explicit version and omit command, or remove checksums-sha256 for other installers.")
	}
	return validateReleaseManifestPin(field, config.Version, config.ChecksumsSHA256)
}

func validateReleaseManifestPin(field, version, digest string) error {
	if !releaseChecksumsSHA256Pattern.MatchString(digest) {
		return NewValidationError(field+".checksums-sha256", digest,
			"expected a literal SHA256 checksum-manifest digest containing exactly 64 lowercase hexadecimal characters",
			"Set checksums-sha256 to the independently trusted digest of checksums.txt (AWF) or SHA256SUMS.txt (Copilot), not an expression or an individual asset digest.")
	}
	if !pinnedReleaseVersionPattern.MatchString(version) {
		return NewValidationError(field+".version", version,
			"checksums-sha256 requires an explicit semantic release version, not latest or a runtime expression",
			"Pin version and checksums-sha256 together. Example: version: \"1.0.98\" with its SHA256SUMS.txt digest.")
	}
	return nil
}
