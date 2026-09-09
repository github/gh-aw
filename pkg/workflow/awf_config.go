// This file provides AWF (Agentic Workflow Firewall) configuration file generation.
//
// AWF supports loading configuration from a JSON/YAML file via the --config flag.
// Generating a config file rather than a long list of CLI flags improves:
//   - Readability: structured JSON is easier to audit than a one-liner flag list
//   - Correctness: complex values (JSON objects) avoid shell escaping issues
//   - Composability: config files can be layered and merged
//   - Extensibility: new features add JSON fields, not more argv flags
//
// # Config File Schema
//
// The generated config file follows the AWF config file format:
// Cross-reference: /specs/awf-config-sources-spec.md documents the canonical
// gh-aw-firewall spec/schema sources that MUST be checked when evolving mappings.
//
//	{
//	  "$schema": "https://github.com/github/gh-aw-firewall/releases/download/vX.Y.Z/awf-config.schema.json",
//	  "network": {
//	    "allowDomains": ["github.com", "api.github.com"],
//	    "blockDomains": ["ads.example.com"]
//	  },
//	  "apiProxy": {
//	    "enabled": true,
//	    "targets": {
//	      "openai":    { "host": "api.openai.com" },
//	      "anthropic": { "host": "api.anthropic.com" },
//	      "copilot":   { "host": "api.githubcopilot.com" },
//	      "gemini":        { "host": "generativelanguage.googleapis.com" }
//	    },
//	    "models": {
//	      "sonnet": ["mygateway/*sonnet*"],
//	      "":       ["sonnet", "gpt-5-mini"]
//	    }
//	  },
//	  "container": {
//	    "imageTag": "0.25.29,squid=sha256:..."
//	  },
//	  "chroot": {
//	    "binariesSourcePath": "/tmp/gh-aw",
//	    "identity": {
//	      "user": "runner",
//	      "uid": 1001,
//	      "gid": 1001,
//	      "home": "/tmp/gh-aw/home"
//	    }
//	  }
//	}
//
// # Runtime Usage
//
// The config file is written to ${RUNNER_TEMP}/gh-aw/awf-config.json before the
// AWF invocation, and referenced via: awf --config "${RUNNER_TEMP}/gh-aw/awf-config.json"
//
// Flags not yet represented in the config schema (--env-all, --exclude-env, --mount,
// --container-workdir, --log-level, --enable-host-access,
// --allow-host-ports, --skip-pull, --tty, --difc-proxy-host, --difc-proxy-ca-cert,
// --ssl-bump, --memory-limit, --diagnostic-logs) remain as CLI flags.
//
// Flags moved to config: --proxy-logs-dir → logging.proxyLogsDir,
// --audit-dir → logging.auditDir, --docker-host-path-prefix → container.dockerHostPathPrefix.
// For ARC/DinD, --proxy-logs-dir and --audit-dir CLI flags still override config at runtime
// (they use ${RUNNER_TEMP} paths that require shell expansion).

package workflow

// AWFConfigFile represents the AWF configuration file schema.
// This is the top-level structure written to awf-config.json.
type AWFConfigFile struct {
	// Schema is the JSON schema reference for IDE auto-complete support.
	Schema string `json:"$schema,omitempty"`

	// Runner contains runner topology metadata that AWF uses to activate
	// topology-specific behaviors (split-filesystem handling, network isolation,
	// tool cache redirection, sysroot image selection).
	Runner *AWFRunnerConfig `json:"runner,omitempty"`

	// Network contains network egress control configuration.
	Network *AWFNetworkConfig `json:"network,omitempty"`

	// Filesystem contains host filesystem write-boundary configuration.
	Filesystem *AWFFilesystemConfig `json:"filesystem,omitempty"`

	// Platform contains GitHub deployment metadata used by AWF auth handling.
	Platform *AWFPlatformConfig `json:"platform,omitempty"`

	// APIProxy contains API proxy (LLM gateway) configuration.
	APIProxy *AWFAPIProxyConfig `json:"apiProxy,omitempty"`

	// Enclaves configures the unified AWF-owned script and agent enclave subsystem.
	Enclaves []map[string]any `json:"enclaves,omitempty"`

	// Container contains container execution configuration.
	Container *AWFContainerConfig `json:"container,omitempty"`

	// CloudHypervisor contains Cloud Hypervisor microVM execution configuration.
	CloudHypervisor *AWFCloudHypervisorConfig `json:"cloudHypervisor,omitempty"`

	// Logging contains logging and diagnostics configuration.
	Logging *AWFLoggingConfig `json:"logging,omitempty"`

	// Chroot contains chroot execution overrides for split-filesystem ARC/DinD runners.
	// This field is not populated at compile time; it is injected at runtime when DinD topology is detected.
	Chroot *AWFChrootConfig `json:"chroot,omitempty"`
}

// AWFRunnerConfig is the "runner" section of the AWF config file.
// It provides a single stable contract between gh-aw and AWF for runner topology
// detection, letting AWF resolve all internal details (network isolation, sysroot
// image, path-prefix probes, tool cache validation) from this signal.
type AWFRunnerConfig struct {
	// Topology identifies the runner execution topology.
	// Currently supported values: "arc-dind" (ARC with Docker-in-Docker sidecar).
	// When set to "arc-dind", AWF activates split-filesystem handling, network
	// isolation, sysroot image staging, and DinD pre-staging automatically.
	Topology string `json:"topology,omitempty"`
}

// AWFNetworkConfig is the "network" section of the AWF config file.
// It maps to the --allow-domains and --block-domains CLI flags.
type AWFNetworkConfig struct {
	// AllowDomains is the list of allowed egress domains.
	// Supports wildcards (e.g. "*.github.com") and exact matches.
	// Maps to: --allow-domains <comma-separated>
	AllowDomains []string `json:"allowDomains,omitempty"`

	// BlockDomains is the list of explicitly blocked egress domains.
	// Maps to: --block-domains <comma-separated>
	BlockDomains []string `json:"blockDomains,omitempty"`

	// Isolation enables topology-based egress isolation mode.
	// Maps to: --network-isolation
	Isolation bool `json:"isolation,omitempty"`

	// VerifySbxEgress enables fail-closed direct-egress verification for Docker sbx.
	// Maps to: --verify-sbx-egress
	VerifySbxEgress bool `json:"verifySbxEgress,omitempty"`

	// TopologyAttach lists container names AWF should attach to awf-net.
	// Maps to: --topology-attach <name> (repeatable)
	TopologyAttach []string `json:"topologyAttach,omitempty"`
}

// AWFFilesystemConfig is the "filesystem" section of the AWF config file.
type AWFFilesystemConfig struct {
	// AllowWrite lists guest-visible absolute paths that may remain writable.
	AllowWrite []string `json:"allowWrite"`
}

// AWFPlatformConfig is the "platform" section of the AWF config file.
type AWFPlatformConfig struct {
	// Type is the GitHub deployment type consumed by AWF for auth behavior.
	Type string `json:"type,omitempty"`
}

// AWFAPIProxyConfig is the "apiProxy" section of the AWF config file.
// It maps to the apiProxy.* fields in the AWF config schema.
// Note: --enable-api-proxy is deprecated since AWF v0.27.32 (API proxy is always on).
type AWFAPIProxyConfig struct {
	// Enabled enables the API proxy sidecar for LLM gateway credential isolation.
	// Since AWF v0.27.32, the API proxy is always enabled; this field is kept
	// for backward compatibility with older AWF versions.
	Enabled bool `json:"enabled"`

	// EnableTokenSteering enables budget-warning system message injection near ET budget exhaustion.
	EnableTokenSteering *bool `json:"enableTokenSteering,omitempty"`

	// MaxRuns is the maximum number of LLM invocations allowed for a run.
	MaxRuns int `json:"maxRuns,omitempty"`

	// MaxTurnCacheMisses is the maximum number of consecutive cache misses allowed for a run.
	MaxTurnCacheMisses int `json:"maxCacheMisses,omitempty"`

	// MaxAICredits is the explicit per-run AI credits budget enforced by the API proxy.
	MaxAICredits int64 `json:"maxAiCredits,omitempty"`

	// ModelFallback configures the model fallback policy for unresolved model selections.
	// When nil, the AWF default (enabled=true, strategy=middle_power) is used.
	// Set enabled=false to prevent AWF from silently rewriting deployment names, which
	// is needed for BYOK Azure OpenAI deployments where rewriting causes HTTP 404.
	ModelFallback *AWFModelFallbackConfig `json:"modelFallback,omitempty"`

	// ModelMultipliers configures per-model ET accounting multipliers in AWF.
	ModelMultipliers map[string]float64 `json:"modelMultipliers,omitempty"`

	// DefaultAiCreditsPricing is the fallback per-token pricing ($/1M tokens) for
	// models not in the AWF built-in pricing table. When maxAiCredits is active and
	// a model is unrecognized, this rate is used instead of rejecting with HTTP 400.
	DefaultAiCreditsPricing *AiCreditsPricingConfig `json:"defaultAiCreditsPricing,omitempty"`

	// Targets holds per-provider API target overrides.
	// Supported keys: "openai", "anthropic", "copilot", "gemini"
	Targets map[string]*AWFAPITargetConfig `json:"targets,omitempty"`

	// Providers holds per-provider model pricing overlays used by the API proxy
	// AI-credits guardrails for models not present in the built-in pricing table.
	// Structure matches models.json provider format:
	//   providers.<provider>.models.<model>.cost.{input,output,cache_read,cache_write,reasoning}
	Providers map[string]any `json:"providers,omitempty"`

	// Models contains model alias and fallback policy definitions.
	// Keys are alias names (empty string "" = default policy); values are ordered
	// lists of vendor/modelid patterns or other alias names to try in sequence.
	// AWF resolves aliases recursively; loops are not permitted.
	// Per the AWF config schema, this lives under apiProxy.models.
	Models map[string][]string `json:"models,omitempty"`

	// AllowedModels is the explicit allowlist policy for model names/patterns.
	AllowedModels []string `json:"allowedModels,omitempty"`
	// DisallowedModels is the explicit denylist policy for model names/patterns.
	DisallowedModels []string `json:"disallowedModels,omitempty"`

	// CACert is a host path to an additional CA certificate for api-proxy
	// upstream TLS verification. Maps to frontmatter sandbox.agent.ca-cert.
	// Only emitted for AWF v0.28.10+ (see AWFAPIProxyCACertMinVersion); older
	// AWF strict config validation rejects the unknown property.
	CACert string `json:"caCert,omitempty"`
}

// AWFModelFallbackConfig is the "apiProxy.modelFallback" section of the AWF config file.
// It controls whether model fallback is enabled for unresolved model selections.
type AWFModelFallbackConfig struct {
	// Enabled controls whether middle-power fallback is applied when model resolution fails.
	// It accepts literal booleans and GitHub Actions expressions. A nil value omits the field,
	// letting AWF use its default.
	Enabled *TemplatableBool `json:"enabled,omitempty"`
}

// AWFAPITargetConfig is a single API proxy target entry.
// Maps to: --<provider>-api-target <host>
type AWFAPITargetConfig struct {
	// Host is the hostname (and optional port) of the API endpoint, or an
	// explicit http:// URL when the effective AWF version supports HTTP targets.
	// AWF currently normalizes explicit target ports to the scheme default, so
	// custom ports are not supported for these targets.
	Host string `json:"host,omitempty"`

	// AuthHeader is the custom authentication header name sent with API requests.
	// When set, the raw API key is sent as "<authHeader>: <key>" instead of the
	// provider default (e.g. "Authorization: ******" for OpenAI, or
	// "x-api-key: <key>" for Anthropic). This supports gateways like Azure OpenAI
	// that require "api-key: <rawkey>" in place of the standard provider scheme.
	// Maps to: --openai-api-auth-header / --anthropic-api-auth-header
	AuthHeader string `json:"authHeader,omitempty"`

	// ExtraHeaders holds additional non-sensitive headers injected on Copilot BYOK upstream
	// requests. Only valid for the "copilot" provider target (copilotTarget in the AWF schema).
	// Maps to AWF_BYOK_EXTRA_HEADERS in the sidecar.
	ExtraHeaders map[string]string `json:"extraHeaders,omitempty"`

	// ExtraBodyFields holds additional non-sensitive JSON body fields injected on Copilot BYOK
	// upstream requests. Only valid for the "copilot" provider target.
	// Maps to AWF_BYOK_EXTRA_BODY_FIELDS in the sidecar.
	ExtraBodyFields map[string]string `json:"extraBodyFields,omitempty"`

	// SessionId is an opt-in session identifier injected as the x-session-id request header
	// and session_id body field on Copilot BYOK upstream requests. Only valid for the
	// "copilot" provider target. Must be set explicitly; never auto-derived from GITHUB_RUN_ID.
	// Maps to AWF_PROVIDER_SESSION_ID in the sidecar.
	SessionId string `json:"sessionId,omitempty"`
}

// AWFContainerConfig is the "container" section of the AWF config file.
// It maps to container execution CLI flags.
type AWFContainerConfig struct {
	// ImageTag is the pinned AWF Docker image tag, with optional digest metadata.
	// Format: "<tag>" or "<tag>,squid=sha256:...,agent=sha256:..."
	// Maps to: --image-tag <value>
	ImageTag string `json:"imageTag,omitempty"`

	// AgentTimeout is the maximum time (in minutes) the agent command may run.
	// MicroVM runtimes require this so AWF passes a concrete guest execution timeout.
	AgentTimeout int `json:"agentTimeout,omitempty"`

	// DockerHostPathPrefix prefixes bind-mount source paths so the Docker daemon can
	// resolve runner filesystem paths. Required for ARC DinD sidecar runners where the
	// runner and daemon have separate filesystems.
	// Maps to: --docker-host-path-prefix <value>
	DockerHostPathPrefix string `json:"dockerHostPathPrefix,omitempty"`

	// ContainerRuntime specifies the OCI runtime for the agent container.
	// "gvisor" enables gVisor's runsc runtime for additional kernel-level isolation.
	// AWF translates "gvisor" → "runsc" internally.
	ContainerRuntime string `json:"containerRuntime,omitempty"`

	// Images is the closed manifest of digest-pinned AWF infrastructure images,
	// keyed by AWF image role (squid, agent, apiProxy, ...). Mapped from the
	// sandbox.agent.images frontmatter field. When present, AWF fails closed
	// instead of falling back to the official registry, and it cannot be combined
	// with legacy image selectors such as imageTag or agentImage.
	Images map[string]string `json:"images,omitempty"`
}

// AWFCloudHypervisorConfig is the "cloudHypervisor" section of the AWF config file.
type AWFCloudHypervisorConfig struct {
	PreviewEnabled                      bool                            `json:"previewEnabled,omitempty"`
	MountPolicy                         string                          `json:"mountPolicy,omitempty"`
	CloudHypervisorBinary               string                          `json:"cloudHypervisorBinary,omitempty"`
	KernelPath                          string                          `json:"kernelPath,omitempty"`
	RootfsPath                          string                          `json:"rootfsPath,omitempty"`
	SupervisorPath                      string                          `json:"supervisorPath,omitempty"`
	ArtifactManifestPath                string                          `json:"artifactManifestPath,omitempty"`
	ArtifactManifestBundlePath          string                          `json:"artifactManifestBundlePath,omitempty"`
	ArtifactReleaseTag                  string                          `json:"artifactReleaseTag,omitempty"`
	DevelopmentAllowUnattestedArtifacts bool                            `json:"developmentAllowUnattestedArtifacts,omitempty"`
	VCPUCount                           int                             `json:"vcpuCount,omitempty"`
	MemoryMiB                           int                             `json:"memoryMib,omitempty"`
	APITimeoutMs                        int                             `json:"apiTimeoutMs,omitempty"`
	SHA256                              *AWFCloudHypervisorSHA256Config `json:"sha256,omitempty"`
}

// AWFCloudHypervisorSHA256Config contains development-only legacy artifact hashes.
type AWFCloudHypervisorSHA256Config struct {
	CloudHypervisor string `json:"cloudHypervisor,omitempty"`
	Virtiofsd       string `json:"virtiofsd,omitempty"`
	Kernel          string `json:"kernel,omitempty"`
	Rootfs          string `json:"rootfs,omitempty"`
	Supervisor      string `json:"supervisor,omitempty"`
}

// AWFLoggingConfig is the "logging" section of the AWF config file.
// It maps to logging and diagnostics CLI flags.
type AWFLoggingConfig struct {
	// ProxyLogsDir is the directory path for Squid proxy access logs.
	// Maps to: --proxy-logs-dir <path>
	ProxyLogsDir string `json:"proxyLogsDir,omitempty"`

	// AuditDir is the directory path for audit logs (policy-manifest.json, squid.conf, etc).
	// Maps to: --audit-dir <path>
	AuditDir string `json:"auditDir,omitempty"`
}

// AWFChrootConfig is the "chroot" section of the AWF config file.
// It configures chroot execution overrides for split-filesystem ARC/DinD runners.
// These fields let AWF handle binary staging and identity resolution natively,
// eliminating the need for bootstrap actions on ARC/DinD topologies.
type AWFChrootConfig struct {
	// BinariesSourcePath is the runner-side directory to overlay at /usr/local/bin
	// inside chroot mode for split-filesystem ARC/DinD runners.
	BinariesSourcePath string `json:"binariesSourcePath,omitempty"`

	// Identity configures identity values applied after chroot pivot to override
	// HOME/USER/LOGNAME defaults inside chroot mode.
	Identity *AWFChrootIdentityConfig `json:"identity,omitempty"`
}

// AWFChrootIdentityConfig is the "chroot.identity" section of the AWF config file.
// It provides identity values applied after chroot pivot to override HOME/USER
// defaults inside chroot mode.
type AWFChrootIdentityConfig struct {
	// User is the USER/LOGNAME string to export inside chroot mode.
	User string `json:"user,omitempty"`

	// UID is the UID hint used for chroot identity synthesis and user switching.
	// Must be >= 1 (root is not supported).
	UID int `json:"uid,omitempty"`

	// GID is the GID hint used for chroot identity synthesis and user switching.
	// Must be >= 1.
	GID int `json:"gid,omitempty"`

	// Home is the home directory path to export inside chroot mode.
	Home string `json:"home,omitempty"`
}
