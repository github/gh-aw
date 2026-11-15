package json_decoder_trailing_data_unchecked

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

func badString(s string) error {
	var v map[string]any
	dec := json.NewDecoder(strings.NewReader(s))
	return dec.Decode(&v) // want "accepts trailing data"
}

func badBytesParen(b []byte) error {
	var v map[string]any
	dec := (json.NewDecoder((bytes.NewReader(b))))
	if err := dec.Decode(&v); err != nil { // want "accepts trailing data"
		return err
	}
	return nil
}

func badVarDecl(s string) error {
	var v any
	var dec = json.NewDecoder(strings.NewReader(s))
	_ = dec.Decode(&v) // want "accepts trailing data"
	_ = dec.Decode(&v)
	return nil
}

func badChained(s string) error {
	var v any
	return json.NewDecoder(strings.NewReader(s)).Decode(&v) // want "accepts trailing data"
}

func goodEOF(s string) error {
	var v any
	dec := json.NewDecoder(strings.NewReader(s))
	if err := dec.Decode(&v); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("trailing data")
	}
	return nil
}

func goodSuppressed(s string) error {
	var v any
	dec := json.NewDecoder(strings.NewReader(s))
	return dec.Decode(&v) //nolint:jsondecodertrailingdataunchecked // streaming prefix is intended.
}

func goodHTTP(resp *http.Response, r io.Reader) error {
	var v any
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return err
	}
	return json.NewDecoder(r).Decode(&v)
}
