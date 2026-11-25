package trimspacestringbytes

import "strings"

type myBytes []byte

type myString string

func direct(b []byte) string {
	return strings.TrimSpace(string(b)) // want `strings\.TrimSpace\(string\(b\)\) converts a \[\]byte`
}

func parenthesized(b []byte) string {
	return strings.TrimSpace((string(b))) // want `strings\.TrimSpace\(string\(b\)\) converts a \[\]byte`
}

func parenFun(b []byte) string {
	return strings.TrimSpace((string)(b)) // want `strings\.TrimSpace\(string\(b\)\) converts a \[\]byte`
}

func namedBytes(b myBytes) string {
	return strings.TrimSpace(string(b)) // want `strings\.TrimSpace\(string\(b\)\) converts a \[\]byte`
}

func suppressed(b []byte) string {
	return strings.TrimSpace(string(b)) //nolint:trimspacestringbytes // intentional
}

func fromString(s string) string {
	return strings.TrimSpace(string(s))
}

func fromNamedString(s myString) string {
	return strings.TrimSpace(string(s))
}

func fromRunes(r []rune) string {
	return strings.TrimSpace(string(r))
}

func fromInt(i int) string {
	return strings.TrimSpace(string(rune(i)))
}

func plain(s string) string {
	return strings.TrimSpace(s)
}

func trimOther(b []byte) string {
	return strings.TrimLeft(string(b), " ")
}
