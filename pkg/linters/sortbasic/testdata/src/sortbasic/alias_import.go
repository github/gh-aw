package sortbasic

import s "sort"

func BadAliasedImport(items []string) {
	s.Strings(items) // want `sort\.Strings is a legacy helper`
}
