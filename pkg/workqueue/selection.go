package workqueue

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type Condition struct {
	Field string          `json:"field"`
	Op    string          `json:"op"`
	Value json.RawMessage `json:"value"`
}

type SortField struct {
	Field     string `json:"field"`
	Direction string `json:"direction,omitempty"`
}

type GroupPolicy struct {
	Fields    []string `json:"fields"`
	MaxActive int      `json:"max_active,omitempty"`
}

type Selection struct {
	Filter []Condition  `json:"filter,omitempty"`
	Sort   []SortField  `json:"sort,omitempty"`
	Group  *GroupPolicy `json:"group,omitempty"`
}

func ParseSelection(data []byte) (Selection, error) {
	var selection Selection
	if len(bytes.TrimSpace(data)) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return selection, errors.New("selection must be a JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&selection); err != nil {
		return selection, fmt.Errorf("invalid selection: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return selection, errors.New("selection must contain exactly one JSON object")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return selection, err
	}
	for _, field := range []string{"filter", "sort", "group"} {
		if value, present := raw[field]; present && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return selection, fmt.Errorf("selection %s must not be null", field)
		}
	}
	var rawSort []map[string]json.RawMessage
	if err := json.Unmarshal(raw["sort"], &rawSort); len(raw["sort"]) > 0 && err != nil {
		return selection, err
	}
	for _, field := range rawSort {
		if _, present := field["direction"]; present {
			var direction string
			if json.Unmarshal(field["direction"], &direction) != nil || direction != "asc" && direction != "desc" {
				return selection, errors.New("sort direction must be asc or desc")
			}
		}
	}
	if selection.Group != nil {
		var group map[string]json.RawMessage
		if err := json.Unmarshal(raw["group"], &group); err != nil {
			return selection, err
		}
		if _, present := group["max_active"]; present && selection.Group.MaxActive == 0 {
			return selection, errors.New("group max_active must be between 1 and 100")
		}
	}
	return selection, selection.Validate()
}

var pointerPattern = regexp.MustCompile(`^/(?:[^~]|~[01])*$`)

func validPointer(field string) bool { return pointerPattern.MatchString(field) }

func scalar(value any) bool {
	switch value.(type) {
	case nil, bool, float64, string:
		return true
	}
	return false
}

func (selection Selection) Validate() error {
	if len(selection.Filter) > 32 || len(selection.Sort) > 32 {
		return errors.New("selection supports at most 32 filters and sort fields")
	}
	for _, condition := range selection.Filter {
		if !validPointer(condition.Field) {
			return errors.New("filter field must be a JSON Pointer into the work payload")
		}
		var value any
		if len(condition.Value) == 0 || json.Unmarshal(condition.Value, &value) != nil {
			return errors.New("filter value is required and must be JSON")
		}
		switch condition.Op {
		case "eq", "ne", "gt", "gte", "lt", "lte":
			if !scalar(value) {
				return errors.New("filter value must be a scalar")
			}
		case "in":
			values, ok := value.([]any)
			if !ok || len(values) > 100 || len(values) == 0 {
				return errors.New("in filter value must be a non-empty array of at most 100 scalars")
			}
			if slices.ContainsFunc(values, func(value any) bool { return !scalar(value) }) {
				return errors.New("in filter values must be scalars")
			}
		case "exists":
			if _, ok := value.(bool); !ok {
				return errors.New("exists filter value must be a boolean")
			}
		default:
			return fmt.Errorf("unknown filter operator %q", condition.Op)
		}
	}
	for _, field := range selection.Sort {
		if !validPointer(field.Field) || field.Direction != "" && field.Direction != "asc" && field.Direction != "desc" {
			return errors.New("sort requires a JSON Pointer field and asc or desc direction")
		}
	}
	if selection.Group != nil {
		if len(selection.Group.Fields) < 1 || len(selection.Group.Fields) > 8 || selection.Group.MaxActive < 0 || selection.Group.MaxActive > 100 {
			return errors.New("group requires 1-8 fields and max_active between 1 and 100 (default 1)")
		}
		for _, field := range selection.Group.Fields {
			if !validPointer(field) {
				return errors.New("group field must be a JSON Pointer into the work payload")
			}
		}
	}
	return nil
}

type fieldValue struct {
	Value   any  `json:"value"`
	Present bool `json:"present"`
}

func lookup(payload any, pointer string) fieldValue {
	value := payload
	for part := range strings.SplitSeq(strings.TrimPrefix(pointer, "/"), "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		switch object := value.(type) {
		case map[string]any:
			var ok bool
			value, ok = object[part]
			if !ok {
				return fieldValue{}
			}
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || strconv.Itoa(index) != part {
				return fieldValue{}
			}
			if index >= 0 && index < len(object) {
				value = object[index]
			} else {
				return fieldValue{}
			}
		default:
			return fieldValue{}
		}
	}
	if number, ok := value.(float64); ok && number == 0 {
		value = float64(0)
	}
	return fieldValue{Value: value, Present: true}
}

type candidate struct {
	work  WorkState
	sort  []sortValue
	group string
}

// SelectNext never considers claimed or terminal work. Objectives are followed
// by FIFO and stable identity tie-breaks; grouping counts all active work,
// including work excluded by this dispatcher's filter.
func SelectNext(transactions []Transaction, selection Selection) (*WorkState, error) {
	if err := selection.Validate(); err != nil {
		return nil, err
	}
	projection, err := Replay(transactions)
	if err != nil {
		return nil, err
	}
	filters, err := compileConditions(selection.Filter)
	if err != nil {
		return nil, err
	}
	candidates, active, err := selectionCandidates(projection, selection, filters)
	if err != nil {
		return nil, err
	}
	if err := sortCandidates(candidates); err != nil {
		return nil, err
	}
	for _, item := range candidates {
		if selection.Group == nil || active[item.group] < max(1, selection.Group.MaxActive) {
			return &item.work, nil
		}
	}
	return nil, nil
}

func selectionCandidates(projection Projection, selection Selection, filters []compiledCondition) ([]candidate, map[string]int, error) {
	active := map[string]int{}
	var candidates []candidate
	for _, work := range projection.Works {
		if work.State != "available" && work.State != "claimed" {
			continue
		}
		var payload any
		if err := json.Unmarshal(work.Work, &payload); err != nil {
			return nil, nil, err
		}
		item := candidate{work: work}
		var err error
		item.group, err = groupKey(payload, selection.Group)
		if err != nil {
			return nil, nil, err
		}
		if work.State == "claimed" {
			active[item.group]++
			continue
		}
		if slices.ContainsFunc(filters, func(condition compiledCondition) bool { return !matches(payload, condition) }) {
			continue
		}
		item.sort, err = sortValues(payload, selection.Sort)
		if err != nil {
			return nil, nil, err
		}
		candidates = append(candidates, item)
	}
	return candidates, active, nil
}

type ClaimResult struct {
	Work  WorkState   `json:"work"`
	Claim Transaction `json:"claim"`
}

func (b Branch) ClaimNext(ctx context.Context, selection Selection, claimID, runID string) (*ClaimResult, error) {
	if claimID == "" || runID == "" {
		return nil, errors.New("claim and owning run identities are required")
	}
	if err := selection.Validate(); err != nil {
		return nil, err
	}
	var result *ClaimResult
	_, _, err := b.Update(ctx, func(current []Transaction) ([]Transaction, bool, error) {
		result = nil
		work, err := SelectNext(current, selection)
		if err != nil || work == nil {
			return current, false, err
		}
		claim := Transaction{Version: CurrentVersion, Kind: "Claim", WorkID: work.WorkID, ClaimID: claimID, RunID: runID}
		next, changed, err := Apply(current, claim)
		if err == nil {
			work.State, work.Winner = "claimed", claimID
			work.Claims = append(work.Claims, ClaimState{ClaimID: claimID, RunID: runID, State: "effective"})
			result = &ClaimResult{Work: *work, Claim: claim}
		}
		return next, changed, err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
