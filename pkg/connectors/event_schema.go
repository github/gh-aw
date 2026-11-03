package connectors

import (
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

func eventPredicateSchema() (*jsonschema.Schema, error) {
	const definition = `{
	  "type":"object", "additionalProperties":false,
	  "properties":{
	    "all":{"type":"array","minItems":1,"maxItems":64,"items":{"$ref":"#/$defs/eventPredicate"}},
	    "any":{"type":"array","minItems":1,"maxItems":64,"items":{"$ref":"#/$defs/eventPredicate"}},
	    "path":{"type":"string","pattern":"^/","maxLength":1024},
	    "op":{"enum":["equals","present","absent","null","timestamp","text","changed","set-added","set-removed","enters","leaves","length","any-item","prefix","path-prefix","member-prefix","url-id"]},
	    "value":{"type":["string","integer","boolean"]},
	    "other":{"type":"string","pattern":"^/","maxLength":1024},
	    "fallback":{"type":"string","pattern":"^/","maxLength":1024},
	    "values":{"type":"array","maxItems":100,"items":{"type":"string","minLength":1,"maxLength":1024}},
	    "separator":{"type":"string","minLength":1,"maxLength":1},
	    "root":{"type":"boolean"},
	    "items":{"$ref":"#/$defs/eventPredicate"}
	  },
	  "oneOf":[
	    {"required":["all"],"maxProperties":1},
	    {"required":["any"],"maxProperties":1},
	    {"required":["path","op"],"not":{"anyOf":[{"required":["all"]},{"required":["any"]}]}}
	  ]
	}`
	var schema jsonschema.Schema
	if err := json.Unmarshal([]byte(definition), &schema); err != nil {
		return nil, fmt.Errorf("decode event predicate schema: %w", err)
	}
	return &schema, nil
}
