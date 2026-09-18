package cli

import "github.com/github/gh-aw/pkg/logger"

var aicCacheSemanticsLog = logger.New("cli:aic_cache_semantics")

func providerIncludesCacheReadsInInput(normalizedProvider string) bool {
	// Cache read accounting is provider-specific:
	// - bundled semantics: cache_read_tokens are already included in input_tokens,
	//   so we subtract once before applying input weight.
	// - additive semantics: cache_read_tokens are separate from input_tokens,
	//   so no subtraction is applied.
	//
	// Known providers currently using bundled semantics are listed below.
	// Unknown non-empty providers default to additive semantics to avoid
	// under-counting input tokens. Empty provider values are treated as bundled
	// semantics for backward compatibility with older usage records that omitted
	// the provider field.
	// We include both "azure-openai" and "azure_openai" to handle observed
	// provider naming variants in historical logs.
	// Callers should pass the catalog-normalized provider so canonical aliases like
	// "github", "copilot", and "github_models" collapse to "github-copilot"
	// before this check.
	switch normalizedProvider {
	case "", "anthropic", "openai", "azure-openai", "azure_openai", "github-copilot":
		aicCacheSemanticsLog.Printf("provider %q uses bundled cache-read semantics (cache reads included in input; subtracting once)", normalizedProvider)
		return true
	default:
		aicCacheSemanticsLog.Printf("provider %q uses additive cache-read semantics (cache reads counted separately from input)", normalizedProvider)
		return false
	}
}
