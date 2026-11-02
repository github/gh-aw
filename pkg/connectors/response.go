package connectors

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

func (p *Plan) validateResponse(op Operation, data []byte) ([]byte, error) {
	if len(data) == 0 {
		if len(op.ResponseChecks)+len(op.ResponseRequired)+len(op.ResponseResources)+len(op.Project) != 0 || op.Request.GraphQL != "" {
			return nil, errors.New("connector response is empty; required evidence is missing")
		}
		return []byte("null"), nil
	}
	var document any
	if err := decodeJSON(data, &document); err != nil {
		return nil, errors.New("connector response must be valid JSON")
	}
	if op.Request.GraphQL != "" {
		if failures, ok := lookupPointer(document, "/errors"); ok && failures != nil {
			entries, array := failures.([]any)
			if !array || len(entries) != 0 {
				return nil, errors.New("connector GraphQL response reports errors")
			}
		}
		if _, ok := lookupPointer(document, "/data"); !ok {
			return nil, errors.New("connector GraphQL response is missing data")
		}
	}
	if err := checkResponseEvidence(document, op); err != nil {
		return nil, err
	}
	if err := checkResponseResources(document, op.ResponseResources, p.connection.Resources); err != nil {
		return nil, err
	}
	if len(op.Project) != 0 {
		fields, err := projectFields(document, op.Project)
		if err != nil {
			return nil, err
		}
		return json.Marshal(fields)
	}
	return data, nil
}

func checkResponseEvidence(document any, op Operation) error {
	for pointer, expected := range op.ResponseChecks {
		actual, ok := lookupPointer(document, pointer)
		actualJSON, actualErr := json.Marshal(actual)
		expectedJSON, expectedErr := json.Marshal(expected)
		if !ok || actualErr != nil || expectedErr != nil || !bytes.Equal(actualJSON, expectedJSON) {
			return fmt.Errorf("connector response check failed at %q", pointer)
		}
	}
	for _, pointer := range op.ResponseRequired {
		value, ok := lookupPointer(document, pointer)
		if !ok || value == nil || value == "" {
			return fmt.Errorf("connector response is missing required evidence at %q", pointer)
		}
	}
	return nil
}

func checkResponseResources(document any, constraints map[string]string, resources map[string][]string) error {
	for pointer, resource := range constraints {
		value, ok := lookupPointer(document, pointer)
		text, isScalar := value.(string)
		if number, isNumber := value.(json.Number); isNumber {
			text, isScalar = number.String(), true
		}
		if !ok || !isScalar || !slices.Contains(resources[resource], text) {
			return fmt.Errorf("connector evidence at %q is outside approved resources", pointer)
		}
	}
	return nil
}
