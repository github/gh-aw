package conformance

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const minimalLock = `
jobs:
  activation: {}
  agent:
    needs: activation
    permissions: {contents: read, copilot-requests: write}
    steps:
      - uses: actions/checkout@0000000000000000000000000000000000000000
        with: {persist-credentials: false}
      - uses: actions/upload-artifact@0000000000000000000000000000000000000000
        with: {name: agent}
  detection:
    needs: agent
  safe_outputs:
    needs: [agent, detection]
    if: needs.detection.result == 'success'
    steps:
      - uses: actions/download-artifact@0000000000000000000000000000000000000000
        with: {name: agent}
`

func TestCompiledProfileMutations(t *testing.T) {
	tests := []struct {
		name, before, after, invariant string
	}{
		{"secure", "", "", ""},
		{"write", "contents: read", "contents: write", "JobIsolation"},
		{"agent-gate", "needs: activation", "needs: activation\n    if: always()", "JobIsolation"},
		{"persistence", "persist-credentials: false", "persist-credentials: true", "NoCredentialPersistence"},
		{"detection", "needs.detection.result == 'success'", "always()", "DetectionGate"},
		{"bypass", "needs.detection.result == 'success'", "needs.detection.result == 'success' || always()", "DetectionGate"},
		{"origin", "with: {name: agent}", "with: {name: agent, run-id: 7}", "ArtifactProvenance"},
		{"pinning", "actions/checkout@0000000000000000000000000000000000000000", "actions/checkout@main", "TrustedConfiguration"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := minimalLock
			if tc.before != "" {
				data = strings.ReplaceAll(data, tc.before, tc.after)
			}
			var doc document
			if err := yaml.Unmarshal([]byte(data), &doc); err != nil {
				t.Fatal(err)
			}
			got := strings.Join(verify(doc, "daily"), "\n")
			if tc.invariant == "" && got != "" {
				t.Fatalf("secure profile rejected: %s", got)
			}
			if tc.invariant != "" && !strings.Contains(got, tc.invariant) {
				t.Fatalf("expected %s, got %s", tc.invariant, got)
			}
		})
	}
}

func TestDockerActionPinning(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name, uses string
		pinned     bool
	}{
		{"digest", "docker://ghcr.io/example/image:v1@sha256:" + digest, true},
		{"digest without tag", "docker://ghcr.io/example/image@sha256:" + digest, true},
		{"tag", "docker://ghcr.io/example/image:v1", false},
		{"short digest", "docker://ghcr.io/example/image@sha256:" + digest[:63], false},
		{"nonhex digest", "docker://ghcr.io/example/image@sha256:" + strings.Repeat("g", 64), false},
		{"git sha", "docker://ghcr.io/example/image@" + strings.Repeat("a", 40), false},
		{"expression", "docker://${{ inputs.image }}@sha256:" + digest, false},
		{"not docker", "ghcr.io/example/image@sha256:" + digest, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := strings.Replace(minimalLock,
				"actions/upload-artifact@0000000000000000000000000000000000000000", tc.uses, 1)
			var doc document
			if err := yaml.Unmarshal([]byte(data), &doc); err != nil {
				t.Fatal(err)
			}
			got := strings.Join(verify(doc, "daily"), "\n")
			if strings.Contains(got, "TrustedConfiguration") == tc.pinned {
				t.Fatalf("pinned=%t, violations: %s", tc.pinned, got)
			}
		})
	}
}

func TestSeedCredentialMutations(t *testing.T) {
	const base = `
jobs:
  agent:
    steps:
      - uses: actions/checkout@0000000000000000000000000000000000000000
        with:
          repository: example/private-dependency
          token: ${{ secrets.MODEL_DEPENDENCY_READ_TOKEN }}
  safe_outputs:
    steps:
      - uses: actions/create-github-app-token@0000000000000000000000000000000000000000
        with:
          repositories: gh-aw
          permission-issues: write
`
	tests := []struct {
		name, before, after, invariant string
	}{
		{"secure", "", "", ""},
		{"routing", "MODEL_DEPENDENCY_READ_TOKEN", "WRONG_TOKEN", "GitAuthorization"},
		{"scope", "repositories: gh-aw", `repositories: "*"`, "AppLeastPrivilege"},
		{"permissions", "permission-issues: write", "permission-issues: write\n          permission-contents: write", "AppLeastPrivilege"},
		{"revocation", "permission-issues: write", "permission-issues: write\n          skip-token-revoke: true", "TokenLifetime"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := base
			if tc.before != "" {
				data = strings.ReplaceAll(data, tc.before, tc.after)
			}
			var doc document
			if err := yaml.Unmarshal([]byte(data), &doc); err != nil {
				t.Fatal(err)
			}
			got := strings.Join(checkSeed(doc), "\n")
			if tc.invariant == "" && got != "" {
				t.Fatalf("secure seed rejected: %s", got)
			}
			if tc.invariant != "" && !strings.Contains(got, tc.invariant) {
				t.Fatalf("expected %s, got %s", tc.invariant, got)
			}
		})
	}
}
