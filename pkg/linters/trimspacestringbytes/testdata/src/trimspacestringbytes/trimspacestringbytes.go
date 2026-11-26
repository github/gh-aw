package trimspacestringbytes

import s "strings"

type namedBytes []byte
type byteAlias = []uint8
type stringAlias = string
type namedString string

func examples(b []byte, nb namedBytes, ab byteAlias, text string) {
	_ = s.TrimSpace(string(b))                   // want `consider bytes.TrimSpace`
	_ = (s.TrimSpace)(((string)((nb))))          // want `consider bytes.TrimSpace`
	_ = s.TrimSpace(stringAlias(ab))             // want `consider bytes.TrimSpace`
	_ = s.TrimSpace(string([]byte("  hello  "))) // want `consider bytes.TrimSpace`
	trim := s.TrimSpace
	_ = trim(string(b)) // want `consider bytes.TrimSpace`

	_ = s.TrimSpace(text)
	_ = s.TrimSpace(string(text))
	_ = s.TrimSpace(string([]rune{'x'}))
	_ = s.TrimSpace(string(65))
	_ = s.TrimSpace(toString(b))
	_ = s.TrimSpace(string(namedString(text)))
	_ = s.Trim(string(b), " ")

	_ = s.TrimSpace(string(b)) //nolint:trimspacestringbytes
	//nolint:trimspacestringbytes
	_ = s.TrimSpace(string(b))
	_ = s.TrimSpace(string(b)) //nolint:all

	_ = s.TrimSpace(string(b)) //nolint:other // want `consider bytes.TrimSpace`
}

func toString(b []byte) string { return string(b) }

func shadowed(b []byte) {
	string := toString
	_ = s.TrimSpace(string(b))
}

func shadowedPackage(b []byte) {
	s := struct{ TrimSpace func(string) string }{toStringValue}
	_ = s.TrimSpace(string(b))
}

func toStringValue(s string) string { return s }

func mutableAlias(b []byte) {
	trim := s.TrimSpace
	trim = toStringValue
	_ = trim(string(b))
}
