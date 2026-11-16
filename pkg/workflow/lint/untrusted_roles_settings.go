package lint

import "slices"

// UntrustedRolesOnSettingKeys returns a sorted copy of the on: keys that the
// untrusted-roles-detection rule treats as gh-aw settings rather than events.
// pkg/workflow tests use it to detect drift from the compiler's settings list.
func UntrustedRolesOnSettingKeys() []string {
	keys := slices.Clone(untrustedRolesOnSettings)
	slices.Sort(keys)
	return keys
}
