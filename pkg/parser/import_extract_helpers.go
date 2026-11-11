package parser

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
)

func unmarshalJSONFromString(content string, target any) error {
	decoder := json.NewDecoder(strings.NewReader(content))
	if err := decoder.Decode(target); err != nil {
		return err
	}

	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return err
		}
		return errors.New("unexpected trailing JSON value")
	}

	return nil
}
