package workflow

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
)

//go:embed data/provider_issue_webhooks.json
var providerIssueCatalogJSON []byte

var providerIssueTriggers = map[string]string{
	"jira-issue":   "jira_issue",
	"linear-issue": "linear_issue",
}

func isWebhookTrigger(name string) bool {
	_, provider := providerIssueTriggers[name]
	_, project := webhookTriggerAliases[name]
	_, github := githubNotificationTriggers[name]
	_, security := securityAlertTriggers[name]
	return provider || project || github || security || name == "slash-command" || name == "label-command"
}

// WebhookActivityNormalization identifies the documented, payload-only policy
// that maps native provider updates to GitHub-style issue activity names.
type WebhookActivityNormalization struct {
	Policy string `json:"policy"`
}

func addProviderIssueCatalog(catalog map[string]WebhookEvent) error {
	var providers map[string]WebhookEvent
	if err := json.Unmarshal(providerIssueCatalogJSON, &providers); err != nil {
		return fmt.Errorf("invalid embedded provider issue webhook catalog: %w", err)
	}
	maps.Copy(catalog, providers)
	return nil
}

func addProviderIssueConfigs(on, events map[string]any, paths map[string]string) {
	for alias, event := range providerIssueTriggers {
		if raw, exists := on[alias]; exists {
			events[event] = raw
			paths[event] = "on.gateway-dispatch." + alias
		}
	}
}

func providerIssueConfig(config map[string]any, path, event string, definition WebhookEvent) (map[string]any, error) {
	if definition.Provider != "" {
		return providerIssueSelectors(config, path, event)
	}
	return config, nil
}

func providerIssueSelectors(config map[string]any, path, event string) (map[string]any, error) {
	scope := map[string]string{
		"jira_issue": "project", "linear_issue": "organization-id",
	}[event]
	if _, exists := config["filters"]; exists {
		return nil, fmt.Errorf("%s.filters is not supported; put selectors directly under %s, including required %s", path, path, scope)
	}
	rawScope, exists := config[scope]
	if !exists {
		return nil, fmt.Errorf("%s.%s is required", path, scope)
	}
	values, err := webhookStringValues(rawScope, path+"."+scope)
	if err != nil {
		return nil, err
	}
	selectors := maps.Clone(config)
	selectors[scope] = values
	if event == "linear_issue" {
		if raw, exists := selectors["team-id"]; exists {
			selectors["team-id"], err = webhookStringValues(raw, path+".team-id")
			if err != nil {
				return nil, err
			}
		}
	}
	normalized := map[string]any{"filters": selectors}
	if raw, exists := config["types"]; exists {
		normalized["types"] = raw
		delete(selectors, "types")
	}
	return normalized, nil
}

func applyProviderIssueNormalization(subscription *WebhookSubscription, definition WebhookEvent, path string) error {
	if definition.Provider == "" {
		return nil
	}
	subscription.Provider = definition.Provider
	subscription.ActivityNormalization = &WebhookActivityNormalization{
		Policy: definition.Provider + "-issue/v1",
	}
	for _, activity := range subscription.Types {
		native, exists := definition.NativeEvents[activity]
		if !exists {
			return fmt.Errorf("missing native event mapping for %s activity %q", path, activity)
		}
		subscription.NativeEvents = append(subscription.NativeEvents, native)
	}
	sort.Strings(subscription.NativeEvents)
	subscription.NativeEvents = slices.Compact(subscription.NativeEvents)
	return nil
}
