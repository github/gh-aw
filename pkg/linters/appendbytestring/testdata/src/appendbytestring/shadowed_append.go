package appendbytestring

// A locally shadowed "append" is not the builtin, so it must not be flagged:
// rewriting the call to append(b, s...) would not compile for this signature.
func goodShadowedAppend(b []byte, s string) []byte {
	append := func(dst []byte, src ...byte) []byte { return dst }
	return append(b, []byte(s)...)
}
