package actionslanguageservice

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
)

//go:embed workflow-v1.0.json
var workflowSchema []byte

type definition struct {
	Context       []string          `json:"context"`
	OneOf         []string          `json:"one-of"`
	AllowedValues []string          `json:"allowed-values"`
	String        *stringDefinition `json:"string"`
	Number        *struct{}         `json:"number"`
	Boolean       *struct{}         `json:"boolean"`
	Null          *struct{}         `json:"null"`
	Sequence      *struct {
		ItemType string `json:"item-type"`
	} `json:"sequence"`
	Mapping *struct {
		Properties map[string]property `json:"properties"`
		LooseKey   string              `json:"loose-key-type"`
		LooseValue string              `json:"loose-value-type"`
	} `json:"mapping"`
}

type stringDefinition struct {
	Constant        string `json:"constant"`
	RequireNonEmpty bool   `json:"require-non-empty"`
}

type property struct {
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

func (p *property) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		return json.Unmarshal(data, &p.Type)
	}
	type plain property
	return json.Unmarshal(data, (*plain)(p))
}

var loadSchema = sync.OnceValues(func() (map[string]definition, error) {
	var schema struct {
		Definitions map[string]definition `json:"definitions"`
	}
	if err := json.Unmarshal(workflowSchema, &schema); err != nil {
		return nil, fmt.Errorf("loading Actions language schema: %w", err)
	}
	for name, source := range map[string]string{
		"null": `{"null":{}}`, "boolean": `{"boolean":{}}`,
		"number": `{"number":{}}`, "string": `{"string":{}}`,
		"sequence": `{"sequence":{"item-type":"any"}}`,
		"mapping":  `{"mapping":{"loose-key-type":"string","loose-value-type":"any"}}`,
		"any":      `{"one-of":["null","boolean","number","string","sequence","mapping"]}`,
	} {
		var d definition
		if err := json.Unmarshal([]byte(source), &d); err != nil {
			return nil, err
		}
		schema.Definitions[name] = d
	}
	return schema.Definitions, nil
})

func expandDefinitions(schema map[string]definition, name string) ([]definition, error) {
	d, exists := schema[name]
	if !exists {
		return nil, fmt.Errorf("actions schema definition %q not found", name)
	}
	if len(d.AllowedValues) > 0 {
		values := make([]definition, 0, len(d.AllowedValues))
		for _, value := range d.AllowedValues {
			values = append(values, definition{String: &stringDefinition{Constant: value}})
		}
		return values, nil
	}
	if len(d.OneOf) == 0 {
		return []definition{d}, nil
	}
	var result []definition
	for _, name := range d.OneOf {
		children, err := expandDefinitions(schema, name)
		if err != nil {
			return nil, err
		}
		result = append(result, children...)
	}
	return result, nil
}
