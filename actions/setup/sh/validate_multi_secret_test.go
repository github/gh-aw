package sh_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func runValidateMultiSecret(t *testing.T, env map[string]string, secretNames ...string) (string, string, error) {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate test file")
	}

	tmpDir := t.TempDir()
	args := append([]string{filepath.Join(filepath.Dir(filename), "validate_multi_secret.sh")}, secretNames...)
	args = append(args, "TestEngine", "https://example.invalid/docs")
	cmd := exec.Command("bash", args...)
	cmd.Env = append(os.Environ(),
		"GITHUB_OUTPUT="+filepath.Join(tmpDir, "output"),
		"GITHUB_STEP_SUMMARY="+filepath.Join(tmpDir, "summary"),
	)
	for name, value := range env {
		cmd.Env = append(cmd.Env, name+"="+value)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func assertSecretValueNotLogged(t *testing.T, stdout, stderr, secretValue string) {
	t.Helper()

	if strings.TrimSpace(secretValue) == "" {
		return
	}
	if strings.Contains(stdout, secretValue) || strings.Contains(stderr, secretValue) {
		t.Fatalf("validation output leaked the secret value\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestValidateMultiSecretLogsPresentSecretLength(t *testing.T) {
	t.Parallel()
	stdout, stderr, err := runValidateMultiSecret(t, map[string]string{
		"TEST_SECRET": "super-secret-token",
	}, "TEST_SECRET")
	if err != nil {
		t.Fatalf("expected validation to succeed, got %v; stderr:\n%s", err, stderr)
	}

	if !strings.Contains(stdout, "TEST_SECRET: present (length=18)") {
		t.Fatalf("expected stdout to include length-only presence log, got:\n%s", stdout)
	}
	assertSecretValueNotLogged(t, stdout, stderr, "super-secret-token")
}

func TestValidateMultiSecretRejectsPlaceholderValues(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		value  string
		length int
	}{
		{name: "null", value: "null", length: 4},
		{name: "null-with-spaces", value: "  null  ", length: 8},
		{name: "undefined", value: "undefined", length: 9},
		{name: "whitespace", value: "   ", length: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, err := runValidateMultiSecret(t, map[string]string{
				"TEST_SECRET": tc.value,
			}, "TEST_SECRET")
			if err == nil {
				t.Fatalf("expected validation to fail; stdout:\n%s", stdout)
			}

			if !strings.Contains(stderr, "TEST_SECRET secret has an invalid value") {
				t.Fatalf("expected invalid value error, got:\n%s", stderr)
			}
			if !strings.Contains(stderr, "length="+strconv.Itoa(tc.length)) {
				t.Fatalf("expected length %d in stderr, got:\n%s", tc.length, stderr)
			}
			assertSecretValueNotLogged(t, stdout, stderr, tc.value)
		})
	}
}

func TestValidateMultiSecretRejectsInvalidSecretBeforeFallback(t *testing.T) {
	t.Parallel()
	stdout, stderr, err := runValidateMultiSecret(t, map[string]string{
		"PRIMARY_SECRET":  "null",
		"FALLBACK_SECRET": "valid-fallback-token",
	}, "PRIMARY_SECRET", "FALLBACK_SECRET")
	if err == nil {
		t.Fatalf("expected validation to fail; stdout:\n%s", stdout)
	}

	if !strings.Contains(stderr, "PRIMARY_SECRET secret has an invalid value") {
		t.Fatalf("expected primary secret invalid error, got:\n%s", stderr)
	}
	if strings.Contains(stdout, "FALLBACK_SECRET") {
		t.Fatalf("validation reported fallback secret before rejecting primary\nstdout:\n%s", stdout)
	}
	assertSecretValueNotLogged(t, stdout, stderr, "valid-fallback-token")
}

func TestValidateMultiSecretExplainsRequirement(t *testing.T) {
	t.Parallel()
	stdout, stderr, err := runValidateMultiSecret(t, nil, "JIRA_API_TOKEN")
	if err == nil {
		t.Fatalf("expected validation to fail; stdout:\n%s", stdout)
	}
	if !strings.Contains(stderr, "JIRA_API_TOKEN is required by TestEngine.") {
		t.Fatalf("expected requirement explanation, got:\n%s", stderr)
	}
	if strings.Contains(stderr, "❌") {
		t.Fatalf("expected error message without emoji, got:\n%s", stderr)
	}
}
