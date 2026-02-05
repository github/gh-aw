package workflow

import "github.com/github/gh-aw/pkg/logger"

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

		// Handle object format: { allowed: [...], firewall: ... } or {}
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

			// Extract firewall configuration if present
			if firewall, hasFirewall := networkObj["firewall"]; hasFirewall {
				frontmatterExtractionSecurityLog.Print("Extracting firewall configuration")
				permissions.Firewall = c.extractFirewallConfig(firewall)
			}

			// Empty object {} means no network access (empty allowed list)
			return permissions
		}
	}
	frontmatterExtractionSecurityLog.Print("No network permissions found in frontmatter")
	return nil
}

// extractFirewallConfig extracts firewall configuration from various formats
func (c *Compiler) extractFirewallConfig(firewall any) *FirewallConfig {
	// Handle null/empty object format: firewall: or firewall: {}
	if firewall == nil {
		return &FirewallConfig{
			Enabled: true,
		}
	}

	// Handle boolean format: firewall: true or firewall: false
	if firewallBool, ok := firewall.(bool); ok {
		return &FirewallConfig{
			Enabled: firewallBool,
		}
	}

	// Handle string format: firewall: "disable"
	if firewallStr, ok := firewall.(string); ok {
		if firewallStr == "disable" {
			return &FirewallConfig{
				Enabled: false,
			}
		}
		// Unknown string format, return nil
		return nil
	}

	// Handle object format: firewall: { args: [...], version: "..." }
	if firewallObj, ok := firewall.(map[string]any); ok {
		config := &FirewallConfig{
			Enabled: true, // Default to enabled when object is specified
		}

		// Extract args if present
		if args, hasArgs := firewallObj["args"]; hasArgs {
			if argsSlice, ok := args.([]any); ok {
				for _, arg := range argsSlice {
					if argStr, ok := arg.(string); ok {
						config.Args = append(config.Args, argStr)
					}
				}
			}
		}

		// Extract version if present
		if version, hasVersion := firewallObj["version"]; hasVersion {
			if versionStr, ok := version.(string); ok {
				config.Version = versionStr
			}
		}

		// Extract log-level if present
		if logLevel, hasLogLevel := firewallObj["log-level"]; hasLogLevel {
			if logLevelStr, ok := logLevel.(string); ok {
				config.LogLevel = logLevelStr
			}
		}

		// Extract ssl-bump if present
		if sslBump, hasSslBump := firewallObj["ssl-bump"]; hasSslBump {
			if sslBumpBool, ok := sslBump.(bool); ok {
				config.SSLBump = sslBumpBool
			}
		}

		// Extract allow-urls if present
		if allowUrls, hasAllowUrls := firewallObj["allow-urls"]; hasAllowUrls {
			if urlsSlice, ok := allowUrls.([]any); ok {
				for _, url := range urlsSlice {
					if urlStr, ok := url.(string); ok {
						config.AllowURLs = append(config.AllowURLs, urlStr)
					}
				}
			}
		}

		return config
	}

	return nil
}

// extractSandboxConfig extracts sandbox configuration from front matter
func (c *Compiler) extractSandboxConfig(frontmatter map[string]any) *SandboxConfig {
	frontmatterExtractionSecurityLog.Print("Extracting sandbox configuration from frontmatter")

	sandbox, exists := frontmatter["sandbox"]
	if !exists {
		frontmatterExtractionSecurityLog.Print("No sandbox configuration found")
		return nil
	}

	// Handle boolean format: sandbox: false (disables all sandbox features)
	if sandboxBool, ok := sandbox.(bool); ok {
		if !sandboxBool {
			frontmatterExtractionSecurityLog.Print("Sandbox explicitly disabled with sandbox: false")
			// Return a marker config with Disabled flag set
			return &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Disabled: true,
				},
			}
		}
		// sandbox: true is not meaningful, treat as no configuration
		frontmatterExtractionSecurityLog.Print("Sandbox: true specified but has no effect, treating as unconfigured")
		return nil
	}

	// Handle legacy string format: "default" or "sandbox-runtime"
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

		// Check if agent is set to false (boolean) - this is no longer supported
		if agentBool, ok := agentVal.(bool); ok && !agentBool {
			// Return a marker config that will be caught during validation
			return &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Disabled: true, // This will be caught by validation
				},
			}
		}

		config.Agent = c.extractAgentSandboxConfig(agentVal)
	}

	if mcpVal, hasMCP := sandboxObj["mcp"]; hasMCP {
		frontmatterExtractionSecurityLog.Print("Extracting MCP gateway configuration")
		config.MCP = c.extractMCPGatewayConfig(mcpVal)
	}

	// If we found agent field, return the new format config
	if config.Agent != nil {
		frontmatterExtractionSecurityLog.Print("Sandbox configured with new format (agent)")
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
func (c *Compiler) extractAgentSandboxConfig(agentVal any) *AgentSandboxConfig {
	// Handle boolean format: REJECTED - sandbox.agent: false is no longer supported
	if _, ok := agentVal.(bool); ok {
		// Both true and false are invalid - must use string or object format
		return nil
	}

	// Handle string format: "awf" or "srt"
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

	return agentConfig
}

// extractMCPGatewayConfig extracts MCP gateway configuration from frontmatter
// Per MCP Gateway Specification v1.0.0: Only container-based execution is supported.
// Direct command execution is not supported.
func (c *Compiler) extractMCPGatewayConfig(mcpVal any) *MCPGatewayRuntimeConfig {
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

	// Extract apiKey
	if apiKeyVal, hasAPIKey := mcpObj["api-key"]; hasAPIKey {
		if apiKeyStr, ok := apiKeyVal.(string); ok {
			mcpConfig.APIKey = apiKeyStr
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

	return mcpConfig
}

// extractSRTConfig extracts Sandbox Runtime configuration from a map
func (c *Compiler) extractSRTConfig(configVal any) *SandboxRuntimeConfig {
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
