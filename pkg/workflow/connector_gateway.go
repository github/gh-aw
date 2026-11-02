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
	ID                  string            `json:"id"`
	Alias               string            `json:"alias"`
	Trigger             string            `json:"trigger"`
	Digest              string            `json:"digest"`
	VerificationProfile string            `json:"verification_profile"`
	NativeEvents        []string          `json:"native_events"`
	Normalize           map[string]string `json:"normalize"`
	Types               map[string]string `json:"types,omitempty"`
	Origins             []string          `json:"origins"`
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
		names, err := webhookStringList(gateway[alias])
		if err != nil || len(names) == 0 {
			return fmt.Errorf("on.gateway-dispatch.%s requires a nonempty trigger list", alias)
		}
		if registration["on"] != nil {
			return fmt.Errorf("connector %q trigger selection is duplicated", alias)
		}
		registration["on"] = names
	}
	return nil
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
		encoded, err := configs[alias].Plan.Encode()
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
