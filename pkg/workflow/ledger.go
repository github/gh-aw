package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	defaultLedgerName      = "default"
	maxLedgerSchemaBytes   = 1024 * 1024
	defaultLedgerRecordKB  = 32
	defaultLedgerSegmentKB = 100
	defaultLedgerPatchKB   = 10
	ledgerProjectionRoot   = "/tmp/gh-aw/ledgers"
)

var ledgerNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// LedgerConfig describes one standalone, Git-backed ledger.
type LedgerConfig struct {
	Name         string
	Schema       map[string]any
	SchemaPath   string
	MaxRecordKB  int
	MaxSegmentKB int
	MaxPatchKB   int
	BranchName   string
}

// LedgerToolConfig is the normalized tools.ledger configuration.
type LedgerToolConfig struct {
	Ledgers []LedgerConfig
}

func (c *LedgerToolConfig) Enabled() bool { return c != nil && len(c.Ledgers) > 0 }

func ledgerBranchName(name string) string { return "ledgers/" + name }

func parseLedgerToolConfig(raw any) (*LedgerToolConfig, error) {
	if raw == nil {
		raw = map[string]any{}
	}
	root, ok := raw.(map[string]any)
	if !ok {
		return nil, errors.New("tools.ledger must be an object")
	}
	result := &LedgerToolConfig{}
	// A schema/limit property identifies the concise single-ledger form. All
	// other properties are names, which keeps the two forms unambiguous.
	single := false
	for key := range root {
		switch key {
		case "schema", "max-record-kb", "max-segment-kb", "max-patch-kb":
			single = true
		}
	}
	if single {
		cfg, err := parseLedgerConfig(defaultLedgerName, root)
		if err != nil {
			return nil, err
		}
		result.Ledgers = []LedgerConfig{cfg}
		return result, nil
	}
	if len(root) == 0 {
		cfg, err := parseLedgerConfig(defaultLedgerName, root)
		if err != nil {
			return nil, err
		}
		result.Ledgers = []LedgerConfig{cfg}
		return result, nil
	}
	names := make([]string, 0, len(root))
	for name := range root {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		cfgMap, ok := root[name].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tools.ledger.%s must be an object", name)
		}
		cfg, err := parseLedgerConfig(name, cfgMap)
		if err != nil {
			return nil, err
		}
		result.Ledgers = append(result.Ledgers, cfg)
	}
	return result, nil
}

func parseLedgerConfig(name string, raw map[string]any) (LedgerConfig, error) {
	if !ledgerNamePattern.MatchString(name) {
		return LedgerConfig{}, fmt.Errorf("tools.ledger name %q must contain only letters, numbers, hyphens, and underscores", name)
	}
	cfg := LedgerConfig{Name: name, BranchName: ledgerBranchName(name), MaxRecordKB: defaultLedgerRecordKB, MaxSegmentKB: defaultLedgerSegmentKB, MaxPatchKB: defaultLedgerPatchKB}
	for key, value := range raw {
		switch key {
		case "schema":
			switch schema := value.(type) {
			case string:
				if schema == "" || strings.HasPrefix(schema, "/") || strings.Contains(schema, "..") || strings.ContainsAny(schema, `\${{}`) {
					return LedgerConfig{}, fmt.Errorf("tools.ledger.%s.schema must be a repository-relative path without expressions or traversal", name)
				}
				cfg.SchemaPath = schema
			case map[string]any:
				if err := validateInlineLedgerSchema(schema, 0); err != nil {
					return LedgerConfig{}, fmt.Errorf("tools.ledger.%s.schema: %w", name, err)
				}
				encoded, err := json.Marshal(schema)
				if err != nil || len(encoded) > maxLedgerSchemaBytes {
					return LedgerConfig{}, fmt.Errorf("tools.ledger.%s.schema exceeds the maximum size", name)
				}
				cfg.Schema = schema
			default:
				return LedgerConfig{}, fmt.Errorf("tools.ledger.%s.schema must be a path or JSON Schema object", name)
			}
		case "max-record-kb", "max-segment-kb", "max-patch-kb":
			number, ok := value.(int)
			if !ok || number < 1 || number > 10240 {
				return LedgerConfig{}, fmt.Errorf("tools.ledger.%s.%s must be a positive integer", name, key)
			}
			switch key {
			case "max-record-kb":
				cfg.MaxRecordKB = number
			case "max-segment-kb":
				cfg.MaxSegmentKB = number
			default:
				cfg.MaxPatchKB = number
			}
		default:
			return LedgerConfig{}, fmt.Errorf("tools.ledger.%s has unsupported property %q", name, key)
		}
	}
	if cfg.MaxRecordKB > cfg.MaxSegmentKB {
		return LedgerConfig{}, fmt.Errorf("tools.ledger.%s.max-record-kb cannot exceed max-segment-kb", name)
	}
	return cfg, nil
}

func validateInlineLedgerSchema(value map[string]any, depth int) error {
	if depth > 32 {
		return errors.New("schema is too deeply nested")
	}
	for key, child := range value {
		if strings.Contains(key, "${{") {
			return errors.New("schema cannot contain GitHub expressions")
		}
		switch key {
		case "type", "enum", "required":
			if strings.Contains(fmt.Sprint(child), "${{") {
				return errors.New("schema cannot contain GitHub expressions")
			}
		case "properties":
			properties, ok := child.(map[string]any)
			if !ok {
				return errors.New("properties must be an object")
			}
			for name, property := range properties {
				if strings.Contains(name, "${{") {
					return errors.New("schema cannot contain GitHub expressions")
				}
				object, ok := property.(map[string]any)
				if !ok {
					return fmt.Errorf("property %q must be an object", name)
				}
				if err := validateInlineLedgerSchema(object, depth+1); err != nil {
					return err
				}
			}
		case "items":
			object, ok := child.(map[string]any)
			if !ok {
				return errors.New("items must be an object")
			}
			if err := validateInlineLedgerSchema(object, depth+1); err != nil {
				return err
			}
		case "additionalProperties", "oneOf", "anyOf":
			// These keywords are accepted by the existing JSON-schema validator.
		default:
			return fmt.Errorf("unsupported JSON Schema keyword %q", key)
		}
	}
	return nil
}

func buildLedgerPromptSection(config *LedgerToolConfig) *PromptSection {
	if !config.Enabled() {
		return nil
	}
	var b strings.Builder
	b.WriteString("Persistent ledgers available (SQLite is read-only and disposable):\n")
	for _, ledger := range config.Ledgers {
		fmt.Fprintf(&b, "- %s: %s\n", ledger.Name, filepath.Join(ledgerProjectionRoot, ledger.Name, "ledger.db"))
	}
	b.WriteString("Query the SQLite projection to inspect prior records. Submit durable records only with the ledger append safe output; never edit ledger files or SQLite directly. Temporary IDs may reference records in the same batch and are resolved during trusted validation. Accepted requests are not durable until push_ledger_changes succeeds.")
	return &PromptSection{Content: b.String()}
}
