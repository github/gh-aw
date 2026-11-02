package connectors

import (
	"encoding/json"
	"fmt"
	"os"
)

// LoadServiceSpec supports self-contained Markdown imports. Only the connection
// is repository-local; the caller's spec is strictly decoded and copied first.
func LoadServiceSpec(root string, spec ConnectorSpec, connectionPath string) (*Service, error) {
	data, err := json.Marshal(spec)
	if err != nil {
		return nil, fmt.Errorf("encode inline connector definition: %w", err)
	}
	var frozen ConnectorSpec
	if err := Decode(data, &frozen); err != nil {
		return nil, err
	}
	if err := frozen.Validate(); err != nil {
		return nil, err
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("open connector repository root: %w", err)
	}
	defer func() {
		if err := dir.Close(); err != nil {
			log.Printf("close connector repository root: %v", err)
		}
	}()
	var connection Connection
	if err := readLocal(dir, connectionPath, &connection); err != nil {
		return nil, err
	}
	if err := validateConnection(frozen, connection); err != nil {
		return nil, err
	}
	return &Service{spec: frozen, connection: connection, definition: "inline:" + frozen.ID, path: connectionPath}, nil
}
