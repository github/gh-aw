// This file implements support for the sandbox.agent.images frontmatter field,
// which selects compiler-authorized, digest-pinned AWF infrastructure images.
//
// The manifest is emitted as container.images in the generated AWF config file.
// AWF treats the manifest as closed: when it is present, every image role required
// by the enabled feature set must be listed and AWF fails closed instead of falling
// back to the official registry.
//
// The repository-level .github/workflows/aw.json "container_pins" setting redirects
// default AWF references for predownload and lock metadata. Detection jobs also
// translate those redirects into a closed runtime manifest. An explicitly configured
// manifest remains authoritative for runtime role selection and matching predownload.

package workflow

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var sandboxAgentImagesLog = logger.New("workflow:sandbox_agent_images")

// AWF image roles accepted by container.images. This set is closed: AWF rejects
// unknown roles, so the compiler must reject them too.
const (
	awfImageRoleSquid            = "squid"
	awfImageRoleAgent            = "agent"
	awfImageRoleAPIProxy         = "apiProxy"
	awfImageRoleRouter           = "router"
	awfImageRoleCliProxy         = "cliProxy"
	awfImageRoleBuildTools       = "buildTools"
	awfImageRoleDohProxy         = "dohProxy"
	awfImageRoleEnclaveScript    = "enclaveScript"
	awfImageRoleEnclaveAgent     = "enclaveAgent"
	awfImageRoleEnclaveMcpServer = "enclaveMcpServer"
	awfImageRoleDindStaging      = "dindStaging"
)

// awfImageRoles lists the supported container.images roles in documentation order.
var awfImageRoles = []string{
	awfImageRoleSquid,
	awfImageRoleAgent,
	awfImageRoleAPIProxy,
	awfImageRoleRouter,
	awfImageRoleCliProxy,
	awfImageRoleBuildTools,
	awfImageRoleDohProxy,
	awfImageRoleEnclaveScript,
	awfImageRoleEnclaveAgent,
	awfImageRoleEnclaveMcpServer,
	awfImageRoleDindStaging,
}

// awfPinnedImagePattern is AWF's canonical digestPinnedImage grammar. It follows
// distribution/reference while requiring an explicit registry host, tag, and
// lowercase SHA-256 digest.
var awfPinnedImagePattern = regexp.MustCompile(`^(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?)+(?::[0-9]{1,5})?|[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?:[0-9]{1,5}|localhost)/[a-z0-9]+(?:(?:[._]|__|[-]+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]|__|[-]+)[a-z0-9]+)*)*:[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}@sha256:[a-f0-9]{64}$`)

// getSandboxAgentImages returns the configured sandbox.agent.images manifest, or nil.
func getSandboxAgentImages(workflowData *WorkflowData) map[string]string {
	agentConfig := getAgentConfig(workflowData)
	if agentConfig == nil || len(agentConfig.Images) == 0 {
		return nil
	}
	return agentConfig.Images
}

// isKnownAWFImageRole reports whether role is part of the closed AWF role set.
func isKnownAWFImageRole(role string) bool {
	return slices.Contains(awfImageRoles, role)
}

// requiredAWFImageRoles returns the image roles that must be present in the
// manifest for the workflow's enabled feature set.
func requiredAWFImageRoles(workflowData *WorkflowData) []string {
	required := []string{awfImageRoleSquid, awfImageRoleAgent, awfImageRoleAPIProxy}
	args := customAWFArgs(workflowData)
	if isCliProxyNeeded(workflowData) || hasEnabledAWFArg(args, "--difc-proxy-host") {
		required = append(required, awfImageRoleCliProxy)
	}
	if isArcDindTopology(workflowData) {
		required = append(required, awfImageRoleBuildTools)
	}
	legacySecurityEnabled := isLegacySecurityRuntime(workflowData) || hasEnabledAWFArg(args, "--legacy-security")
	if legacySecurityEnabled && hasEnabledAWFArg(args, "--dns-over-https") {
		required = append(required, awfImageRoleDohProxy)
	}
	if hasEnabledAWFArg(
		args,
		"--dind-pre-stage-dirs",
		"--dind-stage-engine-binary-path",
		"--dind-stage-engine-binary-target-path",
	) {
		required = append(required, awfImageRoleDindStaging)
	}

	enclaveEnabled := false
	for _, enclave := range workflowData.Enclaves {
		executor, ok := enclaveExecutor(enclave)
		if !ok {
			continue
		}
		enclaveEnabled = true
		switch executor {
		case "script":
			required = appendUniqueRole(required, awfImageRoleEnclaveScript)
		case "agent":
			required = appendUniqueRole(required, awfImageRoleEnclaveAgent)
		}
	}
	if enclaveEnabled {
		required = append(required, awfImageRoleEnclaveMcpServer)
	}
	return required
}

// defaultAWFImageForRole returns the image AWF resolves when no closed manifest
// is configured. Most roles use the selected AWF version under the official
// registry; DoH and DinD staging retain AWF's external legacy defaults.
func defaultAWFImageForRole(role, imageTag string) string {
	switch role {
	case awfImageRoleSquid:
		return constants.DefaultFirewallRegistry + "/squid:" + imageTag
	case awfImageRoleAgent:
		return constants.DefaultFirewallRegistry + "/agent:" + imageTag
	case awfImageRoleAPIProxy:
		return constants.DefaultFirewallRegistry + "/api-proxy:" + imageTag
	case awfImageRoleRouter:
		return constants.DefaultFirewallRegistry + "/router:" + imageTag
	case awfImageRoleCliProxy:
		return constants.DefaultFirewallRegistry + "/cli-proxy:" + imageTag
	case awfImageRoleBuildTools:
		return constants.DefaultFirewallRegistry + "/build-tools:" + imageTag
	case awfImageRoleDohProxy:
		return "cloudflare/cloudflared:latest"
	case awfImageRoleEnclaveScript:
		return constants.DefaultFirewallRegistry + "/enclave-script:" + imageTag
	case awfImageRoleEnclaveAgent:
		return constants.DefaultFirewallRegistry + "/enclave-agent:" + imageTag
	case awfImageRoleEnclaveMcpServer:
		return constants.DefaultFirewallRegistry + "/enclave-mcp-server:" + imageTag
	case awfImageRoleDindStaging:
		return constants.DefaultFirewallRegistry + "/agent:latest"
	default:
		return ""
	}
}

func customAWFArgs(workflowData *WorkflowData) []string {
	var args []string
	if agentConfig := getAgentConfig(workflowData); agentConfig != nil {
		args = append(args, agentConfig.Args...)
	}
	if firewallConfig := getFirewallConfig(workflowData); firewallConfig != nil {
		args = append(args, firewallConfig.Args...)
	}
	return args
}

// hasEnabledAWFArg reports whether one of the named boolean/optional-value AWF
// arguments is present. An explicit "=false" is treated as disabled.
func hasEnabledAWFArg(args []string, names ...string) bool {
	for _, arg := range args {
		name, value, hasValue := strings.Cut(arg, "=")
		for _, candidate := range names {
			if name == candidate && (!hasValue || !strings.EqualFold(value, "false")) {
				return true
			}
		}
	}
	return false
}

func appendUniqueRole(roles []string, role string) []string {
	if slices.Contains(roles, role) {
		return roles
	}
	return append(roles, role)
}

// validateSandboxAgentImages validates the sandbox.agent.images manifest.
// It enforces the closed role set, literal digest-pinned references, coverage of
// every required role, the AWF minimum version, and the absence of conflicting
// image selectors.
func validateSandboxAgentImages(workflowData *WorkflowData) error {
	images := getSandboxAgentImages(workflowData)
	if images == nil {
		return nil
	}

	roles := make([]string, 0, len(images))
	for role := range images {
		roles = append(roles, role)
	}
	sort.Strings(roles)

	// Closed role set and literal, digest-pinned references.
	for _, role := range roles {
		if !isKnownAWFImageRole(role) {
			return NewValidationError(
				"sandbox.agent.images."+role,
				role,
				"unknown AWF image role",
				fmt.Sprintf("Use one of the supported image roles: %s.\n\nSee: %s", strings.Join(awfImageRoles, ", "), constants.DocsSandboxURL),
			)
		}
		if err := validateAWFPinnedImageReference("sandbox.agent.images."+role, images[role]); err != nil {
			return err
		}
	}

	if err := validateAWFImageManifestCoverage(workflowData, images, roles); err != nil {
		return err
	}

	firewallConfig := getFirewallConfig(workflowData)
	if err := validateAWFImageManifestVersionGates(firewallConfig, images, roles); err != nil {
		return err
	}

	if err := validateNoConflictingAWFImageSelectors(workflowData, firewallConfig, roles); err != nil {
		return err
	}

	sandboxAgentImagesLog.Printf("Validated sandbox.agent.images: %d role(s)", len(images))
	return nil
}

// validateAWFImageManifestCoverage ensures the manifest covers every image role
// required by the enabled feature set: AWF fails closed rather than falling back
// to the official registry.
func validateAWFImageManifestCoverage(workflowData *WorkflowData, images map[string]string, roles []string) error {
	var missing []string
	for _, role := range requiredAWFImageRoles(workflowData) {
		if _, ok := images[role]; !ok {
			missing = append(missing, role)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return NewValidationError(
		"sandbox.agent.images",
		strings.Join(roles, ", "),
		"incomplete image manifest: missing required role(s) "+strings.Join(missing, ", "),
		fmt.Sprintf("AWF fails closed when container.images is set, so every image role required by the enabled features must be pinned.\n\nAdd the missing role(s):\n\nsandbox:\n  agent:\n    images:\n      %s: registry.example.com/approved/%s:v0.28.4@sha256:<64-hex-digest>\n\nSee: %s", missing[0], missing[0], constants.DocsSandboxURL), //nolint:uncheckedsliceindex // missing is non-empty per the len(missing) == 0 check above
	)
}

// validateAWFImageManifestVersionGates enforces the AWF minimum versions required
// by the manifest: the base container.images support (AWF v0.28.4+) and, when
// present, the "router" role (AWF v0.28.21+).
func validateAWFImageManifestVersionGates(firewallConfig *FirewallConfig, images map[string]string, roles []string) error {
	effectiveVersion := string(constants.DefaultFirewallVersion)
	if firewallConfig != nil && firewallConfig.Version != "" {
		effectiveVersion = firewallConfig.Version
	}

	if !awfSupportsContainerImages(firewallConfig) {
		return NewValidationError(
			"sandbox.agent.images",
			strings.Join(roles, ", "),
			fmt.Sprintf("sandbox.agent.images requires AWF %s or newer", constants.AWFContainerImagesMinVersion),
			fmt.Sprintf("The custom image manifest maps to container.images, which older AWF versions reject.\n\nThe effective AWF version is %s. Set sandbox.agent.version (or firewall.version) to %s or newer.", effectiveVersion, constants.AWFContainerImagesMinVersion),
		)
	}

	if _, ok := images[awfImageRoleRouter]; ok && !awfSupportsRouterImageRole(firewallConfig) {
		return NewValidationError(
			"sandbox.agent.images.router",
			images[awfImageRoleRouter],
			fmt.Sprintf("sandbox.agent.images.router requires AWF %s or newer", constants.AWFRouterImageRoleMinVersion),
			fmt.Sprintf("The router role in container.images is rejected by older AWF versions.\n\nThe effective AWF version is %s. Set sandbox.agent.version (or firewall.version) to %s or newer.", effectiveVersion, constants.AWFRouterImageRoleMinVersion),
		)
	}

	return nil
}

// validateNoConflictingAWFImageSelectors rejects other configuration that could
// produce a different effective image than the manifest.
func validateNoConflictingAWFImageSelectors(workflowData *WorkflowData, firewallConfig *FirewallConfig, roles []string) error {
	if firewallConfig != nil && firewallConfig.SSLBump {
		return NewValidationError(
			"sandbox.agent.images",
			strings.Join(roles, ", "),
			"sandbox.agent.images cannot be combined with SSL bump",
			"AWF rejects a custom image manifest together with security.sslBump because SSL bump selects a different Squid image.\n\nExample: remove the ssl_bump firewall setting or the images manifest.",
		)
	}
	for _, enclave := range workflowData.Enclaves {
		if enclave != nil && enclave.Image != "" {
			return NewValidationError(
				"sandbox.agent.images",
				enclave.Image,
				"sandbox.agent.images cannot be combined with per-enclave image overrides",
				"Remove the 'image' field from the enclave configuration and pin the enclave images through sandbox.agent.images instead.\n\nExample:\nsandbox:\n  agent:\n    images:\n      enclaveScript: registry.example.com/approved/enclave-script:v0.28.4@sha256:<64-hex-digest>",
			)
		}
	}
	return validateNoConflictingAWFImageArgs(workflowData)
}

// awfImageSelectorArgs lists generic AWF arguments that select images through
// legacy controls, which cannot be combined with a custom image manifest.
var awfImageSelectorArgs = []string{
	"--image-registry",
	"--image-tag",
	"--agent-image",
	"--build-local",
	"--sysroot-image",
	"--dind-staging-image",
	"--ssl-bump",
}

// validateNoConflictingAWFImageArgs rejects raw AWF arguments that select images
// through legacy controls when sandbox.agent.images is configured.
func validateNoConflictingAWFImageArgs(workflowData *WorkflowData) error {
	args := customAWFArgs(workflowData)
	for _, arg := range args {
		name := arg
		if idx := strings.Index(name, "="); idx >= 0 {
			name = name[:idx]
		}
		for _, selector := range awfImageSelectorArgs {
			if name == selector {
				return NewValidationError(
					"sandbox.agent.images",
					arg,
					selector+" selects AWF images through a legacy control and cannot be combined with sandbox.agent.images",
					"Remove "+selector+" from the AWF arguments, or remove the sandbox.agent.images manifest.",
				)
			}
		}
	}
	return nil
}

// validateAWFPinnedImageReference validates a single image reference value.
func validateAWFPinnedImageReference(field, value string) error {
	if value == "" {
		return NewValidationError(
			field,
			value,
			"image reference cannot be empty",
			fmt.Sprintf("Provide a literal reference with both a tag and a digest, e.g. registry.example.com/approved/squid:v0.28.4@sha256:<64-hex-digest>.\n\nSee: %s", constants.DocsSandboxURL),
		)
	}
	if githubActionsExpressionPattern.MatchString(value) || strings.Contains(value, "${") {
		return NewValidationError(
			field,
			value,
			"image reference must be a literal value: expressions and interpolation are not allowed",
			fmt.Sprintf("Infrastructure image references must be static so that no runtime input can influence them. Replace the expression with a literal reference, e.g. registry.example.com/approved/squid:v0.28.4@sha256:<64-hex-digest>.\n\nSee: %s", constants.DocsSandboxURL),
		)
	}
	if !awfPinnedImagePattern.MatchString(value) {
		return NewValidationError(
			field,
			value,
			"image reference must be a registry-qualified reference with both a tag and a sha256 digest",
			fmt.Sprintf("Use the format 'registry/repository:tag@sha256:<64 lowercase hex characters>', e.g. registry.example.com/approved/squid:v0.28.4@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef.\n\nSee: %s", constants.DocsSandboxURL),
		)
	}
	return nil
}
