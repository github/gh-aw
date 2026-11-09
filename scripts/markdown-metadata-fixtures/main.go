// Developer-only fixture compiler. This command is not embedded, provisioned, or
// installed in actions/setup; deployed Actions runtimes never invoke it.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/goccy/go-yaml"
)

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
			for _, block := range parser.ExtractMarkdownMetadataBlocks(content) {
				value, err := parser.CompileMarkdownMetadata(block.Source)
				encoded, encodeErr := json.Marshal(value)
				if encodeErr != nil {
					panic(encodeErr)
				}
				hash := sha256.Sum256([]byte(block.Source))
				record := map[string]any{"sha256": hex.EncodeToString(hash[:]), "kind": "artifact", "value": json.RawMessage(encoded)}
				if err != nil {
					record["error"] = err.Error()
				}
				if block.Error != "" {
					record["error"] = block.Error
				}
				records[block.Source] = record
			}
		}
		result = records
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		panic(err)
	}
}
