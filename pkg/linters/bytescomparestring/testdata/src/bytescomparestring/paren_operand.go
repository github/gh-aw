package bytescomparestring

func badParenOperand(a, b []byte) bool {
	return string(a) == (string(b)) // want `string\(a\) == string\(b\) is a \[\]byte comparison written the long way; use bytes\.Equal\(a, b\) for clearer intent`
}
