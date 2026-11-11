package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// SafeOutputRequest is a credential-free typed request artifact. Host safe-output
// handling must authorize writes and execute it in a separate phase. Preparing
// this request is never an execution result.
type SafeOutputRequest struct {
	Digest           string         `json:"digest"`
	Operation        string         `json:"operation"`
	Arguments        map[string]any `json:"arguments"`
	UnresolvedInputs []string       `json:"unresolvedInputs,omitempty"`
}

func (p *Plan) PrepareSafeOutput(name string, args map[string]any) (*SafeOutputRequest, error) {
	if !slices.Contains(p.binding.SafeOutputs, name) {
		return nil, errors.New("connector safe-output operation is not selected")
	}
	op := p.spec.SafeOutputs[name]
	if len(op.TrustedInputs) > 0 && p.trustedOperation != name {
		return nil, errors.New("trusted connector inputs must be resolved by the safe-output host")
	}
	unresolved, err := p.validateLookupArguments(op, args)
	if err != nil {
		return nil, err
	}
	if len(unresolved) == 0 {
		if _, err := p.buildRequest(context.Background(), op, args); err != nil {
			return nil, err
		}
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("encode safe-output arguments: %w", err)
	}
	if len(encoded) > MaxDocumentBytes {
		return nil, errors.New("safe-output arguments exceed byte bound")
	}
	var frozen map[string]any
	if err := decodeJSON(encoded, &frozen); err != nil {
		return nil, fmt.Errorf("freeze safe-output arguments: %w", err)
	}
	return &SafeOutputRequest{Digest: p.Digest, Operation: name, Arguments: frozen, UnresolvedInputs: unresolved}, nil
}

func (p *Plan) ExecuteSafeOutput(ctx context.Context, runtime Runtime, request SafeOutputRequest) (*Result, error) {
	if request.Digest != p.Digest {
		return nil, errors.New("connector safe-output request does not match the frozen plan digest")
	}
	unresolved, err := p.validateLookupArguments(p.spec.SafeOutputs[request.Operation], request.Arguments)
	if err != nil || !slices.Equal(unresolved, request.UnresolvedInputs) {
		return nil, errors.New("connector intent unresolved inputs do not match its arguments")
	}
	return p.Execute(ctx, runtime, "safe-output", request.Operation, request.Arguments)
}
