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
		return errors.New("trailing or invalid JSON data")
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
		return errors.New("trailing or invalid JSON data")
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
		return errors.New("trailing or invalid JSON data")
	}
	return nil
}

func badEOFComparisonReturnsNilError(input string) error {
	var value, trailing any
	decoder := json.NewDecoder(strings.NewReader(input))
	if err := decoder.Decode(&value); err != nil { // want `first Decode on an in-memory JSON reader`
		return err
	}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return err
	}
	return nil
}

func badErrorsIsReturnsNilError(input string) error {
	var value, trailing any
	decoder := json.NewDecoder(strings.NewReader(input))
	if err := decoder.Decode(&value); err != nil { // want `first Decode on an in-memory JSON reader`
		return err
	}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func badConditionalFollowUp(input string, strict bool) error {
	var value, trailing any
	decoder := json.NewDecoder(strings.NewReader(input))
	if err := decoder.Decode(&value); err != nil { // want `first Decode on an in-memory JSON reader`
		return err
	}
	if strict {
		if err := decoder.Decode(&trailing); err != io.EOF {
			return errors.New("trailing or invalid JSON data")
		}
	}
	return nil
}

func badMutuallyExclusiveFollowUp(input string, strict bool) error {
	var value, trailing any
	decoder := json.NewDecoder(strings.NewReader(input))
	if strict {
		if err := decoder.Decode(&value); err != nil { // want `first Decode on an in-memory JSON reader`
			return err
		}
	} else if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("trailing or invalid JSON data")
	}
	return nil
}

func goodDecodeUntilEOF(input string) error {
	decoder := json.NewDecoder(strings.NewReader(input))
	for {
		var value any
		if err := decoder.Decode(&value); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func goodTokenDrivenDecode(input string) error {
	decoder := json.NewDecoder(strings.NewReader(input))
	if _, err := decoder.Token(); err != nil {
		return err
	}
	var value any
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("trailing JSON data")
	}
	return nil
}

func badTokenWithUnrelatedEOFCheck(input string) error {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input))
	if err := decoder.Decode(&value); err != nil { // want `first Decode on an in-memory JSON reader`
		return err
	}
	otherErr := errors.New("unrelated error")
	if _, tokenErr := decoder.Token(); otherErr != io.EOF {
		return tokenErr
	}
	return nil
}

func badTokenEOFCheckWithoutIfInit(input string) error {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input))
	if err := decoder.Decode(&value); err != nil { // want `first Decode on an in-memory JSON reader`
		return err
	}
	_, err := decoder.Token()
	if err == io.EOF {
		return nil
	}
	return nil
}

func badReassignedInMemoryDecoder(input, replacement string) error {
	var value, trailing any
	decoder := json.NewDecoder(strings.NewReader(input))
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("trailing or invalid JSON data")
	}
	decoder = json.NewDecoder(strings.NewReader(replacement))
	return decoder.Decode(&value) // want `first Decode on an in-memory JSON reader`
}

func excludedReassignedGenericDecoder(input string, reader io.Reader) error {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input))
	decoder = json.NewDecoder(reader)
	return decoder.Decode(&value)
}

func badInlineStringReader(input string) error {
	var value any
	return json.NewDecoder(strings.NewReader(input)).Decode(&value) // want `first Decode on an in-memory JSON reader`
}

func badInlineBytesReader(input []byte) error {
	var value any
	return json.NewDecoder(bytes.NewReader(input)).Decode(&value) // want `first Decode on an in-memory JSON reader`
}

func goodDirectDecodeEOFComparison(input string) error {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input))
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if decoder.Decode(&value) != io.EOF {
		return errors.New("trailing or invalid JSON data")
	}
	return nil
}

func goodDirectDecodeErrorsIs(input string) error {
	var value any
	decoder := json.NewDecoder(bytes.NewReader([]byte(input)))
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if !errors.Is(decoder.Decode(&value), io.EOF) {
		return errors.New("trailing or invalid JSON data")
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
