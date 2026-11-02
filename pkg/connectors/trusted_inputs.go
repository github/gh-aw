package connectors

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
)

func (preflight TrustedPreflight) operation(credential string) Operation {
	return Operation{
		Credential: credential, Inputs: preflight.Inputs, Request: preflight.Request, MaxResponseBytes: preflight.MaxResponseBytes,
		ResponseRequired: preflight.ResponseRequired, ResponseTypes: preflight.ResponseTypes, ResponseInputs: preflight.ResponseInputs,
		ResponseContains: preflight.ResponseContains, Project: preflight.Project,
		ResponseChecks: preflight.ResponseChecks,
	}
}

func (s *ConnectorSpec) validateTrustedInputs(name string, op Operation, phase string) error {
	if len(op.TrustedInputs) == 0 && op.Preflight == nil {
		return nil
	}
	if phase != "safe-output" || len(op.TrustedInputs) == 0 {
		return errors.New("trustedInputs and preflight require author-bound safe-output inputs")
	}
	for input, source := range op.TrustedInputs {
		field, exists := op.Inputs[input]
		if !exists || !field.Required || field.Type != "string" || op.Resources[input] == "" ||
			op.CreatedResources[input] != "" || (source.Secret == "") == (source.Value == "") {
			return fmt.Errorf("trusted input %q requires one secret name or literal and a required resource-constrained string input", input)
		}
		if source.Secret != "" && (!secretPattern.MatchString(source.Secret) || strings.HasPrefix(source.Secret, "GITHUB_")) {
			return fmt.Errorf("trusted input %q requires a non-reserved secret name", input)
		}
		if strings.Contains(source.Value, "${") || len(source.Value) > 1024 {
			return fmt.Errorf("trusted input %q requires a bounded literal, not an Actions expression", input)
		}
	}
	if op.Preflight == nil {
		return nil
	}
	lookup := op.Preflight.operation(op.Credential)
	if lookup.Request.Endpoint != op.Request.Endpoint ||
		(lookup.Request.Method != http.MethodGet && (lookup.Request.Method != http.MethodPost || !strings.HasPrefix(strings.TrimSpace(lookup.Request.GraphQL), "query ") || writeGraphQLPattern.MatchString(lookup.Request.GraphQL))) {
		return errors.New("trusted preflight must be a fixed read on the mutation endpoint")
	}
	if len(lookup.Inputs) != len(op.TrustedInputs) || len(lookup.Project) == 0 {
		return errors.New("trusted preflight requires exact trusted inputs and a bounded projection")
	}
	for input := range lookup.Inputs {
		if _, exists := op.TrustedInputs[input]; !exists {
			return errors.New("trusted preflight cannot consume agent-controlled inputs")
		}
	}
	for field := range lookup.Project {
		if _, exists := op.TrustedInputs[field]; !exists {
			return errors.New("trusted preflight can only bind declared trusted inputs")
		}
	}
	return s.validateOperation(name, lookup, "safe-output")
}

func (p *Plan) resolveTrustedInputs(ctx context.Context, runtime Runtime, name string, args map[string]any) (*Plan, map[string]any, error) {
	op, selected := p.spec.SafeOutputs[name]
	if !selected || !slices.Contains(p.binding.SafeOutputs, name) || runtime.Phase != "safe-output" || !runtime.AllowWrites {
		return nil, nil, errors.New("trusted connector input resolution requires selected safe-output host authorization")
	}
	if err := runtime.checkOrigin(p.spec.Endpoints[op.Request.Endpoint]); err != nil {
		return nil, nil, err
	}
	if err := p.validateUnboundArguments(op, args); err != nil {
		return nil, nil, err
	}
	bound, err := resolveTrustedSources(ctx, runtime, op.TrustedInputs)
	if err != nil {
		return nil, nil, err
	}
	if op.Preflight != nil {
		lookup := op.Preflight.operation(op.Credential)
		if err := validateArguments(lookup.Inputs, nil, nil, bound); err != nil {
			return nil, nil, errors.New("trusted preflight inputs violate their declared constraints")
		}
		result, err := p.executeOperation(ctx, runtime, "preflight", name, lookup, bound)
		if err != nil {
			return nil, nil, err
		}
		var projected map[string]any
		if err := decodeJSON(result.Data, &projected); err != nil {
			return nil, nil, errors.New("trusted preflight projection is invalid")
		}
		maps.Copy(bound, projected)
	}
	plan := *p
	plan.trustedOperation = name
	plan.connection.Resources = maps.Clone(p.connection.Resources)
	resolved := maps.Clone(args)
	for input, value := range bound {
		if err := validateArgument(op.Inputs[input], value); err != nil {
			return nil, nil, fmt.Errorf("trusted input %q violates the final resource constraint", input)
		}
		text, ok := value.(string)
		if !ok || text == "" {
			return nil, nil, fmt.Errorf("trusted input %q must resolve to one exact resource", input)
		}
		plan.connection.Resources[op.Resources[input]] = []string{text}
		resolved[input] = text
	}
	return &plan, resolved, nil
}

func (p *Plan) validateUnboundArguments(op Operation, args map[string]any) error {
	for input := range op.TrustedInputs {
		if _, supplied := args[input]; supplied {
			return fmt.Errorf("connector argument %q is author-bound and cannot be supplied by the agent", input)
		}
	}
	// Validate all agent-controlled fields before any secret lookup or request.
	intent := op
	intent.Inputs, intent.Resources = maps.Clone(op.Inputs), maps.Clone(op.Resources)
	for input := range op.TrustedInputs {
		delete(intent.Inputs, input)
		delete(intent.Resources, input)
	}
	for input := range op.CreatedResources {
		if value, ok := args[input].(string); ok && temporaryResourcePattern.MatchString(value) {
			delete(intent.Inputs, input)
			delete(intent.Resources, input)
		}
	}
	ordinary := maps.Clone(args)
	for input := range op.CreatedResources {
		if value, ok := args[input].(string); ok && temporaryResourcePattern.MatchString(value) {
			delete(ordinary, input)
		}
	}
	if err := validateArguments(intent.Inputs, intent.Resources, p.connection.Resources, ordinary); err != nil {
		return err
	}
	return nil
}

func resolveTrustedSources(ctx context.Context, runtime Runtime, sources map[string]TrustedInput) (map[string]any, error) {
	bound := map[string]any{}
	for input, source := range sources {
		value := source.Value
		if source.Secret != "" {
			if runtime.ResolveSecret == nil {
				return nil, errors.New("trusted input secret resolver is unavailable")
			}
			var err error
			value, err = runtime.ResolveSecret(ctx, "safe-output", source.Secret)
			if err != nil {
				return nil, fmt.Errorf("trusted input %q is unavailable", input)
			}
		}
		if value == "" {
			return nil, fmt.Errorf("trusted input %q is unavailable", input)
		}
		bound[input] = value
	}
	return bound, nil
}
