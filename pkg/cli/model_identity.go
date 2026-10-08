package cli

import (
	"encoding/json"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/workflow"
)

var (
	modelDateSuffix            = regexp.MustCompile(`-[0-9]{4}(?:-[0-9]{2}-[0-9]{2}|[0-9]{4})$`)
	claudeModelVersionSpelling = regexp.MustCompile(`^(claude-.+)-([0-9]+)-([0-9]+)$`)
)

type modelIdentityResolver struct {
	aliases map[string][]string
}

func newModelIdentityResolver(runDir string) *modelIdentityResolver {
	aliases := workflow.BuiltinModelAliases()
	infoPath := findAwInfoPath(runDir)
	if infoPath != "" {
		content, err := os.ReadFile(infoPath)
		if err == nil {
			var info struct {
				Models []struct {
					Model    string   `json:"model"`
					Patterns []string `json:"patterns"`
				} `json:"sub_agent_models"`
			}
			if json.Unmarshal(content, &info) == nil {
				for _, model := range info.Models {
					if model.Model != "" && len(model.Patterns) > 0 {
						aliases[model.Model] = append([]string(nil), model.Patterns...)
					}
				}
			}
		}
	}
	return &modelIdentityResolver{aliases: aliases}
}

func normalizeModelIdentity(model string) string {
	model, _, _ = strings.Cut(strings.ToLower(strings.TrimSpace(model)), "?")
	model = modelDateSuffix.ReplaceAllString(model, "")
	return claudeModelVersionSpelling.ReplaceAllString(model, "$1-$2.$3")
}

func (resolver *modelIdentityResolver) resolve(model, provider string, observed []string) string {
	normalized := normalizeModelIdentity(model)
	patterns := resolver.aliases[normalized]
	if len(patterns) == 0 {
		return normalized
	}
	for _, candidate := range observed {
		for _, pattern := range patterns {
			if modelPatternMatches(pattern, candidate, provider) {
				return normalizeModelIdentity(modelNameWithoutProvider(candidate))
			}
		}
	}
	return normalized
}

func (resolver *modelIdentityResolver) matches(pattern, observed, provider string) bool {
	patterns := resolver.aliases[normalizeModelIdentity(pattern)]
	if len(patterns) == 0 {
		patterns = []string{pattern}
	}
	for _, candidatePattern := range patterns {
		if modelPatternMatches(candidatePattern, observed, provider) {
			return true
		}
	}
	return modelPatternMatches(pattern, observed, provider)
}

func modelPatternMatches(pattern, observed, provider string) bool {
	pattern, _, _ = strings.Cut(strings.ToLower(pattern), "?")
	observed, _, _ = strings.Cut(strings.ToLower(observed), "?")
	patternProvider, patternModel, patternQualified := strings.Cut(pattern, "/")
	observedProvider, observedModel, observedQualified := strings.Cut(observed, "/")
	if patternQualified {
		if provider == "" && observedQualified {
			provider = observedProvider
		}
		if provider != "" && normalizeModelProvider(patternProvider) != normalizeModelProvider(provider) {
			return false
		}
		pattern = patternModel
	}
	if observedQualified {
		if provider != "" && normalizeModelProvider(observedProvider) != normalizeModelProvider(provider) {
			return false
		}
		observed = observedModel
	}
	patterns := []string{pattern, normalizeModelIdentity(pattern)}
	observedModels := []string{observed, normalizeModelIdentity(observed)}
	for _, candidatePattern := range patterns {
		for _, candidate := range observedModels {
			if matched, err := path.Match(candidatePattern, candidate); err == nil && matched {
				return true
			}
		}
	}
	return false
}

func normalizeModelProvider(provider string) string {
	switch provider {
	case "github", "copilot", "github-copilot":
		return "github-copilot"
	case "codex", "openai":
		return "openai"
	case "gemini", "google":
		return "google"
	default:
		return provider
	}
}

func modelNameWithoutProvider(model string) string {
	_, name, qualified := strings.Cut(model, "/")
	if qualified {
		return name
	}
	return model
}
