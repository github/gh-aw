package workflow

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPrepareOperationalValueGrader(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	workflowPath := filepath.Join(repoRoot, ".github", "workflows", "example.md")
	evaluatorPath := filepath.Join(repoRoot, ".github", "workflows", "graders", "example-operational-value.sh")
	if err := os.MkdirAll(filepath.Dir(workflowPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(evaluatorPath), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "#!/usr/bin/env bash\nset -euo pipefail\n"
	if err := os.WriteFile(evaluatorPath, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	data := operationalValueGraderWorkflowData(".github/workflows/graders/example-operational-value.sh")

	if err := (&Compiler{}).prepareOperationalValueGrader(data, workflowPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	grader := data.Graders.Graders["operational-value"]
	if grader.evaluatorContent != content {
		t.Fatal("expected operational-value evaluator content to be frozen")
	}
	if len(grader.EvaluatorDigest()) != 64 {
		t.Fatalf("expected SHA-256 digest, got %q", grader.EvaluatorDigest())
	}
}

func TestPrepareOperationalValueGraderResolvesLocalDotPath(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	workflowPath := filepath.Join(repoRoot, ".github", "workflows", "example.md")
	evaluatorPath := filepath.Join(repoRoot, ".github", "workflows", "graders", "example-operational-value.sh")
	if err := os.MkdirAll(filepath.Dir(evaluatorPath), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "#!/usr/bin/env bash\nset -euo pipefail\n"
	if err := os.WriteFile(evaluatorPath, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	data := operationalValueGraderWorkflowData("./graders/example-operational-value.sh")

	if err := (&Compiler{}).prepareOperationalValueGrader(data, workflowPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data.Graders.Graders["operational-value"].evaluatorContent != content {
		t.Fatal("expected ./ evaluator path to resolve relative to the workflow")
	}
}

func TestPrepareOperationalValueGraderFreezesInlineScript(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	workflowPath := filepath.Join(repoRoot, ".github", "workflows", "example.md")
	if err := os.MkdirAll(filepath.Dir(workflowPath), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "#!/usr/bin/env bash\nset -euo pipefail\n"
	data := operationalValueGraderWorkflowData("")
	data.Graders.Graders["operational-value"].Script = content

	if err := (&Compiler{}).prepareOperationalValueGrader(data, workflowPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	grader := data.Graders.Graders["operational-value"]
	if grader.evaluatorContent != content {
		t.Fatal("expected inline operational-value evaluator content to be frozen")
	}
	if grader.Run != "" {
		t.Fatalf("inline evaluator must not expose a file-backed run path, got %q", grader.Run)
	}
	if grader.evaluatorSourcePath != ".github/workflows/example.md" {
		t.Fatalf("expected inline source workflow path, got %q", grader.evaluatorSourcePath)
	}
}

func TestPrepareOperationalValueGraderAcceptsMaximumFileSize(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	workflowPath := filepath.Join(repoRoot, ".github", "workflows", "example.md")
	evaluatorPath := filepath.Join(repoRoot, ".github", "graders", "example-operational-value.sh")
	if err := os.MkdirAll(filepath.Dir(workflowPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(evaluatorPath), 0o755); err != nil {
		t.Fatal(err)
	}
	prefix := "#!/usr/bin/env bash\n#"
	content := prefix + strings.Repeat("x", maxOperationalValueEvaluatorSize-len(prefix)-1) + "\n"
	if err := os.WriteFile(evaluatorPath, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}

	data := operationalValueGraderWorkflowData(".github/graders/example-operational-value.sh")
	if err := (&Compiler{}).prepareOperationalValueGrader(data, workflowPath); err != nil {
		t.Fatalf("expected file-backed evaluator of exactly %d bytes to pass, got: %v", maxOperationalValueEvaluatorSize, err)
	}
}

func TestValidateOperationalValueEvaluatorContentRejectsOneByteOverLimit(t *testing.T) {
	prefix := "#!/usr/bin/env bash\n#"
	content := prefix + strings.Repeat("x", maxOperationalValueEvaluatorSize-len(prefix)) + "\n"
	err := validateOperationalValueEvaluatorContent(content)
	if err == nil || !strings.Contains(err.Error(), "65536-byte limit") {
		t.Fatalf("expected one-byte-over-limit error, got: %v", err)
	}
}

func TestPrepareOperationalValueGraderRejectsInvalidInlineScript(t *testing.T) {
	tests := []struct {
		name    string
		content string
		errText string
	}{
		{name: "not bash", content: "echo operational value\n", errText: "Bash shebang"},
		{name: "invalid bash", content: "#!/usr/bin/env bash\nif true; then\n", errText: "invalid Bash syntax"},
		{name: "oversized", content: "#!/usr/bin/env bash\n" + strings.Repeat("x", maxOperationalValueEvaluatorSize), errText: "exceeds"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repoRoot := t.TempDir()
			if err := os.Mkdir(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
				t.Fatal(err)
			}
			workflowPath := filepath.Join(repoRoot, ".github", "workflows", "example.md")
			if err := os.MkdirAll(filepath.Dir(workflowPath), 0o755); err != nil {
				t.Fatal(err)
			}
			data := operationalValueGraderWorkflowData("")
			data.Graders.Graders["operational-value"].Script = test.content

			err := (&Compiler{}).prepareOperationalValueGrader(data, workflowPath)
			if err == nil || !strings.Contains(err.Error(), test.errText) {
				t.Fatalf("expected error containing %q, got %v", test.errText, err)
			}
		})
	}
}

func TestPrepareOperationalValueGraderRejectsInvalidFiles(t *testing.T) {
	tests := []struct {
		name    string
		content string
		errText string
	}{
		{name: "missing", errText: "cannot inspect"},
		{name: "not bash", content: "echo operational value\n", errText: "Bash shebang"},
		{name: "invalid bash", content: "#!/usr/bin/env bash\nif true; then\n", errText: "invalid Bash syntax"},
		{name: "oversized", content: "#!/usr/bin/env bash\n" + strings.Repeat("x", maxOperationalValueEvaluatorSize), errText: "exceeds"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repoRoot := t.TempDir()
			if err := os.Mkdir(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
				t.Fatal(err)
			}
			workflowPath := filepath.Join(repoRoot, ".github", "workflows", "example.md")
			evaluatorPath := filepath.Join(repoRoot, ".github", "workflows", "graders", "example-operational-value.sh")
			if err := os.MkdirAll(filepath.Dir(workflowPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if test.content != "" {
				if err := os.MkdirAll(filepath.Dir(evaluatorPath), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(evaluatorPath, []byte(test.content), 0o755); err != nil {
					t.Fatal(err)
				}
			}

			err := (&Compiler{}).prepareOperationalValueGrader(operationalValueGraderWorkflowData(".github/workflows/graders/example-operational-value.sh"), workflowPath)
			if err == nil || !strings.Contains(err.Error(), test.errText) {
				t.Fatalf("expected error containing %q, got %v", test.errText, err)
			}
		})
	}
}

func TestPrepareOperationalValueGraderRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires additional privileges on Windows")
	}
	repoRoot := t.TempDir()
	outside := filepath.Join(t.TempDir(), "operational-value.sh")
	if err := os.WriteFile(outside, []byte("#!/usr/bin/env bash\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	workflowPath := filepath.Join(repoRoot, ".github", "workflows", "example.md")
	evaluatorPath := filepath.Join(repoRoot, ".github", "workflows", "graders", "example-operational-value.sh")
	if err := os.MkdirAll(filepath.Dir(workflowPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(evaluatorPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, evaluatorPath); err != nil {
		t.Fatal(err)
	}

	err := (&Compiler{}).prepareOperationalValueGrader(operationalValueGraderWorkflowData(".github/workflows/graders/example-operational-value.sh"), workflowPath)
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("expected symlink escape error, got %v", err)
	}
}

func TestPrepareOperationalValueGraderRejectsRepositorySymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires additional privileges on Windows")
	}
	repoRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	workflowPath := filepath.Join(repoRoot, ".github", "workflows", "example.md")
	gradersDir := filepath.Join(repoRoot, ".github", "workflows", "graders")
	targetPath := filepath.Join(gradersDir, "target.sh")
	evaluatorPath := filepath.Join(gradersDir, "example-operational-value.sh")
	if err := os.MkdirAll(filepath.Dir(workflowPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(gradersDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("#!/usr/bin/env bash\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(targetPath, evaluatorPath); err != nil {
		t.Fatal(err)
	}

	err := (&Compiler{}).prepareOperationalValueGrader(operationalValueGraderWorkflowData(".github/workflows/graders/example-operational-value.sh"), workflowPath)
	if err == nil || !strings.Contains(err.Error(), "must not be a symbolic link") {
		t.Fatalf("expected symlink rejection error, got %v", err)
	}
}

func operationalValueGraderWorkflowData(evaluatorPath string) *WorkflowData {
	return &WorkflowData{
		Graders: &GradersConfig{
			Graders: map[string]*GraderDefinition{
				"operational-value": {ID: "operational-value", Run: evaluatorPath},
			},
		},
	}
}
