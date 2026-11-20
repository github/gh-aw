package prefercutprefix

import "strings"

type config struct {
	name   string
	prefix string
}

func badPrefix(value, affix string) string {
	if strings.HasPrefix(value, affix) {
		return strings.TrimPrefix(value, affix) // want `strings.TrimPrefix inside a strings.HasPrefix/HasSuffix guard can be replaced with strings.CutPrefix`
	}
	return value
}

func badSuffix(value, affix string) string {
	if strings.HasSuffix(value, affix) {
		return strings.TrimSuffix(value, affix) // want `strings.TrimSuffix inside a strings.HasPrefix/HasSuffix guard can be replaced with strings.CutSuffix`
	}
	return value
}

func badSelectors(c config) string {
	if strings.HasPrefix(c.name, c.prefix) {
		return strings.TrimPrefix(c.name, c.prefix) // want `strings.TrimPrefix inside a strings.HasPrefix/HasSuffix guard can be replaced with strings.CutPrefix`
	}
	return c.name
}

func badLiterals() string {
	if strings.HasPrefix("prefix-value", "prefix") {
		return strings.TrimPrefix("prefix-value", "prefix") // want `strings.TrimPrefix inside a strings.HasPrefix/HasSuffix guard can be replaced with strings.CutPrefix`
	}
	return ""
}

func badParenthesized(value, affix string) string {
	if strings.HasPrefix((value), (affix)) {
		return strings.TrimPrefix((value), (affix)) // want `strings.TrimPrefix inside a strings.HasPrefix/HasSuffix guard can be replaced with strings.CutPrefix`
	}
	return value
}

func goodMismatchedOperand(value, affix, other string) string {
	if strings.HasPrefix(value, affix) {
		return strings.TrimPrefix(other, affix)
	}
	return value
}

func goodComplexOperand(value, affix string) string {
	if strings.HasPrefix(strings.ToLower(value), affix) {
		return strings.TrimPrefix(strings.ToLower(value), affix)
	}
	return value
}

func goodReassignedRoot(value, affix, replacement string) string {
	if strings.HasPrefix(value, affix) {
		value = replacement
		return strings.TrimPrefix(value, affix)
	}
	return value
}

func goodNegatedGuard(value, affix string) string {
	if !strings.HasPrefix(value, affix) {
		return value
	}
	return strings.TrimPrefix(value, affix)
}

func goodBooleanGuard(value, affix string) string {
	if len(value) > 0 && strings.HasPrefix(value, affix) {
		return strings.TrimPrefix(value, affix)
	}
	return value
}

func goodClosure(value, affix string) string {
	if strings.HasPrefix(value, affix) {
		return func() string {
			return strings.TrimPrefix(value, affix)
		}()
	}
	return value
}
