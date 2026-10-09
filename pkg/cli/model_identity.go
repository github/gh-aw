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
	patterns := resolver.patternsFor(normalized)
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
	patterns := resolver.patternsFor(normalizeModelIdentity(pattern))
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

func (resolver *modelIdentityResolver) patternsFor(model string) []string {
	var patterns []string
	visited := make(map[string]struct{})
	var expand func(string)
	expand = func(alias string) {
		alias = normalizeModelIdentity(alias)
		if _, ok := visited[alias]; ok {
			return
		}
		visited[alias] = struct{}{}
		for _, pattern := range resolver.aliases[alias] {
			nested := normalizeModelIdentity(pattern)
			if !strings.ContainsAny(pattern, "/*?") && len(resolver.aliases[nested]) > 0 {
				expand(nested)
				continue
			}
			patterns = append(patterns, pattern)
		}
	}
	expand(model)
	return patterns
}

func modelPatternMatches(pattern, observed, provider string) bool {
	pattern, _, _ = strings.Cut(strings.ToLower(pattern), "?")
	observed, _, _ = strings.Cut(strings.ToLower(observed), "?")
	originalObserved := observed
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
			return modelPatternMatches(patternModel, originalObserved, "")
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
