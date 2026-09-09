package slicecontainsloop

func contains(items []string, want string) bool {
	for _, item := range items { // want `use slices\.Contains\(items, want\) instead of a manual contains loop`
		if item == want {
			return true
		}
	}
	return false
}

func containsAliasedValue(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			_ = item
			return true
		}
	}
	return false
}

func notAContainsLoop(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return false
		}
	}
	return true
}

func nolintDirective(items []string, want string) bool {
	for _, item := range items { //nolint:slicecontainsloop
		if item == want {
			return true
		}
	}
	return false
}
