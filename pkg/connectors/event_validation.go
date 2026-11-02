package connectors

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

func validateTriggerRules(trigger Trigger) error {
	if trigger.URLIdentity != "" || trigger.URLPathPrefix != "" {
		prefix := trigger.URLPathPrefix
		if !validPointer(trigger.URLIdentity) || len(trigger.URLIdentity) > 1024 || len(prefix) > 1024 || trigger.Normalize["url"] == "" || !strings.HasPrefix(prefix, "/") || strings.HasPrefix(prefix, "//") || !strings.HasSuffix(prefix, "/") || strings.ContainsAny(prefix, "\r\n\t?#%{}$\\") || strings.Contains(prefix, "..") {
			return errors.New("trigger URL binding requires urlIdentity, normalize.url and a fixed absolute urlPathPrefix ending in /")
		}
	}
	if trigger.When != nil {
		if err := ValidateEventPredicate(*trigger.When); err != nil {
			return err
		}
	}
	if len(trigger.Filters) > 16 || len(trigger.Variants) > 4 {
		return errors.New("triggers support at most 16 named filters and 4 explicit evidence variants")
	}
	for name, filter := range trigger.Filters {
		if !namePattern.MatchString(name) || !validPointer(filter.Path) || len(filter.Path) > 1024 || len(filter.Values) > 100 || !slices.Contains([]string{"string", "boolean", "integer"}, filter.Type) {
			return errors.New("trigger filters require names, bounded JSON Pointers, primitive types and at most 100 values")
		}
		for _, value := range filter.Values {
			if validateArgument(Input{Type: filter.Type, MaxLength: 1024}, value) != nil || !eventComparable(value) || strings.Contains(fmt.Sprint(value), "${{") {
				return errors.New("trigger filter values must be bounded typed literals")
			}
		}
	}
	for _, variant := range trigger.Variants {
		if err := ValidateEventPredicate(variant.When); err != nil {
			return err
		}
		if len(variant.Normalize) != len(trigger.Normalize) || len(variant.Resources) != len(trigger.Resources) {
			return errors.New("trigger variants must preserve all normalized field names and resource constraints")
		}
		for name, pointer := range variant.Normalize {
			if _, exists := trigger.Normalize[name]; !exists || !validPointer(pointer) {
				return errors.New("trigger variant normalization must preserve field names with valid JSON Pointers")
			}
		}
		expected := slices.Sorted(maps.Values(trigger.Resources))
		if !slices.Equal(expected, slices.Sorted(maps.Values(variant.Resources))) {
			return errors.New("trigger variants must preserve every resource constraint")
		}
		for pointer := range variant.Resources {
			if !validPointer(pointer) {
				return errors.New("trigger variants cannot add resource authority")
			}
		}
		if err := validatePrimitiveTypes(variant.Types); err != nil {
			return err
		}
	}
	return nil
}

func requireEventTransitionValues(rule *EventPredicate) error {
	if rule == nil {
		return nil
	}
	if (rule.Op == "enters" || rule.Op == "leaves") && len(rule.Values) == 0 {
		return errors.New("selected transition trigger requires explicitly configured terminal values (closed-states)")
	}
	for _, child := range append(slices.Clone(rule.All), rule.Any...) {
		if err := requireEventTransitionValues(&child); err != nil {
			return err
		}
	}
	return requireEventTransitionValues(rule.Items)
}
