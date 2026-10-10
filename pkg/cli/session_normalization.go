package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/parser"
)

var publishedSessionNormalizers = map[string]func([]byte) (bool, error){
	"pi": hasLegacyPiObservations,
}

func normalizePublishedSession(ctx context.Context, run *parser.GitHubURLComponents, hostname string, names []string, root, sessionPath string, content []byte, verbose bool) ([]byte, error) {
	if err := validateSessionJSONL(content); err != nil {
		return nil, err
	}
	candidates := make(map[string]struct{})
	for engine, needsNormalization := range publishedSessionNormalizers {
		matches, err := needsNormalization(content)
		if err != nil {
			return nil, fmt.Errorf("failed to inspect published session for engine %q: %w", engine, err)
		}
		if matches {
			candidates[engine] = struct{}{}
		}
	}
	if len(candidates) == 0 {
		return content, nil
	}
	engine, err := sessionEngine(ctx, run, hostname, names, root, verbose)
	if err != nil {
		return nil, err
	}
	if _, matches := candidates[engine]; !matches {
		return content, nil
	}
	normalized, err := runSessionParser(ctx, "normalize", sessionPath, engine)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize published session for engine %q: %w", engine, err)
	}
	if err := os.WriteFile(sessionPath, normalized, constants.FilePermSensitive); err != nil {
		return nil, fmt.Errorf("failed to write normalized session for engine %q: %w", engine, err)
	}
	return normalized, nil
}
