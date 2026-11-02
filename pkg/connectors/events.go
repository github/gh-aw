package connectors

import (
	"errors"
	"fmt"
	"slices"
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
	fields, err := projectFields(document, trigger.Normalize)
	if err != nil {
		return nil, err
	}
	return &EventProjection{Connector: p.spec.ID, Trigger: name, Untrusted: true, Operational: false, Fields: fields}, nil
}

// ActivateTriggers always fails until an approved receiver and gateway source
// enrollment path exists. A validated profile identifier is not authorization.
func (p *Plan) ActivateTriggers() error {
	return ErrIngressUnavailable
}
