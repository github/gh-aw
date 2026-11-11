package connectors

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

func (s *ConnectorSpec) validateOperationChecks(name string, op Operation, phase string) error {
	if len(op.Checks) > 4 || (len(op.Checks) != 0 && phase != "safe-output") {
		return errors.New("checks require at most four safe-output pre-mutation reads")
	}
	for _, check := range op.Checks {
		read := check.operation(op.Credential)
		if read.Request.Endpoint != op.Request.Endpoint || len(read.Project) != 0 ||
			(read.Request.Method != http.MethodGet && (read.Request.Method != http.MethodPost || !strings.HasPrefix(strings.TrimSpace(read.Request.GraphQL), "query ") || writeGraphQLPattern.MatchString(read.Request.GraphQL))) {
			return errors.New("checks require fixed reads on the operation endpoint without argument projection")
		}
		for input, field := range read.Inputs {
			final, exists := op.Inputs[input]
			if !exists || !sameLookupInput(field, final) || !field.Required {
				return errors.New("check inputs must be canonical required inputs of the final operation")
			}
		}
		for input, resource := range read.Resources {
			if op.Resources[input] != resource {
				return errors.New("checks cannot change an operation input's resource grant")
			}
		}
		if err := s.validateOperation(name, read, phase); err != nil {
			return err
		}
	}
	return nil
}

func (p *Plan) executeOperationChecks(ctx context.Context, runtime Runtime, op Operation, args map[string]any) error {
	for _, check := range op.Checks {
		inputs := map[string]any{}
		for input := range check.Inputs {
			inputs[input] = args[input]
		}
		if err := validateArguments(check.Inputs, check.Resources, p.connection.Resources, inputs); err != nil {
			return fmt.Errorf("pre-mutation check arguments: %w", err)
		}
		if _, err := p.executeOperation(ctx, runtime, "check", "pre-mutation", check.operation(op.Credential), inputs); err != nil {
			return fmt.Errorf("pre-mutation check rejected the effect: %w", err)
		}
	}
	return nil
}
