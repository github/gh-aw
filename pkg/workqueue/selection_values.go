package workqueue

import (
	"cmp"
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

type scalarValue struct {
	kind    int
	boolean bool
	number  float64
	text    string
}

func scalarValueOf(value any) (scalarValue, error) {
	switch value := value.(type) {
	case nil:
		return scalarValue{}, nil
	case bool:
		return scalarValue{kind: 1, boolean: value}, nil
	case float64:
		return scalarValue{kind: 2, number: value}, nil
	case string:
		return scalarValue{kind: 3, text: value}, nil
	default:
		return scalarValue{}, errors.New("selection field must contain a scalar value")
	}
}

func compareScalar(left, right scalarValue) int {
	if left.kind != right.kind {
		return left.kind - right.kind
	}
	switch left.kind {
	case 1:
		if left.boolean != right.boolean {
			if left.boolean {
				return 1
			}
			return -1
		}
	case 2:
		return cmp.Compare(left.number, right.number)
	case 3:
		return strings.Compare(left.text, right.text)
	}
	return 0
}

type compiledCondition struct {
	field  string
	op     string
	value  scalarValue
	values []scalarValue
	exists bool
}

func compileConditions(conditions []Condition) ([]compiledCondition, error) {
	result := make([]compiledCondition, 0, len(conditions))
	for _, condition := range conditions {
		var value any
		if err := json.Unmarshal(condition.Value, &value); err != nil {
			return nil, err
		}
		item := compiledCondition{field: condition.Field, op: condition.Op}
		switch condition.Op {
		case "exists":
			exists, ok := value.(bool)
			if !ok {
				return nil, errors.New("exists filter value must be a boolean")
			}
			item.exists = exists
		case "in":
			values, ok := value.([]any)
			if !ok {
				return nil, errors.New("in filter value must be an array")
			}
			for _, value := range values {
				scalar, err := scalarValueOf(value)
				if err != nil {
					return nil, err
				}
				item.values = append(item.values, scalar)
			}
		default:
			var err error
			item.value, err = scalarValueOf(value)
			if err != nil {
				return nil, err
			}
		}
		result = append(result, item)
	}
	return result, nil
}

func matches(payload any, condition compiledCondition) bool {
	field := lookup(payload, condition.field)
	if condition.op == "exists" {
		return field.Present == condition.exists
	}
	if !field.Present {
		return false
	}
	value, err := scalarValueOf(field.Value)
	if err != nil {
		return false
	}
	if condition.op == "in" {
		return slices.ContainsFunc(condition.values, func(expected scalarValue) bool { return compareScalar(value, expected) == 0 })
	}
	comparison := compareScalar(value, condition.value)
	switch condition.op {
	case "eq":
		return comparison == 0
	case "ne":
		return comparison != 0
	}
	if value.kind != condition.value.kind {
		return false
	}
	switch condition.op {
	case "lt":
		return comparison < 0
	case "lte":
		return comparison <= 0
	case "gt":
		return comparison > 0
	case "gte":
		return comparison >= 0
	}
	return false
}

func groupKey(payload any, group *GroupPolicy) (string, error) {
	if group == nil {
		return "", nil
	}
	values := make([]fieldValue, 0, len(group.Fields))
	for _, field := range group.Fields {
		value := lookup(payload, field)
		if !scalar(value.Value) {
			return "", errors.New("group fields must contain scalar values")
		}
		values = append(values, value)
	}
	key, err := json.Marshal(values)
	return string(key), err
}

type sortValue struct {
	value      scalarValue
	present    bool
	descending bool
}

func sortValues(payload any, fields []SortField) ([]sortValue, error) {
	values := make([]sortValue, 0, len(fields))
	for _, field := range fields {
		value := lookup(payload, field.Field)
		scalar, err := scalarValueOf(value.Value)
		if err != nil {
			return nil, err
		}
		values = append(values, sortValue{value: scalar, present: value.Present, descending: field.Direction == "desc"})
	}
	return values, nil
}

func compareCandidates(a, b candidate) (int, error) {
	if len(a.sort) != len(b.sort) {
		return 0, errors.New("work queue candidates have inconsistent sort objectives")
	}
	rightFields := b.sort
	for i, left := range a.sort {
		if i >= 0 && i < len(rightFields) {
			right := rightFields[i]
			if left.present != right.present {
				if left.present {
					return -1, nil
				}
				return 1, nil
			}
			comparison := compareScalar(left.value, right.value)
			if left.descending {
				comparison = -comparison
			}
			if comparison != 0 {
				return comparison, nil
			}
		} else {
			return 0, errors.New("work queue candidates have inconsistent sort objectives")
		}
	}
	return cmp.Or(cmp.Compare(a.work.Sequence, b.work.Sequence), strings.Compare(a.work.WorkID, b.work.WorkID)), nil
}

func sortCandidates(candidates []candidate) error {
	var sortErr error
	slices.SortFunc(candidates, func(a, b candidate) int {
		if sortErr != nil {
			return 0
		}
		comparison, err := compareCandidates(a, b)
		if err != nil {
			sortErr = err
		}
		return comparison
	})
	return sortErr
}
