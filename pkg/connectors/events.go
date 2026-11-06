package connectors

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

var ErrIngressUnavailable = errors.New("custom connector ingress is not operational: no approved receiver, source enrollment, provenance or admission integration is installed")

type EventProjection struct {
	Connector   string         `json:"connector"`
	Trigger     string         `json:"trigger"`
	Untrusted   bool           `json:"untrusted"`
	Operational bool           `json:"operational"`
	Fields      map[string]any `json:"fields"`
}

// ProjectEvent validates fixture evidence and projects native payload fields.
// It deliberately does not authenticate, enroll, dispatch, or activate events.
func (p *Plan) ProjectEvent(name, nativeEvent string, payload []byte) (*EventProjection, error) {
	if !slices.Contains(p.binding.Triggers, name) {
		return nil, fmt.Errorf("trigger %q is not selected", name)
	}
	trigger := p.spec.Triggers[name]
	if !slices.Contains(trigger.NativeEvents, nativeEvent) {
		return nil, errors.New("native event is not declared for the selected trigger")
	}
	if len(payload) > MaxDocumentBytes {
		return nil, errors.New("trigger payload exceeds the bounded fixture size")
	}
	var document any
	if err := decodeJSON(payload, &document); err != nil {
		return nil, fmt.Errorf("decode trigger evidence: %w", err)
	}
	if trigger.When != nil {
		matched, known := EvaluateEventPredicate(*trigger.When, document)
		if !known {
			return nil, errors.New("trigger has insufficient transition evidence")
		}
		if !matched {
			return nil, errors.New("trigger transition predicate did not match")
		}
	}
	var err error
	trigger, err = selectEventVariant(trigger, document)
	if err != nil {
		return nil, err
	}
	if err := checkPrimitiveTypes(document, trigger.Types); err != nil {
		return nil, err
	}
	for pointer, expected := range trigger.Match {
		actual, ok := lookupPointer(document, pointer)
		if !ok || actual != expected {
			return nil, fmt.Errorf("trigger evidence does not match %q", pointer)
		}
	}
	if err := checkResponseResources(document, trigger.Resources, p.connection.Resources); err != nil {
		return nil, err
	}
	if err := checkEventFilters(trigger.Filters, document); err != nil {
		return nil, err
	}
	fields, err := projectFields(document, trigger.Normalize)
	if err != nil {
		return nil, err
	}
	if err := checkEventURL(fields, trigger, document); err != nil {
		return nil, err
	}
	return &EventProjection{Connector: p.spec.ID, Trigger: name, Untrusted: true, Operational: false, Fields: fields}, nil
}

func selectEventVariant(trigger Trigger, document any) (Trigger, error) {
	if len(trigger.Variants) != 0 {
		var selected *EventVariant
		for index := range trigger.Variants {
			variant := &trigger.Variants[index]
			matched, known := EvaluateEventPredicate(variant.When, document)
			if !known || !matched {
				continue
			}
			if selected != nil {
				return Trigger{}, errors.New("trigger evidence matches multiple payload variants")
			}
			selected = variant
		}
		if selected == nil {
			return Trigger{}, errors.New("trigger evidence does not match a supported payload variant")
		}
		trigger.Normalize, trigger.Resources, trigger.Types = selected.Normalize, selected.Resources, selected.Types
	}
	return trigger, nil
}

func checkEventFilters(filters map[string]EventFilter, document any) error {
	for name, filter := range filters {
		if len(filter.Values) == 0 {
			continue
		}
		matched := false
		for _, value := range eventFilterEvidence(document, filter.Path) {
			if validateArgument(Input{Type: filter.Type, MaxLength: 1024}, value) != nil {
				continue
			}
			for _, expected := range filter.Values {
				if equalEventValue(value, expected) {
					matched = true
				}
			}
		}
		if !matched {
			return fmt.Errorf("trigger filter %q did not match", name)
		}
	}
	return nil
}

func checkEventURL(fields map[string]any, trigger Trigger, document any) error {
	if raw, exists := fields["url"]; exists {
		text, ok := raw.(string)
		parsed, err := url.Parse(text)
		if !ok || err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return errors.New("trigger URL must be a canonical HTTPS resource reference")
		}
		if len(trigger.ResourceOrigins) != 0 && !slices.Contains(trigger.ResourceOrigins, parsed.Scheme+"://"+strings.ToLower(parsed.Host)) {
			return errors.New("trigger URL is outside approved resource origins")
		}
		if trigger.URLIdentity != "" {
			value, present := lookupPointer(document, trigger.URLIdentity)
			identity, valid := responseResourceIdentity(value)
			if !present || !valid || parsed.EscapedPath() != trigger.URLPathPrefix+url.PathEscape(identity) {
				return errors.New("trigger URL does not identify the exact declared source resource")
			}
		}
	}
	return nil
}

func equalEventValue(left, right any) bool {
	if eventPrimitiveType(left) == "integer" && eventPrimitiveType(right) == "integer" && eventComparable(left) && eventComparable(right) {
		l, _ := numericValue(left)
		r, _ := numericValue(right)
		return l == r
	}
	l, le := json.Marshal(left)
	r, re := json.Marshal(right)
	return le == nil && re == nil && bytes.Equal(l, r)
}

func eventFilterEvidence(document any, pointer string) []any {
	prefix, suffix, wildcard := strings.Cut(pointer, "/*")
	value, present := lookupPointer(document, prefix)
	if !present {
		return nil
	}
	if !wildcard {
		return []any{value}
	}
	array, ok := value.([]any)
	if !ok || len(array) > eventArrayLimit {
		return nil
	}
	var result []any
	for _, entry := range array {
		if suffix == "" {
			result = append(result, entry)
		} else if !strings.Contains(suffix, "*") {
			result = append(result, eventFilterEvidence(entry, suffix)...)
		}
	}
	return result
}

// ActivateTriggers always fails until an approved receiver and gateway source
// enrollment path exists. A validated profile identifier is not authorization.
func (p *Plan) ActivateTriggers() error {
	return ErrIngressUnavailable
}
