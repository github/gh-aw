//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNetworkIsolationRootless verifies that the default docker runtime profile
// compiles to a lock.yml with no "sudo" for the AWF binary install or the AWF
// invocation (rootless mode), while docker-sudo-iptables still uses sudo.
func TestNetworkIsolationRootless(t *testing.T) {
	t.Run("default runtime omits sudo from awf invocation and install", func(t *testing.T) {
		workflowsDir := t.TempDir()

		markdown := `---
on:
  workflow_dispatch:
engine: copilot
strict: false
network:
  allowed:
    - github.com
sandbox:
  agent:
    id: awf
---

# Test Network Isolation Rootless

This workflow verifies that sudo is omitted for the default docker runtime profile.
`

		workflowPath := filepath.Join(workflowsDir, "test-network-isolation.md")
		if err := os.WriteFile(workflowPath, []byte(markdown), 0644); err != nil {
			t.Fatalf("Failed to write workflow file: %v", err)
		}

		compiler := NewCompiler()
		if err := compiler.CompileWorkflow(workflowPath); err != nil {
			t.Fatalf("Compilation failed: %v", err)
		}

		lockPath := filepath.Join(workflowsDir, "test-network-isolation.lock.yml")
		lockContent, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("Failed to read compiled workflow: %v", err)
		}
		lockStr := string(lockContent)

		// AWF invocation must not use sudo
		if strings.Contains(lockStr, "sudo -E ") {
			t.Error("Expected no sudo in lock file for the default docker runtime profile")
		}

		// AWF must still be invoked (just without sudo).
		if !strings.Contains(lockStr, "awf --config ") {
			t.Error("Expected rootless 'awf --config' invocation in lock file main execution step")
		}

		// Install step must pass --rootless flag
		if !strings.Contains(lockStr, "install_awf_binary.sh") {
			t.Error("Expected install_awf_binary.sh in lock file")
		}
		if !strings.Contains(lockStr, "--rootless") {
			t.Error("Expected '--rootless' flag in install step when sudo is false (network isolation mode)")
		}

		// The sudo chmod permission-fix step should be absent
		if strings.Contains(lockStr, "sudo chmod -R a+rX") {
			t.Error("Expected no 'sudo chmod -R a+rX' permission-fix step when sudo is false (network isolation mode)")
		}
	})

	t.Run("workflow with sudo omitted defaults to network isolation", func(t *testing.T) {
		workflowsDir := t.TempDir()

		markdown := `---
on:
  workflow_dispatch:
engine: copilot
strict: false
network:
  allowed:
    - github.com
sandbox:
  agent:
    id: awf
---

# Test Default Network Isolation

This workflow verifies that sudo is omitted by default when sudo is not set (network isolation is the new default).
`

		workflowPath := filepath.Join(workflowsDir, "test-default-network-isolation.md")
		if err := os.WriteFile(workflowPath, []byte(markdown), 0644); err != nil {
			t.Fatalf("Failed to write workflow file: %v", err)
		}

		compiler := NewCompiler()
		if err := compiler.CompileWorkflow(workflowPath); err != nil {
			t.Fatalf("Compilation failed: %v", err)
		}

		lockPath := filepath.Join(workflowsDir, "test-default-network-isolation.lock.yml")
		lockContent, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("Failed to read compiled workflow: %v", err)
		}
		lockStr := string(lockContent)

		// Default (sudo not set) must use network isolation mode.
		if strings.Contains(lockStr, "sudo -E ") {
			t.Error("Expected no sudo in lock file when sudo is not set (network isolation is the default)")
		}

		// AWF must still be invoked (without sudo).
		if !strings.Contains(lockStr, "awf --config ") {
			t.Error("Expected rootless 'awf --config' invocation in lock file main execution step")
		}

		// Install step must pass --rootless flag
		if !strings.Contains(lockStr, "install_awf_binary.sh") {
			t.Error("Expected install_awf_binary.sh in lock file")
		}
		if !strings.Contains(lockStr, "--rootless") {
			t.Error("Expected '--rootless' flag in install step when sudo is not set (network isolation is the default)")
		}

		// sudo chmod permission-fix step should be absent
		if strings.Contains(lockStr, "sudo chmod -R a+rX") {
			t.Error("Expected no 'sudo chmod -R a+rX' permission-fix step when sudo is not set (network isolation is the default)")
		}
	})
}

// TestLegacySecurityInstallNonRootless verifies that runtime: docker-sudo-iptables
// compiles to a lock.yml that installs awf without --rootless (to /usr/local/bin, which is
// on sudo's secure_path) and invokes it with sudo.
func TestLegacySecurityInstallNonRootless(t *testing.T) {
	workflowsDir := t.TempDir()

	markdown := `---
on:
  workflow_dispatch:
engine: copilot
strict: false
network:
  allowed:
    - github.com
sandbox:
  agent:
    id: awf
    runtime: docker-sudo-iptables
---

# Test Legacy Security Non-Rootless Install

This workflow verifies that docker-sudo-iptables installs awf without --rootless.
`

	workflowPath := filepath.Join(workflowsDir, "test-legacy-security.md")
	if err := os.WriteFile(workflowPath, []byte(markdown), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Compilation failed: %v", err)
	}

	lockPath := filepath.Join(workflowsDir, "test-legacy-security.lock.yml")
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read compiled workflow: %v", err)
	}
	lockStr := string(lockContent)

	// Install step must NOT pass --rootless: awf must land in /usr/local/bin so that
	// the subsequent privileged invocation can find it on sudo's secure_path.
	if strings.Contains(lockStr, "--rootless") {
		t.Error("Expected no '--rootless' flag in install step for runtime: docker-sudo-iptables")
	}

	// Install step must be present.
	if !strings.Contains(lockStr, "install_awf_binary.sh") {
		t.Error("Expected install_awf_binary.sh in lock file for runtime: docker-sudo-iptables")
	}

	// AWF invocation must restore the runner PATH after sudo applies secure_path.
	if !strings.Contains(lockStr, `sudo -E /usr/bin/env PATH="$PATH" /usr/local/bin/awf`) {
		t.Error("Expected PATH-preserving sudo invocation in lock file for runtime: docker-sudo-iptables")
	}
}
