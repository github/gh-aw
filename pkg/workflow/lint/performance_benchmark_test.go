package lint

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/require"
)

// Parsing is deliberately outside measurement: every rule consumes existing ASTs.
func performanceInput(tb testing.TB, contract ruleContract, target ASTTargets, positive bool, jobs int) Input {
	tb.Helper()
	source := contractSource(contract, target, false)
	prompt := contract.safePrompt
	if positive {
		source = contractSource(contract, target, true)
		prompt = contract.unsafePrompt
	}
	if target == ActionsAST {
		workflow, err := actions.Parse([]byte(source))
		require.NoError(tb, err)
		require.NotEmpty(tb, workflow.Jobs)
		template := *workflow.Jobs[0]
		var producers []*actions.Job
		for _, job := range workflow.Jobs {
			if job.ID == "check" {
				template = *job
			} else {
				producers = append(producers, job)
			}
		}
		if template.ID != "check" {
			producers = nil
		}
		workflow.Jobs = producers
		for i := range jobs {
			job := template
			job.ID = actions.JobID(fmt.Sprintf("check_%d", i))
			workflow.Jobs = append(workflow.Jobs, &job)
		}
		return Input{Workflow: workflow}
	}
	document, err := aw.Parse("---\n" + source + "---\nPublic performance-test prompt.\n")
	require.NoError(tb, err)
	if prompt != "" {
		document.Markdown = prompt
	}
	template := map[string]any{
		"runs-on": "ubuntu-latest",
		"steps":   []any{map[string]any{"shell": "bash", "run": "echo public"}},
	}
	if existing, ok := document.Frontmatter["jobs"].(map[string]any); ok {
		var valid bool
		template, valid = existing["check"].(map[string]any)
		require.True(tb, valid, "execution contract fixtures must retain the check job")
	}
	expanded := make(map[string]any, jobs)
	if existing, ok := document.Frontmatter["jobs"].(map[string]any); ok {
		for id, job := range existing {
			if id != "check" {
				expanded[id] = job
			}
		}
	}
	for i := range jobs {
		expanded[fmt.Sprintf("check_%d", i)] = template
	}
	document.Frontmatter["jobs"] = expanded
	if contract.javaScript {
		return graderJavaScriptPerformanceInput(tb, document, jobs)
	}
	if contract.safeShell != "" {
		source := contract.safeShell
		if positive {
			source = contract.unsafeShell
		}
		// Set up referenced files only once. The integrated rule must reread
		// them on each run; the I/O-free checker has its own package benchmark.
		return prepareGraderShellFixture(tb, Input{Document: document}, source)
	}
	return Input{Document: document}
}

func graderJavaScriptPerformanceInput(tb testing.TB, document *aw.Document, count int) Input {
	tb.Helper()
	options := nativeGraderJavaScriptOptions(tb)
	graders, ok := document.Frontmatter["graders"].(map[string]any)
	require.True(tb, ok)
	template, ok := graders["custom"].(map[string]any)
	require.True(tb, ok)
	source, ok := template["script"].(string)
	require.True(tb, ok)
	_, err := options.Parser.Parse(tb.Context(), source)
	require.NoError(tb, err, "native parsing belongs outside the measured AST rule")
	expanded := make(map[string]any, count)
	for i := range count {
		expanded[fmt.Sprintf("custom_%d", i)] = template
	}
	document.Frontmatter["graders"] = expanded
	return Input{Document: document, JavaScript: options}
}

func forEachPerformanceCase(tb testing.TB, run func(name string, rule Rule, contract ruleContract, target ASTTargets, positive bool)) {
	tb.Helper()
	contracts := builtInRuleContracts()
	rules := Default().Rules()
	require.Len(tb, contracts, len(rules), "new rules require performance fixtures")
	for _, rule := range rules {
		contract, exists := contracts[rule.ID]
		require.True(tb, exists)
		require.Equal(tb, contract.targets, rule.Targets)
		for _, target := range []ASTTargets{AWAST, ActionsAST} {
			if contract.targets&target == 0 {
				continue
			}
			for _, positive := range []bool{false, true} {
				name := fmt.Sprintf("%s/%s/positive=%t", rule.ID, target, positive)
				run(name, rule, contract, target, positive)
			}
		}
	}
}

func TestLintPerformanceBudget(t *testing.T) {
	forEachPerformanceCase(t, func(name string, rule Rule, contract ruleContract, target ASTTargets, positive bool) {
		t.Run(name, func(t *testing.T) {
			runner, err := New(rule)
			require.NoError(t, err)
			var allocations [2]float64
			var durations [2]time.Duration
			for index, jobs := range []int{32, 256} {
				input := performanceInput(t, contract, target, positive, jobs)
				var diagnostics []Diagnostic
				var runErr error
				run := func() {
					diagnostics, runErr = runner.Run(context.Background(), input)
				}
				run()
				require.NoError(t, runErr)
				findings := 0
				for _, diagnostic := range diagnostics {
					if diagnostic.Path != "workflow" {
						findings++
					}
				}
				if positive {
					require.Positive(t, findings, "the measured rule must actually detect its unsafe fixture")
				} else {
					require.Zero(t, findings, "the safe fixture must not introduce findings")
				}
				// Time only rule execution, not AllocsPerRun's GC/accounting.
				var samples [4]time.Duration
				sample := 0
				allocations[index] = testing.AllocsPerRun(3, func() {
					started := time.Now()
					run()
					samples[sample] = time.Since(started)
					sample++
				})
				measured := samples[1:] // Discard the allocation warm-up.
				slices.Sort(measured)
				durations[index] = measured[len(measured)/2]
				require.NoError(t, runErr)
				t.Logf("jobs=%d duration=%s allocs/op=%.0f", jobs, durations[index], allocations[index])
				require.Less(t, durations[index], 250*time.Millisecond, "AST-only checks must stay below the generous loaded-CI budget; profile with BenchmarkLintRules")
			}
			require.LessOrEqual(t, allocations[1], allocations[0]*12+1000,
				"8x more jobs must not cause quadratic allocation growth; profile this rule before increasing the budget")
			require.LessOrEqual(t, durations[1], durations[0]*24+5*time.Millisecond,
				"8x more jobs must not cause excessive CPU growth; the margin tolerates shared-CI scheduling noise")
		})
	})
}

func BenchmarkLintRules(b *testing.B) {
	forEachPerformanceCase(b, func(name string, rule Rule, contract ruleContract, target ASTTargets, positive bool) {
		for _, jobs := range []int{32, 256} {
			b.Run(fmt.Sprintf("%s/jobs=%d", name, jobs), func(b *testing.B) {
				input := performanceInput(b, contract, target, positive, jobs)
				runner, err := New(rule)
				require.NoError(b, err)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if _, err := runner.Run(context.Background(), input); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	})
}
