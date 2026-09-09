package constants

// Version represents a software version string.
// This semantic type distinguishes version strings from arbitrary strings,
// enabling future validation logic (e.g., semver parsing) and making
// version requirements explicit in function signatures.
//
// Example usage:
//
//	const DefaultCopilotVersion Version = "0.0.369"
//	func InstallTool(name string, version Version) error { ... }
type Version string

// String returns the string representation of the version
func (v Version) String() string {
	return string(v)
}

// IsValid returns true if the version is non-empty
func (v Version) IsValid() bool {
	return v != ""
}

// ModelName represents an AI model name identifier.
// This semantic type distinguishes model names from arbitrary strings,
// making model selection explicit in function signatures.
//
// Example usage:
//
//	const DefaultCopilotDetectionModel ModelName = "gpt-5-mini"
//	func ExecuteWithModel(model ModelName) error { ... }
type ModelName string

// DefaultClaudeCodeVersion is the default version of the Claude Code CLI.
const DefaultClaudeCodeVersion Version = "2.1.247"

// DefaultCopilotVersion is the default version of the GitHub Copilot CLI.
//
// When unpinning or upgrading this version, verify:
//   - MCPs are not blocked from loading (tools.mcp configuration still works end-to-end)
//   - /models does not silently fail on PATs (check that model listing works with PAT auth)
const DefaultCopilotVersion Version = "1.0.80"

// DefaultCopilotSDKVersion is the default version of the @github/copilot-sdk package.
const DefaultCopilotSDKVersion Version = "1.0.11"

// DefaultCodexVersion is the default version of the OpenAI Codex CLI
const DefaultCodexVersion Version = "0.150.1"

// DefaultGeminiVersion is the default version of the Google Gemini CLI
const DefaultGeminiVersion Version = "0.55.1"

// DefaultPiVersion is the default version of the Pi CLI
const DefaultPiVersion Version = "0.84.3"

// DefaultGitHubMCPServerVersion is the default version of the GitHub MCP server Docker image
const DefaultGitHubMCPServerVersion Version = "v1.11.0"

// DefaultFirewallVersion is the default version of the gh-aw-firewall (AWF) binary
//
// ⚠️  IMPORTANT: When updating this version, you must run a full rebuild and recompile twice:
//
//	make build && make recompile && make recompile
//
// The first recompile regenerates all lock files using the new version; the second recompile
// refreshes the container SHA pins that were resolved during the first pass.
const DefaultFirewallVersion Version = "v0.28.14"

// AWFExcludeEnvMinVersion is the minimum AWF version that supports the --exclude-env flag.
// Workflows pinning an older AWF version must not emit --exclude-env flags or the run will fail.
const AWFExcludeEnvMinVersion Version = "v0.25.3"

// AWFCliProxyMinVersion is the minimum supported AWF version for emitting the CLI proxy flags
// (--difc-proxy-host, --difc-proxy-ca-cert). Workflows pinning an older AWF version than
// v0.25.17 must not emit CLI proxy flags or the run will fail.
const AWFCliProxyMinVersion Version = "v0.25.17"

// AWFCliProxyGHListMinVersion is the minimum AWF version whose CLI proxy supports
// `gh issue list` and `gh pr list` without misclassifying github.com as GHES.
const AWFCliProxyGHListMinVersion Version = "v0.28.13"

// AWFAllowHostPortsMinVersion is the minimum AWF version that supports the
// --allow-host-ports flag. Workflows pinning an older AWF version must not emit
// --allow-host-ports or the run will fail at startup with an unknown flag error.
const AWFAllowHostPortsMinVersion Version = "v0.25.24"

// AWFDockerHostPathPrefixMinVersion is the minimum AWF version that supports the
// --docker-host-path-prefix flag used for ARC/DinD split runner/daemon filesystems.
// Workflows pinning an older AWF version must not emit this flag.
const AWFDockerHostPathPrefixMinVersion Version = "v0.25.43"

// AWFTokenSteeringMinVersion is the minimum AWF version that supports
// apiProxy.enableTokenSteering (mapped from frontmatter firewall.effective-token-steering).
const AWFTokenSteeringMinVersion Version = "v0.25.44"

// AWFChrootConfigMinVersion is the minimum AWF version that supports
// chroot.binariesSourcePath and chroot.identity.* in the config file.
// These fields let AWF handle binary staging and identity resolution natively
// for ARC/DinD split runner/daemon filesystem topologies, removing the need
// for bootstrap actions that manually copy binaries and pre-seed /etc/passwd.
const AWFChrootConfigMinVersion Version = "v0.27.1"

// AWFArcDindMinVersion is the minimum AWF version required for runner.topology=arc-dind.
// Earlier versions have known sysroot/chroot mount-handling bugs that can prevent
// the agent container from starting in split-filesystem ARC/DinD environments.
const AWFArcDindMinVersion Version = "v0.27.20"

// AWFContainerRuntimeMinVersion is the minimum AWF version that supports the
// containerRuntime field in the container config (gh-aw-firewall#6093).
const AWFContainerRuntimeMinVersion Version = "v0.27.30"

// AWFCloudHypervisorMinVersion is the minimum AWF version that supports the
// cloud-hypervisor preview runtime and its release assets.
const AWFCloudHypervisorMinVersion Version = "v0.28.11"

// AWFLegacySecurityMinVersion is the minimum AWF version that supports the
// --legacy-security flag and unconditional API proxy (gh-aw-firewall#6207).
// Workflows pinning an older AWF version must use the old --security-mode compat behavior.
const AWFLegacySecurityMinVersion Version = "v0.27.32"

// AWFDefaultAiCreditsPricingMinVersion is the minimum AWF version where
// apiProxy.defaultAiCreditsPricing survives config resolution and reaches the
// api-proxy container as AWF_DEFAULT_AI_CREDITS_PRICING.
const AWFDefaultAiCreditsPricingMinVersion Version = "v0.27.43"

// AWFAPIProxyProvidersMinVersion is the minimum AWF version that supports
// apiProxy.providers in awf-config.json.
// Workflows pinning an older AWF version must not emit this field because older
// AWF strict config validation rejects unknown apiProxy properties.
// v0.27.43 adds apiProxy.providers to awf-config-schema.json.
const AWFAPIProxyProvidersMinVersion Version = "v0.27.43"

// AWFContainerImagesMinVersion is the minimum AWF version that supports the
// container.images manifest in awf-config.json (mapped from frontmatter
// sandbox.agent.images). Older versions reject the unknown property.
const AWFContainerImagesMinVersion Version = "v0.28.4"

// AWFFilesystemAllowWriteMinVersion is the minimum AWF version that added
// filesystem.allowWrite to the AWF config file schema.
//
// Note: schema support is not the same as usable enforcement. The compose
// runtimes (Docker, gVisor) enforce the policy by narrowing AWF's own writable
// bind mounts, including its internal /tmp/awf-init control-plane mount, so any
// policy that does not cover /tmp prevents the agent container from starting.
// The compiler therefore only emits the filesystem section for the Cloud
// Hypervisor runtime; see awfEmitsFilesystemAllowWrite.
const AWFFilesystemAllowWriteMinVersion Version = "v0.28.5"

// AWFCloudHypervisorFilesystemAllowWriteMinVersion is the minimum AWF version that
// supports filesystem.allowWrite for the Cloud Hypervisor microVM runtime.
//
// This is higher than AWFFilesystemAllowWriteMinVersion because selective allowWrite
// was broken on every real (systemd) host until v0.28.6: gh-aw-firewall#7672 fixed a
// mount-propagation defect where staged overlay mounts inherited their source's
// "shared" peer group instead of being private, tripping the planner's fail-closed
// propagation assertion. Versions v0.28.5 reliably fail Cloud Hypervisor allowWrite
// on GitHub-hosted (systemd) runners, so this constant must not be lowered to
// AWFFilesystemAllowWriteMinVersion.
const AWFCloudHypervisorFilesystemAllowWriteMinVersion Version = "v0.28.6"

// AWFEnclaveGitHubIssuesMinVersion is the first AWF version whose
// config schema accepts enclaves[].agent.github.cli = "issues-read-v1".
const AWFEnclaveGitHubIssuesMinVersion Version = "v0.28.9"

// AWFEnclaveTrustedSensitivityMinVersion is the first AWF version whose
// enclave response schema permits free-form string values for trusted repositories.
const AWFEnclaveTrustedSensitivityMinVersion Version = "v0.28.14"

// AWFDynamicRepositoryEnclaveMinVersion is the first AWF version that accepts
// dynamic agent enclave repository policy envelopes and sends per-invocation
// repository admission TTLs to MCPG's github-repository-delegation-v1
// controller as seconds.
//
// v0.28.14 includes the seconds-contract AWF implementation tracked by
// gh-aw-firewall#8292.
const AWFDynamicRepositoryEnclaveMinVersion Version = "v0.28.14"

// AWFAPIProxyCACertMinVersion is the minimum AWF version that supports
// apiProxy.caCert in awf-config.json (mapped from frontmatter
// sandbox.agent.ca-cert). Older AWF versions reject the unknown property
// under strict config validation.
const AWFAPIProxyCACertMinVersion Version = "v0.28.10"

// AWFVerifySbxEgressMinVersion is the minimum AWF version that supports
// network.verifySbxEgress for fail-closed Docker sbx egress verification.
const AWFVerifySbxEgressMinVersion Version = "v0.28.13"

// AWFHTTPAPITargetMinVersion is the minimum AWF version that supports explicit
// http:// schemes in apiProxy target hosts.
const AWFHTTPAPITargetMinVersion Version = "v0.28.13"

// DefaultGVisorVersion is the pinned gVisor release used by the compiler-generated
// install step. A specific dated release name is used instead of "latest" to ensure
// reproducible, verifiable installs. Each release provides SHA-512 files for
// integrity verification before the binaries are installed with root privileges.
// Bump this constant after reviewing the release notes at
// https://github.com/google/gvisor/releases.
const DefaultGVisorVersion = "20250707.0"

// CopilotNoAskUserMinVersion is the minimum Copilot CLI version that supports the --no-ask-user
// flag, which enables fully autonomous agentic runs by suppressing interactive prompts.
// Workflows using an older Copilot CLI version must not emit --no-ask-user or the run will fail.
const CopilotNoAskUserMinVersion Version = "1.0.19"

// DefaultMCPGatewayVersion is the default version of the MCP Gateway (gh-aw-mcpg) Docker image
//
// ⚠️  IMPORTANT: When updating this version, you must run a full rebuild and recompile twice:
//
//	make build && make recompile && make recompile
//
// The first recompile regenerates all lock files using the new version; the second recompile
// refreshes the container SHA pins that were resolved during the first pass.
const DefaultMCPGatewayVersion Version = "v0.4.20"

// MCPGIntegrityReactionsMinVersion is the minimum MCPG version that supports
// endorsement-reactions and disapproval-reactions in the allow-only policy.
const MCPGIntegrityReactionsMinVersion Version = "v0.2.18"

// MCPGEnclaveGitHubIssuesMinVersion is the first MCPG version with
// concurrent per-agent isolation for the issues-read-v1 enclave capability.
const MCPGEnclaveGitHubIssuesMinVersion Version = "v0.4.15"

// MCPGEnclaveAgentToolsMinVersion is the first MCPG version whose distinct
// enclave identity supports agent.tools.github allowlists and guard policies.
// Currently identical to MCPGEnclaveGitHubIssuesMinVersion because both
// enclave GitHub shapes share the same distinct-identity implementation;
// kept as a separate constant so the two gates can diverge independently.
const MCPGEnclaveAgentToolsMinVersion Version = "v0.4.15"

// MCPGDynamicRepositoryDelegationMinVersion is the first MCPG version that
// advertises the github-repository-delegation-v1 dynamic repository delegation
// controller required by dynamic agent enclave admission and interprets TTL
// wire values as seconds.
//
// v0.4.16 advertised the controller but rejected the strict-stdin
// "delegationControllers" config field the compiler previously emitted, never
// started the real controller, and never handed AWF a private control
// endpoint (gh-aw-mcpg#12604). That contract is fixed by gh-aw-mcpg#12605,
// released in v0.4.17, but that release still interprets TTL wire values as
// nanoseconds. v0.4.18 adds the seconds-only TTL contract, and v0.4.19 fixes
// dynamic GitHub delegation guard/write-sink compatibility so workflows with
// tools.github: false compile to runnable lock files without manual edits.
// Dynamic enclave compilation/runtime setup therefore fails closed on older
// MCPG builds that lack the compatible owner-scoped envelope, bounded dynamic
// schema admission, transactional reconciliation, persisted TTL binding,
// durability, DIFC isolation, redaction behavior, and seconds-based TTL
// interpretation.
const MCPGDynamicRepositoryDelegationMinVersion Version = "v0.4.19"

// DefaultPlaywrightCLIVersion is the default version of the @playwright/cli package.
// Used when tools.playwright is enabled.
// Keep this version outside the default 3-day npm release-age cooldown window enforced by
// generated Playwright CLI install steps. See TestDefaultPlaywrightCLIVersionOutsideCooldownWindow.
const DefaultPlaywrightCLIVersion Version = "0.1.18"

// DefaultMCPSDKVersion is the default version of the @modelcontextprotocol/sdk package
const DefaultMCPSDKVersion Version = "1.30.0"

// DefaultGitHubScriptVersion is the default version of the actions/github-script action
const DefaultGitHubScriptVersion Version = "v9"

// DefaultThreatDetectVersion is the version of the gh-aw-threat-detection binary to install.
// This is used by the default external threat-detection path and when
// `features: gh-aw-detection: true` is set in the workflow frontmatter, enabling the external
// threat-detect binary path instead of the inline engine execution path.
const DefaultThreatDetectVersion Version = "v0.5.1"

// GhSkillsMinVersion is the minimum gh CLI version required for frontmatter skill support
// (installing gh extensions via `gh extension install`). Workflows that install frontmatter
// skills must have at least this version of gh installed before running skill install steps.
const GhSkillsMinVersion Version = "2.90.0"

// DefaultBunVersion is the default version of Bun for runtime setup
const DefaultBunVersion Version = "1.1"

// DefaultNodeVersion is the default version of Node.js for runtime setup
const DefaultNodeVersion Version = "24"

// DefaultPythonVersion is the default version of Python for runtime setup
const DefaultPythonVersion Version = "3.12"

// DefaultRubyVersion is the default version of Ruby for runtime setup
const DefaultRubyVersion Version = "3.3"

// DefaultDotNetVersion is the default version of .NET for runtime setup
const DefaultDotNetVersion Version = "8.0"

// DefaultJavaVersion is the default version of Java for runtime setup
const DefaultJavaVersion Version = "21"

// DefaultElixirVersion is the default version of Elixir for runtime setup
const DefaultElixirVersion Version = "1.17"

// DefaultGoVersion is the default version of Go for runtime setup
const DefaultGoVersion Version = "1.26"

// DefaultHaskellVersion is the default version of GHC for runtime setup
const DefaultHaskellVersion Version = "9.10"

// DefaultDenoVersion is the default version of Deno for runtime setup
const DefaultDenoVersion Version = "2.x"
