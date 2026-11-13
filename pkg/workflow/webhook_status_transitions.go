package workflow

import (
	"fmt"
	"slices"
	"strings"
)

func applyWebhookStatusTransitions(subscription *WebhookSubscription, config map[string]any, definition WebhookEvent) error {
	raw, exists := config["status-transitions"]
	if !exists {
		return nil
	}
	const path = "on.gateway-dispatch.projects-items.status-transitions"
	transitions, ok := raw.([]any)
	if !ok || len(transitions) == 0 {
		return fmt.Errorf("%s must be a non-empty list of from/to status pairs. Example: [{from: Todo, to: Done}]", path)
	}
	if !slices.Contains(subscription.Types, "edited") {
		return fmt.Errorf("%s requires types to include edited", path)
	}
	for index, raw := range transitions {
		pair, ok := raw.(map[string]any)
		if !ok || len(pair) != 2 {
			return fmt.Errorf("%s[%d] must be an object containing only from and to. Example: {from: Todo, to: Done}", path, index)
		}
		group := make([]WebhookCondition, 0, 2)
		for _, direction := range []string{"from", "to"} {
			value, ok := pair[direction].(string)
			if !ok || strings.TrimSpace(value) == "" {
				return fmt.Errorf("%s[%d].%s must be a non-empty status name. Example: %s: Done", path, index, direction, direction)
			}
			filter := "field." + direction + ".name"
			spec := definition.Filters[filter]
			values, err := webhookFilterValues(value, spec)
			if err != nil {
				return fmt.Errorf("invalid %s[%d].%s: %w", path, index, direction, err)
			}
			group = append(group, WebhookCondition{
				Filter: filter, Path: slices.Clone(spec.Path), Operator: "equals", ValueType: spec.Type, Values: values,
			})
		}
		subscription.AnyOf = append(subscription.AnyOf, group)
	}
	for _, field := range []struct{ name, value string }{{"field.name", "Status"}, {"field.type", "single_select"}} {
		spec := definition.Filters[field.name]
		subscription.Conditions = append(subscription.Conditions, WebhookCondition{
			Filter: field.name, Path: slices.Clone(spec.Path), Operator: "equals", ValueType: spec.Type, Values: []any{field.value},
		})
	}
	slices.SortFunc(subscription.Conditions, func(a, b WebhookCondition) int { return strings.Compare(a.Filter, b.Filter) })
	if _, explicit := config["types"]; !explicit {
		subscription.Types = []string{"edited"}
	}
	return nil
}
