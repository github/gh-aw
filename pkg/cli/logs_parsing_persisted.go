package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

// Persisted and supported native transcripts carry their own schema and identity; rendering them
// must not depend on an imported engine still being installed in the registry.
func parsePersistedAgentLog(runDir string) (bool, error) {
	found := false
	for _, name := range []string{"agent-session.jsonl", "agent-stdio.log"} {
		stat, err := os.Lstat(filepath.Join(runDir, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		found = found || stat.Mode().IsRegular()
	}
	if !found {
		return false, nil
	}
	// Omitting the engine argument deliberately avoids loading executable
	// definitions. The collector obtains identity from metadata/persisted events.
	session, err := runSessionParser(context.Background(), "reconstruct", runDir)
	if err != nil {
		if errors.Is(err, errNoRecognizableAgentSession) {
			return false, nil
		}
		return false, err
	}
	readable := false
	for line := range strings.SplitSeq(string(session), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event struct {
			Type       string `json:"type"`
			Provenance struct {
				Component string `json:"component"`
			} `json:"provenance"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return false, fmt.Errorf("invalid reconstructed agent session: %w", err)
		}
		if event.Provenance.Component == "agent" {
			switch event.Type {
			case "assistant.message", "assistant.reasoning", "assistant.refusal", "tool.execution_start", "tool.execution_complete":
				readable = true
			}
		}
	}
	if !readable {
		return false, nil
	}
	output, err := runSessionParserWithSources(
		context.Background(),
		map[string][]byte{"persisted_session.jsonl": session},
		"markdown", "persisted_session.jsonl",
	)
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(filepath.Join(runDir, "log.md"), []byte(strings.TrimSpace(string(output))), constants.FilePermPublic); err != nil {
		return false, fmt.Errorf("failed to write log.md: %w", err)
	}
	return true, nil
}
