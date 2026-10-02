//go:build !integration

package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseBootstrapBool(t *testing.T) {
	t.Parallel()
	t.Run("truthy", func(t *testing.T) {
		truthy := []string{"1", "true", "yes", "on"}
		for _, raw := range truthy {
			got, err := parseBootstrapBool(raw)
			if err != nil {
				t.Fatalf("parseBootstrapBool(%q) returned error: %v", raw, err)
			}
			if !got {
				t.Fatalf("expected %q to parse as true", raw)
			}
		}
	})

	t.Run("falsy", func(t *testing.T) {
		falsy := []string{"0", "false", "no", "off"}
		for _, raw := range falsy {
			got, err := parseBootstrapBool(raw)
			if err != nil {
				t.Fatalf("parseBootstrapBool(%q) returned error: %v", raw, err)
			}
			if got {
				t.Fatalf("expected %q to parse as false", raw)
			}
		}
	})

	t.Run("invalid", func(t *testing.T) {
		if _, err := parseBootstrapBool("maybe"); err == nil {
			t.Fatal("expected invalid boolean error")
		}
	})
}

func TestWorkflowGrantsCopilotRequestsWrite_UsesFrontmatterPermissions(t *testing.T) {
	t.Parallel()
	t.Run("requires structural permission", func(t *testing.T) {
		content := []byte("---\nengine: copilot\npermissions:\n  contents: read\n---\n\ncopilot-requests: write\n")
		if workflowGrantsCopilotRequestsWrite(content) {
			t.Fatal("expected prompt body text to be ignored")
		}
	})

	t.Run("accepts explicit permission", func(t *testing.T) {
		content := []byte("---\nengine: copilot\npermissions:\n  copilot-requests: write\n---\n")
		if !workflowGrantsCopilotRequestsWrite(content) {
			t.Fatal("expected explicit copilot-requests: write permission")
		}
	})
}

func TestBootstrapRepositoryInputEnvNames(t *testing.T) {
	t.Parallel()
	if got := bootstrapRepositoryVariableEnvName("CENTRAL_AGENTIC_OPS_MODE"); got != "GH_AW_BOOTSTRAP_VAR_CENTRAL_AGENTIC_OPS_MODE" {
		t.Fatalf("unexpected variable env name: %s", got)
	}
	if got := bootstrapRepositorySecretEnvName("copilot-token.pem"); got != "GH_AW_BOOTSTRAP_SECRET_COPILOT_TOKEN_PEM" {
		t.Fatalf("unexpected secret env name: %s", got)
	}
}

func TestProfileSourcesUseActionsTokenCopilotAuth(t *testing.T) {
	t.Parallel()
	workflowDir := t.TempDir()
	workflowPath := filepath.Join(workflowDir, "copilot.md")

	t.Run("rejects prompt-body false positive", func(t *testing.T) {
		content := "---\nengine: copilot\npermissions:\n  contents: read\n---\n\ncopilot-requests: write\n"
		if err := os.WriteFile(workflowPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write workflow: %v", err)
		}
		ok, err := profileSourcesUseActionsTokenCopilotAuth(context.Background(), []string{workflowPath})
		if err != nil {
			t.Fatalf("profileSourcesUseActionsTokenCopilotAuth returned error: %v", err)
		}
		if ok {
			t.Fatal("expected missing frontmatter permission to disable actions-token auth")
		}
	})

	t.Run("accepts explicit frontmatter permission", func(t *testing.T) {
		content := "---\nengine: copilot\npermissions:\n  copilot-requests: write\n---\n"
		if err := os.WriteFile(workflowPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write workflow: %v", err)
		}
		ok, err := profileSourcesUseActionsTokenCopilotAuth(context.Background(), []string{workflowPath})
		if err != nil {
			t.Fatalf("profileSourcesUseActionsTokenCopilotAuth returned error: %v", err)
		}
		if !ok {
			t.Fatal("expected explicit frontmatter permission to enable actions-token auth")
		}
	})
}

func TestRunBootstrapRequireOwnerType(t *testing.T) {
	originalCheckOwnerType := bootstrapCheckOwnerType
	t.Cleanup(func() {
		bootstrapCheckOwnerType = originalCheckOwnerType
	})

	bootstrapCheckOwnerType = func(context.Context, string) (string, error) { return "Organization", nil }
	if err := runBootstrapRequireOwnerType(context.Background(), "octo/platform-ops", repositoryPackageBootstrapAction{Value: "org"}); err != nil {
		t.Fatalf("expected matching owner type, got %v", err)
	}
	if err := runBootstrapRequireOwnerType(context.Background(), "octo/platform-ops", repositoryPackageBootstrapAction{Value: "user"}); err == nil {
		t.Fatal("expected mismatched owner type error")
	}
}

func TestResolveBootstrapTextValue_NonInteractivePaths(t *testing.T) {
	t.Run("uses env value", func(t *testing.T) {
		t.Setenv("BOOTSTRAP_TEXT", "choice-a")
		value, ok, err := resolveBootstrapTextValue("BOOTSTRAP_TEXT", "Title", "Description", "", []string{"choice-a", "choice-b"}, false)
		if err != nil {
			t.Fatalf("resolveBootstrapTextValue returned error: %v", err)
		}
		if !ok || value != "choice-a" {
			t.Fatalf("unexpected result: value=%q ok=%t", value, ok)
		}
	})

	t.Run("uses default value", func(t *testing.T) {
		value, ok, err := resolveBootstrapTextValue("BOOTSTRAP_TEXT_DEFAULT", "Title", "Description", "fallback", nil, false)
		if err != nil {
			t.Fatalf("resolveBootstrapTextValue returned error: %v", err)
		}
		if !ok || value != "fallback" {
			t.Fatalf("unexpected result: value=%q ok=%t", value, ok)
		}
	})

	t.Run("optional empty", func(t *testing.T) {
		value, ok, err := resolveBootstrapTextValue("BOOTSTRAP_TEXT_OPTIONAL", "Title", "Description", "", nil, true)
		if err != nil {
			t.Fatalf("resolveBootstrapTextValue returned error: %v", err)
		}
		if ok || value != "" {
			t.Fatalf("expected omitted optional value, got value=%q ok=%t", value, ok)
		}
	})

	t.Run("required missing", func(t *testing.T) {
		if _, _, err := resolveBootstrapTextValue("BOOTSTRAP_TEXT_REQUIRED", "Title", "Description", "", nil, false); err == nil {
			t.Fatal("expected missing required value error")
		}
	})
}

func TestResolveBootstrapSecretValue_NonInteractivePaths(t *testing.T) {
	t.Run("uses env value", func(t *testing.T) {
		t.Setenv("BOOTSTRAP_SECRET", "secret-value\n")
		value, ok, err := resolveBootstrapSecretValue("BOOTSTRAP_SECRET", "Secret", "Description", false)
		if err != nil {
			t.Fatalf("resolveBootstrapSecretValue returned error: %v", err)
		}
		if !ok || value != "secret-value" {
			t.Fatalf("unexpected result: value=%q ok=%t", value, ok)
		}
	})

	t.Run("optional empty", func(t *testing.T) {
		value, ok, err := resolveBootstrapSecretValue("BOOTSTRAP_SECRET_OPTIONAL", "Secret", "Description", true)
		if err != nil {
			t.Fatalf("resolveBootstrapSecretValue returned error: %v", err)
		}
		if ok || value != "" {
			t.Fatalf("expected omitted optional value, got value=%q ok=%t", value, ok)
		}
	})

	t.Run("required missing", func(t *testing.T) {
		if _, _, err := resolveBootstrapSecretValue("BOOTSTRAP_SECRET_REQUIRED", "Secret", "Description", false); err == nil {
			t.Fatal("expected missing required secret error")
		}
	})

	t.Run("interactive empty required input", func(t *testing.T) {
		value, ok, err := normalizeBootstrapPromptSecretValue("\n", false)
		if err == nil {
			t.Fatal("expected empty required secret error")
		}
		if ok || value != "" {
			t.Fatalf("expected empty secret result, got value=%q ok=%t", value, ok)
		}
	})
}

func TestBootstrapHelperUtilities(t *testing.T) {
	t.Parallel()
	if got := parseBootstrapNames([]byte("\nOMEGA\nALPHA\n\n")); !strings.EqualFold(strings.Join(got, ","), "ALPHA,OMEGA") {
		t.Fatalf("unexpected parsed names: %#v", got)
	}
	if err := validateBootstrapEnumValue("invalid", []string{"allowed"}, false); err == nil {
		t.Fatal("expected enum validation error")
	}
	if got := deriveBootstrapAppName("octo/platform-ops", ""); got != "octo-platform-ops" {
		t.Fatalf("unexpected app name: %q", got)
	}
	if got := deriveBootstrapAppName("octo/platform-ops", strings.Repeat("abc-", 20)); len(got) > 34 {
		t.Fatalf("expected truncated app name, got %q", got)
	}
	if got := firstNonEmpty("", "  ", "value", "other"); got != "value" {
		t.Fatalf("unexpected firstNonEmpty result: %q", got)
	}
	if got := firstNonEmpty("", "  value  ", "other"); got != "value" {
		t.Fatalf("unexpected trimmed firstNonEmpty result: %q", got)
	}
	if got := buildBootstrapGitHubAppRegistrationURL("octo", "Organization", "state"); !strings.Contains(got, "/organizations/octo/settings/apps/new?state=state") {
		t.Fatalf("unexpected org registration URL: %q", got)
	}
	if got := buildBootstrapGitHubAppRegistrationURL("octo", "User", "state"); got != "https://github.com/settings/apps/new?state=state" {
		t.Fatalf("unexpected user registration URL: %q", got)
	}
	if got := buildBootstrapGitHubAppInstallURL("agentic-ops"); got != "https://github.com/apps/agentic-ops/installations/new" {
		t.Fatalf("unexpected install URL: %q", got)
	}
	if got := buildBootstrapGitHubAppInstallURL("   "); got != "" {
		t.Fatalf("expected blank slug to produce empty URL, got %q", got)
	}
	if got, err := bootstrapRandomHex(8); err != nil || len(got) != 16 {
		t.Fatalf("unexpected random hex result: %q err=%v", got, err)
	}
	listener, err := netListener()
	if err != nil {
		t.Fatalf("netListener returned error: %v", err)
	}
	_ = listener.Close()
}

func TestBootstrapGitHubAppManifestHelpers(t *testing.T) {
	t.Parallel()
	manifest := buildBootstrapGitHubAppManifest(repositoryPackageBootstrapAction{}, "agentic-ops", "https://github.com/octo/platform-ops", "http://127.0.0.1/callback", "Bootstrap app")
	if manifest["name"] != "agentic-ops" {
		t.Fatalf("unexpected manifest name: %#v", manifest["name"])
	}
	registrationURL := "https://github.com/settings/apps/new?state=abc&foo=bar"
	page, err := renderBootstrapGitHubAppRegistrationPage(registrationURL, manifest)
	if err != nil {
		t.Fatalf("renderBootstrapGitHubAppRegistrationPage returned error: %v", err)
	}
	if !strings.Contains(page, "manifest-form") || !strings.Contains(page, "&#34;name&#34;") {
		t.Fatalf("unexpected registration page: %s", page)
	}
	if !strings.Contains(page, `action="https://github.com/settings/apps/new?state=abc&amp;foo=bar"`) {
		t.Fatalf("registration page did not contain escaped action URL: %s", page)
	}
}

func TestRenderBootstrapGitHubAppRegistrationPageEscapesUntrustedValues(t *testing.T) {
	t.Parallel()

	registrationURL := `<script>alert(1)</script>`
	manifest := map[string]any{"name": `<script>alert(1)</script>`}
	page, err := renderBootstrapGitHubAppRegistrationPage(registrationURL, manifest)
	if err != nil {
		t.Fatalf("renderBootstrapGitHubAppRegistrationPage returned error: %v", err)
	}
	if !strings.Contains(page, `action="%3cscript%3ealert%281%29%3c/script%3e"`) {
		t.Fatalf("registration URL was not escaped: %s", page)
	}
	if !strings.Contains(page, `\u003cscript\u003ealert(1)\u003c/script\u003e`) {
		t.Fatalf("manifest value was not safely encoded: %s", page)
	}
	if strings.Contains(page, `<script>alert(1)</script>`) {
		t.Fatalf("untrusted input was rendered as markup: %s", page)
	}
}
