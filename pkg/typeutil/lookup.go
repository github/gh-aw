package typeutil

import "github.com/github/gh-aw/pkg/logger"

var lookupLog = logger.New("typeutil:lookup")

// ParseBool extracts a boolean value from a map[string]any by key.
// Returns false if the map is nil, the key is absent, or the value is not a bool.
func ParseBool(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	if v, ok := m[key]; ok {
		b, isBool := v.(bool)
		return isBool && b
	}
	return false
}

// LookupMap extracts a map[string]any value from m by key.
func LookupMap(m map[string]any, key string) (map[string]any, bool) {
	if m == nil {
		return nil, false
	}

	value, ok := m[key]
	if !ok {
		return nil, false
	}

	result, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}

	return result, true
}

// LookupString extracts a string value from m by key.
func LookupString(m map[string]any, key string) (string, bool) {
	if m == nil {
		return "", false
	}

	value, ok := m[key]
	if !ok {
		return "", false
	}

	result, ok := value.(string)
	if !ok {
		return "", false
	}

	return result, true
}

// LookupStringPath extracts a nested string value from m by path.
// It returns ("", false) if any step in the path is missing or has an invalid type.
func LookupStringPath(m map[string]any, path ...string) (string, bool) {
	if len(path) == 0 {
		return "", false
	}

	current := m
	for i, key := range path {
		value, ok := current[key]
		if !ok {
			lookupLog.Printf("Path lookup stopped: key %q missing at step %d/%d", key, i, len(path))
			return "", false
		}

		if i == len(path)-1 {
			result, ok := value.(string)
			if !ok {
				lookupLog.Printf("Path lookup failed: final value at key %q is not a string", key)
				return "", false
			}
			return result, true
		}

		next, ok := value.(map[string]any)
		if !ok {
			lookupLog.Printf("Path lookup failed: value at key %q is not a nested map (step %d/%d)", key, i, len(path))
			return "", false
		}
		current = next
	}

	return "", false
}
