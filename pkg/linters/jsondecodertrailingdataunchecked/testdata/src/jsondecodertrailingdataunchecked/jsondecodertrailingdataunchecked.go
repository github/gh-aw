package jsondecodertrailingdataunchecked

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

func badStringReader(input string) error {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input))
	if err := decoder.Decode(&value); err != nil { // want `first Decode on an in-memory JSON reader`
		return err
	}
	return nil
}

func badBytesReader(input []byte) error {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(input))
	err := decoder.Decode(&value) // want `first Decode on an in-memory JSON reader`
	if err != nil {
		return err
	}
	return nil
}

func badUncheckedFollowUp(input string) error {
	var value, trailing any
	decoder := json.NewDecoder(strings.NewReader(input))
	if err := decoder.Decode(&value); err != nil { // want `first Decode on an in-memory JSON reader`
		return err
	}
	_ = decoder.Decode(&trailing)
	return nil
}

func goodEOFComparison(input string) error {
	var value, trailing any
	decoder := json.NewDecoder(strings.NewReader(input))
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return err
	}
	return nil
}

func goodEOFCheckAfterAssignment(input string) error {
	var value, trailing any
	decoder := json.NewDecoder(bytes.NewReader([]byte(input)))
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	err := decoder.Decode(&trailing)
	if err != io.EOF {
		return err
	}
	return nil
}

func goodErrorsIs(input string) error {
	var value, trailing any
	decoder := json.NewDecoder(strings.NewReader(input))
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func excludedGenericReader(reader io.Reader) error {
	var value any
	decoder := json.NewDecoder(reader)
	return decoder.Decode(&value)
}

func excludedHTTPReader(response *http.Response) error {
	var value any
	decoder := json.NewDecoder(response.Body)
	return decoder.Decode(&value)
}

func suppressedByNolint(input string) error {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input))
	if err := decoder.Decode(&value); err != nil { //nolint:jsondecodertrailingdataunchecked
		return err
	}
	return nil
}
