package connectors

import (
	"encoding/json"
	"fmt"
)

// NewService freezes an AW declaration and its inline grants without file I/O.
func NewService(spec ConnectorSpec, connection Connection) (*Service, error) {
	data, err := json.Marshal(struct {
		Spec       ConnectorSpec
		Connection Connection
	}{spec, connection})
	if err != nil {
		return nil, fmt.Errorf("encode connector declaration: %w", err)
	}
	var frozen struct {
		Spec       ConnectorSpec
		Connection Connection
	}
	if err := Decode(data, &frozen); err != nil {
		return nil, err
	}
	if err := frozen.Spec.Validate(); err != nil {
		return nil, err
	}
	if err := validateConnection(frozen.Spec, frozen.Connection); err != nil {
		return nil, err
	}
	return &Service{
		spec: frozen.Spec, connection: frozen.Connection,
		definition: "aw:" + frozen.Spec.ID, path: "aw:" + frozen.Spec.ID,
	}, nil
}
