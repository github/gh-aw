//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompileWorkflow_IncludesObservabilitySummaryStepWhenOTLPEnabled(t *testing.T) {
	tmpDir := t.TempDir()
	workflowPath := filepath.Join(tmpDir, "observability-summary.md")
	content := `---
on: push
permissions:
  contents: read
observability:
  otlp:
    endpoint: https://traces.example.com:4317
engine: copilot
---

# Test Observability Summary with OTLP
`

	if err := os.WriteFile(workflowPath, []byte(content), 0o644); err != nil {
		t.Fatalf("Failed to write workflow: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Unexpected compile error: %v", err)
	}

	lockPath := filepath.Join(tmpDir, "observability-summary.lock.yml")
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	compiled := string(lockContent)
	if !strings.Contains(compiled, "- name: Generate observability summary") {
		t.Fatal("Expected observability summary step to be generated when OTLP is enabled")
	}
	if !strings.Contains(compiled, "require(path.join(actionsDir, 'generate_observability_summary.cjs'))") {
		t.Fatal("Expected generated workflow to load generate_observability_summary.cjs")
	}
}

func TestCompileWorkflow_IncludesObservabilitySummaryStepWithEnterpriseDefaultOTLP(t *testing.T) {
	tmpDir := t.TempDir()
	workflowPath := filepath.Join(tmpDir, "no-observability-summary.md")
	content := `---
on: push
permissions:
  contents: read
engine: copilot
---

# Test No Observability Summary
`

	if err := os.WriteFile(workflowPath, []byte(content), 0o644); err != nil {
		t.Fatalf("Failed to write workflow: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Unexpected compile error: %v", err)
	}

	lockPath := filepath.Join(tmpDir, "no-observability-summary.lock.yml")
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	compiled := string(lockContent)
	// Without observability frontmatter the compiler falls back to the enterprise
	// default OTLP environment, so the observability summary step is still emitted.
	if !strings.Contains(compiled, "- name: Generate observability summary") {
		t.Fatal("Expected observability summary step when the enterprise default OTLP endpoint is used")
	}
	if !strings.Contains(compiled, "OTEL_EXPORTER_OTLP_ENDPOINT: ${{ secrets.GH_AW_DEFAULT_OTLP_ENDPOINT || vars.GH_AW_DEFAULT_OTLP_ENDPOINT }}") {
		t.Fatal("Expected the enterprise default OTLP endpoint secret and variable fallback in the compiled workflow")
	}
}

func TestCompileWorkflow_IncludesObservabilitySummaryStepWhenOTLPEnabledViaImport(t *testing.T) {
	tmpDir := t.TempDir()

	// Create an imported workflow with OTLP configured
	importedPath := filepath.Join(tmpDir, "shared-otlp.md")
	importedContent := `---
observability:
  otlp:
    endpoint: https://traces.example.com:4317
---
`
	if err := os.WriteFile(importedPath, []byte(importedContent), 0o644); err != nil {
		t.Fatalf("Failed to write imported workflow: %v", err)
	}

	// Main workflow imports the shared OTLP config but has no observability section itself
	workflowPath := filepath.Join(tmpDir, "main-import-otlp.md")
	content := `---
on: push
permissions:
  contents: read
engine: copilot
imports:
  - ./shared-otlp.md
---

# Test Observability Summary via Import
`
	if err := os.WriteFile(workflowPath, []byte(content), 0o644); err != nil {
		t.Fatalf("Failed to write main workflow: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Unexpected compile error: %v", err)
	}

	lockPath := filepath.Join(tmpDir, "main-import-otlp.lock.yml")
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	compiled := string(lockContent)
	if !strings.Contains(compiled, "- name: Generate observability summary") {
		t.Fatal("Expected observability summary step when OTLP is enabled via import")
	}
	if !strings.Contains(compiled, "OTEL_EXPORTER_OTLP_ENDPOINT") {
		t.Fatal("Expected OTEL_EXPORTER_OTLP_ENDPOINT env var to be injected when OTLP is configured via import")
	}
}

// TestCompileWorkflow_MasksOTLPHeadersWhenConfigured verifies that the compiled
// workflow includes a masking step that calls mask_otlp_headers.sh in all
// relevant jobs when headers are configured.
func TestCompileWorkflow_MasksOTLPHeadersWhenConfigured(t *testing.T) {
	tmpDir := t.TempDir()
	workflowPath := filepath.Join(tmpDir, "otlp-with-headers.md")
	content := `---
on: push
permissions:
  contents: read
observability:
  otlp:
    endpoint: https://traces.example.com:4317
    headers: "Authorization=Bearer supersecrettoken"
engine: copilot
---

# Test OTLP Headers Masking
`

	if err := os.WriteFile(workflowPath, []byte(content), 0o644); err != nil {
		t.Fatalf("Failed to write workflow: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Unexpected compile error: %v", err)
	}

	lockPath := filepath.Join(tmpDir, "otlp-with-headers.lock.yml")
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	compiled := string(lockContent)

	// The masking step must appear in the compiled YAML and delegate to the .sh script.
	if !strings.Contains(compiled, "- name: Mask OTLP telemetry headers") {
		t.Fatal("Expected OTLP headers masking step to be generated when headers are configured")
	}
	if !strings.Contains(compiled, "mask_otlp_headers.sh") {
		t.Fatal("Expected masking step to delegate to mask_otlp_headers.sh")
	}

	// The masking step must appear in both the activation job and the agent job.
	// Count occurrences: each job that runs has its own instance of the masking step.
	maskCount := strings.Count(compiled, "- name: Mask OTLP telemetry headers")
	if maskCount < 2 {
		t.Fatalf("Expected masking step in at least 2 jobs (activation + agent), found %d", maskCount)
	}
}

// TestCompileWorkflow_DoesNotMaskOTLPHeadersWhenNotConfigured verifies that no
// masking step is emitted when OTLP headers are not configured.
func TestCompileWorkflow_DoesNotMaskOTLPHeadersWhenNotConfigured(t *testing.T) {
	tmpDir := t.TempDir()
	workflowPath := filepath.Join(tmpDir, "otlp-no-headers.md")
	content := `---
on: push
permissions:
  contents: read
observability:
  otlp:
    endpoint: https://traces.example.com:4317
engine: copilot
---

# Test No OTLP Headers Masking
`

	if err := os.WriteFile(workflowPath, []byte(content), 0o644); err != nil {
		t.Fatalf("Failed to write workflow: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Unexpected compile error: %v", err)
	}

	lockPath := filepath.Join(tmpDir, "otlp-no-headers.lock.yml")
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	compiled := string(lockContent)
	if strings.Contains(compiled, "- name: Mask OTLP telemetry headers") {
		t.Fatal("Did not expect OTLP headers masking step when headers are not configured")
	}
	if strings.Contains(compiled, "Mask OTLP") {
		t.Fatal("Did not expect any OTLP masking when headers are not configured")
	}
}

// TestCompileWorkflow_MasksOTLPHeadersBeforeCheckout verifies that the masking
// step appears before the checkout step in the agent job, so the header value is
// masked as early as possible.
func TestCompileWorkflow_MasksOTLPHeadersBeforeCheckout(t *testing.T) {
	tmpDir := t.TempDir()
	workflowPath := filepath.Join(tmpDir, "otlp-mask-order.md")
	content := `---
on: push
permissions:
  contents: read
observability:
  otlp:
    endpoint: https://traces.example.com:4317
    headers: "Authorization=Bearer supersecrettoken"
engine: copilot
---

# Test OTLP Headers Masking Order
`

	if err := os.WriteFile(workflowPath, []byte(content), 0o644); err != nil {
		t.Fatalf("Failed to write workflow: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Unexpected compile error: %v", err)
	}

	lockPath := filepath.Join(tmpDir, "otlp-mask-order.lock.yml")
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	compiled := string(lockContent)

	if !strings.Contains(compiled, "- name: Mask OTLP telemetry headers") {
		t.Fatal("Expected OTLP headers masking step")
	}

	// Find the first checkout step in the agent job section (after the activation job)
	agentJobIdx := strings.Index(compiled, "agent:")
	if agentJobIdx < 0 {
		t.Fatal("Expected agent job section")
	}

	checkoutIdxInAgent := strings.Index(compiled[agentJobIdx:], "- name: Checkout repository")
	if checkoutIdxInAgent < 0 {
		t.Skip("No checkout step found in agent job, skipping order check")
	}
	checkoutIdx := agentJobIdx + checkoutIdxInAgent

	// Find the mask step in the agent job section
	maskIdxInAgent := strings.Index(compiled[agentJobIdx:], "- name: Mask OTLP telemetry headers")
	if maskIdxInAgent < 0 {
		t.Fatal("Expected OTLP headers masking step in agent job")
	}
	maskAbsIdx := agentJobIdx + maskIdxInAgent

	if maskAbsIdx >= checkoutIdx {
		t.Fatal("OTLP headers masking step should appear before checkout step in agent job")
	}
}

// TestCompileWorkflow_MultipleImportsOTLPEndpointsMerged is a regression test for the scenario
// where multiple shared workflows each define their own observability.otlp.endpoint.
// All endpoints must appear in the compiled GH_AW_OTLP_ENDPOINTS JSON array (fan-out), and
// the first endpoint (in import order) must be the primary OTEL_EXPORTER_OTLP_ENDPOINT.
// Main-workflow endpoints take precedence over import endpoints for duplicate-URL deduplication.
func TestCompileWorkflow_MultipleImportsOTLPEndpointsMerged(t *testing.T) {
	tmpDir := t.TempDir()

	// Import 1: defines a single endpoint via string notation
	import1Path := filepath.Join(tmpDir, "shared-vendor.md")
	import1Content := `---
observability:
  otlp:
    endpoint: https://vendor.example.com:4317
---
`
	if err := os.WriteFile(import1Path, []byte(import1Content), 0o644); err != nil {
		t.Fatalf("Failed to write import1 file: %v", err)
	}

	// Import 2: defines a single endpoint via object notation with headers
	import2Path := filepath.Join(tmpDir, "shared-internal.md")
	import2Content := `---
observability:
  otlp:
    endpoint:
      url: https://internal.corp:4317
      headers:
        X-Token: ${{ secrets.INTERNAL_TOKEN }}
---
`
	if err := os.WriteFile(import2Path, []byte(import2Content), 0o644); err != nil {
		t.Fatalf("Failed to write import2 file: %v", err)
	}

	// Import 3: defines two endpoints via array notation
	import3Path := filepath.Join(tmpDir, "shared-multi.md")
	import3Content := `---
observability:
  otlp:
    endpoint:
      - url: https://primary.example.com:4317
      - url: https://vendor.example.com:4317
---
`
	if err := os.WriteFile(import3Path, []byte(import3Content), 0o644); err != nil {
		t.Fatalf("Failed to write import3 file: %v", err)
	}

	// Main workflow: imports all three, also defines its own endpoint (takes precedence on dedup)
	workflowPath := filepath.Join(tmpDir, "main.md")
	workflowContent := `---
on: push
permissions:
  contents: read
engine: copilot
observability:
  otlp:
    endpoint: https://main.example.com:4317
imports:
  - ./shared-vendor.md
  - ./shared-internal.md
  - ./shared-multi.md
---

# Multi-import OTLP merge test
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0o644); err != nil {
		t.Fatalf("Failed to write main workflow: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("CompileWorkflow failed: %v", err)
	}

	lockPath := filepath.Join(tmpDir, "main.lock.yml")
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	compiled := string(lockContent)

	// Primary OTLP endpoint = main workflow's endpoint (takes precedence)
	if !strings.Contains(compiled, "OTEL_EXPORTER_OTLP_ENDPOINT: https://main.example.com:4317") {
		t.Error("Expected main workflow endpoint to be the primary OTEL_EXPORTER_OTLP_ENDPOINT")
	}

	// GH_AW_OTLP_ENDPOINTS must contain all distinct endpoints
	if !strings.Contains(compiled, "GH_AW_OTLP_ENDPOINTS:") {
		t.Error("Expected GH_AW_OTLP_ENDPOINTS env var to be injected for multi-endpoint setup")
	}
	if !strings.Contains(compiled, "main.example.com") {
		t.Error("Expected main.example.com in GH_AW_OTLP_ENDPOINTS")
	}
	if !strings.Contains(compiled, "vendor.example.com") {
		t.Error("Expected vendor.example.com in GH_AW_OTLP_ENDPOINTS (import 1)")
	}
	if !strings.Contains(compiled, "internal.corp") {
		t.Error("Expected internal.corp in GH_AW_OTLP_ENDPOINTS (import 2)")
	}
	if !strings.Contains(compiled, "primary.example.com") {
		t.Error("Expected primary.example.com in GH_AW_OTLP_ENDPOINTS (import 3 array)")
	}

	// vendor.example.com appears in import 1 (string) and import 3 (array) but must be
	// deduplicated within the GH_AW_OTLP_ENDPOINTS JSON array.
	if strings.Contains(compiled, "GH_AW_OTLP_ENDPOINTS:") {
		// Extract the single-quoted JSON array value to count URL occurrences within it.
		if _, rest, found := strings.Cut(compiled, "GH_AW_OTLP_ENDPOINTS: '"); found {
			endpointsJSON, _, _ := strings.Cut(rest, "'\n")
			vendorInEndpoints := strings.Count(endpointsJSON, "vendor.example.com")
			if vendorInEndpoints > 1 {
				t.Errorf("vendor.example.com appears %d times in GH_AW_OTLP_ENDPOINTS — expected deduplication to produce only 1 entry", vendorInEndpoints)
			}
		}
	}

	// Headers from import 2 must be present for masking
	if !strings.Contains(compiled, "INTERNAL_TOKEN") {
		t.Error("Expected X-Token/INTERNAL_TOKEN header from import 2 to appear in compiled output")
	}
}

// TestCompileWorkflow_TwoImportsWithOTLPNoMainEndpoint verifies that when the main workflow
// has no observability section but two imports each define distinct endpoints, both endpoints
// are merged into GH_AW_OTLP_ENDPOINTS and the first import's endpoint is the primary one.
func TestCompileWorkflow_TwoImportsWithOTLPNoMainEndpoint(t *testing.T) {
	tmpDir := t.TempDir()

	import1Path := filepath.Join(tmpDir, "shared-a.md")
	if err := os.WriteFile(import1Path, []byte(`---
observability:
  otlp:
    endpoint: https://a.example.com:4317
---
`), 0o644); err != nil {
		t.Fatalf("Failed to write import1: %v", err)
	}

	import2Path := filepath.Join(tmpDir, "shared-b.md")
	if err := os.WriteFile(import2Path, []byte(`---
observability:
  otlp:
    endpoint: https://b.example.com:4317
---
`), 0o644); err != nil {
		t.Fatalf("Failed to write import2: %v", err)
	}

	workflowPath := filepath.Join(tmpDir, "no-main-obs.md")
	if err := os.WriteFile(workflowPath, []byte(`---
on: push
permissions:
  contents: read
engine: copilot
imports:
  - ./shared-a.md
  - ./shared-b.md
---

# Two imports, no main-workflow observability
`), 0o644); err != nil {
		t.Fatalf("Failed to write main workflow: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("CompileWorkflow failed: %v", err)
	}

	lockPath := filepath.Join(tmpDir, "no-main-obs.lock.yml")
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	compiled := string(lockContent)

	// Primary OTLP endpoint = first import's endpoint
	if !strings.Contains(compiled, "OTEL_EXPORTER_OTLP_ENDPOINT: https://a.example.com:4317") {
		t.Error("Expected first import's endpoint to be the primary OTEL_EXPORTER_OTLP_ENDPOINT")
	}

	// GH_AW_OTLP_ENDPOINTS must contain both endpoints
	if !strings.Contains(compiled, "GH_AW_OTLP_ENDPOINTS:") {
		t.Error("Expected GH_AW_OTLP_ENDPOINTS env var with multiple endpoints")
	}
	if !strings.Contains(compiled, "a.example.com") {
		t.Error("Expected a.example.com in GH_AW_OTLP_ENDPOINTS")
	}
	if !strings.Contains(compiled, "b.example.com") {
		t.Error("Expected b.example.com in GH_AW_OTLP_ENDPOINTS")
	}
}

// TestCompileWorkflow_DuplicateOTLPEndpointAcrossImportsDeduped verifies that when
// two imports define the same URL, it only appears once in the final endpoint list.
func TestCompileWorkflow_DuplicateOTLPEndpointAcrossImportsDeduped(t *testing.T) {
	tmpDir := t.TempDir()

	sameURL := "https://shared-collector.example.com:4317"

	import1Path := filepath.Join(tmpDir, "shared-x.md")
	if err := os.WriteFile(import1Path, []byte("---\nobservability:\n  otlp:\n    endpoint: "+sameURL+"\n---\n"), 0o644); err != nil {
		t.Fatalf("Failed to write import1: %v", err)
	}
	import2Path := filepath.Join(tmpDir, "shared-y.md")
	if err := os.WriteFile(import2Path, []byte("---\nobservability:\n  otlp:\n    endpoint: "+sameURL+"\n---\n"), 0o644); err != nil {
		t.Fatalf("Failed to write import2: %v", err)
	}

	workflowPath := filepath.Join(tmpDir, "dedup-test.md")
	if err := os.WriteFile(workflowPath, []byte(`---
on: push
permissions:
  contents: read
engine: copilot
imports:
  - ./shared-x.md
  - ./shared-y.md
---
# Deduplication test
`), 0o644); err != nil {
		t.Fatalf("Failed to write main workflow: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("CompileWorkflow failed: %v", err)
	}

	lockPath := filepath.Join(tmpDir, "dedup-test.lock.yml")
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	compiled := string(lockContent)

	// The URL must appear, but only once in the OTLP endpoints JSON (deduplication).
	// It may appear multiple times due to YAML comments or network permissions,
	// but GH_AW_OTLP_ENDPOINTS should have only one entry — so the URL count within
	// single-quoted section should be 1.
	if !strings.Contains(compiled, "shared-collector.example.com") {
		t.Fatal("Expected the collector URL to appear at all in compiled output")
	}

	// Single endpoint: GH_AW_OTLP_ENDPOINTS should NOT be injected (only injected for multi-endpoint)
	// OR it is injected with exactly one entry. Either way, the URL count in the
	// GH_AW_OTLP_ENDPOINTS value must be 1 (deduplication worked).
	if strings.Contains(compiled, "GH_AW_OTLP_ENDPOINTS:") {
		// Count occurrences of the URL in the single-quoted endpoints JSON
		if _, rest, found := strings.Cut(compiled, "GH_AW_OTLP_ENDPOINTS: '"); found {
			endpointsJSON, _, _ := strings.Cut(rest, "'\n")
			count := strings.Count(endpointsJSON, "shared-collector.example.com")
			if count > 1 {
				t.Errorf("URL appears %d times in GH_AW_OTLP_ENDPOINTS — expected deduplication to produce only 1 entry", count)
			}
		}
	}
}
