package workqueue

import (
	"encoding/json"
	"slices"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

var requestSchemas = sync.OnceValues(func() (map[string]*jsonschema.Schema, error) {
	compiler := jsonschema.NewCompiler()
	for _, name := range []string{"Actor", "QueueRequest"} {
		resource, err := contractIdentitySchema(name)
		if err != nil {
			return nil, err
		}
		if err := compiler.AddResource(name+".json", resource); err != nil {
			return nil, err
		}
	}
	result := map[string]*jsonschema.Schema{}
	for _, name := range []string{"Actor", "QueueRequest"} {
		schema, err := compiler.Compile(name + ".json")
		if err != nil {
			return nil, err
		}
		result[name] = schema
	}
	return result, nil
})

var requestRoleKinds = map[string][]string{
	"administrator": {"policy", "control", "submit", "dispatch_next", "observe", "cancel_work"},
	"producer":      {"submit", "cancel_work"},
	"dispatcher":    {"submit", "dispatch_next", "observe", "dispatch"},
	"worker":        {"submit", "dispatch_next", "observe", "finish", "dispatch"},
	"reconciler":    {"observe", "dispatch", "release", "result", "delivery_failure", "cancel_claim", "cancel_work"},
}

func validateRequestOrigin(actor Actor, request Request) error {
	compiled, err := requestSchemas()
	if err != nil {
		return err
	}
	for _, input := range []struct {
		name  string
		value any
	}{{"Actor", actor}, {"QueueRequest", request}} {
		data, err := canonicalValue(input.value)
		if err != nil {
			return err
		}
		if len(data) > maxLineBytes {
			return queueError("resource_limit", "request origin exceeds parser bound")
		}
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		if err := compiled[input.name].Validate(value); err != nil {
			return queueError("request_invalid", "invalid closed %s: %v", input.name, err)
		}
		if err := validateContractIdentityBytes(input.name, value); err != nil {
			return err
		}
	}
	if err := validateActorOrigin(actor); err != nil {
		return err
	}
	fingerprint, err := Fingerprint(actor, request.Kind, request.Parameters)
	if err != nil {
		return err
	}
	if fingerprint != request.Fingerprint {
		return queueError("request_fingerprint", "request origin does not bind actor and immutable intent")
	}
	return nil
}

func validateActorOrigin(actor Actor) error {
	if !decimalIdentity(actor.Principal) || !repoPattern.MatchString(actor.Repository) ||
		(actor.RunID == "") != (actor.RunAttempt == 0) ||
		actor.RunID != "" && !decimalIdentity(actor.RunID) {
		return queueError("actor_unauthorized", "request origin requires normalized repository and paired positive native run provenance")
	}
	if actor.Role == "worker" && (actor.Workflow == "" || actor.DispatchID == "" || actor.RunAttempt != 1) {
		return queueError("actor_unauthorized", "worker origin requires original workflow/run/dispatch provenance")
	}
	return nil
}

func validateRequestRole(actor Actor, kind string) error {
	if !slices.Contains(requestRoleKinds[actor.Role], kind) {
		return queueError("actor_unauthorized", "%s cannot publish %s", actor.Role, kind)
	}
	return nil
}
