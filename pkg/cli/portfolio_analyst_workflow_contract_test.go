package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

// TestPortfolioAnalystAvoidsInvalidSentryAggregations verifies the guard that
// prevents the agent from using sum()/avg()/percentile aggregations on
// unconfirmed-numeric Sentry fields.  The tokens below are the stable contract
// surface; changing them requires updating both the workflow source and this test.
func TestPortfolioAnalystAvoidsInvalidSentryAggregations(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "portfolio-analyst.md"))
	if err != nil {
		t.Fatalf("failed to read portfolio analyst workflow source: %v", err)
	}
	text := string(content)

	// Semantic contract tokens – deliberately minimal so legitimate rewording
	// of surrounding prose does not cause spurious failures.
	for _, token := range []string{
		"sum()",
		"400",
		"aggregate",
		"count()",
		"schema type is not confirmed numeric",
	} {
		if !strings.Contains(text, token) {
			t.Fatalf("expected portfolio-analyst.md to contain token %q", token)
		}
	}

	// Guard must survive compilation through the compiler-selected author file.
	lockContent, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "portfolio-analyst.lock.yml"))
	if err != nil {
		t.Fatalf("failed to read portfolio-analyst.lock.yml: %v", err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Env map[string]string
			}
		}
	}
	if err := yaml.Unmarshal(lockContent, &workflow); err != nil {
		t.Fatalf("failed to parse portfolio-analyst.lock.yml: %v", err)
	}
	for _, job := range workflow.Jobs {
		for _, step := range job.Steps {
			value, ok := step.Env["GH_AW_PROMPT_CONFIG"]
			if !ok {
				continue
			}
			var config struct {
				Items []struct {
					AuthorFile string `json:"author_file"`
				} `json:"items"`
			}
			if err := json.Unmarshal([]byte(value), &config); err != nil {
				t.Fatalf("failed to parse prompt render config: %v", err)
			}
			for _, item := range config.Items {
				if item.AuthorFile == ".github/workflows/portfolio-analyst.md" {
					return
				}
			}
		}
	}
	t.Fatal("portfolio-analyst.lock.yml does not load the author file; the guard may have been dropped during compilation")
}

// TestSharedSentryImportFailsFastOnRepeatedQuery400s verifies the shared Sentry
// import contains the retry-termination guard and fallback chain.
// The tokens below are the stable contract surface; changing them requires
// updating both the shared import and this test.
func TestSharedSentryImportFailsFastOnRepeatedQuery400s(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "shared", "mcp", "sentry.md"))
	if err != nil {
		t.Fatalf("failed to read shared Sentry MCP import: %v", err)
	}
	text := string(content)

	// Semantic contract tokens.
	for _, token := range []string{
		"400",
		"terminal",
		"count()",
		"fallback chain",
		"stop retrying",
	} {
		if !strings.Contains(text, token) {
			t.Fatalf("expected shared/mcp/sentry.md to contain token %q", token)
		}
	}

	// Guard must survive compilation: the portfolio-analyst lock file should
	// include a runtime-import for the shared sentry import.
	lockContent, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "portfolio-analyst.lock.yml"))
	if err != nil {
		t.Fatalf("failed to read portfolio-analyst.lock.yml: %v", err)
	}
	if !strings.Contains(string(lockContent), "runtime-import .github/workflows/shared/mcp/sentry.md") {
		t.Fatalf("portfolio-analyst.lock.yml does not import shared/mcp/sentry.md; the guard may have been dropped during compilation")
	}
}
