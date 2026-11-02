package connectors

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/goccy/go-yaml"
)

const MaxDocumentBytes = 256 * 1024

var log = logger.New("connectors:parse")

// Decode accepts YAML 1.2 or JSON, rejects unknown fields and extra documents,
// and never resolves remote references.
func Decode(data []byte, target any) error {
	if len(data) > MaxDocumentBytes {
		return fmt.Errorf("connector document exceeds %d bytes", MaxDocumentBytes)
	}
	if json.Valid(data) {
		return decodeStrictJSON(data, target)
	}
	var document any
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&document); err != nil {
		return fmt.Errorf("decode connector YAML: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("connector document must contain exactly one document")
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return fmt.Errorf("encode connector document: %w", err)
	}
	return decodeStrictJSON(encoded, target)
}

func decodeStrictJSON(data []byte, target any) error {
	strict := json.NewDecoder(bytes.NewReader(data))
	strict.UseNumber()
	strict.DisallowUnknownFields()
	if err := strict.Decode(target); err != nil {
		return fmt.Errorf("decode connector fields: %w", err)
	}
	var extra any
	if err := strict.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("connector JSON must contain exactly one document")
	}
	return nil
}

func LoadSpec(path string) (*ConnectorSpec, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open connector definition: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("close connector definition: %v", err)
		}
	}()
	data, err := io.ReadAll(io.LimitReader(file, MaxDocumentBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read connector definition: %w", err)
	}
	var spec ConnectorSpec
	if err := Decode(data, &spec); err != nil {
		return nil, err
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	return &spec, nil
}
