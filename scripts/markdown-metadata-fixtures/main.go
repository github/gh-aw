// Developer-only fixture compiler. This command is not embedded, provisioned, or
// installed in actions/setup; deployed Actions runtimes never invoke it.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/goccy/go-yaml"
)

var metadataPattern = regexp.MustCompile(`(?m)(?:\A|^##[ \t]+(?:agent|skill):[^\n]*\n(?:[ \t]*\n)*)---[ \t]*\n([\s\S]*?)\n---[ \t]*(?:\n|$)`)

func main() {
	var input struct {
		Document string   `json:"document"`
		Sources  []string `json:"sources"`
	}
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		panic(err)
	}
	var result any
	if input.Sources == nil {
		var value any
		err := yaml.Unmarshal([]byte(input.Document), &value)
		errorText := ""
		if err != nil {
			errorText = err.Error()
		}
		result = map[string]any{"value": value, "error": errorText}
	} else {
		records := map[string]any{}
		for _, content := range input.Sources {
			for _, match := range metadataPattern.FindAllStringSubmatch(strings.ReplaceAll(content, "\r\n", "\n"), -1) {
				if len(match) < 2 {
					continue
				}
				value, err := parser.CompileMarkdownMetadata(match[1]) //nolint:uncheckedsliceindex // The regexp capture count is checked above.
				encoded, encodeErr := json.Marshal(value)
				if encodeErr != nil {
					panic(encodeErr)
				}
				hash := sha256.Sum256([]byte(match[1])) //nolint:uncheckedsliceindex // The regexp capture count is checked above.
				record := map[string]any{"sha256": hex.EncodeToString(hash[:]), "kind": "artifact", "value_json": string(encoded)}
				if err != nil {
					record["error"] = err.Error()
				}
				records[match[1]] = record //nolint:uncheckedsliceindex // The regexp capture count is checked above.
			}
		}
		result = records
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		panic(err)
	}
}
