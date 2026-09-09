package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var frontmatterExtractionSecurityLog = logger.New("workflow:frontmatter_extraction_security")

// extractNetworkPermissions extracts network permissions from frontmatter
func (c *Compiler) extractNetworkPermissions(frontmatter map[string]any) *NetworkPermissions {
	frontmatterExtractionSecurityLog.Print("Extracting network permissions from frontmatter")

	if network, exists := frontmatter["network"]; exists {
		// Handle string format: "defaults"
		if networkStr, ok := network.(string); ok {
			frontmatterExtractionSecurityLog.Printf("Network permissions string format: %s", networkStr)
			if networkStr == "defaults" {
				return &NetworkPermissions{
					Allowed:           []string{"defaults"},
					ExplicitlyDefined: true,
				}
			}
			// Unknown string format, return nil
			frontmatterExtractionSecurityLog.Printf("Unknown network string format: %s", networkStr)
			return nil
		}

		// Handle object format: { allowed: [...], blocked: [...] } or {}
		if networkObj, ok := network.(map[string]any); ok {
			frontmatterExtractionSecurityLog.Printf("Network permissions object format with %d fields", len(networkObj))
			permissions := &NetworkPermissions{
				ExplicitlyDefined: true,
			}

			// Extract allowed domains if present
			if allowed, hasAllowed := networkObj["allowed"]; hasAllowed {
				if allowedSlice, ok := allowed.([]any); ok {
					for _, domain := range allowedSlice {
						if domainStr, ok := domain.(string); ok {
							permissions.Allowed = append(permissions.Allowed, domainStr)
						}
					}
					frontmatterExtractionSecurityLog.Printf("Extracted %d allowed domains", len(permissions.Allowed))
				}
			}

			if allowedInput, hasAllowedInput := networkObj["allowed-input"]; hasAllowedInput {
				if allowedInputBool, ok := allowedInput.(bool); ok {
					permissions.AllowedInput = allowedInputBool
				}
			}

			// Extract blocked domains if present
			if blocked, hasBlocked := networkObj["blocked"]; hasBlocked {
				if blockedSlice, ok := blocked.([]any); ok {
					for _, domain := range blockedSlice {
						if domainStr, ok := domain.(string); ok {
							permissions.Blocked = append(permissions.Blocked, domainStr)
						}
					}
					frontmatterExtractionSecurityLog.Printf("Extracted %d blocked domains", len(permissions.Blocked))
				}
			}

			// Empty object {} means no network access (empty allowed list)
			return permissions
		}
	}
	frontmatterExtractionSecurityLog.Print("No network permissions found in frontmatter")
	return nil
}

// extractSandboxConfig extracts sandbox configuration from front matter
func (c *Compiler) extractSandboxConfig(frontmatter map[string]any) *SandboxConfig { //nolint:largefunc // Existing parser handles legacy and current sandbox shapes in one place.
	frontmatterExtractionSecurityLog.Print("Extracting sandbox configuration from frontmatter")

	sandbox, exists := frontmatter["sandbox"]
	if !exists {
		frontmatterExtractionSecurityLog.Print("No sandbox configuration found")
		return nil
	}

	// Handle boolean format: sandbox: false (NO LONGER SUPPORTED)
	// This format has been removed - only sandbox.agent: false is supported
	if _, ok := sandbox.(bool); ok {
		frontmatterExtractionSecurityLog.Print("Top-level sandbox: false is no longer supported")
		// Return nil to trigger schema validation error
		return nil
	}

	// Handle legacy string format: "default" or "awf" (legacy srt/sandbox-runtime are auto-migrated)
	if sandboxStr, ok := sandbox.(string); ok {
		frontmatterExtractionSecurityLog.Printf("Sandbox string format: type=%s", sandboxStr)
		sandboxType := SandboxType(sandboxStr)
		if isSupportedSandboxType(sandboxType) {
			return &SandboxConfig{
				Type: sandboxType,
			}
		}
		// Unknown string format, return nil
		frontmatterExtractionSecurityLog.Printf("Unsupported sandbox type: %s", sandboxStr)
		return nil
	}

	// Handle object format
	sandboxObj, ok := sandbox.(map[string]any)
	if !ok {
		return nil
	}

	frontmatterExtractionSecurityLog.Printf("Sandbox object format with %d fields", len(sandboxObj))

	config := &SandboxConfig{}

	// Check for new format: { agent: ..., mcp: ... }
	if agentVal, hasAgent := sandboxObj["agent"]; hasAgent {
		frontmatterExtractionSecurityLog.Print("Extracting agent sandbox configuration")
		config.Agent = c.extractAgentSandboxConfig(agentVal)
	}

	if mcpVal, hasMCP := sandboxObj["mcp"]; hasMCP {
		frontmatterExtractionSecurityLog.Print("Extracting MCP gateway configuration")
		config.MCP = c.extractMCPGatewayConfig(mcpVal)
	}

	// Agent and MCP select the new sandbox format.
	if config.Agent != nil || config.MCP != nil {
		frontmatterExtractionSecurityLog.Print("Sandbox configured with new format")
		return config
	}

	// Handle legacy object format: { type: "...", config: {...} }
	if typeVal, hasType := sandboxObj["type"]; hasType {
		if typeStr, ok := typeVal.(string); ok {
			config.Type = SandboxType(typeStr)
		}
	}

	// Extract config if present (custom SRT config)
	if configVal, hasConfig := sandboxObj["config"]; hasConfig {
		config.Config = c.extractSRTConfig(configVal)
	}

	return config
}

// extractAgentSandboxConfig extracts agent sandbox configuration
func (c *Compiler) extractAgentSandboxConfig(agentVal any) *AgentSandboxConfig { //nolint:largefunc // Existing parser handles all sandbox.agent fields in one pass.
	// Handle boolean format: agent: false (disables agent sandbox but keeps MCP gateway)
	if agentBool, ok := agentVal.(bool); ok {
		if !agentBool {
			frontmatterExtractionSecurityLog.Print("Agent sandbox explicitly disabled with agent: false")
			return &AgentSandboxConfig{
				Disabled: true,
			}
		}
		// agent: true is not meaningful, treat as unconfigured
		frontmatterExtractionSecurityLog.Print("Agent: true specified but has no effect, treating as unconfigured")
		return nil
	}

	// Handle string format: "awf" or false (legacy srt values are auto-migrated)
	if agentStr, ok := agentVal.(string); ok {
		agentType := SandboxType(agentStr)
		if isSupportedSandboxType(agentType) {
			return &AgentSandboxConfig{
				Type: agentType,
			}
		}
		return nil
	}

	// Handle object format: { id/type: "...", config: {...}, command: "...", args: [...], env: {...} }
	agentObj, ok := agentVal.(map[string]any)
	if !ok {
		return nil
	}

	agentConfig := &AgentSandboxConfig{}

	// Extract ID field (new format)
	if idVal, hasID := agentObj["id"]; hasID {
		if idStr, ok := idVal.(string); ok {
			agentConfig.ID = idStr
		}
	}

	// Extract Type field (legacy format)
	if typeVal, hasType := agentObj["type"]; hasType {
		if typeStr, ok := typeVal.(string); ok {
			agentConfig.Type = SandboxType(typeStr)
		}
	}

	// Extract version (AWF version override)
	if versionVal, hasVersion := agentObj["version"]; hasVersion {
		if versionStr, ok := versionVal.(string); ok {
			agentConfig.Version = versionStr
		}
	}

	// Extract platform (AWF platform.type override)
	if platformVal, hasPlatform := agentObj["platform"]; hasPlatform {
		if platformStr, ok := platformVal.(string); ok {
			agentConfig.Platform = platformStr
		}
	}

	// Extract config for SRT
	if configVal, hasConfig := agentObj["config"]; hasConfig {
		agentConfig.Config = c.extractSRTConfig(configVal)
	}

	// Extract command (custom command to replace AWF binary download)
	if commandVal, hasCommand := agentObj["command"]; hasCommand {
		if commandStr, ok := commandVal.(string); ok {
			agentConfig.Command = commandStr
		}
	}

	// Extract args (additional arguments to append)
	if argsVal, hasArgs := agentObj["args"]; hasArgs {
		if argsSlice, ok := argsVal.([]any); ok {
			for _, arg := range argsSlice {
				if argStr, ok := arg.(string); ok {
					agentConfig.Args = append(agentConfig.Args, argStr)
				}
			}
		}
	}

	// Extract env (environment variables to set on the step)
	if envVal, hasEnv := agentObj["env"]; hasEnv {
		if envObj, ok := envVal.(map[string]any); ok {
			agentConfig.Env = make(map[string]string)
			for key, value := range envObj {
				if valueStr, ok := value.(string); ok {
					agentConfig.Env[key] = valueStr
				}
			}
		}
	}

	// Extract mounts (container mounts for AWF)
	if mountsVal, hasMounts := agentObj["mounts"]; hasMounts {
		if mountsSlice, ok := mountsVal.([]any); ok {
			for _, mount := range mountsSlice {
				if mountStr, ok := mount.(string); ok {
					agentConfig.Mounts = append(agentConfig.Mounts, mountStr)
				}
			}
		}
	}

	// Extract memory (memory limit for the AWF container)
	if memoryVal, hasMemory := agentObj["memory"]; hasMemory {
		if memoryStr, ok := memoryVal.(string); ok {
			agentConfig.Memory = memoryStr
			frontmatterExtractionSecurityLog.Printf("Extracted sandbox.agent.memory: %s", memoryStr)
		}
	}

	// Extract runtime (container runtime for the agent container)
	if runtimeVal, hasRuntime := agentObj["runtime"]; hasRuntime {
		if runtimeStr, ok := runtimeVal.(string); ok {
			agentConfig.Runtime = AgentRuntime(runtimeStr)
			frontmatterExtractionSecurityLog.Printf("Extracted sandbox.agent.runtime: %s", runtimeStr)
		}
	}

	// Extract runtime-install (controls generation of runtime install steps)
	if runtimeInstallVal, hasRuntimeInstall := agentObj["runtime-install"]; hasRuntimeInstall {
		if runtimeInstallBool, ok := runtimeInstallVal.(bool); ok {
			agentConfig.RuntimeInstall = &runtimeInstallBool
			frontmatterExtractionSecurityLog.Printf("Extracted sandbox.agent.runtime-install: %t", runtimeInstallBool)
		}
	}

	// Extract allow-host-ports (additional host TCP ports for the AWF sandbox)
	if portsVal, hasPorts := agentObj["allow-host-ports"]; hasPorts {
		if portsSlice, ok := portsVal.([]any); ok {
			for _, portVal := range portsSlice {
				switch v := portVal.(type) {
				case int:
					agentConfig.AllowHostPorts = append(agentConfig.AllowHostPorts, v)
				case int64:
					agentConfig.AllowHostPorts = append(agentConfig.AllowHostPorts, int(v))
				case uint64:
					agentConfig.AllowHostPorts = append(agentConfig.AllowHostPorts, int(v))
				case float64:
					if float64(int(v)) == v {
						agentConfig.AllowHostPorts = append(agentConfig.AllowHostPorts, int(v))
					}
				}
			}
			frontmatterExtractionSecurityLog.Printf("Extracted sandbox.agent.allow-host-ports: %v", agentConfig.AllowHostPorts)
		}
	}

	// Extract images (digest-pinned AWF infrastructure image manifest)
	if imagesVal, hasImages := agentObj["images"]; hasImages {
		if imagesObj, ok := imagesVal.(map[string]any); ok {
			agentConfig.Images = make(map[string]string, len(imagesObj))
			for role, value := range imagesObj {
				if valueStr, ok := value.(string); ok {
					agentConfig.Images[role] = strings.TrimSpace(valueStr)
					continue
				}
				// Preserve non-string values as their formatted form so validation
				// can report an actionable error instead of silently dropping them.
				agentConfig.Images[role] = fmt.Sprintf("%v", value)
			}
			frontmatterExtractionSecurityLog.Printf("Extracted sandbox.agent.images: %d role(s)", len(agentConfig.Images))
		}
	}

	// Extract model-fallback (AWF API proxy model fallback enable/disable flag)
	if mfVal, hasMF := agentObj["model-fallback"]; hasMF {
		switch v := mfVal.(type) {
		case bool:
			value := TemplatableBool("false")
			if v {
				value = TemplatableBool("true")
			}
			agentConfig.ModelFallback = &value
			frontmatterExtractionSecurityLog.Printf("Extracted sandbox.agent.model-fallback")
		case string:
			if isExpression(v) {
				value := TemplatableBool(v)
				agentConfig.ModelFallback = &value
				frontmatterExtractionSecurityLog.Printf("Extracted sandbox.agent.model-fallback")
			}
		}
	}

	// Extract token-steering (AWF API proxy token steering enable/disable flag).
	if tsVal, hasTS := agentObj["token-steering"]; hasTS {
		if value, ok := tsVal.(bool); ok {
			agentConfig.TokenSteering = &value
			frontmatterExtractionSecurityLog.Print("Extracted sandbox.agent.token-steering")
		}
	}

	// Extract ca-cert (host path to an additional CA certificate for API proxy
	// upstream TLS verification; maps to apiProxy.caCert).
	if caCertVal, hasCACert := agentObj["ca-cert"]; hasCACert {
		if value, ok := caCertVal.(string); ok {
			agentConfig.CACert = strings.TrimSpace(value)
			frontmatterExtractionSecurityLog.Print("Extracted sandbox.agent.ca-cert")
		}
	}

	// Extract targets (per-provider API proxy target overrides, e.g. authHeader, extraHeaders)
	if targetsVal, hasTargets := agentObj["targets"]; hasTargets {
		if targetsObj, ok := targetsVal.(map[string]any); ok {
			agentConfig.Targets = make(map[string]*AgentAPIProxyTargetConfig)
			for provider, targetAny := range targetsObj {
				targetObj, ok := targetAny.(map[string]any)
				if !ok {
					continue
				}
				targetConfig := &AgentAPIProxyTargetConfig{}
				if authHeader, ok := targetObj["authHeader"].(string); ok {
					targetConfig.AuthHeader = authHeader
				}
				if extraHeaders, ok := targetObj["extraHeaders"].(map[string]any); ok {
					targetConfig.ExtraHeaders = make(map[string]string)
					for k, v := range extraHeaders {
						if s, ok := v.(string); ok {
							targetConfig.ExtraHeaders[k] = s
						}
					}
				}
				if extraBodyFields, ok := targetObj["extraBodyFields"].(map[string]any); ok {
					targetConfig.ExtraBodyFields = make(map[string]string)
					for k, v := range extraBodyFields {
						if s, ok := v.(string); ok {
							targetConfig.ExtraBodyFields[k] = s
						}
					}
				}
				if sessionId, ok := targetObj["sessionId"].(string); ok {
					targetConfig.SessionId = sessionId
				}
				agentConfig.Targets[provider] = targetConfig
			}
			frontmatterExtractionSecurityLog.Printf("Extracted sandbox.agent.targets: %d provider(s)", len(agentConfig.Targets))
		}
	}

	return agentConfig
}

// extractMCPGatewayConfig extracts MCP gateway configuration from frontmatter
// Per MCP Gateway Specification v1.0.0: Only container-based execution is supported.
// Direct command execution is not supported.
func (c *Compiler) extractMCPGatewayConfig(mcpVal any) *MCPGatewayRuntimeConfig { //nolint:largefunc // Existing parser handles all sandbox.mcp fields in one pass.
	// Handle nil or boolean false
	if mcpVal == nil {
		return nil
	}
	if mcpBool, ok := mcpVal.(bool); ok && !mcpBool {
		return nil
	}

	// Handle object format: { container: "...", port: ..., args: [...], env: {...} }
	mcpObj, ok := mcpVal.(map[string]any)
	if !ok {
		frontmatterExtractionSecurityLog.Printf("MCP gateway configuration is not an object: %T", mcpVal)
		return nil
	}

	mcpConfig := &MCPGatewayRuntimeConfig{}

	// Extract container (required for MCP gateway)
	if containerVal, hasContainer := mcpObj["container"]; hasContainer {
		if containerStr, ok := containerVal.(string); ok {
			mcpConfig.Container = containerStr
		}
	}

	// Extract version (for container)
	if versionVal, hasVersion := mcpObj["version"]; hasVersion {
		if versionStr, ok := versionVal.(string); ok {
			mcpConfig.Version = versionStr
		}
	}

	// Extract entrypoint (optional container entrypoint override)
	if entrypointVal, hasEntrypoint := mcpObj["entrypoint"]; hasEntrypoint {
		if entrypointStr, ok := entrypointVal.(string); ok {
			mcpConfig.Entrypoint = entrypointStr
		}
	}

	// Extract port
	if portVal, hasPort := mcpObj["port"]; hasPort {
		switch v := portVal.(type) {
		case int:
			mcpConfig.Port = v
		case int64:
			mcpConfig.Port = int(v)
		case uint:
			mcpConfig.Port = int(v)
		case uint64:
			mcpConfig.Port = int(v)
		case float64:
			mcpConfig.Port = int(v)
		}
	}

	// Extract agent ID
	if agentIDVal, hasAgentID := mcpObj["agent-id"]; hasAgentID {
		if agentIDStr, ok := agentIDVal.(string); ok {
			mcpConfig.AgentID = agentIDStr
		}
	}

	// Extract domain
	if domainVal, hasDomain := mcpObj["domain"]; hasDomain {
		if domainStr, ok := domainVal.(string); ok {
			mcpConfig.Domain = domainStr
		}
	}

	// Extract args (additional arguments)
	if argsVal, hasArgs := mcpObj["args"]; hasArgs {
		if argsSlice, ok := argsVal.([]any); ok {
			for _, arg := range argsSlice {
				if argStr, ok := arg.(string); ok {
					mcpConfig.Args = append(mcpConfig.Args, argStr)
				}
			}
		}
	}

	// Extract entrypointArgs (for container only)
	if entrypointArgsVal, hasEntrypointArgs := mcpObj["entrypointArgs"]; hasEntrypointArgs {
		if entrypointArgsSlice, ok := entrypointArgsVal.([]any); ok {
			for _, arg := range entrypointArgsSlice {
				if argStr, ok := arg.(string); ok {
					mcpConfig.EntrypointArgs = append(mcpConfig.EntrypointArgs, argStr)
				}
			}
		}
	}

	// Extract env (environment variables)
	if envVal, hasEnv := mcpObj["env"]; hasEnv {
		if envObj, ok := envVal.(map[string]any); ok {
			mcpConfig.Env = make(map[string]string)
			for key, value := range envObj {
				if valueStr, ok := value.(string); ok {
					mcpConfig.Env[key] = valueStr
				}
			}
		}
	}

	// Extract mounts (volume mounts for container)
	if mountsVal, hasMounts := mcpObj["mounts"]; hasMounts {
		if mountsSlice, ok := mountsVal.([]any); ok {
			for _, mount := range mountsSlice {
				if mountStr, ok := mount.(string); ok {
					mcpConfig.Mounts = append(mcpConfig.Mounts, mountStr)
				}
			}
		}
	}

	// Extract payloadDir / payload-dir (directory for storing large payload JSON files)
	for _, key := range []string{"payloadDir", "payload-dir"} {
		if payloadDirVal, hasPayloadDir := mcpObj[key]; hasPayloadDir {
			if payloadDirStr, ok := payloadDirVal.(string); ok {
				mcpConfig.PayloadDir = payloadDirStr
				break
			}
		}
	}

	// Extract payloadPathPrefix / payload-path-prefix (path prefix to remap payload paths)
	for _, key := range []string{"payloadPathPrefix", "payload-path-prefix"} {
		if payloadPathPrefixVal, hasPayloadPathPrefix := mcpObj[key]; hasPayloadPathPrefix {
			if payloadPathPrefixStr, ok := payloadPathPrefixVal.(string); ok {
				mcpConfig.PayloadPathPrefix = payloadPathPrefixStr
				break
			}
		}
	}

	// Extract payloadSizeThreshold / payload-size-threshold (size threshold in bytes)
	for _, key := range []string{"payloadSizeThreshold", "payload-size-threshold"} {
		if payloadSizeThresholdVal, hasPayloadSizeThreshold := mcpObj[key]; hasPayloadSizeThreshold {
			switch v := payloadSizeThresholdVal.(type) {
			case int:
				mcpConfig.PayloadSizeThreshold = v
			case int64:
				mcpConfig.PayloadSizeThreshold = int(v)
			case uint:
				mcpConfig.PayloadSizeThreshold = int(v)
			case uint64:
				mcpConfig.PayloadSizeThreshold = int(v)
			case float64:
				mcpConfig.PayloadSizeThreshold = int(v)
			}
			if mcpConfig.PayloadSizeThreshold != 0 {
				break
			}
		}
	}

	// Extract trustedBots / trusted-bots (additional bot identities to pass to the gateway)
	for _, key := range []string{"trustedBots", "trusted-bots"} {
		if trustedBotsVal, hasTrustedBots := mcpObj[key]; hasTrustedBots {
			if trustedBotsSlice, ok := trustedBotsVal.([]any); ok {
				for _, bot := range trustedBotsSlice {
					if botStr, ok := bot.(string); ok {
						mcpConfig.TrustedBots = append(mcpConfig.TrustedBots, botStr)
					}
				}
			}
			if len(mcpConfig.TrustedBots) > 0 {
				break
			}
		}
	}

	// Extract keepaliveInterval / keepalive-interval (keepalive ping interval in seconds for HTTP MCP backends)
	// 0 = unset (gateway default: 1500s), -1 = disable keepalive, >0 = custom interval in seconds
	for _, key := range []string{"keepaliveInterval", "keepalive-interval"} {
		if keepaliveVal, hasKeepalive := mcpObj[key]; hasKeepalive {
			switch v := keepaliveVal.(type) {
			case int:
				mcpConfig.KeepaliveInterval = v
			case int64:
				mcpConfig.KeepaliveInterval = int(v)
			case uint:
				mcpConfig.KeepaliveInterval = int(v)
			case uint64:
				mcpConfig.KeepaliveInterval = int(v)
			case float64:
				mcpConfig.KeepaliveInterval = int(v)
			}
			// Break when the key exists (even if value is 0, to avoid picking up a second key variant)
			break
		}
	}

	return mcpConfig
}

// extractSRTConfig extracts Sandbox Runtime configuration from a map
func (c *Compiler) extractSRTConfig(configVal any) *SandboxRuntimeConfig { //nolint:largefunc // Existing SRT config extraction preserves legacy key handling.
	configObj, ok := configVal.(map[string]any)
	if !ok {
		return nil
	}

	srtConfig := &SandboxRuntimeConfig{}

	// Extract network config
	if networkVal, hasNetwork := configObj["network"]; hasNetwork {
		if networkObj, ok := networkVal.(map[string]any); ok {
			netConfig := &SRTNetworkConfig{}

			// Extract allowedDomains
			if allowedDomains, hasAllowed := networkObj["allowedDomains"]; hasAllowed {
				if domainsSlice, ok := allowedDomains.([]any); ok {
					for _, domain := range domainsSlice {
						if domainStr, ok := domain.(string); ok {
							netConfig.AllowedDomains = append(netConfig.AllowedDomains, domainStr)
						}
					}
				}
			}

			// Extract blockedDomains
			if blockedDomains, hasBlocked := networkObj["blockedDomains"]; hasBlocked {
				if domainsSlice, ok := blockedDomains.([]any); ok {
					for _, domain := range domainsSlice {
						if domainStr, ok := domain.(string); ok {
							netConfig.BlockedDomains = append(netConfig.BlockedDomains, domainStr)
						}
					}
				}
			}

			// Extract allowUnixSockets
			if unixSockets, hasUnixSockets := networkObj["allowUnixSockets"]; hasUnixSockets {
				if socketsSlice, ok := unixSockets.([]any); ok {
					for _, socket := range socketsSlice {
						if socketStr, ok := socket.(string); ok {
							netConfig.AllowUnixSockets = append(netConfig.AllowUnixSockets, socketStr)
						}
					}
				}
			}

			// Extract allowLocalBinding
			if allowLocalBinding, hasAllowLocalBinding := networkObj["allowLocalBinding"]; hasAllowLocalBinding {
				if bindingBool, ok := allowLocalBinding.(bool); ok {
					netConfig.AllowLocalBinding = bindingBool
				}
			}

			// Extract allowAllUnixSockets
			if allowAllUnixSockets, hasAllowAllUnixSockets := networkObj["allowAllUnixSockets"]; hasAllowAllUnixSockets {
				if unixSocketsBool, ok := allowAllUnixSockets.(bool); ok {
					netConfig.AllowAllUnixSockets = unixSocketsBool
				}
			}

			srtConfig.Network = netConfig
		}
	}

	// Extract filesystem config
	if filesystemVal, hasFilesystem := configObj["filesystem"]; hasFilesystem {
		if filesystemObj, ok := filesystemVal.(map[string]any); ok {
			fsConfig := &SRTFilesystemConfig{}

			// Extract denyRead
			if denyRead, hasDenyRead := filesystemObj["denyRead"]; hasDenyRead {
				if pathsSlice, ok := denyRead.([]any); ok {
					fsConfig.DenyRead = []string{}
					for _, path := range pathsSlice {
						if pathStr, ok := path.(string); ok {
							fsConfig.DenyRead = append(fsConfig.DenyRead, pathStr)
						}
					}
				}
			}

			// Extract allowWrite
			if allowWrite, hasAllowWrite := filesystemObj["allowWrite"]; hasAllowWrite {
				if pathsSlice, ok := allowWrite.([]any); ok {
					fsConfig.AllowWrite = []string{}
					for _, path := range pathsSlice {
						if pathStr, ok := path.(string); ok {
							fsConfig.AllowWrite = append(fsConfig.AllowWrite, pathStr)
						}
					}
				}
			}

			// Extract denyWrite
			if denyWrite, hasDenyWrite := filesystemObj["denyWrite"]; hasDenyWrite {
				if pathsSlice, ok := denyWrite.([]any); ok {
					fsConfig.DenyWrite = []string{}
					for _, path := range pathsSlice {
						if pathStr, ok := path.(string); ok {
							fsConfig.DenyWrite = append(fsConfig.DenyWrite, pathStr)
						}
					}
				}
			}

			srtConfig.Filesystem = fsConfig
		}
	}

	// Extract ignoreViolations
	if ignoreViolations, hasIgnoreViolations := configObj["ignoreViolations"]; hasIgnoreViolations {
		if violationsObj, ok := ignoreViolations.(map[string]any); ok {
			violations := make(map[string][]string)
			for key, value := range violationsObj {
				if pathsSlice, ok := value.([]any); ok {
					var paths []string
					for _, path := range pathsSlice {
						if pathStr, ok := path.(string); ok {
							paths = append(paths, pathStr)
						}
					}
					violations[key] = paths
				}
			}
			srtConfig.IgnoreViolations = violations
		}
	}

	// Extract enableWeakerNestedSandbox
	if enableWeakerNestedSandbox, hasEnableWeaker := configObj["enableWeakerNestedSandbox"]; hasEnableWeaker {
		if weakerBool, ok := enableWeakerNestedSandbox.(bool); ok {
			srtConfig.EnableWeakerNestedSandbox = weakerBool
		}
	}

	return srtConfig
}
