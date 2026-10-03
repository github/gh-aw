package workflow

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/github/gh-aw/pkg/syncutil"
)

//go:embed schemas/pi_engine_config.schema.json
var piEngineConfigSchema string

var piEngineConfigSchemaLoader syncutil.OnceLoader[*jsonschema.Schema]

func validatePiNestedConfig(raw string) error {
	schema, err := piEngineConfigSchemaLoader.Get(func() (*jsonschema.Schema, error) {
		return compileSchema(piEngineConfigSchema, "https://github.com/github/gh-aw/pi-engine-config.schema.json")
	})
	if err != nil {
		return fmt.Errorf("cannot load Pi configuration schema: %w", err)
	}
	var config any
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return fmt.Errorf("engine.config for Pi must be valid JSON: %w", err)
	}
	if err := schema.Validate(config); err != nil {
		return fmt.Errorf("engine.config for Pi has invalid nested configuration: %w", err)
	}
	return nil
}
