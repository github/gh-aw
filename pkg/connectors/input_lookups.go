package connectors

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"
)

const maxInputLookups = 4

func (s *ConnectorSpec) validateInputLookups(name string, op Operation, phase string) error {
	if err := validateLookupResponseConstraints(op); err != nil {
		return err
	}
	if len(op.Lookups) == 0 {
		return nil
	}
	if len(op.Lookups) > maxInputLookups || len(op.TrustedInputs)+len(op.CreatedResources) != 0 || op.Preflight != nil || op.Creates != nil {
		return errors.New("lookups require at most four fixed reads without trusted-input or creation-receipt bindings")
	}
	targets := map[string]int{}
	for index, lookup := range op.Lookups {
		if lookup.Input == "" {
			if len(lookup.Project) != 0 {
				return errors.New("relationship lookup cannot project arguments")
			}
			continue
		}
		_, duplicate := targets[lookup.Input]
		if duplicate {
			return errors.New("lookup targets must be unique")
		}
		targets[lookup.Input] = index
		if err := validateLookupTarget(op, lookup); err != nil {
			return err
		}
	}
	if len(targets) == 0 {
		return errors.New("lookups require at least one alias target")
	}
	for index, lookup := range op.Lookups {
		read := lookup.operation(op.Credential)
		if read.Request.Endpoint != op.Request.Endpoint || (read.Request.Method != http.MethodGet &&
			(read.Request.Method != http.MethodPost || !strings.HasPrefix(strings.TrimSpace(read.Request.GraphQL), "query ") || writeGraphQLPattern.MatchString(read.Request.GraphQL))) {
			return errors.New("lookup must be a fixed read on the operation endpoint")
		}
		if len(read.Inputs) == 0 {
			return errors.New("lookup requires bounded operation inputs")
		}
		for input, field := range read.Inputs {
			if input == lookup.Input {
				continue
			}
			final, exists := op.Inputs[input]
			dependency, targeted := targets[input]
			if !exists || !sameLookupInput(field, final) || !field.Required || (targeted && dependency >= index) {
				return errors.New("lookup dependencies must be canonical required inputs resolved earlier; cycles are prohibited")
			}
		}
		if err := s.validateOperation(name, read, phase); err != nil {
			return err
		}
	}
	return nil
}

func sameLookupInput(left, right Input) bool {
	if (left.Minimum == nil) != (right.Minimum == nil) || (left.Maximum == nil) != (right.Maximum == nil) {
		return false
	}
	if left.Minimum != nil && *left.Minimum != *right.Minimum {
		return false
	}
	if left.Maximum != nil && *left.Maximum != *right.Maximum {
		return false
	}
	return left.Type == right.Type && left.Required == right.Required && left.MaxLength == right.MaxLength &&
		left.MinLength == right.MinLength && left.Sanitize == right.Sanitize && left.Pattern == right.Pattern && slices.Equal(left.Enum, right.Enum)
}

func validateLookupResponseConstraints(op Operation) error {
	for pointer, input := range op.ResponseContains {
		field, exists := op.Inputs[input]
		if strings.Count(pointer, "/*/") != 1 || !validPointer(pointer) || !exists || !field.Required || field.Type != "string" {
			return errors.New("responseContains requires an array-element pointer and a required string input")
		}
	}
	for pointer, maximum := range op.ResponseLimits {
		if (pointer != "" && !validPointer(pointer)) || maximum < 1 || maximum > 100 {
			return errors.New("responseLimits requires array pointers and maximum counts from 1 to 100")
		}
	}
	for input, pointers := range op.ResponseAnyInputs {
		field, exists := op.Inputs[input]
		if !exists || !field.Required || field.Type != "string" || len(pointers) < 1 || len(pointers) > 4 {
			return errors.New("responseAnyInputs requires a required string input and one to four identity pointers")
		}
		for _, pointer := range pointers {
			if !validPointer(pointer) {
				return errors.New("responseAnyInputs requires valid JSON Pointers")
			}
		}
	}
	return nil
}

func validateLookupTarget(op Operation, lookup InputLookup) error {
	field, exists := op.Inputs[lookup.Input]
	if !exists || !field.Required || field.Type != "string" || field.Sanitize != "" || op.Resources[lookup.Input] == "" {
		return errors.New("lookup targets must be required resource-constrained string inputs")
	}
	pointer := lookup.Project[lookup.Input]
	alias, exists := lookup.Inputs[lookup.Input]
	if !exists || !alias.Required || alias.Type != "string" || alias.Sanitize != "" ||
		lookup.Resources[lookup.Input] == "" || lookup.Resources[lookup.Input] == op.Resources[lookup.Input] ||
		len(lookup.Project) != 1 || pointer == "" || lookup.ResponseResources[pointer] != op.Resources[lookup.Input] {
		return errors.New("lookup requires an exact alias grant and an independently granted canonical projection")
	}
	if !slices.Contains(slices.Collect(maps.Values(lookup.ResponseInputs)), lookup.Input) && len(lookup.ResponseAnyInputs[lookup.Input]) == 0 {
		return errors.New("lookup must correlate its response with the exact requested alias")
	}
	return nil
}

// Only an invalid canonical shape can select an alias path. A well-formed but
// unapproved UUID must never fall back to a lookup with different authority.
func (p *Plan) validateLookupArguments(op Operation, args map[string]any) ([]string, error) {
	inputs, resources := maps.Clone(op.Inputs), maps.Clone(op.Resources)
	var unresolved []string
	for _, lookup := range op.Lookups {
		input := lookup.Input
		if input == "" || validateArgument(op.Inputs[input], args[input]) == nil {
			continue
		}
		inputs[input], resources[input] = lookup.Inputs[input], lookup.Resources[input]
		unresolved = append(unresolved, input)
	}
	if err := validateArguments(inputs, resources, p.connection.Resources, args); err != nil {
		return nil, err
	}
	if len(unresolved) > 0 {
		for _, lookup := range op.Lookups {
			if lookup.Input != "" && !slices.Contains(unresolved, lookup.Input) {
				continue
			}
			if err := requireResources(lookup.Resources, p.connection.Resources); err != nil {
				return nil, err
			}
			if err := requireResources(lookup.ResponseResources, p.connection.Resources); err != nil {
				return nil, err
			}
		}
	}
	slices.Sort(unresolved)
	return unresolved, nil
}

func (p *Plan) resolveInputLookups(ctx context.Context, runtime Runtime, op Operation, args map[string]any) (map[string]any, error) {
	unresolved, err := p.validateLookupArguments(op, args)
	if err != nil || len(unresolved) == 0 {
		return args, err
	}
	// Bound the entire chain, not just each individual HTTP request.
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	resolved := maps.Clone(args)
	for _, lookup := range op.Lookups {
		if lookup.Input != "" && !slices.Contains(unresolved, lookup.Input) {
			continue
		}
		read := lookup.operation(op.Credential)
		inputs := map[string]any{}
		for input := range read.Inputs {
			inputs[input] = resolved[input]
		}
		if err := validateArguments(read.Inputs, read.Resources, p.connection.Resources, inputs); err != nil {
			return nil, err
		}
		result, err := p.executeOperation(ctx, runtime, runtime.Phase, "lookup", read, inputs)
		if err != nil {
			return nil, err
		}
		if lookup.Input != "" {
			var projected map[string]any
			if err := decodeJSON(result.Data, &projected); err != nil {
				return nil, errors.New("lookup projection is invalid")
			}
			value := projected[lookup.Input]
			if err := validateArgument(op.Inputs[lookup.Input], value); err != nil {
				return nil, fmt.Errorf("lookup %q did not return a canonical identity", lookup.Input)
			}
			resolved[lookup.Input] = value
		}
	}
	if err := validateArguments(op.Inputs, op.Resources, p.connection.Resources, resolved); err != nil {
		return nil, err
	}
	return resolved, nil
}

func lookupPublicInputs(op Operation, inputs map[string]Input) map[string]Input {
	for _, lookup := range op.Lookups {
		if lookup.Input == "" {
			continue
		}
		field := inputs[lookup.Input]
		alias := lookup.Inputs[lookup.Input]
		field.MaxLength = max(field.MaxLength, alias.MaxLength)
		field.MinLength = min(field.MinLength, alias.MinLength)
		if field.Pattern != "" && alias.Pattern != "" {
			field.Pattern = "(?:" + field.Pattern + ")|(?:" + alias.Pattern + ")"
		} else {
			field.Pattern = ""
		}
		field.Enum = nil
		inputs[lookup.Input] = field
	}
	return inputs
}
