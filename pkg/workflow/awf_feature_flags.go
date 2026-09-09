// This file contains AWF version-gated capability helpers.

package workflow

import "github.com/github/gh-aw/pkg/constants"

// awfSupportsExcludeEnv returns true when the effective AWF version supports --exclude-env
// (introduced in AWF v0.25.3).
func awfSupportsExcludeEnv(firewallConfig *FirewallConfig) bool {
	return awfVersionAtLeast(firewallConfig, constants.AWFExcludeEnvMinVersion)
}

// awfVersionAtLeast returns true when the effective AWF version is at or above minVersion.
//
// If firewallConfig has no version set, DefaultFirewallVersion is used. "latest" always
// returns true. Non-semver strings (e.g. branch names) return false (conservative).
func awfVersionAtLeast(firewallConfig *FirewallConfig, minVersion constants.Version) bool {
	var versionStr string
	if firewallConfig != nil && firewallConfig.Version != "" {
		versionStr = firewallConfig.Version
	}
	return versionAtLeast(versionStr, string(constants.DefaultFirewallVersion), string(minVersion))
}

// awfSupportsCliProxy returns true when the effective AWF version supports --difc-proxy-host
// and --difc-proxy-ca-cert (introduced in AWF v0.25.17).
func awfSupportsCliProxy(firewallConfig *FirewallConfig) bool {
	return awfVersionAtLeast(firewallConfig, constants.AWFCliProxyMinVersion)
}

// awfSupportsAllowHostPorts returns true when the effective AWF version supports
// --allow-host-ports.
func awfSupportsAllowHostPorts(firewallConfig *FirewallConfig) bool {
	return awfVersionAtLeast(firewallConfig, constants.AWFAllowHostPortsMinVersion)
}

// awfSupportsDockerHostPathPrefix returns true when the effective AWF version supports
// --docker-host-path-prefix.
func awfSupportsDockerHostPathPrefix(firewallConfig *FirewallConfig) bool {
	return awfVersionAtLeast(firewallConfig, constants.AWFDockerHostPathPrefixMinVersion)
}

// awfSupportsTokenSteering returns true when the effective AWF version supports
// apiProxy.enableTokenSteering.
func awfSupportsTokenSteering(firewallConfig *FirewallConfig) bool {
	return awfVersionAtLeast(firewallConfig, constants.AWFTokenSteeringMinVersion)
}

// awfSupportsChrootConfig returns true when the effective AWF version supports
// chroot.binariesSourcePath and chroot.identity.* in the config file (AWF v0.27.1+).
func awfSupportsChrootConfig(firewallConfig *FirewallConfig) bool {
	return awfVersionAtLeast(firewallConfig, constants.AWFChrootConfigMinVersion)
}

// awfSupportsContainerRuntime returns true when the effective AWF version supports the
// containerRuntime field in the container config (gh-aw-firewall#6093).
// The field must not be emitted for older versions that do not recognise it.
func awfSupportsContainerRuntime(firewallConfig *FirewallConfig) bool {
	return awfVersionAtLeast(firewallConfig, constants.AWFContainerRuntimeMinVersion)
}

// awfSupportsCloudHypervisor returns true when the effective AWF version supports
// the cloud-hypervisor preview runtime and its required CLI flags.
func awfSupportsCloudHypervisor(firewallConfig *FirewallConfig) bool {
	return awfVersionAtLeast(firewallConfig, constants.AWFCloudHypervisorMinVersion)
}

// awfSupportsLegacySecurity returns true when the effective AWF version supports the
// --legacy-security flag (v0.27.32+). Older versions default to legacy mode and do not
// recognize this flag.
func awfSupportsLegacySecurity(firewallConfig *FirewallConfig) bool {
	return awfVersionAtLeast(firewallConfig, constants.AWFLegacySecurityMinVersion)
}

// awfSupportsDefaultAiCreditsPricing returns true when apiProxy.defaultAiCreditsPricing
// survives AWF config resolution and reaches the api-proxy container.
func awfSupportsDefaultAiCreditsPricing(firewallConfig *FirewallConfig) bool {
	return awfVersionAtLeast(firewallConfig, constants.AWFDefaultAiCreditsPricingMinVersion)
}

// awfSupportsAPIProxyProviders returns true when the effective AWF version supports
// apiProxy.providers in awf-config.json.
func awfSupportsAPIProxyProviders(firewallConfig *FirewallConfig) bool {
	return awfVersionAtLeast(firewallConfig, constants.AWFAPIProxyProvidersMinVersion)
}

// awfSupportsContainerImages returns true when the effective AWF version supports
// the container.images manifest in awf-config.json.
func awfSupportsContainerImages(firewallConfig *FirewallConfig) bool {
	return awfVersionAtLeast(firewallConfig, constants.AWFContainerImagesMinVersion)
}

// awfSupportsCloudHypervisorFilesystemAllowWrite returns true when the effective
// AWF version supports filesystem.allowWrite for the Cloud Hypervisor microVM
// runtime (gh-aw-firewall v0.28.6+; see AWFCloudHypervisorFilesystemAllowWriteMinVersion).
func awfSupportsCloudHypervisorFilesystemAllowWrite(firewallConfig *FirewallConfig) bool {
	return awfVersionAtLeast(firewallConfig, constants.AWFCloudHypervisorFilesystemAllowWriteMinVersion)
}

// awfSupportsAPIProxyCACert returns true when the effective AWF version supports
// apiProxy.caCert in awf-config.json.
func awfSupportsAPIProxyCACert(firewallConfig *FirewallConfig) bool {
	return awfVersionAtLeast(firewallConfig, constants.AWFAPIProxyCACertMinVersion)
}

// awfSupportsVerifySbxEgress returns true when the effective AWF version supports
// network.verifySbxEgress for Docker sbx runtime egress verification.
func awfSupportsVerifySbxEgress(firewallConfig *FirewallConfig) bool {
	return awfVersionAtLeast(firewallConfig, constants.AWFVerifySbxEgressMinVersion)
}

// awfSupportsHTTPAPITargets returns true when the effective AWF version supports
// explicit http:// schemes in apiProxy target hosts.
func awfSupportsHTTPAPITargets(firewallConfig *FirewallConfig) bool {
	return awfVersionAtLeast(firewallConfig, constants.AWFHTTPAPITargetMinVersion)
}

// awfEmitsFilesystemAllowWrite reports whether the compiler may emit the
// filesystem section of awf-config.json for this workflow.
//
// Only the Cloud Hypervisor runtime enforces filesystem.allowWrite in a way that
// leaves a working agent: it stages its own virtiofs exports (see
// AWFCloudHypervisorFilesystemAllowWriteMinVersion). The other runtimes are
// excluded on purpose:
//
//   - Docker and gVisor narrow AWF's own writable bind mounts to read-only, which
//     includes the internal /tmp/awf-init control-plane mount nested under the
//     narrowed /tmp bind. runc then cannot create that mountpoint and the agent
//     container never starts, so any policy that does not cover /tmp is fatal.
//   - docker-sbx has no enforcement path and AWF fails closed with
//     "filesystem.allowWrite is not yet supported by the sbx runtime".
//
// emitGeneralToolWarnings warns when a workflow declares allowWrite on a runtime
// where it is dropped, so the opt-in is never silently ignored.
func awfEmitsFilesystemAllowWrite(workflowData *WorkflowData, firewallConfig *FirewallConfig) bool {
	if !isCloudHypervisorRuntime(workflowData) {
		return false
	}
	return awfSupportsCloudHypervisorFilesystemAllowWrite(firewallConfig)
}
