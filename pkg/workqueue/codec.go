package workqueue

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed schema/*.json
var schemas embed.FS

const maxParseBytes = 80 << 20
const maxLineBytes = 8 << 20

var commitSchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	data, err := schemas.ReadFile("schema/QueueCommit.json")
	if err != nil {
		return nil, err
	}
	var resource any
	if err := json.Unmarshal(data, &resource); err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("QueueCommit.json", resource); err != nil {
		return nil, err
	}
	return compiler.Compile("QueueCommit.json")
})

func queueError(code, message string, args ...any) error {
	return fmt.Errorf("%s: %s", code, fmt.Sprintf(message, args...))
}

func readValue(decoder *json.Decoder, depth int) (any, error) {
	if depth > 64 {
		return nil, queueError("resource_limit", "JSON nesting exceeds 64")
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			result := map[string]any{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				name, ok := key.(string)
				if !ok {
					return nil, errors.New("JSON object key must be a string")
				}
				if _, exists := result[name]; exists {
					return nil, queueError("duplicate_key", "%q", name)
				}
				child, err := readValue(decoder, depth+1)
				if err != nil {
					return nil, err
				}
				result[name] = child
			}
			if _, err := decoder.Token(); err != nil {
				return nil, err
			}
			return result, nil
		case '[':
			result := []any{}
			for decoder.More() {
				if len(result) >= 16384 {
					return nil, queueError("resource_limit", "JSON array exceeds 16384 members")
				}
				child, err := readValue(decoder, depth+1)
				if err != nil {
					return nil, err
				}
				result = append(result, child)
			}
			if _, err := decoder.Token(); err != nil {
				return nil, err
			}
			return result, nil
		}
		return nil, errors.New("unexpected JSON delimiter")
	case json.Number:
		n, err := strconv.ParseInt(string(value), 10, 64)
		if err != nil || n < -MaxTimestamp || n > MaxTimestamp || strconv.FormatInt(n, 10) != string(value) {
			return nil, queueError("noncanonical_number", "use safe integer numbers or canonical decimal strings")
		}
		return value, nil
	default:
		return value, nil
	}
}

func decodeStrict(data []byte) (any, error) {
	if !utf8.Valid(data) {
		return nil, queueError("invalid_utf8", "JSON must be valid UTF-8")
	}
	// encoding/json replaces lone surrogate escapes; reject them instead.
	for i := 0; i < len(data); i++ {
		if data[i] != '\\' || i+1 >= len(data) {
			continue
		}
		if data[i+1] == '\\' {
			i++
			continue
		}
		if data[i+1] != 'u' || i+6 > len(data) {
			continue
		}
		unit, err := strconv.ParseUint(string(data[i+2:i+6]), 16, 16)
		if err != nil {
			continue
		}
		if unit >= 0xdc00 && unit <= 0xdfff {
			return nil, queueError("invalid_unicode", "unpaired surrogate escape")
		}
		if unit >= 0xd800 && unit <= 0xdbff {
			if i+12 > len(data) || string(data[i+6:i+8]) != `\u` {
				return nil, queueError("invalid_unicode", "unpaired surrogate escape")
			}
			low, err := strconv.ParseUint(string(data[i+8:i+12]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return nil, queueError("invalid_unicode", "unpaired surrogate escape")
			}
			i += 11
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := readValue(decoder, 0)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, queueError("invalid_json", "trailing JSON content")
	}
	return value, nil
}

func writeCanonical(buffer *bytes.Buffer, value any) error {
	switch value := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		buffer.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				buffer.WriteByte(',')
			}
			if err := writeCanonical(buffer, key); err != nil {
				return err
			}
			buffer.WriteByte(':')
			if err := writeCanonical(buffer, value[key]); err != nil {
				return err
			}
		}
		buffer.WriteByte('}')
	case []any:
		buffer.WriteByte('[')
		for i, child := range value {
			if i > 0 {
				buffer.WriteByte(',')
			}
			if err := writeCanonical(buffer, child); err != nil {
				return err
			}
		}
		buffer.WriteByte(']')
	case string:
		buffer.WriteByte('"')
		for _, char := range value {
			switch char {
			case '"':
				buffer.WriteString(`\"`)
			case '\\':
				buffer.WriteString(`\\`)
			case '\b':
				buffer.WriteString(`\b`)
			case '\f':
				buffer.WriteString(`\f`)
			case '\n':
				buffer.WriteString(`\n`)
			case '\r':
				buffer.WriteString(`\r`)
			case '\t':
				buffer.WriteString(`\t`)
			default:
				if char < 0x20 {
					fmt.Fprintf(buffer, `\u%04x`, char)
				} else {
					buffer.WriteRune(char)
				}
			}
		}
		buffer.WriteByte('"')
	case json.Number:
		buffer.WriteString(string(value))
	case bool:
		buffer.WriteString(strconv.FormatBool(value))
	case nil:
		buffer.WriteString("null")
	default:
		return fmt.Errorf("unsupported canonical value %T", value)
	}
	return nil
}

// Canonical uses the contract's integer-only JSON profile and UTF-8 key order.
func Canonical(data []byte) ([]byte, error) {
	value, err := decodeStrict(data)
	if err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	if err := writeCanonical(&buffer, value); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func canonicalValue(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return Canonical(data)
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func NodeID(graphID, nodeKey string) string {
	data, _ := canonicalValue(map[string]string{"graph_id": graphID, "node_key": nodeKey})
	return hashBytes(data)
}

func Fingerprint(actor Actor, kind string, parameters json.RawMessage) (string, error) {
	data, err := canonicalValue(map[string]any{"actor": actor, "kind": kind, "parameters": parameters})
	if err != nil {
		return "", err
	}
	return hashBytes(data), nil
}

func NewRequest(id, kind string, actor Actor, parameters any) (Request, error) {
	data, err := canonicalValue(parameters)
	if err != nil {
		return Request{}, err
	}
	fingerprint, err := Fingerprint(actor, kind, data)
	return Request{ID: id, Kind: kind, Parameters: data, Fingerprint: fingerprint}, err
}

func Op(value any) Operation {
	data, err := canonicalValue(value)
	if err != nil {
		panic(err)
	}
	return data
}

func operationKind(op Operation) string {
	var value struct {
		Kind string `json:"kind"`
	}
	_ = json.Unmarshal(op, &value)
	return value.Kind
}

func ValidateCommit(commit QueueCommit) error {
	data, err := canonicalValue(commit)
	if err != nil {
		return err
	}
	if len(data) > maxLineBytes {
		return queueError("resource_limit", "commit exceeds parser bound")
	}
	schema, err := commitSchema()
	if err != nil {
		return err
	}
	// The schema library consumes float64 for safe numbers, while canonical
	// validation has already checked their exact representability.
	var schemaValue any
	if err := json.Unmarshal(data, &schemaValue); err != nil {
		return err
	}
	if err := schema.Validate(schemaValue); err != nil {
		return queueError("unsupported_protocol", "invalid version-3 QueueCommit: %v", err)
	}
	if err := validateContractIdentityBytes("QueueCommit", schemaValue); err != nil {
		return err
	}
	if err := validateActorOrigin(commit.Actor); err != nil {
		return err
	}
	fingerprint, err := Fingerprint(commit.Actor, commit.Request.Kind, commit.Request.Parameters)
	if err != nil {
		return err
	}
	if fingerprint != commit.Request.Fingerprint {
		return queueError("request_fingerprint", "request %s does not bind actor and intent", commit.Request.ID)
	}
	return nil
}

func Parse(data []byte) ([]QueueCommit, error) {
	if len(data) > maxParseBytes {
		return nil, queueError("resource_limit", "ledger exceeds 80 MiB parser bound")
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		return nil, queueError("ledger_invalid", "queue log must be nonempty and newline terminated")
	}
	result := []QueueCommit{}
	for index, line := range bytes.Split(data[:len(data)-1], []byte{'\n'}) {
		if len(line) == 0 || len(line) > maxLineBytes {
			return nil, queueError("ledger_invalid", "invalid line %d size", index+1)
		}
		value, err := decodeStrict(line)
		if err != nil {
			return nil, queueError("ledger_invalid", "line %d: %v", index+1, err)
		}
		var canonical bytes.Buffer
		if err := writeCanonical(&canonical, value); err != nil {
			return nil, err
		}
		schema, err := commitSchema()
		if err != nil {
			return nil, err
		}
		var raw any
		if err := json.Unmarshal(canonical.Bytes(), &raw); err != nil {
			return nil, err
		}
		if err := schema.Validate(raw); err != nil {
			return nil, queueError("unsupported_protocol", "line %d: invalid version-3 QueueCommit: %v", index+1, err)
		}
		var commit QueueCommit
		decoder := json.NewDecoder(bytes.NewReader(canonical.Bytes()))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&commit); err != nil {
			return nil, queueError("unsupported_protocol", "line %d: %v", index+1, err)
		}
		if err := ValidateCommit(commit); err != nil {
			return nil, queueError("ledger_invalid", "line %d: %v", index+1, err)
		}
		result = append(result, commit)
	}
	return result, nil
}

func causalOrder(commits []QueueCommit) ([]QueueCommit, error) {
	if len(commits) == 0 {
		return nil, queueError("policy_missing", "existing queue has no policy genesis")
	}
	byID := map[string]QueueCommit{}
	successor := map[string]string{}
	genesis := ""
	for _, commit := range commits {
		if err := ValidateCommit(commit); err != nil {
			return nil, err
		}
		if existing, ok := byID[commit.ID]; ok {
			a, _ := canonicalValue(existing)
			b, _ := canonicalValue(commit)
			if !bytes.Equal(a, b) {
				return nil, queueError("ledger_invalid", "conflicting commit %s", commit.ID)
			}
			continue
		}
		byID[commit.ID] = commit
		if commit.Previous == nil {
			if genesis != "" {
				return nil, queueError("ledger_invalid", "multiple genesis commits")
			}
			genesis = commit.ID
		} else {
			if _, ok := successor[*commit.Previous]; ok {
				return nil, queueError("ledger_invalid", "fork after %s", *commit.Previous)
			}
			successor[*commit.Previous] = commit.ID
		}
	}
	if genesis == "" {
		return nil, queueError("ledger_invalid", "missing genesis")
	}
	result := make([]QueueCommit, 0, len(byID))
	for id := genesis; id != ""; id = successor[id] {
		if len(result) >= len(byID) {
			return nil, queueError("ledger_invalid", "causal cycle")
		}
		commit, ok := byID[id]
		if !ok {
			return nil, queueError("ledger_invalid", "missing predecessor")
		}
		result = append(result, commit)
	}
	if len(result) != len(byID) {
		return nil, queueError("ledger_invalid", "disconnected chain, missing predecessor or cycle")
	}
	return result, nil
}

func Serialize(commits []QueueCommit) ([]byte, error) {
	ordered, err := causalOrder(commits)
	if err != nil {
		return nil, err
	}
	var result bytes.Buffer
	for _, commit := range ordered {
		data, err := canonicalValue(commit)
		if err != nil {
			return nil, err
		}
		result.Write(data)
		result.WriteByte('\n')
	}
	return result.Bytes(), nil
}

func Compact(commits []QueueCommit) ([]QueueCommit, error) {
	if _, err := Replay(commits); err != nil {
		return nil, err
	}
	return causalOrder(commits)
}

func sameJSON(a, b any) bool {
	left, err := canonicalValue(a)
	if err != nil {
		return false
	}
	right, err := canonicalValue(b)
	return err == nil && bytes.Equal(left, right)
}

func validKey(key string) bool {
	if !utf8.ValidString(key) || len(key) > 128 {
		return false
	}
	for _, char := range key {
		if unicode.IsControl(char) {
			return false
		}
	}
	return true
}
