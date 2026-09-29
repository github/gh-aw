// Package actionpins provides action pin resolution for GitHub Actions,
// mapping repository references to their pinned commit SHAs.
// It is intentionally free of dependencies on pkg/workflow so it can be
// imported by any package without introducing import cycles.
package actionpins

import "context"

// ActionYAMLInput holds an input definition parsed from a GitHub Action's action.yml.
type ActionYAMLInput struct {
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Required    bool   `yaml:"required,omitempty"    json:"required,omitempty"`
	Default     string `yaml:"default,omitempty"     json:"default,omitempty"`
}

// ActionPin represents a pinned GitHub Action with its commit SHA.
type ActionPin struct {
	Repo    string                      `json:"repo"`
	Version string                      `json:"version"`
	SHA     string                      `json:"sha"`
	Inputs  map[string]*ActionYAMLInput `json:"inputs,omitempty"`
}

// ContainerPin represents a pinned container image reference.
type ContainerPin struct {
	Image       string `json:"image"`
	Digest      string `json:"digest"`
	PinnedImage string `json:"pinned_image"`
}

// ActionPinsData represents the structure of the embedded JSON file.
type ActionPinsData struct {
	Entries    map[string]ActionPin    `json:"entries"`
	Containers map[string]ContainerPin `json:"containers,omitempty"`
}

// SHAResolver resolves a GitHub Action's commit SHA for a given version tag.
type SHAResolver interface {
	ResolveSHA(ctx context.Context, repo, version string) (string, error)
}

// ResolutionErrorType classifies unresolved action-ref pinning outcomes for auditing.
type ResolutionErrorType string

const (
	// ResolutionErrorTypeDynamicResolutionFailed indicates dynamic tag/ref -> SHA resolution failed.
	ResolutionErrorTypeDynamicResolutionFailed ResolutionErrorType = "dynamic_resolution_failed"
	// ResolutionErrorTypePinNotFound indicates no usable hardcoded pin was found for the ref.
	ResolutionErrorTypePinNotFound ResolutionErrorType = "pin_not_found"
)

// ResolutionFailure captures an unresolved action-ref pinning event.
type ResolutionFailure struct {
	Repo      string
	Ref       string
	ErrorType ResolutionErrorType
}

// PinContext provides the runtime context needed for action pin resolution.
// Callers construct one from their own state (e.g. WorkflowData fields).
// The Warnings map is mutated in place to deduplicate warning output.
type PinContext struct {
	// Ctx is the context to propagate into dynamic SHA resolution calls.
	// When nil, context.Background() is used as a fallback.
	Ctx context.Context
	// Resolver resolves SHAs dynamically via GitHub CLI. May be nil.
	Resolver SHAResolver
	// StrictMode controls how resolution failures are handled.
	StrictMode bool
	// EnforcePinned requires unresolved refs to fail unless AllowActionRefs is true.
	EnforcePinned bool
	// AllowActionRefs lowers unresolved pinning failures to warnings.
	// When false, unresolved action refs return an error.
	AllowActionRefs bool
	// GHES selects action versions compatible with GitHub Enterprise Server.
	GHES bool
	// Warnings is a shared map for deduplicating warning messages.
	// Keys are cache keys in the form "repo@version".
	Warnings map[string]bool
	// SourceVersions holds inline version comments for already-pinned steps, keyed by repo@SHA.
	SourceVersions map[string]string
	// RecordResolutionFailure receives unresolved pinning failures for auditing.
	RecordResolutionFailure func(f ResolutionFailure)
	// SkipHardcodedFallback skips version→SHA hardcoded fallback when dynamic
	// resolution fails. Exact SHA→version labeling is still allowed so
	// already-pinned actions keep their human-readable version comments. Set this
	// when GH_HOST is configured to a non-github.com host: the dynamic resolver
	// will query the wrong host and fail, so silently falling back to bundled pins
	// would produce unverified SHA pins and mask the real misconfiguration.
	SkipHardcodedFallback bool
	// Mappings redirects action repository@version references to replacement
	// repository@version references before pin resolution. Keys and values use
	// the format "owner/repo@ref". Set from aw.json action_pins.
	Mappings map[string]string
	// ContainerMappings redirects container image references to replacement
	// image references before pin resolution. Keys are source image references
	// (e.g. "ghcr.io/owner/image:tag") and values are replacement image
	// references. Set from aw.json container_pins.
	ContainerMappings map[string]string
}
