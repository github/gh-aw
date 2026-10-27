package actionslanguageservice

// ActionsIdentifier applies the shared ASCII identifier policy, not the full
// Actions grammar. Hyphens, when allowed, may only appear after the first character.
func ActionsIdentifier(value string, allowHyphen bool) bool {
	if value == "" {
		return false
	}
	for i, ch := range value {
		if ch == '_' || ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' ||
			i > 0 && (ch >= '0' && ch <= '9' || allowHyphen && ch == '-') {
			continue
		}
		return false
	}
	return true
}
