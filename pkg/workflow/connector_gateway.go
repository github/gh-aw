package workflow

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/github/gh-aw/pkg/sliceutil"
	workflowlint "github.com/github/gh-aw/pkg/workflow/lint"
)

// ConnectorWebhookSource is receiver-owned source policy, not sender evidence.
type ConnectorWebhookSource struct {
	ID                  string                            `json:"id"`
	Alias               string                            `json:"alias"`
	Trigger             string                            `json:"trigger"`
	Digest              string                            `json:"digest"`
	VerificationProfile string                            `json:"verification_profile"`
	NativeEvents        []string                          `json:"native_events"`
	Normalize           map[string]string                 `json:"normalize"`
	Types               map[string]string                 `json:"types,omitempty"`
	Origins             []string                          `json:"origins"`
	EvaluationVersion   string                            `json:"evaluation_version,omitempty"`
	When                *connectors.EventPredicate        `json:"when,omitempty"`
	Filters             map[string]connectors.EventFilter `json:"filters,omitempty"`
	Variants            []connectors.EventVariant         `json:"variants,omitempty"`
	URLIdentity         string                            `json:"url_identity,omitempty"`
	URLPathPrefix       string                            `json:"url_path_prefix,omitempty"`
}

func connectorGatewaySelections(frontmatter map[string]any, registrations map[string]any) error {
	on, ok := frontmatter["on"].(map[string]any)
	if !ok {
		return nil
	}
	gateway, ok := on["gateway-dispatch"].(map[string]any)
	if !ok {
		return nil
	}
	if _, legacy := gateway["connectors"]; legacy {
		return errors.New("on.gateway-dispatch.connectors is removed; select imported connectors directly as on.gateway-dispatch.<id>: [trigger]")
	}
	if diagnostics := workflowlint.CheckGatewayDispatchLiterals(gateway); len(diagnostics) != 0 {
		return errors.New("on.gateway-dispatch requires literal connector and trigger names")
	}
	for _, alias := range sliceutil.SortedKeys(gateway) {
		if reservedConnectorGatewayName(alias) {
			continue
		}
		registration, ok := registrations[alias].(map[string]any)
		if !ok {
			return fmt.Errorf("gateway connector %q requires an imported declaration", alias)
		}
		names, err := connectorGatewayTriggerNames(gateway[alias], registration)
		if err != nil {
			return fmt.Errorf("on.gateway-dispatch.%s: %w", alias, err)
		}
		if len(names) == 0 {
			return fmt.Errorf("on.gateway-dispatch.%s requires a nonempty trigger list or filter mapping", alias)
		}
		if registration["on"] != nil {
			return fmt.Errorf("connector %q trigger selection is duplicated", alias)
		}
		registration["on"] = names
	}
	return nil
}

func connectorGatewayTriggerNames(raw any, registration map[string]any) ([]string, error) {
	selections, mapping := raw.(map[string]any)
	if !mapping {
		return webhookStringList(raw)
	}
	data, err := json.Marshal(registration["spec"])
	if err != nil {
		return nil, err
	}
	var spec connectors.ConnectorSpec
	if err := connectors.Decode(data, &spec); err != nil {
		return nil, err
	}
	for _, name := range sliceutil.SortedKeys(selections) {
		trigger, exists := spec.Triggers[name]
		if !exists {
			return nil, fmt.Errorf("unknown connector trigger %q", name)
		}
		if selections[name] == nil {
			continue
		}
		filters, ok := selections[name].(map[string]any)
		if !ok {
			return nil, errors.New("connector trigger selections require null or a mapping of declared filters")
		}
		for filterName, raw := range filters {
			filter, exists := trigger.Filters[filterName]
			if !exists {
				return nil, fmt.Errorf("trigger %q has no filter %q", name, filterName)
			}
			values, ok := raw.([]any)
			if !ok || len(values) == 0 {
				return nil, fmt.Errorf("trigger filter %q requires a nonempty array of typed literals", filterName)
			}
			filter.Values = values
			trigger.Filters[filterName] = filter
		}
		spec.Triggers[name] = trigger
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	data, err = json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	var normalized map[string]any
	if err := json.Unmarshal(data, &normalized); err != nil {
		return nil, err
	}
	registration["spec"] = normalized
	return sliceutil.SortedKeys(selections), nil
}

func reservedConnectorGatewayName(name string) bool {
	if isWebhookTrigger(name) {
		return true
	}
	switch name {
	case "repos", "orgs", "connectors", "security-alerts", "jira-issue", "linear-issue", "azure-devops-issue":
		return true
	default:
		return false
	}
}

func normalizeConnectorGatewayOn(frontmatter map[string]any, registrations map[string]any) error {
	for _, alias := range sliceutil.SortedKeys(registrations) {
		registration, ok := registrations[alias].(map[string]any)
		if !ok {
			return fmt.Errorf("connector %q registration must be an object", alias)
		}
		if registration["on"] == nil {
			continue
		}
		if reservedConnectorGatewayName(alias) {
			return fmt.Errorf("connector %q conflicts with a reserved webhook gateway field; use a different connector id", alias)
		}
		names, err := webhookStringList(registration["on"])
		if err != nil || len(names) == 0 {
			return fmt.Errorf("connector %q requires a nonempty trigger list", alias)
		}
		on := ensureOnMap(frontmatter)
		if on == nil {
			return errors.New("connector triggers require mapping on syntax with on.gateway-dispatch")
		}
		gateway := map[string]any{}
		if raw, exists := on["gateway-dispatch"]; exists {
			configured, ok := raw.(map[string]any)
			if !ok {
				return errors.New("on.gateway-dispatch must be an object containing connector trigger selections")
			}
			gateway = configured
		} else {
			on["gateway-dispatch"] = gateway
		}
		gateway[alias] = names
		if err := validateGatewayDispatchExclusive(on); err != nil {
			return err
		}
	}
	return nil
}

func connectorWebhookSubscriptions(configs map[string]*WorkflowConnector, workflowID string) ([]WebhookSubscription, error) {
	var subscriptions []WebhookSubscription
	for _, alias := range sliceutil.SortedKeys(configs) {
		if len(configs[alias].Plan.TriggerNames()) == 0 {
			continue
		}
		triggerPlan, err := configs[alias].Plan.ForPhase("trigger")
		if err != nil {
			return nil, err
		}
		encoded, err := triggerPlan.Encode()
		if err != nil {
			return nil, err
		}
		raw, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, err
		}
		var snapshot struct {
			Digest     string                   `json:"digest"`
			Spec       connectors.ConnectorSpec `json:"spec"`
			Connection connectors.Connection    `json:"connection"`
			Binding    connectors.Binding       `json:"binding"`
		}
		if err := json.Unmarshal(raw, &snapshot); err != nil {
			return nil, err
		}
		for _, name := range snapshot.Binding.Triggers {
			subscription, err := connectorWebhookSubscription(alias, name, workflowID, snapshot.Digest, snapshot.Spec, snapshot.Connection)
			if err != nil {
				return nil, err
			}
			subscriptions = append(subscriptions, subscription)
		}
	}
	return subscriptions, nil
}

func connectorWebhookSubscription(alias, name, workflowID, digest string, spec connectors.ConnectorSpec, connection connectors.Connection) (WebhookSubscription, error) {
	trigger := spec.Triggers[name]
	if len(trigger.ResourceOrigins) == 0 || len(trigger.Resources) == 0 {
		return WebhookSubscription{}, fmt.Errorf("connector trigger %s/%s requires resourceOrigins and exact resource constraints for gateway routing", alias, name)
	}
	conditions, err := connectorWebhookConditions(trigger, connection.Resources)
	if err != nil {
		return WebhookSubscription{}, err
	}
	source := &ConnectorWebhookSource{
		ID: spec.ID, Alias: alias, Trigger: name, Digest: digest,
		VerificationProfile: trigger.VerificationProfile, NativeEvents: slices.Clone(trigger.NativeEvents),
		Normalize: trigger.Normalize, Types: trigger.Types,
	}
	if trigger.When != nil || len(trigger.Filters)+len(trigger.Variants) != 0 || trigger.URLIdentity != "" {
		source.EvaluationVersion = connectors.EventEvaluationVersion
		source.When, source.Filters, source.Variants = trigger.When, trigger.Filters, trigger.Variants
		source.URLIdentity, source.URLPathPrefix = trigger.URLIdentity, trigger.URLPathPrefix
	}
	for _, origin := range trigger.ResourceOrigins {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil ||
			strings.ContainsAny(origin, "*?#") || (parsed.Path != "" && parsed.Path != "/") {
			return WebhookSubscription{}, fmt.Errorf("connector trigger %s/%s resourceOrigins must be fixed HTTPS origins", alias, name)
		}
		source.Origins = append(source.Origins, parsed.Scheme+"://"+strings.ToLower(parsed.Host))
	}
	slices.Sort(source.Origins)
	source.Origins = slices.Compact(source.Origins)
	if err := connectorGatewaySourceLiterals(source, conditions); err != nil {
		return WebhookSubscription{}, err
	}
	event := sha256.Sum256([]byte(alias + "\x00" + name + "\x00" + digest))
	subscription := newWebhookSubscription(workflowID, fmt.Sprintf("connector_%x", event[:12]), []string{name}, conditions)
	subscription.Connector = source
	for _, variant := range trigger.Variants {
		conditions, err := connectorWebhookConditions(connectors.Trigger{Resources: variant.Resources, Types: variant.Types}, connection.Resources)
		if err != nil {
			return WebhookSubscription{}, err
		}
		subscription.AnyOf = append(subscription.AnyOf, conditions)
	}
	if len(trigger.Variants) != 0 {
		// Each explicit variant supplies the complete resource evidence group.
		subscription.Conditions = slices.DeleteFunc(subscription.Conditions, func(condition WebhookCondition) bool {
			return strings.HasPrefix(condition.Filter, "connector_scope_")
		})
	}
	return subscription, nil
}

func connectorGatewaySourceLiterals(source *ConnectorWebhookSource, conditions []WebhookCondition) error {
	data, err := json.Marshal(map[string]any{"source": source, "conditions": conditions})
	if err != nil {
		return err
	}
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		return err
	}
	if len(workflowlint.CheckGatewayDispatchLiterals(document)) != 0 {
		return errors.New("connector gateway source metadata must not contain GitHub Actions expressions")
	}
	return nil
}

func connectorWebhookConditions(trigger connectors.Trigger, grants map[string][]string) ([]WebhookCondition, error) {
	conditions := []WebhookCondition{}
	for index, pointer := range sliceutil.SortedKeys(trigger.Match) {
		conditions = append(conditions, connectorWebhookCondition(pointer, fmt.Sprintf("connector_match_%d", index), "string", []any{trigger.Match[pointer]}))
	}
	for index, pointer := range sliceutil.SortedKeys(trigger.Resources) {
		values := grants[trigger.Resources[pointer]]
		if len(values) == 0 {
			return nil, errors.New("connector gateway resource constraints require explicit nonempty grants")
		}
		for _, name := range sliceutil.SortedKeys(trigger.Filters) {
			filter := trigger.Filters[name]
			if len(filter.Values) != 0 {
				conditions = append(conditions, connectorWebhookCondition(filter.Path, "connector_filter_"+strings.ReplaceAll(name, "-", "_"), filter.Type, filter.Values))
			}
		}
		var literals []any
		valueType := "string"
		for _, value := range values {
			if trigger.Types[pointer] == "integer" {
				numeric, err := strconv.ParseUint(value, 10, 53)
				if err != nil || strconv.FormatUint(numeric, 10) != value {
					return nil, errors.New("connector gateway integer resource grants require canonical safe integer strings")
				}
				literals = append(literals, numeric)
				valueType = "integer"
			} else {
				literals = append(literals, value)
			}
		}
		conditions = append(conditions, connectorWebhookCondition(pointer, fmt.Sprintf("connector_scope_%d", index), valueType, literals))
	}
	return conditions, nil
}

func connectorWebhookCondition(pointer, name, valueType string, values []any) WebhookCondition {
	path := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	for index, part := range path {
		path[index] = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
	}
	return WebhookCondition{Filter: name, Path: path, Operator: "equals", ValueType: valueType, Values: values}
}
