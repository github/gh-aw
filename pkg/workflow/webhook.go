package workflow

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	workflowlint "github.com/github/gh-aw/pkg/workflow/lint"
)

var webhookLog = logger.New("workflow:webhook")

// webhookCatalogJSON is the compiler's authoritative, payload-only filter catalog.
//
//go:embed data/webhook_events.json
var webhookCatalogJSON []byte

// webhookTriggerAliases maps first-class Projects triggers to wire event names.
var webhookTriggerAliases = map[string]string{
	"projects-items": "projects_v2_item",
}

var securityAlertTriggers = map[string]string{
	"code-scanning-alert": "code_scanning_alert",
	"dependabot-alerts":   "dependabot_alert",
}

// WebhookFilter describes an allowlisted path in the original webhook payload.
// A "*" path segment traverses array elements, never object keys.
type WebhookFilter struct {
	Path        []string `json:"path"`
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Actions     []string `json:"actions,omitempty"`
	Enum        []any    `json:"enum,omitempty"`
}

// WebhookEvent describes a source event available through the webhook gateway.
type WebhookEvent struct {
	Provider     string                   `json:"provider,omitempty"`
	NativeEvents map[string]string        `json:"native_events,omitempty"`
	Actions      []string                 `json:"actions"`
	Description  string                   `json:"description"`
	Source       string                   `json:"source"`
	Availability []string                 `json:"availability"`
	Filters      map[string]WebhookFilter `json:"filters"`
}

// WebhookCondition uses strict typed equality: AND between conditions, OR between
// values and array elements. Missing and null payload values never match.
type WebhookCondition struct {
	Filter    string   `json:"filter"`
	Path      []string `json:"path"`
	Operator  string   `json:"operator"`
	ValueType string   `json:"value_type"`
	Values    []any    `json:"values"`
}

// WebhookContext maps the inline envelope into the standard GitHub context.
type WebhookContext struct {
	Envelope   string `json:"envelope"`
	AWContext  string `json:"aw_context"`
	Event      string `json:"event"`
	Deliveries string `json:"deliveries"`
	Items      string `json:"items"`
}

// WebhookBatch declares receiver-side homogeneous batching and delivery identity.
type WebhookBatch struct {
	Mode          string `json:"mode"`
	DeduplicateBy string `json:"deduplicate_by"`
}

// WebhookDispatch is the routing contract for a subscription.
// Repository is resolved against the repository containing the compiled workflow,
// not the repository in the incoming webhook payload.
type WebhookDispatch struct {
	Repository        string                    `json:"repository"`
	Method            string                    `json:"method"`
	EventType         string                    `json:"event_type"`
	Workflow          string                    `json:"workflow"`
	PayloadProjection *WebhookPayloadProjection `json:"payload_projection,omitempty"`
	Context           WebhookContext            `json:"context"`
	Batch             WebhookBatch              `json:"batch"`
	MaxPayloadBytes   int                       `json:"max_payload_bytes"`
	MaxRequestBytes   int                       `json:"max_request_bytes"`
}

// WebhookPayloadProjection limits the source payload fields forwarded by CAO.
type WebhookPayloadProjection struct {
	Policy string `json:"policy"`
}

// WebhookPayloadProjectionPolicy versions the closed per-delivery notification.
const WebhookPayloadProjectionPolicy = "resource-url-types-filters/v2"

// WebhookSubscription is the compiler's versioned filter and routing declaration.
type WebhookSubscription struct {
	Provider              string                        `json:"provider,omitempty"`
	NativeEvents          []string                      `json:"native_events,omitempty"`
	ActivityNormalization *WebhookActivityNormalization `json:"activity_normalization,omitempty"`
	CommandFilter         *WebhookCommandFilter         `json:"command_filter,omitempty"`
	SchemaVersion         int                           `json:"schema_version"`
	Event                 string                        `json:"event"`
	Types                 []string                      `json:"types"`
	Repos                 []string                      `json:"repos,omitempty"`
	Orgs                  []string                      `json:"orgs,omitempty"`
	Conditions            []WebhookCondition            `json:"conditions"`
	// AnyOf requires a complete group to match, in addition to Conditions.
	AnyOf    [][]WebhookCondition `json:"any_of,omitempty"`
	Dispatch WebhookDispatch      `json:"dispatch"`
}

// WebhookCatalog returns an independent copy of the embedded event catalog.
func WebhookCatalog() (map[string]WebhookEvent, error) {
	var catalog map[string]WebhookEvent
	if err := json.Unmarshal(webhookCatalogJSON, &catalog); err != nil {
		return nil, fmt.Errorf("invalid embedded webhook catalog: %w", err)
	}
	if err := addProviderIssueCatalog(catalog); err != nil {
		return nil, err
	}
	if err := addGitHubNotificationCatalog(catalog); err != nil {
		return nil, err
	}
	webhookLog.Printf("Loaded webhook catalog with %d event definition(s)", len(catalog))
	return catalog, nil
}

// WebhookDispatchEventType scopes repository_dispatch to exactly one workflow.
func WebhookDispatchEventType(workflowID string) string {
	digest := sha256.Sum256([]byte(workflowID))
	return fmt.Sprintf("gh-aw-webhook-%x", digest[:12])
}

// ParseWebhookSubscriptions validates and normalizes webhook triggers without
// fetching or hydrating payload fields. Catalog paths are emitted for CAO.
func ParseWebhookSubscriptions(on any, workflowID string) ([]WebhookSubscription, error) {
	webhookLog.Printf("Parsing webhook subscriptions for workflow %s", workflowID)
	onMap, err := webhookOnConfig(on)
	if err != nil {
		return nil, err
	}
	events, paths, err := webhookTriggerConfigs(onMap)
	if err != nil {
		return nil, err
	}
	_, hasSlashCommand := onMap["slash-command"]
	_, hasLabelCommand := onMap["label-command"]
	if len(events) == 0 && !hasSlashCommand && !hasLabelCommand {
		webhookLog.Print("No gateway-dispatch triggers configured; skipping webhook subscription parsing")
		return nil, nil
	}
	repos, err := parseWebhookRepositories(onMap, "on.gateway-dispatch")
	if err != nil {
		return nil, err
	}
	orgs, err := parseWebhookOrganizations(onMap, "on.gateway-dispatch")
	if err != nil {
		return nil, err
	}
	catalog, err := WebhookCatalog()
	if err != nil {
		return nil, err
	}
	subscriptions := make([]WebhookSubscription, 0, len(events))
	for _, name := range sortedWebhookKeys(events) {
		definition, known := catalog[name]
		if !known {
			return nil, fmt.Errorf("missing embedded webhook catalog event %q for %s", name, paths[name])
		}
		subscription, err := parseWebhookSubscription(events[name], paths[name], name, workflowID, definition)
		if err != nil {
			return nil, err
		}
		if _, repositoryScoped := definition.Filters["repository"]; repositoryScoped {
			subscription.Repos = slices.Clone(repos)
		}
		if _, organizationScoped := definition.Filters["organization"]; organizationScoped {
			subscription.Orgs = slices.Clone(orgs)
		}
		subscriptions = append(subscriptions, subscription)
	}
	commands, err := parseGatewayCommandSubscriptions(onMap, workflowID, repos, orgs)
	if err != nil {
		return nil, err
	}
	result := append(subscriptions, commands...)
	webhookLog.Printf("Parsed %d webhook subscription(s) for workflow %s", len(result), workflowID)
	return result, nil
}

func parseWebhookSubscription(raw any, path, event, workflowID string, definition WebhookEvent) (WebhookSubscription, error) {
	config, ok := raw.(map[string]any)
	if !ok {
		return WebhookSubscription{}, fmt.Errorf("%s must be an object", path)
	}
	config, closedStates, err := providerIssueConfig(config, path, event, definition)
	if err != nil {
		return WebhookSubscription{}, err
	}
	for _, key := range sortedWebhookKeys(config) {
		supported := key == "types" || key == "filters" ||
			(event == "projects_v2_item" && key == "status-transitions") ||
			(event == "azure_devops_issue" && key == "closed-states")
		if !supported {
			return WebhookSubscription{}, fmt.Errorf("unsupported %s property %q", path, key)
		}
	}
	types, err := parseWebhookTypes(config, path, definition.Actions)
	if err != nil {
		return WebhookSubscription{}, err
	}
	conditions, err := parseWebhookConditions(config, path, types, definition)
	if err != nil {
		return WebhookSubscription{}, err
	}
	subscription := newWebhookSubscription(workflowID, event, types, conditions)
	if definition.Provider == "" && len(definition.NativeEvents) > 0 {
		for _, activity := range types {
			subscription.NativeEvents = append(subscription.NativeEvents, definition.NativeEvents[activity])
		}
		slices.Sort(subscription.NativeEvents)
		subscription.NativeEvents = slices.Compact(subscription.NativeEvents)
	}
	if err := applyWebhookStatusTransitions(&subscription, config, definition); err != nil {
		return WebhookSubscription{}, err
	}
	if err := applyProviderIssueNormalization(&subscription, definition, closedStates, path); err != nil {
		return WebhookSubscription{}, err
	}
	return subscription, nil
}

func webhookOnConfig(on any) (map[string]any, error) {
	for _, name := range rawOnSectionTriggerNames(on) {
		if (isWebhookTrigger(name) && name != "discussion") || name == "repos" || name == "orgs" {
			return nil, fmt.Errorf("on.%s is not supported; define %s under on.gateway-dispatch", name, name)
		}
	}
	onMap, ok := on.(map[string]any)
	if !ok {
		if slices.Contains(rawOnSectionTriggerNames(on), "gateway-dispatch") {
			return nil, errors.New("on.gateway-dispatch requires mapping syntax with at least one nested gateway trigger")
		}
		return nil, nil
	}
	raw, exists := onMap["gateway-dispatch"]
	if !exists {
		return nil, nil
	}
	if diagnostics := workflowlint.CheckGatewayDispatchLiterals(raw); len(diagnostics) > 0 {
		return nil, fmt.Errorf("%s: %s", diagnostics[0].Path, diagnostics[0].Message)
	}
	gateway, ok := raw.(map[string]any)
	if !ok {
		return nil, errors.New("on.gateway-dispatch must be an object with at least one nested gateway trigger")
	}
	if err := validateGatewayDispatchExclusive(onMap); err != nil {
		return nil, err
	}
	hasTrigger := false
	for _, name := range sortedWebhookKeys(gateway) {
		if isWebhookTrigger(name) {
			hasTrigger = true
		} else if name != "repos" && name != "orgs" {
			return nil, fmt.Errorf("unsupported on.gateway-dispatch property %q; use supported nested gateway triggers or shared repos/orgs", name)
		}
	}
	if !hasTrigger {
		return nil, errors.New("on.gateway-dispatch requires at least one nested gateway trigger; repos/orgs alone are not triggers")
	}
	return gateway, nil
}

func webhookTriggerConfigs(on map[string]any) (map[string]any, map[string]string, error) {
	events := map[string]any{}
	paths := map[string]string{}
	for _, alias := range sortedWebhookKeys(webhookTriggerAliases) {
		raw, exists := on[alias]
		if !exists {
			continue
		}
		event := webhookTriggerAliases[alias]
		config, ok := raw.(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("on.gateway-dispatch.%s must be an object with a required project_id", alias)
		}
		if _, exists := config["project_id"]; !exists {
			return nil, nil, fmt.Errorf("on.gateway-dispatch.%s.project_id is required", alias)
		}
		config, err := promoteWebhookFilters(config, "on.gateway-dispatch."+alias, "project_id")
		if err != nil {
			return nil, nil, err
		}
		events[event] = config
		paths[event] = "on.gateway-dispatch." + alias
	}
	if err := addSecurityAlertConfigs(on, events, paths); err != nil {
		return nil, nil, err
	}
	addProviderIssueConfigs(on, events, paths)
	addGitHubNotificationConfigs(on, events, paths)
	return events, paths, nil
}

func promoteWebhookFilters(config map[string]any, path, field string) (map[string]any, error) {
	config = maps.Clone(config)
	filters := map[string]any{}
	if raw, exists := config["filters"]; exists {
		existing, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s.filters must be an object", path)
		}
		maps.Copy(filters, existing)
	}
	if _, exists := filters[field]; exists {
		return nil, fmt.Errorf("%s.filters.%s is not supported; use %s.%s beside types", path, field, path, field)
	}
	if raw, exists := config[field]; exists {
		values, err := webhookStringValues(raw, path+"."+field)
		if err != nil {
			return nil, err
		}
		filters[field] = values
		delete(config, field)
	}
	if len(filters) > 0 {
		config["filters"] = filters
	}
	return config, nil
}

func webhookStringValues(raw any, path string) ([]string, error) {
	if text, ok := raw.(string); ok {
		raw = []string{text}
	}
	values, err := webhookStringList(raw)
	if err != nil || len(values) == 0 || slices.ContainsFunc(values, func(value string) bool { return strings.TrimSpace(value) == "" }) {
		return nil, fmt.Errorf("%s must be a non-empty string or a non-empty list of non-empty strings", path)
	}
	sort.Strings(values)
	return slices.Compact(values), nil
}

func newWebhookSubscription(workflowID, event string, types []string, conditions []WebhookCondition) WebhookSubscription {
	return WebhookSubscription{
		SchemaVersion: 2, Event: event, Types: types, Conditions: conditions,
		Dispatch: WebhookDispatch{
			Repository: "${{ github.repository }}", Method: "repository_dispatch",
			EventType:         WebhookDispatchEventType(workflowID),
			Workflow:          constants.WorkflowsDirSlash + workflowID + ".lock.yml",
			PayloadProjection: &WebhookPayloadProjection{Policy: WebhookPayloadProjectionPolicy},
			Context: WebhookContext{
				Envelope: "github.event.client_payload", AWContext: "github.event.client_payload.aw_context",
				Event:      "github.event.client_payload.aw_context.webhook.event",
				Deliveries: "github.event.client_payload.aw_context.webhook.deliveries",
				Items:      "aw_context.items",
			},
			Batch:           WebhookBatch{Mode: "same-event", DeduplicateBy: "delivery_id"},
			MaxPayloadBytes: 65535, MaxRequestBytes: 65535,
		},
	}
}

func parseWebhookTypes(config map[string]any, path string, supported []string) ([]string, error) {
	value, exists := config["types"]
	if !exists {
		result := slices.Clone(supported)
		sort.Strings(result)
		if result == nil {
			result = []string{}
		}
		return result, nil
	}
	types, err := webhookStringList(value)
	if err != nil || len(types) == 0 {
		return nil, fmt.Errorf("%s.types must be a non-empty list of supported activity types", path)
	}
	for _, activity := range types {
		if !slices.Contains(supported, activity) {
			return nil, fmt.Errorf("unsupported activity type %q for %s; supported types: %s", activity, path, strings.Join(supported, ", "))
		}
	}
	sort.Strings(types)
	return slices.Compact(types), nil
}

func parseWebhookConditions(config map[string]any, path string, types []string, definition WebhookEvent) ([]WebhookCondition, error) {
	conditions := []WebhookCondition{}
	raw, exists := config["filters"]
	if !exists {
		return conditions, nil
	}
	filters, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s.filters must be an object", path)
	}
	flat := map[string]any{}
	if err := flattenWebhookFilters("", filters, flat); err != nil {
		return nil, err
	}
	for _, name := range sortedWebhookKeys(flat) {
		spec, supported := definition.Filters[name]
		if !supported {
			return nil, fmt.Errorf("unsupported filter %q for %s; only fields present in the delivered payload can be filtered (labels and item status may require unavailable hydration); supported filters: %s", name, path, strings.Join(sortedWebhookKeys(definition.Filters), ", "))
		}
		if len(spec.Actions) > 0 && !slices.ContainsFunc(types, func(activity string) bool { return slices.Contains(spec.Actions, activity) }) {
			return nil, fmt.Errorf("filter %q for %s requires one of these activity types: %s", name, path, strings.Join(spec.Actions, ", "))
		}
		values, err := webhookFilterValues(flat[name], spec)
		if err != nil {
			return nil, fmt.Errorf("invalid filter %q for %s: %w", name, path, err)
		}
		conditions = append(conditions, WebhookCondition{
			Filter: name, Path: slices.Clone(spec.Path), Operator: "equals", ValueType: spec.Type, Values: values,
		})
	}
	return conditions, nil
}

func flattenWebhookFilters(prefix string, input map[string]any, output map[string]any) error {
	for _, key := range sortedWebhookKeys(input) {
		if key == "" || strings.Contains(key, ".") {
			return errors.New("webhook filter keys must be non-empty names; express nested filters as YAML objects, not dotted keys")
		}
		name := key
		if prefix != "" {
			name = prefix + "." + key
		}
		if nested, ok := input[key].(map[string]any); ok {
			if len(nested) == 0 {
				return fmt.Errorf("webhook filter %q must not be an empty object", name)
			}
			if err := flattenWebhookFilters(name, nested, output); err != nil {
				return err
			}
		} else {
			output[name] = input[key]
		}
	}
	return nil
}

func webhookFilterValues(raw any, spec WebhookFilter) ([]any, error) {
	var values []any
	switch value := raw.(type) {
	case []any:
		values = slices.Clone(value)
	case []string:
		for _, item := range value {
			values = append(values, item)
		}
	default:
		values = []any{value}
	}
	if len(values) == 0 {
		return nil, errors.New("accepted values must not be empty")
	}
	for i, value := range values {
		valid := false
		switch spec.Type {
		case "string":
			text, isString := value.(string)
			valid = isString
			if isString && strings.Contains(text, "${{") {
				return nil, errors.New("filter values must be literals, not GitHub Actions expressions")
			}
		case "boolean":
			_, valid = value.(bool)
		case "integer":
			switch number := value.(type) {
			case int:
				valid = int64(number) >= -9007199254740991 && int64(number) <= 9007199254740991
			case int64:
				valid = number >= -9007199254740991 && number <= 9007199254740991
			case uint64:
				valid = number <= 9007199254740991
			case float64:
				valid = !math.IsNaN(number) && !math.IsInf(number, 0) && math.Trunc(number) == number && math.Abs(number) <= 9007199254740991
			}
		}
		if !valid {
			return nil, fmt.Errorf("expected %s or a non-empty list of %s values (integers must be JSON-safe)", spec.Type, spec.Type)
		}
		if len(spec.Enum) > 0 && !slices.ContainsFunc(spec.Enum, func(allowed any) bool { return fmt.Sprint(allowed) == fmt.Sprint(value) }) {
			return nil, fmt.Errorf("value %v is not one of %v", value, spec.Enum)
		}
		values[i] = value
	}
	// Every value has the same validated scalar type.
	slices.SortFunc(values, func(a, b any) int {
		return strings.Compare(fmt.Sprint(a), fmt.Sprint(b))
	})
	return slices.CompactFunc(values, func(a, b any) bool {
		return fmt.Sprint(a) == fmt.Sprint(b)
	}), nil
}

func webhookStringList(raw any) ([]string, error) {
	var result []string
	switch value := raw.(type) {
	case []string:
		return slices.Clone(value), nil
	case []any:
		for _, item := range value {
			text, ok := item.(string)
			if !ok || text == "" {
				return nil, errors.New("expected non-empty strings")
			}
			result = append(result, text)
		}
	default:
		return nil, errors.New("expected a list of strings")
	}
	return result, nil
}

func sortedWebhookKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
