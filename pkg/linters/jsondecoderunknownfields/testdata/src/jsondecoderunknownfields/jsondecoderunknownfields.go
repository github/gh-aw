package jsondecoderunknownfields

import (
	stdjson "encoding/json"
	"io"
	"strings"
)

type cfg struct{ A int }

func direct(r io.Reader) error {
	var c cfg
	return stdjson.NewDecoder(r).Decode(&c) // want "json.Decoder decodes without DisallowUnknownFields"
}

func variable(r io.Reader) error {
	var c cfg
	dec := stdjson.NewDecoder(strings.NewReader("{}"))
	_ = r
	return (dec).Decode(&c) // want "json.Decoder decodes without DisallowUnknownFields"
}

func funcAlias(r io.Reader) error {
	var c cfg
	newDec := stdjson.NewDecoder
	dec := newDec(r)
	return dec.Decode(&c) // want "json.Decoder decodes without DisallowUnknownFields"
}

func strict(r io.Reader) error {
	var c cfg
	dec := stdjson.NewDecoder(r)
	dec.DisallowUnknownFields()
	return dec.Decode(&c)
}

func strictParen(r io.Reader) error {
	var c cfg
	dec := stdjson.NewDecoder(r)
	(dec).DisallowUnknownFields()
	return (dec).Decode(&c)
}

func strictAlias(r io.Reader) error {
	var c cfg
	dec := stdjson.NewDecoder(r)
	other := dec
	other.DisallowUnknownFields()
	return dec.Decode(&c)
}

func param(dec *stdjson.Decoder) error {
	var c cfg
	return dec.Decode(&c)
}

func suppressed(r io.Reader) error {
	var c cfg
	return stdjson.NewDecoder(r).Decode(&c) //nolint:jsondecoderunknownfields
}
