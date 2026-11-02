package workflow

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
)

//go:embed data/provider_issue_webhooks.json
var providerIssueCatalogJSON []byte

var providerIssueTriggers = map[string]string{
	"jira-issue":         "jira_issue",
	"azure-devops-issue": "azure_devops_issue",
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
	Policy       string   `json:"policy"`
	ClosedStates []string `json:"closed_states,omitempty"`
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

func providerIssueConfig(config map[string]any, path, event string, definition WebhookEvent) (map[string]any, []string, error) {
	var err error
	if definition.Provider != "" {
		config, err = providerIssueSelectors(config, path, event)
		if err != nil {
			return nil, nil, err
		}
	}
	if event != "azure_devops_issue" {
		return config, nil, nil
	}
	var closedStates []string
	if raw, exists := config["closed-states"]; exists {
		var err error
		closedStates, err = webhookStringValues(raw, path+".closed-states")
		if err != nil {
			return nil, nil, err
		}
		for _, state := range closedStates {
			if strings.Contains(state, "${{") {
				return nil, nil, fmt.Errorf("%s.closed-states must contain literal state names, not GitHub Actions expressions", path)
			}
		}
	}
	if _, specified := config["types"]; !specified && len(closedStates) == 0 {
		config = maps.Clone(config)
		config["types"] = slices.DeleteFunc(slices.Clone(definition.Actions), func(activity string) bool {
			return activity == "closed" || activity == "reopened"
		})
	}
	return config, closedStates, nil
}

func providerIssueSelectors(config map[string]any, path, event string) (map[string]any, error) {
	scope := map[string]string{
		"jira_issue": "project", "azure_devops_issue": "project_id",
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
	normalized := map[string]any{"filters": selectors}
	if raw, exists := config["types"]; exists {
		normalized["types"] = raw
		delete(selectors, "types")
	}
	if event == "azure_devops_issue" {
		if raw, exists := config["closed-states"]; exists {
			normalized["closed-states"] = raw
			delete(selectors, "closed-states")
		}
	}
	return normalized, nil
}

func applyProviderIssueNormalization(subscription *WebhookSubscription, definition WebhookEvent, closedStates []string, path string) error {
	if definition.Provider == "" {
		return nil
	}
	if definition.Provider == "azure-devops" && len(closedStates) == 0 &&
		slices.ContainsFunc(subscription.Types, func(activity string) bool { return activity == "closed" || activity == "reopened" }) {
		return fmt.Errorf("%s.closed-states is required for closed or reopened activities; use the terminal state names configured for the Azure DevOps project", path)
	}
	subscription.Provider = definition.Provider
	subscription.ActivityNormalization = &WebhookActivityNormalization{
		Policy: definition.Provider + "-issue/v1", ClosedStates: closedStates,
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
