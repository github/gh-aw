package readerstringconv

import (
	"bytes"
	"strings"
)

type namedBytes []byte
type namedString string
type aliasBytes = []byte
type aliasString = string

func conversions(b []byte, s string, nb namedBytes, ns namedString) {
	_ = strings.NewReader(string(b)) // want `use bytes.NewReader instead of converting \[\]byte to string`
	_ = bytes.NewReader([]byte(s))   // want `use strings.NewReader instead of converting string to \[\]byte`
	strings.NewReader(string(b))     // want `use bytes.NewReader`
	bytes.NewReader([]byte(s))       // want `use strings.NewReader`

	_ = (strings.NewReader)(((string)((b))))     // want `use bytes.NewReader`
	_ = (bytes.NewReader)(((aliasBytes)((s))))   // want `use strings.NewReader`
	_ = strings.NewReader(aliasString(nb))       // want `use bytes.NewReader`
	_ = bytes.NewReader(aliasBytes(ns))          // want `use strings.NewReader`
	_ = bytes.NewReader(namedBytes(s))           // want `use strings.NewReader`
	_ = strings.NewReader(string([]uint8{1, 2})) // want `use bytes.NewReader`

	newReader := strings.NewReader
	_ = newReader(string(b)) // want `use bytes.NewReader`

	_ = strings.NewReader(s)
	_ = bytes.NewReader(b)
	_ = strings.NewReader(string(s))
	_ = bytes.NewReader([]byte(b))
	_ = strings.NewReader(string([]rune{'x'}))
	_ = strings.NewReader(string(65))
	_ = strings.NewReader(toString(b))
	_ = bytes.NewReader(toBytes(s))
}

func toString(b []byte) string { return string(b) }
func toBytes(s string) []byte  { return []byte(s) }

func shadowedConversion(b []byte) {
	string := toString
	_ = strings.NewReader(string(b))
}

func shadowedPackages(b []byte, s string) {
	strings := struct{ NewReader func(string) *strings.Reader }{strings.NewReader}
	bytes := struct{ NewReader func([]byte) *bytes.Reader }{bytes.NewReader}
	_ = strings.NewReader(string(b))
	_ = bytes.NewReader([]byte(s))
}

func mutableAlias(b []byte) {
	newReader := strings.NewReader
	newReader = func(s string) *strings.Reader { return strings.NewReader(s) }
	_ = newReader(string(b))
}

func suppressed(b []byte, s string) {
	_ = strings.NewReader(string(b)) //nolint:readerstringconv

	//nolint:readerstringconv
	_ = bytes.NewReader([]byte(s))

	_ = bytes.NewReader([]byte(s)) //nolint:all

	//nolint:anotherlinter
	_ = strings.NewReader(string(b)) // want `use bytes.NewReader`
}
