package parser

import (
	"testing"
)

func TestExtractDetectionPrerequisitesRejectsConflictingDuplicateIDs(t *testing.T) {
	acc := &importAccumulator{}
	frontmatter := func(version string) map[string]any {
		return map[string]any{
			"engine": map[string]any{
				"id":               "agent",
				"detection-engine": "agent",
				"behaviors":        map[string]any{},
			},
			"runtimes": map[string]any{
				"python": map[string]any{"version": version},
			},
		}
	}

	if err := acc.extractDetectionPrerequisites(frontmatter("3.12")); err != nil {
		t.Fatalf("first definition: %v", err)
	}
	if err := acc.extractDetectionPrerequisites(frontmatter("3.12")); err != nil {
		t.Fatalf("identical duplicate definition: %v", err)
	}
	if err := acc.extractDetectionPrerequisites(frontmatter("3.13")); err == nil {
		t.Fatal("expected conflicting duplicate definition to fail")
	}
}
