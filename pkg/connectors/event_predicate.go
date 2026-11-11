package connectors

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const EventEvaluationVersion = "gh-aw/event-predicates/v1"
const eventArrayLimit = 100
const eventEvaluationLimit = 64 * eventArrayLimit

var eventTimestampPattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,9})?(?:Z|[+-](?:0[0-9]|1[0-9]|2[0-3]):[0-5][0-9])$`)
var eventURLIdentity = regexp.MustCompile(`^[A-Za-z0-9._-]{1,256}$`)

func ValidateEventPredicate(rule EventPredicate) error {
	count := 0
	return validateEventPredicate(rule, 0, &count)
}

func validateEventPredicate(rule EventPredicate, depth int, count *int) error {
	*count++
	if depth > 6 || *count > 64 {
		return errors.New("event predicates require at most 64 nodes and depth 6")
	}
	groups := len(rule.All) + len(rule.Any)
	if groups != 0 {
		if len(rule.All) != 0 && len(rule.Any) != 0 || rule.Path != "" || rule.Op != "" || rule.Value != nil || rule.Other != "" || rule.Fallback != "" || len(rule.Values) != 0 || rule.Separator != "" || rule.Root || rule.Items != nil {
			return errors.New("event predicate groups require exactly one nonempty all or any array")
		}
		for _, child := range append(slices.Clone(rule.All), rule.Any...) {
			if err := validateEventPredicate(child, depth+1, count); err != nil {
				return err
			}
		}
		return nil
	}
	if !validPointer(rule.Path) || len(rule.Path) > 1024 || (rule.Other != "" && (!validPointer(rule.Other) || len(rule.Other) > 1024)) || (rule.Fallback != "" && (!validPointer(rule.Fallback) || len(rule.Fallback) > 1024)) {
		return errors.New("event predicates require bounded JSON Pointers")
	}
	if len(rule.Values) > 100 || slices.Contains(rule.Values, "") {
		return errors.New("event membership requires at most 100 nonempty literal values")
	}
	for _, value := range rule.Values {
		if len(value) > 1024 || strings.Contains(value, "${{") {
			return errors.New("event predicate values must be bounded literals")
		}
	}
	return validateEventPredicateOperator(rule, depth, count)
}

func validateEventPredicateOperator(rule EventPredicate, depth int, count *int) error {
	switch rule.Op {
	case "equals":
		if rule.Value == nil && rule.Other == "" || rule.Value != nil && rule.Other != "" {
			return errors.New("event equality requires exactly one literal value or root other pointer")
		}
		if rule.Value != nil && (!eventComparable(rule.Value) || strings.Contains(fmt.Sprint(rule.Value), "${{")) {
			return errors.New("event equality requires a bounded primitive literal")
		}
	case "present", "absent", "null", "timestamp", "text":
		if rule.Value != nil || rule.Other != "" {
			return errors.New("event presence/null/timestamp predicates do not accept comparison values")
		}
	case "changed", "set-added", "set-removed":
		if rule.Other == "" || rule.Value != nil || len(rule.Values) != 0 {
			return errors.New("event changes require an explicit root other pointer")
		}
	case "enters", "leaves":
		if rule.Other == "" || rule.Value != nil {
			return errors.New("event transitions require old/new pointers and explicit terminal values")
		}
	case "prefix", "path-prefix", "member-prefix":
		if rule.Other != "" || rule.Value != nil {
			return errors.New("prefix policies require explicit literal values rather than another pointer")
		}
	case "url-id":
		prefix, ok := rule.Value.(string)
		parsed, err := url.Parse(prefix)
		if !ok || len(prefix) > 1024 || rule.Other == "" || err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || !strings.HasSuffix(prefix, "/") || strings.Contains(prefix, "${") {
			return errors.New("URL identity policies require a fixed HTTPS prefix and explicit identity pointer")
		}
	case "length":
		n, ok := rule.Value.(json.Number)
		if !ok {
			if integer, valid := rule.Value.(int); valid {
				n, ok = json.Number(strconv.Itoa(integer)), true
			}
		}
		value, err := n.Int64()
		if !ok || err != nil || value < 0 || value > eventArrayLimit || rule.Other != "" {
			return errors.New("event length requires an integer from 0 to 100")
		}
	case "any-item":
		if rule.Items == nil || rule.Value != nil || rule.Other != "" {
			return errors.New("event any-item requires one bounded items predicate")
		}
		return validateEventPredicate(*rule.Items, depth+1, count)
	default:
		return fmt.Errorf("unsupported event predicate operator %q", rule.Op)
	}
	if rule.Items != nil || (len(rule.Values) != 0 && !slices.Contains([]string{"enters", "leaves", "prefix", "path-prefix", "member-prefix", "url-id"}, rule.Op)) || (rule.Separator != "" && !slices.Contains([]string{"set-added", "set-removed", "member-prefix"}, rule.Op)) {
		return errors.New("event predicate has options not supported by its operator")
	}
	if rule.Separator != "" && (utf8.RuneCountInString(rule.Separator) != 1 || strings.ContainsAny(rule.Separator, "\r\n")) {
		return errors.New("event set separator must be one literal non-newline character")
	}
	return nil
}

func eventPrimitiveType(value any) string {
	switch value.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case json.Number, int, float64:
		return "integer"
	default:
		return ""
	}
}

func eventComparable(value any) bool {
	if validateArgument(Input{Type: eventPrimitiveType(value), MaxLength: 1024}, value) != nil {
		return false
	}
	if eventPrimitiveType(value) == "integer" {
		n, ok := numericValue(value)
		return ok && math.Abs(n) <= 9007199254740991
	}
	return true
}

// EvaluateEventPredicate returns known=false when required change evidence is
// absent or malformed. Unknown never becomes a matching derived activity.
func EvaluateEventPredicate(rule EventPredicate, document any) (matched, known bool) {
	if ValidateEventPredicate(rule) != nil {
		return false, false
	}
	remaining := eventEvaluationLimit
	matched, known = evaluateEventPredicate(rule, document, document, &remaining)
	if remaining < 0 {
		return false, false
	}
	return matched && known, known
}

func evaluateEventPredicate(rule EventPredicate, document, root any, remaining *int) (bool, bool) {
	*remaining--
	if *remaining < 0 {
		return false, false
	}
	if len(rule.All)+len(rule.Any) != 0 {
		unknown := false
		children := rule.All
		all := len(children) != 0
		if !all {
			children = rule.Any
		}
		for _, child := range children {
			match, known := evaluateEventPredicate(child, document, root, remaining)
			if *remaining < 0 {
				return false, false
			}
			if known && match != all {
				return !all, true
			}
			unknown = unknown || !known
		}
		return all && !unknown, !unknown
	}
	if rule.Root {
		document = root
	}
	value, present := lookupPointer(document, rule.Path)
	if !present && rule.Fallback != "" {
		value, present = lookupPointer(root, rule.Fallback)
	}
	if rule.Op == "present" || rule.Op == "absent" {
		return present == (rule.Op == "present"), true
	}
	if !present {
		return false, false
	}
	other, found := lookupPointer(root, rule.Other)
	if rule.Op == "any-item" {
		return evaluateEventItems(rule, value, root, remaining)
	}
	return evaluateEventValue(rule, value, other, found, remaining)
}

func evaluateEventValue(rule EventPredicate, value, other any, found bool, remaining *int) (bool, bool) {
	switch rule.Op {
	case "null":
		return value == nil, true
	case "timestamp":
		text, ok := value.(string)
		if !ok {
			return false, value == nil
		}
		if !eventTimestampPattern.MatchString(text) {
			return false, false
		}
		_, err := time.Parse(time.RFC3339Nano, text)
		return err == nil, err == nil
	case "text":
		text, ok := value.(string)
		return ok && strings.TrimSpace(text) != "" && len(text) <= 1024, ok
	case "equals":
		expected := rule.Value
		if rule.Other != "" {
			if !found {
				return false, false
			}
			expected = other
		}
		return equalEventValue(value, expected), eventComparable(value) && eventComparable(expected)
	case "length":
		array, ok := value.([]any)
		expected := fmt.Sprint(rule.Value)
		return ok && strconv.Itoa(len(array)) == expected, ok && len(array) <= eventArrayLimit
	case "prefix", "path-prefix", "member-prefix", "url-id":
		return evaluateEventPrefix(rule, value, other, found, remaining)
	}
	return evaluateEventChange(rule, value, other, found)
}

func evaluateEventPrefix(rule EventPredicate, value, other any, found bool, remaining *int) (bool, bool) {
	if rule.Op == "url-id" {
		return evaluateEventURL(rule, value, other, found)
	}
	if rule.Op == "member-prefix" {
		members, valid := eventStringSet(value, rule.Separator)
		if !valid {
			return false, false
		}
		for _, member := range slices.Sorted(maps.Keys(members)) {
			for _, prefix := range rule.Values {
				*remaining--
				if *remaining < 0 {
					return false, false
				}
				if strings.HasPrefix(member, prefix) {
					return true, true
				}
			}
		}
		return false, true
	}

	text, valid := value.(string)
	if !valid || len(text) > 65536 {
		return false, false
	}
	for _, prefix := range rule.Values {
		*remaining--
		if *remaining < 0 {
			return false, false
		}
		if rule.Op == "path-prefix" {
			text, prefix = strings.ToLower(strings.TrimSpace(text)), strings.ToLower(strings.TrimRight(strings.TrimSpace(prefix), "\\"))
			if text == prefix || strings.HasPrefix(text, prefix+"\\") {
				return true, true
			}
		} else if strings.HasPrefix(text, prefix) {
			return true, true
		}
	}
	return false, true
}
func evaluateEventURL(rule EventPredicate, value, other any, found bool) (bool, bool) {
	text, ok := value.(string)
	identity, valid := responseResourceIdentity(other)
	prefix, literal := rule.Value.(string)
	if !ok || !found || !valid || !literal || !eventURLIdentity.MatchString(identity) {
		return false, false
	}
	actual, err := url.Parse(text)
	if err != nil || actual.User != nil || actual.Fragment != "" {
		return false, false
	}
	query, err := url.ParseQuery(actual.RawQuery)
	if err != nil {
		return false, false
	}
	for key, values := range query {
		if !slices.Contains(rule.Values, key) || len(values) != 1 {
			return false, true
		}
		for _, value := range values {
			if len(value) > 1024 {
				return false, true
			}
		}
	}
	base, _, _ := strings.Cut(text, "?")
	return base == prefix+url.PathEscape(identity), true
}

func evaluateEventItems(rule EventPredicate, value, root any, remaining *int) (bool, bool) {
	array, ok := value.([]any)
	if !ok || len(array) > eventArrayLimit {
		return false, false
	}
	unknown := false
	for _, item := range array {
		match, known := evaluateEventPredicate(*rule.Items, item, root, remaining)
		if *remaining < 0 {
			return false, false
		}
		if known && match {
			return true, true
		}
		unknown = unknown || !known
	}
	return false, !unknown
}

func evaluateEventChange(rule EventPredicate, value, other any, found bool) (bool, bool) {
	switch rule.Op {
	case "changed":
		if !found || !eventComparable(value) || !eventComparable(other) || eventPrimitiveType(value) != eventPrimitiveType(other) {
			return false, false
		}
		return !equalEventValue(value, other), true
	case "enters", "leaves":
		before, bok := other.(string)
		after, aok := value.(string)
		if !found || !bok || !aok {
			return false, false
		}
		was, now := slices.Contains(rule.Values, before), slices.Contains(rule.Values, after)
		if rule.Op == "leaves" {
			return was && !now, true
		}
		return !was && now, true
	case "set-added", "set-removed":
		before, bok := eventStringSet(other, rule.Separator)
		after, aok := eventStringSet(value, rule.Separator)
		if !found || !bok || !aok {
			return false, false
		}
		if rule.Op == "set-removed" {
			before, after = after, before
		}
		for entry := range after {
			if _, exists := before[entry]; !exists {
				return true, true
			}
		}
		return false, true
	}
	return false, false
}

func eventStringSet(value any, separator string) (map[string]struct{}, bool) {
	set := map[string]struct{}{}
	var array []any
	if separator != "" {
		text, ok := value.(string)
		if !ok || len(text) > 65536 {
			return nil, false
		}
		for entry := range strings.SplitSeq(text, separator) {
			entry = strings.TrimSpace(entry)
			if entry != "" {
				array = append(array, entry)
			}
		}
	} else {
		var ok bool
		array, ok = value.([]any)
		if !ok {
			return nil, false
		}
	}
	if len(array) > eventArrayLimit {
		return nil, false
	}
	for _, item := range array {
		text, ok := item.(string)
		if !ok || text == "" || len(text) > 1024 {
			return nil, false
		}
		set[text] = struct{}{}
	}
	return set, true
}
