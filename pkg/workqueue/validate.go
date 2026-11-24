package workqueue

import (
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Generic validators over strictly decoded JSON (map[string]any, []any,
// string, json.Number, bool, nil). They mirror work_queue_codec.cjs exactly so
// Go and JS reject the same records with the same codes.

var decimalPattern = regexp.MustCompile(`^[1-9][0-9]*$`)

type object = map[string]any

func closed(value any, required, optional []string, name, code string) (object, error) {
	record, ok := value.(map[string]any)
	if !ok {
		return nil, queueError(code, "%s must be an object", name)
	}
	for _, key := range required {
		if _, ok := record[key]; !ok {
			return nil, queueError(code, "%s has missing or unknown fields", name)
		}
	}
	for key := range record {
		if !slices.Contains(required, key) && !slices.Contains(optional, key) {
			return nil, queueError(code, "%s has missing or unknown fields", name)
		}
	}
	return record, nil
}

func has(record object, key string) bool {
	_, ok := record[key]
	return ok
}

func identity(value any, name string) (string, error) {
	return identityBounded(value, name, false, 256)
}

func identityBounded(value any, name string, allowEmpty bool, maxBytes int) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", queueError("invalid_unicode", "strings must contain valid Unicode scalar values")
	}
	if (!allowEmpty && text == "") || len(text) > maxBytes || strings.ContainsFunc(text, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return "", queueError("ledger_invalid", "invalid %s", name)
	}
	return text, nil
}

func integer(value any, minimum, maximum int64, name string) (int64, error) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, queueError("ledger_invalid", "invalid %s", name)
	}
	parsed, err := strconv.ParseInt(string(number), 10, 64)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, queueError("ledger_invalid", "invalid %s", name)
	}
	return parsed, nil
}

func decimal(value any, name, code string) (string, error) {
	text, ok := value.(string)
	if !ok || !decimalPattern.MatchString(text) || len(text) > 256 {
		return "", queueError(code, "%s must be a positive decimal identity string", name)
	}
	return text, nil
}

func plainObject(value any) bool {
	_, ok := value.(map[string]any)
	return ok
}

func boundedBytes(value any, limit int, name string) error {
	data, err := canonicalGeneric(value)
	if err != nil {
		return err
	}
	if len(data) > limit {
		return queueError("resource_limit", "%s exceeds %d canonical bytes", name, limit)
	}
	return nil
}

func oneOf(value any, allowed []string, name string) (string, error) {
	text, ok := value.(string)
	if !ok || !slices.Contains(allowed, text) {
		return "", queueError("reason_invalid", "%s reason must be one of %s", name, strings.Join(allowed, ", "))
	}
	return text, nil
}

// decodeInto converts a validated generic record into its typed form.
func decodeInto(value any, target any) error {
	data, err := canonicalGeneric(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}
