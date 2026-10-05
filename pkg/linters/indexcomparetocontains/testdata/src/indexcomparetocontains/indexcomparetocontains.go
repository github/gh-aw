package indexcomparetocontains

import "strings"
import "bytes"

func badStringsIndex(s, sub string) bool {
	return strings.Index(s, sub) != -1 // want `use strings\.Contains\(s, sub\) instead of strings\.Index comparison`
}

func badStringsIndexGEQ(s, sub string) bool {
	return strings.Index(s, sub) >= 0 // want `use strings\.Contains\(s, sub\) instead of strings\.Index comparison`
}

func badStringsIndexGTR(s, sub string) bool {
	return strings.Index(s, sub) > -1 // want `use strings\.Contains\(s, sub\) instead of strings\.Index comparison`
}

func badStringsIndexNotContains(s, sub string) bool {
	return strings.Index(s, sub) == -1 // want `use !strings\.Contains\(s, sub\) instead of strings\.Index comparison`
}

func badStringsIndexNotContainsLT(s, sub string) bool {
	return strings.Index(s, sub) < 0 // want `use !strings\.Contains\(s, sub\) instead of strings\.Index comparison`
}

func badStringsIndexNotContainsLEQ(s, sub string) bool {
	return strings.Index(s, sub) <= -1 // want `use !strings\.Contains\(s, sub\) instead of strings\.Index comparison`
}

func badStringsLastIndex(s, sub string) bool {
	return strings.LastIndex(s, sub) != -1 // want `use strings\.Contains\(s, sub\) instead of strings\.LastIndex comparison`
}

func badStringsLastIndexNotContains(s, sub string) bool {
	return strings.LastIndex(s, sub) == -1 // want `use !strings\.Contains\(s, sub\) instead of strings\.LastIndex comparison`
}

func badStringsIndexByte(s string) bool {
	return strings.IndexByte(s, 'a') != -1 // want `use strings\.ContainsRune\(s, 'a'\) instead of strings\.IndexByte comparison`
}

func badBytesIndex(b, sub []byte) bool {
	return bytes.Index(b, sub) != -1 // want `use bytes\.Contains\(b, sub\) instead of bytes\.Index comparison`
}

func badBytesIndexGEQ(b, sub []byte) bool {
	return bytes.Index(b, sub) >= 0 // want `use bytes\.Contains\(b, sub\) instead of bytes\.Index comparison`
}

func badBytesLastIndex(b, sub []byte) bool {
	return bytes.LastIndex(b, sub) != -1 // want `use bytes\.Contains\(b, sub\) instead of bytes\.LastIndex comparison`
}

func badBytesLastIndexNotContains(b, sub []byte) bool {
	return bytes.LastIndex(b, sub) == -1 // want `use !bytes\.Contains\(b, sub\) instead of bytes\.LastIndex comparison`
}

func badBytesIndexByte(b []byte) bool {
	return bytes.IndexByte(b, 'a') != -1 // want `use bytes\.Contains\(b, 'a'\) instead of bytes\.IndexByte comparison`
}

func badBytesIndexByteNotContains(b []byte) bool {
	return bytes.IndexByte(b, 'a') == -1 // want `use !bytes\.Contains\(b, 'a'\) instead of bytes\.IndexByte comparison`
}

func badYodaStringsIndex(s, sub string) bool {
	return -1 != strings.Index(s, sub) // want `use strings\.Contains\(s, sub\) instead of strings\.Index comparison`
}

func badYodaStringsIndexNotContains(s, sub string) bool {
	return -1 == strings.Index(s, sub) // want `use !strings\.Contains\(s, sub\) instead of strings\.Index comparison`
}

func badYodaBytesIndex(b, sub []byte) bool {
	return -1 != bytes.Index(b, sub) // want `use bytes\.Contains\(b, sub\) instead of bytes\.Index comparison`
}

func goodStringsContains(s, sub string) bool {
	return strings.Contains(s, sub)
}

func goodNotStringsContains(s, sub string) bool {
	return !strings.Contains(s, sub)
}

func goodBytesContains(b, sub []byte) bool {
	return bytes.Contains(b, sub)
}

func goodNotBytesContains(b, sub []byte) bool {
	return !bytes.Contains(b, sub)
}

func goodStringsIndexReturnValue(s, sub string) int {
	// Using the index value itself (not just for containment check) is fine.
	return strings.Index(s, sub)
}

func goodBytesIndexReturnValue(b, sub []byte) int {
	// Using the index value itself (not just for containment check) is fine.
	return bytes.Index(b, sub)
}

func goodStringsIndexComparesOtherValue(s, sub string) bool {
	// Comparing against a value other than -1/0 (as a containment sentinel) is fine.
	return strings.Index(s, sub) > 3
}

func goodBytesIndexComparesOtherValue(b, sub []byte) bool {
	// Comparing against a value other than -1/0 (as a containment sentinel) is fine.
	return bytes.Index(b, sub) > 3
}

func goodStringsIndexEqualZero(s, sub string) bool {
	// == 0 is a prefix check (not a containment check) and is intentionally not flagged.
	return strings.Index(s, sub) == 0
}

func goodBytesIndexEqualZero(b, sub []byte) bool {
	// == 0 is a prefix check (not a containment check) and is intentionally not flagged.
	return bytes.Index(b, sub) == 0
}

func badStringsParenIndex(s, sub string) bool {
	return (strings.Index(s, sub)) != -1 // want `use strings\.Contains\(s, sub\) instead of strings\.Index comparison`
}

func badBytesParenIndex(b, sub []byte) bool {
	return (bytes.Index(b, sub)) != -1 // want `use bytes\.Contains\(b, sub\) instead of bytes\.Index comparison`
}

func badStringsIndexWithComments(s, sub string) bool {
	return strings.Index(s, sub /* substr */) != -1 // want `use strings\.Contains\(s, sub\) instead of strings\.Index comparison`
}

func badStringsYodaIndexWithLEQ(s, sub string) bool {
	return 0 <= strings.Index(s, sub) // want `use strings\.Contains\(s, sub\) instead of strings\.Index comparison`
}

func badStringsYodaIndexNotContainsWithGTR(s, sub string) bool {
	return 0 > strings.Index(s, sub) // want `use !strings\.Contains\(s, sub\) instead of strings\.Index comparison`
}

func badBytesYodaIndexWithLEQ(b, sub []byte) bool {
	return 0 <= bytes.Index(b, sub) // want `use bytes\.Contains\(b, sub\) instead of bytes\.Index comparison`
}
