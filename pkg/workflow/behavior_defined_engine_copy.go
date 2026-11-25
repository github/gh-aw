package workflow

import (
	"maps"
	"slices"
)

// deepCopyAny returns a fully independent copy of v for values produced by
// yaml.Unmarshal into interface{}. The possible concrete types are:
// nil, bool, int, float64, string, []any, and map[string]any.
func deepCopyAny(v any) any {
	switch val := v.(type) {
	case map[string]any:
		cp := make(map[string]any, len(val))
		for k, elem := range val {
			cp[k] = deepCopyAny(elem)
		}
		return cp
	case []any:
		cp := make([]any, 0, len(val))
		for _, elem := range val {
			cp = append(cp, deepCopyAny(elem))
		}
		return cp
	default:
		// Scalars (nil, bool, int, float64, string) are immutable value types.
		return v
	}
}

// deepCopyEngineDefinition returns a fully independent copy of src. All reference
// types (pointers, slices, maps) are recursively copied so that neither the caller
// nor the cache can corrupt the other's state through shared references.
func deepCopyEngineDefinition(src EngineDefinition) EngineDefinition {
	dst := src // value copy covers all scalar fields

	// Models.Supported
	if src.Models.Supported != nil {
		dst.Models.Supported = make([]string, len(src.Models.Supported))
		copy(dst.Models.Supported, src.Models.Supported)
	}

	// Auth ([]AuthBinding; elements contain only string fields so element copy suffices)
	if src.Auth != nil {
		dst.Auth = make([]AuthBinding, len(src.Auth))
		copy(dst.Auth, src.Auth)
	}

	// Options (map[string]any; values may contain nested maps or slices from YAML unmarshal)
	if src.Options != nil {
		dst.Options = make(map[string]any, len(src.Options))
		for k, v := range src.Options {
			dst.Options[k] = deepCopyAny(v)
		}
	}

	// Provider.Auth
	if src.Provider.Auth != nil {
		authCopy := *src.Provider.Auth // AuthDefinition contains only string fields
		dst.Provider.Auth = &authCopy
	}

	// Provider.Request
	if src.Provider.Request != nil {
		reqCopy := *src.Provider.Request
		if src.Provider.Request.Query != nil {
			reqCopy.Query = make(map[string]string, len(src.Provider.Request.Query))
			maps.Copy(reqCopy.Query, src.Provider.Request.Query)
		}
		if src.Provider.Request.BodyInject != nil {
			reqCopy.BodyInject = make(map[string]string, len(src.Provider.Request.BodyInject))
			maps.Copy(reqCopy.BodyInject, src.Provider.Request.BodyInject)
		}
		dst.Provider.Request = &reqCopy
	}

	// Behaviors
	if src.Behaviors != nil {
		behaviorsCopy := deepCopyEngineBehaviorDefinition(*src.Behaviors)
		dst.Behaviors = &behaviorsCopy
	}

	return dst
}

// deepCopyEngineBehaviorDefinition returns a fully independent copy of src.
//
//nolint:largefunc // Deep copy initializes all fields explicitly.
func deepCopyEngineBehaviorDefinition(src EngineBehaviorDefinition) EngineBehaviorDefinition {
	dst := src // value copy covers all scalar fields
	if src.Durable != nil {
		durable := *src.Durable
		durable.Installation = append([]EngineInstallationDefinition(nil), src.Durable.Installation...)
		durable.Files = append([]string(nil), src.Durable.Files...)
		for i := range durable.Installation {
			if cooldown := durable.Installation[i].Cooldown; cooldown != nil {
				copy := *cooldown
				durable.Installation[i].Cooldown = &copy
			}
		}
		copy := deepCopyEngineBehaviorDefinition(EngineBehaviorDefinition{Execution: src.Durable.Execution})
		durable.Execution = copy.Execution
		dst.Durable = &durable
	}

	// SupportedEnvVarKeys
	if src.SupportedEnvVarKeys != nil {
		dst.SupportedEnvVarKeys = make([]string, len(src.SupportedEnvVarKeys))
		copy(dst.SupportedEnvVarKeys, src.SupportedEnvVarKeys)
	}

	// Manifest
	if src.Manifest != nil {
		manifestCopy := *src.Manifest
		if src.Manifest.Files != nil {
			manifestCopy.Files = make([]string, len(src.Manifest.Files))
			copy(manifestCopy.Files, src.Manifest.Files)
		}
		if src.Manifest.PathPrefixes != nil {
			manifestCopy.PathPrefixes = make([]string, len(src.Manifest.PathPrefixes))
			copy(manifestCopy.PathPrefixes, src.Manifest.PathPrefixes)
		}
		dst.Manifest = &manifestCopy
	}

	// Network (has Defaults []string and ProviderDomains map[string]string)
	if src.Network != nil {
		networkCopy := *src.Network
		if src.Network.Defaults != nil {
			networkCopy.Defaults = make([]string, len(src.Network.Defaults))
			copy(networkCopy.Defaults, src.Network.Defaults)
		}
		if src.Network.ProviderDomains != nil {
			networkCopy.ProviderDomains = make(map[string]string, len(src.Network.ProviderDomains))
			maps.Copy(networkCopy.ProviderDomains, src.Network.ProviderDomains)
		}
		dst.Network = &networkCopy
	}

	if src.Installation != nil {
		installCopy := *src.Installation
		installCopy.Requirements = slices.Clone(src.Installation.Requirements)
		dst.Installation = &installCopy
	}

	// ConfigFile (only scalar fields)
	if src.ConfigFile != nil {
		cfCopy := *src.ConfigFile
		dst.ConfigFile = &cfCopy
	}

	// Execution (has Args []string and Env map[string]string)
	if src.Execution != nil {
		execCopy := *src.Execution
		if src.Execution.Args != nil {
			execCopy.Args = make([]string, len(src.Execution.Args))
			copy(execCopy.Args, src.Execution.Args)
		}
		if src.Execution.Env != nil {
			execCopy.Env = make(map[string]string, len(src.Execution.Env))
			maps.Copy(execCopy.Env, src.Execution.Env)
		}
		dst.Execution = &execCopy
	}

	// MCP (only scalar fields)
	if src.MCP != nil {
		mcpCopy := *src.MCP
		dst.MCP = &mcpCopy
	}

	return dst
}
