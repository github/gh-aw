// This file resolves engine.model-routing.allowed-models into the router's
// candidate pool: gh-aw aliases are expanded to model patterns in the routing
// namespace (GitHub Copilot, or the engine's native Anthropic or OpenAI provider)
// and the result is narrowed by models.allowed and models.blocked.

package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"sync"

	clidata "github.com/github/gh-aw/pkg/cli/data"
	"github.com/github/gh-aw/pkg/logger"
)

var modelRoutingCandidatesLog = logger.New("workflow:model_routing_candidates")

// copilotRoutingNamespace is the provider namespace of GitHub Copilot routing candidates.
const copilotRoutingNamespace = "github-copilot"

// modelRoutingCandidateGroup is one authored allowed-models entry and the
// candidates in the routing namespace it resolves to.
type modelRoutingCandidateGroup struct {
	// Name is the authored entry without its routing-namespace provider prefix.
	Name string
	// Alias reports that Name is a gh-aw model alias.
	Alias bool
	// Patterns are the provider-qualified candidates: an exact id, an expression, or the alias's patterns.
	Patterns []string
}

// modelRoutingEngineNativeProviders names the alias-table provider whose models
// share the wire API of engines that are limited to one model family.
var modelRoutingEngineNativeProviders = map[string]string{
	"claude": "anthropic/",
	"codex":  "openai/",
}

func modelRoutingAliasMap(data *WorkflowData) map[string][]string {
	if data != nil && len(data.ModelMappings) > 0 {
		return data.ModelMappings
	}
	return getBuiltinOnlyAliasMap()
}

// bundledProviderModelIDs returns the lower-cased model ids of each provider in gh-aw's
// bundled model catalogue (pkg/cli/data/models.json).
var bundledProviderModelIDs = sync.OnceValue(func() map[string]map[string]struct{} {
	var catalog struct {
		Providers map[string]struct {
			Models map[string]json.RawMessage `json:"models"`
		} `json:"providers"`
	}
	ids := make(map[string]map[string]struct{})
	if err := json.Unmarshal(clidata.ModelsJSON, &catalog); err != nil {
		return ids
	}
	for provider, entry := range catalog.Providers {
		ids[provider] = make(map[string]struct{}, len(entry.Models))
		for id := range entry.Models {
			ids[provider][strings.ToLower(id)] = struct{}{}
		}
	}
	return ids
})

// bundledCopilotModelIDs returns the lower-cased github-copilot model ids of the bundled catalogue.
func bundledCopilotModelIDs() map[string]struct{} {
	return bundledProviderModelIDs()[copilotRoutingNamespace]
}

func isBundledCopilotModelID(model string) bool {
	return isBundledProviderModelID(copilotRoutingNamespace, model)
}

func isBundledProviderModelID(provider, model string) bool {
	_, ok := bundledProviderModelIDs()[provider][strings.ToLower(model)]
	return ok
}

func bundledProviderModelMatches(provider, pattern string) bool {
	pattern = strings.ToLower(pattern)
	for id := range bundledProviderModelIDs()[provider] {
		if matched, err := path.Match(pattern, id); err == nil && matched {
			return true
		}
	}
	return false
}

// resolveModelRoutingCandidateGroups resolves each engine.model-routing.allowed-models
// entry in the routing namespace: github-copilot, or the engine's native provider
// (anthropic for Claude, openai for Codex). An entry with the namespace's provider
// prefix (github-copilot/ or copilot/ for Copilot), or a model id of the namespace in
// the bundled catalogue, is exact, even when an alias has the same name; this matches
// resolveConfiguredModel, where a live model id wins over an alias. A native entry
// with another provider's prefix is rejected rather than retargeted. Other aliases
// expand through the workflow's alias map to patterns in the namespace, limited to
// the engine's model family for Claude and Codex. Remaining entries must be GitHub
// Actions expressions or plausible exact model ids; they are not checked against a
// live catalogue.
func resolveModelRoutingCandidateGroups(data *WorkflowData, engineName string) ([]modelRoutingCandidateGroup, error) {
	if !isModelRoutingEnabled(data) {
		return nil, nil
	}
	routing := data.EngineConfig.ModelRouting
	if routing.AllowedModels == nil {
		return nil, nil
	}
	namespace := modelRoutingNamespace(data, engineName)
	modelLabel := "Copilot model"
	if namespace != copilotRoutingNamespace {
		modelLabel = namespace + " model"
	}
	if len(routing.AllowedModels) == 0 {
		return nil, fmt.Errorf("engine.model-routing.allowed-models must contain at least one %s", modelLabel)
	}
	modelRoutingCandidatesLog.Printf("Resolving %d allowed-models entries for engine=%s namespace=%s", len(routing.AllowedModels), engineName, namespace)
	aliases := modelRoutingAliasMap(data)
	groups := make([]modelRoutingCandidateGroup, 0, len(routing.AllowedModels))
	seen := make(map[string]struct{}, len(routing.AllowedModels))
	for _, model := range routing.AllowedModels {
		model = strings.TrimSpace(model)
		unprefixed, prefixed, err := stripModelRoutingNamespacePrefix(model, namespace)
		if err != nil {
			return nil, err
		}
		model = unprefixed
		if !validModelRoutingCandidate(model) {
			return nil, fmt.Errorf("engine.model-routing.allowed-models contains invalid %s %q", modelLabel, model)
		}
		exact := prefixed || containsExpression(model) || isBundledProviderModelID(namespace, model)
		key := model
		if !exact && aliases[model] != nil {
			key = "alias:" + model
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		group := modelRoutingCandidateGroup{Name: model}
		switch {
		case exact:
			group.Patterns = []string{qualifiedModelID(namespace, model)}
		case aliases[model] != nil:
			patterns, err := expandModelRoutingAliasInNamespace(model, aliases, engineName, namespace)
			if err != nil {
				return nil, err
			}
			group.Alias = true
			group.Patterns = patterns
		case plausibleConcreteModelID(model):
			group.Patterns = []string{qualifiedModelID(namespace, model)}
		default:
			return nil, unknownModelRoutingEntryError(model, namespace, aliases)
		}
		groups = append(groups, group)
	}
	return groups, nil
}

// unknownModelRoutingEntryError reports an allowed-models entry that is neither an
// alias nor a plausible exact model id, with an example for the routing namespace.
func unknownModelRoutingEntryError(model, namespace string, aliases map[string][]string) error {
	names := make([]string, 0, len(aliases))
	for name := range aliases {
		names = append(names, name)
	}
	slices.Sort(names)
	example := "an exact Copilot model id such as gpt-5.6-luna"
	switch namespace {
	case string(LLMProviderAnthropic):
		example = "an exact Anthropic model id such as claude-sonnet-5"
	case string(LLMProviderOpenAI):
		example = "an exact OpenAI model id such as gpt-5.4"
	}
	return fmt.Errorf("engine.model-routing.allowed-models contains %q, which is neither a gh-aw model alias nor an exact model id. Use %s, or one of the aliases: %s", model, example, strings.Join(names, ", "))
}

// stripModelRoutingNamespacePrefix removes the routing namespace's provider prefix from
// an allowed-models entry and reports whether it had one. Copilot routing accepts the
// github-copilot/ and copilot/ prefixes. Native routing accepts its own provider prefix
// and rejects any other provider, so a mismatched entry is never silently retargeted.
// A literal provider prefix before a GitHub Actions expression (for example
// anthropic/${{ vars.MODEL }}) is handled the same way; slashes inside expressions are
// ignored, and an entry whose provider is itself an expression is left unchanged.
func stripModelRoutingNamespacePrefix(model, namespace string) (string, bool, error) {
	if namespace == copilotRoutingNamespace {
		unprefixed := strings.TrimPrefix(model, "github-copilot/")
		unprefixed = strings.TrimPrefix(unprefixed, "copilot/")
		return unprefixed, unprefixed != model, nil
	}
	masked := ExpressionPattern.ReplaceAllStringFunc(model, func(expression string) string {
		return strings.Repeat("x", len(expression))
	})
	slash := strings.IndexByte(masked, '/')
	if slash < 0 {
		return model, false, nil
	}
	provider, name := model[:slash], model[slash+1:]
	if containsExpression(provider) {
		return model, false, nil
	}
	if strings.EqualFold(provider, namespace) {
		return name, true, nil
	}
	return "", false, fmt.Errorf("engine.model-routing.allowed-models contains %q, whose provider %q differs from the native %q provider that this workflow routes on; AWF native routing does not cross providers. Use a bare model id or the %s/ prefix", model, provider, namespace, namespace)
}

// plausibleConcreteModelID reports whether model looks like an exact model id rather than
// a misspelt alias: model ids carry a version number or a hyphenated family name.
func plausibleConcreteModelID(model string) bool {
	return routingModelNamePattern.MatchString(model) && strings.ContainsAny(model, "0123456789-")
}

// expandModelRoutingAlias returns the github-copilot/ patterns of alias for GitHub
// Copilot routing.
func expandModelRoutingAlias(alias string, aliases map[string][]string, engineName string) ([]string, error) {
	return expandModelRoutingAliasInNamespace(alias, aliases, engineName, copilotRoutingNamespace)
}

// expandModelRoutingAliasInNamespace returns the patterns of alias in the routing
// namespace (github-copilot, anthropic or openai). Routing never crosses providers, so
// other providers' patterns are dropped. For Claude and Codex, only patterns in the
// engine's model family are kept. A pattern that does not start with the family, from
// an alias that also lists the engine's native provider, is intersected with the
// family (for example *sonnet* becomes claude-*sonnet*, and *claude-sonnet* becomes
// claude-sonnet*); an intersection that matches no model of the namespace in the
// bundled catalogue is dropped rather than emitted as a pattern that only fails at
// run time.
func expandModelRoutingAliasInNamespace(alias string, aliases map[string][]string, engineName, namespace string) ([]string, error) {
	engineName = strings.ToLower(engineName)
	familyPrefix, familyLimited := modelRoutingEngineFamilyPrefixes[engineName]
	nativeProvider := modelRoutingEngineNativeProviders[engineName]
	var patterns []string
	walkModelPatterns(alias, aliases, namespace, func(pattern, source string) {
		name, ok := strings.CutPrefix(pattern, namespace+"/")
		if !ok || name == "" {
			return
		}
		names := []string{name}
		if familyLimited && !strings.HasPrefix(strings.ToLower(name), familyPrefix) {
			if !slices.ContainsFunc(aliases[source], func(entry string) bool {
				return strings.HasPrefix(entry, nativeProvider)
			}) {
				return
			}
			names = slices.DeleteFunc(intersectModelPatternWithFamily(name, familyPrefix), func(narrowed string) bool {
				return !bundledProviderModelMatches(namespace, narrowed)
			})
		}
		for _, name := range names {
			patterns = appendUnique(patterns, qualifiedModelID(namespace, name))
		}
	})
	if len(patterns) > 0 {
		return patterns, nil
	}
	switch engineName {
	case "claude":
		return nil, fmt.Errorf("alias %q has no models compatible with Claude's native Messages API; allow only Claude models", alias)
	case "codex":
		return nil, fmt.Errorf("alias %q has no models compatible with Codex's Responses API; allow only GPT models", alias)
	default:
		return nil, fmt.Errorf("alias %q has no GitHub Copilot models", alias)
	}
}

// intersectModelPatternWithFamily returns path.Match patterns that together match the
// model names that match pattern and start with the literal family prefix. Letters
// are compared case-insensitively. Patterns with character classes or escapes yield
// nothing.
func intersectModelPatternWithFamily(pattern, family string) []string {
	if strings.ContainsAny(pattern, "[\\") {
		return nil
	}
	// rest returns the patterns that match r exactly when p matches s+r.
	var rest func(p, s string) []string
	rest = func(p, s string) []string {
		switch {
		case s == "":
			return []string{p}
		case p == "":
			return nil
		case strings.HasPrefix(p, "*"):
			return append(rest(p[1:], s), rest(p, s[1:])...)
		case strings.HasPrefix(p, "?") || strings.EqualFold(p[:1], s[:1]):
			return rest(p[1:], s[1:])
		default:
			return nil
		}
	}
	var narrowed []string
	for _, suffix := range rest(pattern, family) {
		narrowed = appendUnique(narrowed, family+suffix)
	}
	return narrowed
}

// intersectModelRoutingPolicy narrows the candidate groups by models.allowed and
// models.blocked and returns the candidates with the authored entries that policy
// excludes entirely. A candidate pattern is dropped when a blocked rule covers it;
// AWF still applies partial blocks to the live catalogue. With models.allowed, a
// pattern is kept when an allowed rule covers it and otherwise narrows to the allowed
// rules it covers. When exact is false, AWF intersects routing.candidateModels with the
// policy at run time, so a pattern that only partly overlaps an allowed rule is kept;
// when exact is true, the candidates replace models.allowed and such a pattern is dropped
// rather than widening the policy.
func intersectModelRoutingPolicy(groups []modelRoutingCandidateGroup, allowed, blocked []string, exact bool) ([]string, []string, error) {
	var result, excluded []string
	for _, group := range groups {
		kept := 0
		for _, candidate := range group.Patterns {
			if containsExpression(candidate) {
				if len(allowed) > 0 || len(blocked) > 0 {
					return nil, nil, errors.New("GitHub Actions expressions in engine.model-routing.allowed-models cannot be checked against models.allowed or models.blocked")
				}
				result = appendUnique(result, candidate)
				kept++
				continue
			}
			for _, narrowed := range narrowModelRoutingCandidate(candidate, allowed, blocked, exact) {
				result = appendUnique(result, narrowed)
				kept++
			}
		}
		if kept == 0 {
			excluded = append(excluded, group.Name)
		}
	}
	if len(excluded) > 0 {
		modelRoutingCandidatesLog.Printf("Excluded %d allowed-models entries not covered by model policy: %v", len(excluded), excluded)
	}
	if len(result) == 0 {
		return nil, nil, fmt.Errorf("all engine.model-routing.allowed-models are excluded by models.allowed or models.blocked policy (excluded: %s)", strings.Join(excluded, ", "))
	}
	return result, excluded, nil
}

func appendUnique(values []string, value string) []string {
	if slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}

func narrowModelRoutingCandidate(candidate string, allowed, blocked []string, exact bool) []string {
	if modelPolicyRulesCover(blocked, candidate) {
		return nil
	}
	if len(allowed) == 0 || modelPolicyRulesCover(allowed, candidate) {
		return []string{candidate}
	}
	namespace := modelRoutingCandidateNamespace(candidate)
	var narrowed []string
	partial := false
	for _, rule := range allowed {
		qualified, ok := qualifiedModelPolicyRule(rule, namespace)
		if !ok {
			continue
		}
		if modelPatternCovers(candidate, qualified) {
			if !modelPolicyRulesCover(blocked, qualified) {
				narrowed = appendUnique(narrowed, qualified)
			}
		} else if modelPatternsMayOverlap(candidate, qualified) {
			partial = true
		}
	}
	if partial && !exact {
		return []string{candidate}
	}
	return narrowed
}

// copilotModelPolicyProviders are the provider names that AWF matches against GitHub
// Copilot models (getProviderAliases in gh-aw-firewall).
var copilotModelPolicyProviders = []string{"copilot", "github-copilot", "github"}

// qualifiedModelID returns model qualified with the routing namespace (provider).
func qualifiedModelID(namespace, model string) string {
	return namespace + "/" + model //nolint:manualpathconcat // provider-qualified model IDs are not filesystem paths
}

// qualifiedModelPolicyRule returns rule qualified with the routing namespace, or false
// when its provider cannot match models of that namespace. A bare rule applies to every
// provider, as in AWF. For github-copilot the provider may be any GitHub Copilot
// provider name; a native namespace (anthropic or openai) matches only its own name, so
// a github-copilot/ rule never authorizes a native model and vice versa.
func qualifiedModelPolicyRule(rule, namespace string) (string, bool) {
	provider, model := splitModelPolicyRule(rule)
	names := []string{strings.ToLower(namespace)}
	if namespace == copilotRoutingNamespace {
		names = copilotModelPolicyProviders
	}
	if provider != "" && !slices.ContainsFunc(names, func(name string) bool {
		matched, err := path.Match(strings.ToLower(provider), name)
		return err == nil && matched
	}) {
		return "", false
	}
	return qualifiedModelID(namespace, model), true
}

// modelRoutingCandidateNamespace returns the provider namespace of a provider-qualified
// candidate pattern, defaulting to github-copilot.
func modelRoutingCandidateNamespace(candidate string) string {
	if provider, _, ok := strings.Cut(candidate, "/"); ok && provider != "" {
		return provider
	}
	return copilotRoutingNamespace
}

// modelPolicyRulesCover reports whether some rule matches every model that candidate
// matches. Rules are qualified with the candidate's provider namespace.
func modelPolicyRulesCover(rules []string, candidate string) bool {
	namespace := modelRoutingCandidateNamespace(candidate)
	for _, rule := range rules {
		if qualified, ok := qualifiedModelPolicyRule(rule, namespace); ok && modelPatternCovers(qualified, candidate) {
			return true
		}
	}
	return false
}

// modelPatternCovers reports whether every model matched by specific is matched by general.
// Both are provider-qualified path.Match patterns, compared case-insensitively as AWF does.
// Only * wildcards in general are compared against a wildcard specific; other wildcards
// conservatively do not cover it.
func modelPatternCovers(general, specific string) bool {
	general, specific = strings.ToLower(general), strings.ToLower(specific)
	if general == specific {
		return true
	}
	if strings.ContainsAny(specific, "*?[\\") && strings.ContainsAny(general, "?[\\") {
		return false
	}
	matched, err := path.Match(general, specific)
	return err == nil && matched
}

// modelPatternsMayOverlap reports whether some model can match both path.Match patterns,
// compared case-insensitively. It is exact for * and ? wildcards and conservatively true
// for character classes and escapes.
func modelPatternsMayOverlap(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	if strings.ContainsAny(a, "[\\") || strings.ContainsAny(b, "[\\") {
		return true
	}
	at := func(pattern string, index int) (byte, bool) {
		if index < 0 || index >= len(pattern) {
			return 0, false
		}
		return pattern[index], true
	}
	// compatible reports whether one character of each pattern can match the same model character.
	compatible := func(x, y byte) bool {
		return x == y || (x == '?' && y != '/') || (y == '?' && x != '/')
	}
	memo := make(map[[2]int]bool)
	var overlap func(i, j int) bool
	overlap = func(i, j int) bool {
		x, hasX := at(a, i)
		y, hasY := at(b, j)
		if !hasX && !hasY {
			return true
		}
		key := [2]int{i, j}
		if result, ok := memo[key]; ok {
			return result
		}
		var result bool
		switch {
		case hasX && x == '*' && (overlap(i+1, j) || (hasY && y != '/' && overlap(i, j+1))):
			result = true
		case hasY && y == '*' && (overlap(i, j+1) || (hasX && x != '/' && overlap(i+1, j))):
			result = true
		case hasX && hasY && x != '*' && y != '*' && compatible(x, y) && overlap(i+1, j+1):
			result = true
		}
		memo[key] = result
		return result
	}
	return overlap(0, 0)
}

// modelRoutingAWFCandidates returns the router's candidate pool emitted to AWF: the
// policy-narrowed routing.candidateModels (or the Claude/Codex family default), or, for
// AWF without routing.candidateModels, the list that replaces models.allowed. Copilot
// candidates keep their github-copilot/ prefix; native routing.candidateModels are bare
// model ids or patterns of the routing provider named by apiProxy.routing.provider, while
// native candidates on the legacy allowlist keep their provider prefix.
func modelRoutingAWFCandidates(data *WorkflowData, engineName string, allowed, blocked []string) ([]string, error) {
	modelRoutingCandidatesLog.Printf("Resolving model-routing AWF candidates for engine=%s", engineName)
	groups, err := resolveModelRoutingCandidateGroups(data, engineName)
	if err != nil {
		return nil, NewValidationError("engine.model-routing.allowed-models", "", "invalid model-routing candidates", err.Error())
	}
	namespace := modelRoutingNamespace(data, engineName)
	supportsCandidates := apiProxySupportsRoutingCandidateModels(data)
	var candidates []string
	if len(groups) > 0 {
		candidates, _, err = intersectModelRoutingPolicy(groups, allowed, blocked, !supportsCandidates)
		if err != nil {
			return nil, NewValidationError("engine.model-routing.allowed-models", "", "model-routing candidates violate model policy", err.Error())
		}
	}
	if supportsCandidates && len(candidates) == 0 {
		if err := modelRoutingDefaultCandidatesPolicyError(engineName, namespace, allowed, blocked); err != nil {
			return nil, NewValidationError("engine.model-routing.allowed-models", "", "model-routing candidates violate model policy", err.Error())
		}
		candidates = modelRoutingDefaultCandidates(engineName, namespace)
	}
	// Native candidates lose their provider prefix only in routing.candidateModels. Without
	// it, the candidates replace the request-wide apiProxy.allowedModels and keep their
	// provider prefix, so a bare id never also authorizes the model on another provider.
	if namespace != copilotRoutingNamespace && supportsCandidates {
		for i, candidate := range candidates {
			candidates[i] = strings.TrimPrefix(candidate, namespace+"/")
		}
	}
	modelRoutingCandidatesLog.Printf("Resolved %d model-routing candidate(s) for engine=%s", len(candidates), engineName)
	return candidates, nil
}
