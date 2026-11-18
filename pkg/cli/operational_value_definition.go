package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
)

var valueDefinitionLog = logger.New("cli:value")

const schemaCoverageEvaluator = "schema-example-coverage-v1"

type valueFunctionConfig struct {
	Version   int             `json:"version"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Unit      string          `json:"unit"`
	Direction string          `json:"direction"`
	Evaluator string          `json:"evaluator"`
	Config    json.RawMessage `json:"config"`
}

type schemaCoverageConfig struct {
	Schema   string   `json:"schema"`
	Examples []string `json:"examples"`
}

func loadNativeValueMeasurement(definition valueMeasurementDefinition, path string) (valueMeasurement, error) {
	var config valueFunctionConfig
	digest, err := readValueDeclaration(path, &config)
	if err != nil {
		return nil, err
	}
	if err := validateNativeValueConfig(config); err != nil {
		return nil, err
	}
	definition.ID, definition.Name, definition.Unit = config.ID, config.Name, config.Unit
	definition.Direction, definition.Digest, definition.Evaluator = config.Direction, digest, config.Evaluator
	switch config.Evaluator {
	case schemaCoverageEvaluator:
		var settings schemaCoverageConfig
		if err := decodeValueDeclaration(config.Config, &settings); err != nil {
			return nil, err
		}
		if err := validateSchemaValueConfig(config, settings); err != nil {
			return nil, err
		}
		return schemaCoverageMeasurement{info: definition, config: settings}, nil
	case jsonNumberEvaluator:
		var settings jsonNumberConfig
		if err := decodeValueDeclaration(config.Config, &settings); err != nil {
			return nil, err
		}
		if err := gitutil.ValidateGitPath(settings.Path); err != nil {
			return nil, fmt.Errorf("value evaluator path: %w", err)
		}
		return jsonNumberMeasurement{info: definition, config: settings}, nil
	case valueEvidenceEvaluator:
		settings := valueEvidenceConfig{MinimumSamples: 1, Aggregation: "scalar"}
		if err := decodeValueDeclaration(config.Config, &settings); err != nil {
			return nil, err
		}
		if err := validateValueEvidenceConfig(settings); err != nil {
			return nil, err
		}
		return valueEvidenceMeasurement{info: definition, config: settings}, nil
	default:
		return nil, fmt.Errorf("unsupported value evaluator %q; supported: %s, %s, %s (JavaScript and query execution are not supported by this report)",
			config.Evaluator, schemaCoverageEvaluator, jsonNumberEvaluator, valueEvidenceEvaluator)
	}
}

func validateNativeValueConfig(config valueFunctionConfig) error {
	if config.Version != 1 {
		return errors.New("value definition version must be 1")
	}
	if strings.TrimSpace(config.ID) == "" || strings.TrimSpace(config.Name) == "" || strings.TrimSpace(config.Unit) == "" {
		return errors.New("value definition requires nonempty id, name, and unit")
	}
	if config.Direction != "higher_is_better" && config.Direction != "lower_is_better" {
		return errors.New("value definition direction must be higher_is_better or lower_is_better")
	}
	return nil
}

func validateSchemaValueConfig(config valueFunctionConfig, settings schemaCoverageConfig) error {
	if config.Direction != "higher_is_better" || config.Unit != "fields" {
		return errors.New("schema-example-coverage-v1 requires unit fields and direction higher_is_better")
	}
	if len(settings.Examples) == 0 || len(settings.Examples) > 32 {
		return errors.New("schema-example-coverage-v1 requires between 1 and 32 example directories")
	}
	for _, path := range append([]string{settings.Schema}, settings.Examples...) {
		if err := gitutil.ValidateGitPath(path); err != nil {
			return fmt.Errorf("value evaluator path: %w", err)
		}
	}
	return nil
}

func readValueDeclaration(path string, target any) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("value declaration must be a regular JSON file")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := file.Close(); err != nil {
			valueDefinitionLog.Printf("Failed to close value declaration: %v", err)
		}
	}()
	content, err := io.ReadAll(io.LimitReader(file, 512*1024+1))
	if err != nil {
		return "", err
	}
	if len(content) > 512*1024 {
		return "", errors.New("value declaration exceeds 512 KiB")
	}
	if err := decodeValueDeclaration(content, target); err != nil {
		return "", err
	}
	return valueDeclarationDigest(content), nil
}

func decodeValueDeclaration(content []byte, target any) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil {
		return fmt.Errorf("decode value declaration: %w", err)
	}
	if fields == nil {
		return errors.New("value declaration must be a JSON object")
	}
	if _, ok := fields["script"]; ok {
		return errors.New("value declarations no longer support script; select a named Go evaluator with evaluator and config; run-scoped graders are separate")
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode value declaration: %w", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("value declaration must contain exactly one JSON object")
	}
	return nil
}

func valueDeclarationDigest(content []byte) string {
	digest := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(digest[:])
}
