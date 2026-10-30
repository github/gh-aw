//go:build !integration

package cli

import (
	"fmt"
	"strings"
	"testing"
)

func FuzzStepsRunSecretsToEnvCodemod(f *testing.F) {
	f.Add(uint8(0), "RUNTIME_TOKEN", "RUNTIME_TOKEN", true, false, true, true, false)
	f.Add(uint8(1), "abc123", "lower_case", false, false, true, false, true)
	f.Add(uint8(2), "TOKEN_2", "TOKEN_2", true, true, false, true, true)
	f.Add(uint8(3), "A", "B", false, false, false, false, false)

	f.Fuzz(func(t *testing.T, sectionSelector uint8, secretNameRaw, envNameRaw string, includeSecret, includeComplexSecret, includeEnvExpression, includeGitHubToken, preseedBindings bool) {
		secretName := sanitizeHoistName(secretNameRaw)
		envName := sanitizeHoistName(envNameRaw)

		section := []string{"pre-steps", "steps", "post-steps", "pre-agent-steps"}[int(sectionSelector)%4]
		run, expectedVars := buildHoistFuzzRun(includeSecret, includeComplexSecret, includeEnvExpression, includeGitHubToken, secretName, envName)

		content := buildHoistFuzzContent(section, run, expectedVars, secretName, envName, preseedBindings)
		frontmatter := map[string]any{
			"on":       "push",
			section:    []any{map[string]any{"run": run}},
			"workflow": "fuzz",
		}

		result, applied, err := getStepsRunSecretsToEnvCodemod().Apply(content, frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(expectedVars) == 0 {
			if applied {
				t.Fatalf("expected no mutation for run=%q", run)
			}
			if result != content {
				t.Fatalf("content changed unexpectedly for no-op input")
			}
			return
		}

		if !applied {
			t.Fatalf("expected mutation for run=%q", run)
		}
		runLine := extractFuzzRunLine(result)
		// No ${{ ... }} expression should remain in the run line after applying.
		if strings.Contains(runLine, "${{") {
			t.Fatalf("run line still contains expression interpolation: %q", runLine)
		}
		for _, variable := range expectedVars {
			if !strings.Contains(runLine, "$"+variable) {
				t.Fatalf("run line missing rewritten variable %q: %q", variable, runLine)
			}
			if strings.HasSuffix(variable, "_") {
				if countEnvBindingKeyPrefix(result, variable) != 1 {
					t.Fatalf("expected exactly one env binding with prefix %s", variable)
				}
				continue
			}
			if countEnvBindingKey(result, variable) != 1 {
				t.Fatalf("expected exactly one env binding for %s", variable)
			}
		}
	})
}

// FuzzStepsRunSecretsToEnvCodemodExpr tests the EXPR_* catch-all path that hoists
// arbitrary GitHub Actions property-access chains (e.g. github.repository,
// inputs.my-input, steps.step-id.outputs.result) to EXPR_* env bindings.
func FuzzStepsRunSecretsToEnvCodemodExpr(f *testing.F) {
	f.Add(uint8(0), "github", "repository", false)
	f.Add(uint8(1), "inputs", "my-input", false)
	f.Add(uint8(2), "github", "sha", true)
	f.Add(uint8(3), "runner", "os", false)

	f.Fuzz(func(t *testing.T, sectionSelector uint8, namespace, propNameRaw string, preseedBinding bool) {
		namespace = sanitizeHoistPropertySegment(namespace)
		propName := sanitizeHoistPropertySegment(propNameRaw)
		section := []string{"pre-steps", "steps", "post-steps", "pre-agent-steps"}[int(sectionSelector)%4]

		// Build a simple two-segment property-access expression like "github.sha".
		// Readable binding names require valid property-chain characters; both
		// segments are sanitised above.
		expr := namespace + "." + propName
		run := fmt.Sprintf(`echo "${{ %s }}"`, expr)

		expectedEnvVar := "EXPR_" + strings.ToUpper(strings.NewReplacer(".", "_", "-", "_").Replace(expr))

		var lines []string
		lines = append(lines, "---", "on: push", section+":", "  - name: fuzz")
		if preseedBinding {
			lines = append(lines, "    env:", "      "+expectedEnvVar+": ${{ "+expr+" }}")
		}
		lines = append(lines, "    run: "+run, "---")
		content := strings.Join(lines, "\n") + "\n"

		frontmatter := map[string]any{
			"on":       "push",
			section:    []any{map[string]any{"name": "fuzz", "run": run}},
			"workflow": "fuzz",
		}

		result, applied, err := getStepsRunSecretsToEnvCodemod().Apply(content, frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !applied {
			t.Fatalf("expected codemod to apply for expr=%q run=%q", expr, run)
		}
		runLine := extractFuzzRunLine(result)
		if strings.Contains(runLine, "${{") {
			t.Fatalf("run line still contains expression interpolation after apply: %q", runLine)
		}
		if !strings.Contains(runLine, "$"+expectedEnvVar) {
			t.Fatalf("run line missing %q: %q", "$"+expectedEnvVar, runLine)
		}
		if countEnvBindingKey(result, expectedEnvVar) != 1 {
			t.Fatalf("expected exactly one env binding for %s, result:\n%s", expectedEnvVar, result)
		}
	})
}

// FuzzStepsRunSecretsToEnvCodemodPowerShell tests that PowerShell steps
// (shell: pwsh / shell: powershell) receive $env:VARNAME references instead
// of $VARNAME for all hoisted expressions.
func FuzzStepsRunSecretsToEnvCodemodPowerShell(f *testing.F) {
	f.Add(uint8(0), "MY_TOKEN", true)
	f.Add(uint8(1), "DEPLOY_KEY", false)
	f.Add(uint8(2), "A", true)

	f.Fuzz(func(t *testing.T, shellSelector uint8, secretNameRaw string, includeGitHubToken bool) {
		secretName := sanitizeHoistName(secretNameRaw)
		shell := []string{"pwsh", "powershell"}[int(shellSelector)%2]
		section := "steps"

		parts := []string{"${{ secrets." + secretName + " }}"}
		if includeGitHubToken {
			parts = append(parts, "${{ github.token }}")
		}
		run := `Write-Output "` + strings.Join(parts, " ") + `"`

		content := strings.Join([]string{
			"---",
			"on: push",
			section + ":",
			"  - name: ps fuzz",
			"    shell: " + shell,
			"    run: " + run,
			"---",
		}, "\n") + "\n"

		frontmatter := map[string]any{
			"on": "push",
			section: []any{map[string]any{
				"name":  "ps fuzz",
				"shell": shell,
				"run":   run,
			}},
		}

		result, applied, err := getStepsRunSecretsToEnvCodemod().Apply(content, frontmatter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !applied {
			t.Fatalf("expected codemod to apply for shell=%s run=%q", shell, run)
		}
		runLine := extractFuzzRunLine(result)
		if strings.Contains(runLine, "${{") {
			t.Fatalf("run line still contains expression interpolation: %q", runLine)
		}
		// PowerShell steps must use $env:VARNAME — never bare $VARNAME for the
		// secret binding (the plain $NAME form must not appear in the run line).
		if strings.Contains(runLine, "run: Write-Output \"$"+secretName) {
			t.Fatalf("PowerShell run line uses bare $VARNAME instead of $env:VARNAME: %q", runLine)
		}
		if !strings.Contains(runLine, "$env:"+secretName) {
			t.Fatalf("PowerShell run line missing $env:%s: %q", secretName, runLine)
		}
		if countEnvBindingKey(result, secretName) != 1 {
			t.Fatalf("expected exactly one env binding for %s", secretName)
		}
	})
}

func buildHoistFuzzRun(includeSecret, includeComplexSecret, includeEnvExpr, includeGitHubToken bool, secretName, envName string) (string, []string) {
	parts := make([]string, 0, 3)
	expected := make([]string, 0, 4)

	if includeSecret {
		parts = append(parts, "${{ secrets."+secretName+" }}")
		expected = append(expected, secretName)
	}
	if includeComplexSecret {
		parts = append(parts, "${{ secrets."+secretName+" || 'fallback' }}")
		expected = append(expected, "GH_AW_SECRET_"+secretName+"_")
	}
	if includeEnvExpr {
		parts = append(parts, "${{ env."+envName+" }}")
		expected = append(expected, "GH_AW_ENV_"+envName)
	}
	if includeGitHubToken {
		parts = append(parts, "${{ github.token }}")
		expected = append(expected, "GH_AW_GITHUB_TOKEN")
	}
	if len(parts) == 0 {
		return `echo "ok"`, nil
	}
	duplicated := append(append([]string(nil), parts...), parts...)
	return `echo "` + strings.Join(duplicated, " ") + `"`, expected
}

func buildHoistFuzzContent(section, run string, expectedVars []string, secretName, envName string, preseedBindings bool) string {
	lines := []string{
		"---",
		"on: push",
		section + ":",
		"  - name: fuzz",
	}

	if preseedBindings && len(expectedVars) > 0 {
		lines = append(lines, "    env:")
		for _, variable := range expectedVars {
			switch variable {
			case "GH_AW_GITHUB_TOKEN":
				lines = append(lines, "      "+variable+": ${{ github.token }}")
			case "GH_AW_ENV_" + envName:
				lines = append(lines, "      "+variable+": ${{ env."+envName+" }}")
			case secretName:
				lines = append(lines, "      "+variable+": ${{ secrets."+secretName+" }}")
			}
		}
	}

	lines = append(lines, "    run: "+run, "---")
	return strings.Join(lines, "\n") + "\n"
}

func extractFuzzRunLine(content string) string {
	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "run: ") {
			return trimmed
		}
	}
	return ""
}

func countEnvBindingKey(content, key string) int {
	count := 0
	for line := range strings.SplitSeq(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), key+": ") {
			count++
		}
	}
	return count
}

// countEnvBindingKeyPrefix counts env binding keys by prefix for hashed names
// where only the deterministic prefix is known in advance.
func countEnvBindingKeyPrefix(content, keyPrefix string) int {
	count := 0
	for line := range strings.SplitSeq(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), keyPrefix) {
			count++
		}
	}
	return count
}

// sanitizeHoistName converts arbitrary fuzz input into a valid env-var style token
// ([A-Z0-9_], max 20 chars) and ensures the name does not start with a digit.
func sanitizeHoistName(raw string) string {
	if raw == "" {
		return "TOKEN"
	}
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - ('a' - 'A'))
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_':
			b.WriteRune(r)
		}
		if b.Len() >= 20 {
			break
		}
	}
	s := b.String()
	if s == "" {
		return "TOKEN"
	}
	if s[0] >= '0' && s[0] <= '9' {
		return "T_" + s
	}
	return s
}

// sanitizeHoistPropertySegment converts arbitrary fuzz input into a valid
// GitHub Actions property-access segment eligible for a readable binding name:
// [a-zA-Z_][a-zA-Z0-9_-]*, max 20 chars.
func sanitizeHoistPropertySegment(raw string) string {
	if raw == "" {
		return "prop"
	}
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A')) // lowercase
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-':
			b.WriteRune(r)
		}
		if b.Len() >= 20 {
			break
		}
	}
	s := b.String()
	if s == "" {
		return "prop"
	}
	// Ensure the segment starts with a letter or underscore.
	if (s[0] >= '0' && s[0] <= '9') || s[0] == '-' {
		return "p" + s
	}
	return s
}
