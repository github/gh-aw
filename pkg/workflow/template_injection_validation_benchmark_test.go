package workflow

import (
	"testing"

	"github.com/goccy/go-yaml"
)

func BenchmarkValidateTemplateInjectionFastPath(b *testing.B) {
	compiler := NewCompiler()

	yamlContent := `
name: generated-safe-expression
on: workflow_dispatch
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Allowed generated expression
        run: |
          node ${{ runner.temp }}/actions/foo.cjs
          echo "${{ env.GH_AW_INPUT }}"
          echo done
`

	b.ReportAllocs()
	for b.Loop() {
		if err := compiler.validateTemplateInjection(yamlContent, "", "", nil); err != nil {
			b.Fatalf("validateTemplateInjection() error = %v", err)
		}
	}
}

func BenchmarkValidateNoGitHubExpressionsInRunScriptsFromParsed_NoExpressions(b *testing.B) {
	yamlContent := `
name: no-inline-expression
on: workflow_dispatch
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: No expression run
        run: |
          echo "hello"
          echo "world"
`

	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(yamlContent), &parsed); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		if err := validateNoGitHubExpressionsInRunScriptsFromParsed(parsed); err != nil {
			b.Fatalf("validateNoGitHubExpressionsInRunScriptsFromParsed() error = %v", err)
		}
	}
}
