package prefercutprefix

import "strings"

type cfg struct{ name string }

func flagged(s string, c cfg) string {
	if strings.HasPrefix(s, "x-") { // want `strings.HasPrefix guard followed by strings.TrimPrefix`
		return strings.TrimPrefix(s, "x-")
	}
	if strings.HasSuffix(c.name, ".go") { // want `strings.HasSuffix guard followed by strings.TrimSuffix`
		return strings.TrimSuffix(c.name, ".go")
	}
	switch {
	case strings.HasPrefix(s, "a:"): // want `strings.HasPrefix guard followed by strings.TrimPrefix`
		return strings.TrimPrefix(s, "a:")
	}
	return ""
}

func notFlagged(s, t string) string {
	if strings.HasPrefix(s, "x-") {
		return strings.TrimPrefix(t, "x-") // different operand
	}
	if strings.HasPrefix(s, "y-") {
		s = "z"
		return strings.TrimPrefix(s, "y-") // s reassigned
	}
	if strings.HasPrefix(s, "q") {
		return s
	}
	if strings.HasPrefix(get(), "q") {
		return strings.TrimPrefix(get(), "q") // not side-effect free
	}
	if strings.HasPrefix(s, "k") { //nolint:prefercutprefix
		return strings.TrimPrefix(s, "k")
	}
	return ""
}

func get() string { return "" }
