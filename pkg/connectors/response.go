package connectors

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

func (p *Plan) validateResponse(op Operation, data []byte, args map[string]any) ([]byte, error) {
	if len(data) == 0 {
		if len(op.ResponseChecks)+len(op.ResponseRequired)+len(op.ResponseResources)+len(op.ResponseTypes)+len(op.ResponseInputs)+len(op.ResponseResourceInputs)+len(op.ResponseOptionalEmpty)+len(op.ResponseContains)+len(op.ResponseLimits)+len(op.ResponseAnyInputs)+len(op.Project) != 0 || op.Request.GraphQL != "" || op.ResponseWhen != nil {
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
	if op.ResponseWhen != nil {
		matched, known := EvaluateEventPredicate(*op.ResponseWhen, document)
		if !known || !matched {
			return nil, errors.New("connector response does not satisfy its required source-evidence policy")
		}
	}
	if err := checkResponseResources(document, op.ResponseResources, p.connection.Resources); err != nil {
		return nil, err
	}
	if err := checkResponseBindings(document, op, args); err != nil {
		return nil, err
	}
	for pointer, input := range op.ResponseContains {
		prefix, suffix, _ := strings.Cut(pointer, "/*")
		value, present := lookupPointer(document, prefix)
		items, array := value.([]any)
		matched := false
		for _, item := range items {
			actual, exists := lookupPointer(item, suffix)
			if exists && actual == args[input] {
				matched = true
			}
		}
		if !present || !array || !matched {
			return nil, errors.New("connector response does not establish the required resource relationship")
		}
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

func checkResponseBindings(document any, op Operation, args map[string]any) error {
	if err := checkPrimitiveTypes(document, op.ResponseTypes); err != nil {
		return err
	}
	for input, pointers := range op.ResponseAnyInputs {
		expected, supplied := args[input].(string)
		matched := false
		for _, pointer := range pointers {
			actual, present := lookupPointer(document, pointer)
			if present && supplied && actual == expected {
				matched = true
			}
		}
		if !matched {
			return errors.New("connector response does not match the requested alias identity")
		}
	}
	for pointer, input := range op.ResponseInputs {
		expected, supplied := args[input]
		expectedJSON, expectedErr := json.Marshal(expected)
		values, present := responsePointerValues(document, pointer)
		if !present || !supplied || expectedErr != nil {
			return fmt.Errorf("connector response does not match request input at %q", pointer)
		}
		for _, actual := range values {
			actualJSON, actualErr := json.Marshal(actual)
			if actualErr != nil || !bytes.Equal(actualJSON, expectedJSON) {
				return fmt.Errorf("connector response does not match request input at %q", pointer)
			}
		}
	}
	for pointer, input := range op.ResponseResourceInputs {
		actual, present := lookupPointer(document, pointer)
		expected, supplied := args[input]
		actualID, valid := responseResourceIdentity(actual)
		expectedID, expectedString := expected.(string)
		if !present || !supplied || !valid || !expectedString || actualID != expectedID {
			return fmt.Errorf("connector response resource identity does not match request input at %q", pointer)
		}
	}
	return nil
}

func responseResourceIdentity(value any) (string, bool) {
	if text, ok := value.(string); ok {
		return text, text != ""
	}
	number, ok := value.(json.Number)
	if !ok {
		return "", false
	}
	text := number.String()
	if text == "" || (len(text) > 1 && strings.HasPrefix(text, "0")) {
		return "", false
	}
	for _, character := range text {
		if character < '0' || character > '9' {
			return "", false
		}
	}
	if _, err := strconv.ParseUint(text, 10, 53); err != nil {
		return "", false
	}
	return text, true
}

func checkPrimitiveTypes(document any, types map[string]string) error {
	for pointer, kind := range types {
		value, ok := lookupPointer(document, pointer)
		// Primitive outputs use the same type validator as primitive inputs.
		if !ok || validateArgument(Input{Type: kind, MaxLength: MaxDocumentBytes}, value) != nil {
			return fmt.Errorf("connector response has unexpected type at %q", pointer)
		}
	}
	return nil
}

func checkResponseEvidence(document any, op Operation) error {
	for pointer, maximum := range op.ResponseLimits {
		value, exists := lookupPointer(document, pointer)
		array, valid := value.([]any)
		if !exists || !valid || len(array) > maximum {
			return errors.New("connector response array is missing or exceeds its declared result bound")
		}
	}
	for _, pointer := range op.ResponseOptionalEmpty {
		value, present := lookupPointer(document, pointer)
		if present && value != nil && value != "" {
			return fmt.Errorf("connector response requires absent, null, or empty textual metadata at %q", pointer)
		}
	}
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
		values, ok := responsePointerValues(document, pointer)
		if !ok {
			return fmt.Errorf("connector evidence at %q is outside approved resources", pointer)
		}
		for _, value := range values {
			text, isScalar := value.(string)
			if number, isNumber := value.(json.Number); isNumber {
				text, isScalar = number.String(), true
			}
			if !isScalar || !slices.Contains(resources[resource], text) {
				return fmt.Errorf("connector evidence at %q is outside approved resources", pointer)
			}
		}
	}
	return nil
}

func responsePointerValues(document any, pointer string) ([]any, bool) {
	prefix, suffix, wildcard := strings.Cut(pointer, "/*")
	if !wildcard {
		value, ok := lookupPointer(document, pointer)
		return []any{value}, ok
	}
	if strings.Contains(suffix, "*") || (suffix != "" && !strings.HasPrefix(suffix, "/")) {
		return nil, false
	}
	value, present := lookupPointer(document, prefix)
	array, ok := value.([]any)
	if !present || !ok || len(array) > 100 {
		return nil, false
	}
	values := make([]any, 0, len(array))
	for _, entry := range array {
		value, ok := lookupPointer(entry, suffix)
		if !ok {
			return nil, false
		}
		values = append(values, value)
	}
	return values, true
}
