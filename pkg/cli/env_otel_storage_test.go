//go:build !integration

package cli

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/nacl/box"
)

func TestEnvOTELOrganizationOverrides(t *testing.T) {
	endpoint := `{"name":"GH_AW_DEFAULT_OTLP_ENDPOINT"}`
	headers := `{"name":"GH_AW_DEFAULT_OTLP_HEADERS"}`
	for _, tc := range []struct {
		name        string
		orgSecrets  string
		repoSecrets string
		selectedIDs string
		failRead    bool
		errorText   string
	}{
		{name: "inherits complete organization defaults"},
		{name: "retains complete repository overrides", repoSecrets: endpoint + headers},
		{name: "rejects repository headers with variable endpoint", repoSecrets: headers, errorText: "one/repo has a partial OTLP secret override"},
		{name: "rejects repository endpoint without headers", repoSecrets: endpoint, errorText: "one/repo has a partial OTLP secret override"},
		{name: "rejects unreadable repository metadata", failRead: true, errorText: "one/repo secret access"},
		{name: "rejects organization visibility change", orgSecrets: `{"name":"GH_AW_DEFAULT_OTLP_ENDPOINT","visibility":"private"}`, errorText: "visibility differs"},
		{name: "rejects selected repository change", orgSecrets: `{"name":"GH_AW_DEFAULT_OTLP_HEADERS","visibility":"selected"}`, selectedIDs: "456", errorText: "visibility differs"},
		{name: "retains selected repositories", orgSecrets: `{"name":"GH_AW_DEFAULT_OTLP_HEADERS","visibility":"selected"}`, selectedIDs: "123"},
		{name: "rejects malformed secret metadata", repoSecrets: "not JSON", errorText: "invalid GitHub OTLP secret metadata"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			envOTELMockGH(t, func(args []string, input []byte) ([]byte, error) {
				require.Empty(t, input, "preflight must not write settings")
				line := strings.Join(args, " ")
				switch {
				case strings.Contains(line, "/repositories?"):
					return []byte(tc.selectedIDs), nil
				case strings.Contains(line, "orgs/one/actions/secrets?"):
					return []byte(tc.orgSecrets), nil
				case strings.Contains(line, "repos/one/repo/actions/secrets?"):
					if tc.failRead {
						return nil, errors.New("inaccessible")
					}
					return []byte(tc.repoSecrets), nil
				default:
					t.Fatalf("unexpected API request: %s", line)
					return nil, nil
				}
			})
			err := envOTELCheckOverrides(t.Context(), envOTELPlan{
				target: defaultsTarget{scope: defaultsScopeOrg, org: "one", visibility: defaultsVisibilitySel},
				ids:    []int64{123},
			}, []string{"one/repo"})
			if tc.errorText != "" {
				require.ErrorContains(t, err, tc.errorText)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestEnvOTELRepositoryInventory(t *testing.T) {
	repositories := `{"full_name":"one/public","private":false}
{"full_name":"one/private","private":true}`
	for _, tc := range []struct {
		name       string
		visibility string
		data       string
		public     int
		private    int
		want       []string
		bad        bool
	}{
		{"all", "all", repositories, 1, 1, []string{"one/public", "one/private"}, false},
		{"private", "private", repositories, 1, 1, []string{"one/private"}, false},
		{"missing hidden private repository", "private", repositories, 1, 2, nil, true},
		{"missing public repository", "all", repositories, 2, 1, nil, true},
		{"malformed inventory", "all", "not JSON", 0, 0, nil, true},
		{"missing visibility", "all", `{"full_name":"one/repo"}`, 0, 0, nil, true},
		{"duplicate page entry", "all", repositories + repositories, 2, 2, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			names, err := envOTELRepositoryNames([]byte(tc.data), tc.visibility, tc.public, tc.private)
			if tc.bad {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.want, names)
			}
		})
	}

	t.Run("requires authoritative organization counts", func(t *testing.T) {
		envOTELMockGH(t, func(_ []string, input []byte) ([]byte, error) {
			require.Empty(t, input)
			return []byte(`{"public_repos":1}`), nil
		})
		_, err := envOTELRepositories(t.Context(), defaultsTarget{scope: defaultsScopeOrg, org: "one", visibility: defaultsVisibilityAll}, nil)
		require.ErrorContains(t, err, "cannot verify")
	})
}

func TestEnvOTELPreflightRejectsOverridesBeforeWrites(t *testing.T) {
	public, _, err := box.GenerateKey(rand.Reader)
	require.NoError(t, err)
	key, err := json.Marshal(repoPublicKey{ID: "1", Key: base64.StdEncoding.EncodeToString(public[:])})
	require.NoError(t, err)
	envOTELMockGH(t, func(args []string, input []byte) ([]byte, error) {
		require.Empty(t, input, "every target must pass preflight before any write")
		line := strings.Join(args, " ")
		switch {
		case strings.Contains(line, "public-key"):
			return key, nil
		case strings.Contains(line, "--jq .total_count"):
			return []byte("0"), nil
		case strings.Contains(line, "repos/one/repo --jq .id"):
			return []byte("123"), nil
		case strings.Contains(line, "orgs/one/actions/secrets?"):
			return nil, nil
		case strings.Contains(line, "repos/one/repo/actions/secrets?"):
			return []byte(`{"name":"GH_AW_DEFAULT_OTLP_HEADERS"}`), nil
		default:
			t.Fatalf("unexpected API request: %s", line)
			return nil, nil
		}
	})
	_, err = envOTELPrepare(t.Context(), []defaultsTarget{
		{scope: defaultsScopeRepo, repoOwner: "other", repoName: "repo"},
		{scope: defaultsScopeOrg, org: "one", visibility: defaultsVisibilitySel},
	}, map[string][]string{"one": {"one/repo"}})
	require.ErrorContains(t, err, "preflight one")
	require.ErrorContains(t, err, "one/repo has a partial OTLP secret override")
}
