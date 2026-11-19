package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAuthorizationCommandPolicyDigestDisclosure(t *testing.T) {
	t.Setenv("GH_AW_WORKFLOWS_DIR", "")
	for _, test := range []struct {
		name        string
		policy      string
		pin         string
		wantDigest  bool
		wantFinding string
	}{
		{
			name: "invalid unpinned file", policy: "private non-policy file contents",
			wantFinding: "policy_invalid",
		},
		{
			name: "invalid file with matching pin", policy: "private non-policy file contents",
			pin: "matching", wantFinding: "policy_invalid",
		},
		{
			name: "invalid schema", policy: strings.Replace(authorizationPolicyFixture, `"version": 1`, `"version": 2`, 1),
			wantFinding: "policy_invalid",
		},
		{
			name: "valid policy with mismatched pin", policy: authorizationPolicyFixture,
			pin: strings.Repeat("0", 64), wantFinding: "policy_invalid",
		},
		{
			name: "valid unpinned policy", policy: authorizationPolicyFixture,
			wantDigest: true, wantFinding: "verification_incomplete",
		},
		{
			name: "valid policy with matching pin", policy: authorizationPolicyFixture,
			pin: "matching", wantDigest: true, wantFinding: "verification_incomplete",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			policyPath := filepath.Join(t.TempDir(), "policy.json")
			writeAuthorizationTestFile(t, policyPath, test.policy)
			digest := authorizationDigest([]byte(test.policy))
			args := []string{"--authorization-policy", policyPath, "--json"}
			if test.pin != "" {
				pin := test.pin
				if pin == "matching" {
					pin = digest
				}
				args = append(args, "--authorization-policy-sha256", pin)
			}
			var output bytes.Buffer
			cmd := NewValidateCommand(func(string) error {
				t.Fatal("ordinary validation ran")
				return nil
			})
			cmd.SetArgs(args)
			cmd.SetOut(&output)
			cmd.SetErr(&bytes.Buffer{})
			require.Error(t, cmd.Execute())
			var result AuthorizationPolicyResult
			require.NoError(t, json.Unmarshal(output.Bytes(), &result))
			require.True(t, authorizationHasFinding(result, test.wantFinding))
			if test.wantDigest {
				require.Equal(t, digest, result.PolicyDigest)
			} else {
				require.Empty(t, result.PolicyDigest)
				require.NotContains(t, output.String(), strings.TrimPrefix(digest, "sha256:"))
			}
		})
	}
}
