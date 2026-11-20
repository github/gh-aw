//go:build !integration

package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/nacl/box"
)

// A subprocess allows verification of stdin wiring without storing credentials in argv.
func TestEnvOTELGHHelper(t *testing.T) {
	if os.Getenv("GH_AW_OTEL_TEST_HELPER") != "1" {
		return
	}
	_, _ = io.Copy(os.Stdout, os.Stdin)
	if os.Getenv("GH_AW_OTEL_TEST_FAIL") == "1" {
		os.Exit(1)
	}
	os.Exit(0)
}

func TestEnvOTELGHInput(t *testing.T) {
	old := defaultsExecGH
	t.Cleanup(func() { defaultsExecGH = old })
	defaultsExecGH = func(args ...string) *exec.Cmd {
		for _, arg := range args {
			require.NotContains(t, arg, "private-credential")
		}
		c := exec.Command(os.Args[0], "-test.run=^TestEnvOTELGHHelper$")
		c.Env = append(os.Environ(), "GH_AW_OTEL_TEST_HELPER=1")
		return c
	}
	data, err := envOTELGH(t.Context(), []byte("private-credential"), "api", "--input", "-")
	require.NoError(t, err)
	assert.Equal(t, "private-credential", string(data))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = envOTELGH(ctx, nil, "api")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "private-credential")
	defaultsExecGH = func(args ...string) *exec.Cmd {
		c := exec.Command(os.Args[0], "-test.run=^TestEnvOTELGHHelper$")
		c.Env = append(os.Environ(), "GH_AW_OTEL_TEST_HELPER=1", "GH_AW_OTEL_TEST_FAIL=1")
		return c
	}
	out, err := envOTELGH(t.Context(), []byte("private-credential"), "api")
	require.Error(t, err)
	assert.Empty(t, out)
	assert.NotContains(t, err.Error(), "private-credential")
	defaultsExecGH = func(...string) *exec.Cmd {
		c := exec.Command("gh")
		c.Err = exec.ErrDot
		return c
	}
	_, err = envOTELGH(t.Context(), nil, "api")
	require.ErrorContains(t, err, "could not resolve gh")
}

func TestEnvOTELTargets(t *testing.T) {
	for _, tc := range []struct {
		name string
		o    envOTELOptions
		bad  bool
	}{
		{"repo", envOTELOptions{repos: []string{"org/one", "org/two", "org/one"}}, false},
		{"repo-leading-punctuation", envOTELOptions{repos: []string{"my-org/.github", "my-org/_private", "my-org/-archive"}}, false},
		{"org", envOTELOptions{orgs: []string{"one", "two"}}, false},
		{"ent", envOTELOptions{enterprises: []string{"enterprise"}}, false},
		{"mixed", envOTELOptions{repos: []string{"one/repo"}, orgs: []string{"two"}, enterprises: []string{"three"}}, false},
		{"contradiction", envOTELOptions{scope: "repo", orgs: []string{"one"}}, true},
		{"bad-scope", envOTELOptions{scope: "other"}, true},
		{"bad-repo", envOTELOptions{repos: []string{"github.com/one/two"}}, true},
		{"bad-org", envOTELOptions{orgs: []string{"../one"}}, true},
		{"selected-missing", envOTELOptions{orgs: []string{"one"}, visibility: "selected"}, true},
		{"selected", envOTELOptions{orgs: []string{"one"}, visibility: "selected", selected: []string{"one/repo"}}, false},
		{"enterprise-selected", envOTELOptions{enterprises: []string{"enterprise"}, visibility: "selected", selected: []string{"my-org"}}, false},
		{"selected-repo", envOTELOptions{repos: []string{"one/repo"}, visibility: "selected", selected: []string{"one/repo"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.o.visibility == "" {
				tc.o.visibility = defaultsVisibilityPriv
			}
			targets, err := envOTELTargets(tc.o, false)
			if tc.bad {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotEmpty(t, targets)
				if tc.name == "repo" {
					assert.Len(t, targets, 2)
				}
			}
		})
	}
	_, err := envOTELTargets(envOTELOptions{repos: []string{"one/repo"}, visibility: "all"}, true)
	require.Error(t, err)
	targets, err := envOTELTargets(envOTELOptions{enterprises: []string{"enterprise"}, visibility: "selected", selected: []string{"my-org"}}, false)
	require.NoError(t, err)
	envOTELMockGH(t, func(args []string, input []byte) ([]byte, error) {
		return []byte("my-org\n"), nil
	})
	expanded, err := envOTELExpandEnterprises(t.Context(), targets, []string{"my-org"})
	require.NoError(t, err)
	require.Len(t, expanded, 1)
	assert.Equal(t, "my-org", expanded[0].org)
}

func TestEnvOTELValidate(t *testing.T) {
	for _, endpoint := range []string{
		"http://collector.example", "http://localhost:4318", "******collector.example",
		"https://collector.example/#fragment", "https://collector.example/#", "https://collector.example/%0d%0a",
		"https://collector.example/\n", "not-a-url", "https://collector.example:99999", "https://collector.example:bad",
		`https://collector.example/path?next="collector"`, `https://collector.example/path?next=\collector`,
		"https://collector.example/path?token=%67ho_testcredential",
	} {
		_, _, err := envOTELValidate(endpoint, "http/json", "Authorization=private-credential")
		require.Error(t, err, endpoint)
		assert.NotContains(t, err.Error(), "private-credential")
		assert.NotContains(t, err.Error(), "password")
	}
	for _, headers := range []string{"", "bad", "Host=evil", "Content-Type=x", "Auth=bad%0a", "Auth=bad%xx", "Auth=x,auth=y", "Auth="} {
		_, _, err := envOTELValidate("https://collector.example", "http/json", headers)
		require.Error(t, err, headers)
	}
	u, h, err := envOTELValidate("https://collector.example/base?key=one", "http/json", "Auth=a+b%2Cc%3Dd")
	require.NoError(t, err)
	assert.Equal(t, "/base/v1/traces", u.Path)
	assert.Equal(t, "key=one", u.RawQuery)
	assert.Equal(t, "a+b,c=d", h.Get("Auth"))
	u, h, err = envOTELValidate("https://ingest.sentry.io/otlp/v1/traces", "http/protobuf", "Authorization=credential")
	require.NoError(t, err)
	assert.Equal(t, "/otlp/v1/traces", u.Path)
	assert.Empty(t, h.Get("Authorization"))
	assert.Equal(t, "credential", h.Get("x-sentry-auth"))
	u, _, err = envOTELValidate("https://collector.example/tenant%2Fname?key=one", "http/json", "Auth=credential")
	require.NoError(t, err)
	assert.Equal(t, "/tenant/name/v1/traces", u.Path)
	assert.Equal(t, "/tenant%2Fname/v1/traces", u.EscapedPath())
	for _, host := range []string{"https://sentry.io", "https://ingest.sentry.io"} {
		_, h, err = envOTELValidate(host, "http/json", "Authorization=credential")
		require.NoError(t, err)
		assert.Equal(t, "credential", h.Get("x-sentry-auth"))
	}
	_, h, err = envOTELValidate("https://sentry-proxy.corp.example", "http/json", "Authorization=credential")
	require.NoError(t, err)
	assert.Equal(t, "credential", h.Get("Authorization"))
	assert.Empty(t, h.Get("x-sentry-auth"))
	for _, scheme := range []string{"api-key", "apikey", "basic", "bearer", "concealed", "digest", "dpop", "dsn", "gnap", "hoba", "mutual", "negotiate", "oauth", "privatetoken", "scram-sha-1", "scram-sha-256", "sentry", "vapid"} {
		_, _, err := envOTELValidate("https://collector.example", "http/json", "Authorization="+scheme)
		require.Error(t, err, scheme)
	}
	_, _, err = envOTELValidate("https://collector.example", "grpc", "Auth=x")
	require.Error(t, err)
}

func TestEnvOTELProbe(t *testing.T) {
	for _, tc := range []struct {
		name, protocol, response, contentType string
		status                                int
		bad                                   bool
	}{
		{"json", "http/json", `{}`, "application/json", 200, false},
		{"empty", "http/json", "", "", 200, false},
		{"nulls", "http/json", `{"partialSuccess":{"rejectedSpans":null,"errorMessage":null}}`, "application/json", 200, false},
		{"snake-case-rejected", "http/json", `{"partial_success":{"rejected_spans":"1"}}`, "application/json", 200, true},
		{"snake-case-warning", "http/json", `{"partial_success":{"rejected_spans":"0","error_message":"warning"}}`, "application/json", 200, false},
		{"camel-snake-rejected", "http/json", `{"partialSuccess":{"rejectedSpans":"0","rejected_spans":"1"}}`, "application/json", 200, true},
		{"conflicting-error-aliases", "http/json", `{"partialSuccess":{"rejectedSpans":"0","errorMessage":"first","error_message":"second"}}`, "application/json", 200, true},
		{"conflicting-partial-aliases", "http/json", `{"partialSuccess":{"rejectedSpans":"0"},"partial_success":{"rejectedSpans":"1"}}`, "application/json", 200, true},
		{"warning", "http/json", `{"partialSuccess":{"rejectedSpans":"0","errorMessage":"private-credential"}}`, "application/json", 200, false},
		{"rejected", "http/json", `{"partialSuccess":{"rejectedSpans":"1","errorMessage":"private-credential"}}`, "application/json", 200, true},
		{"malformed", "http/json", "private-credential", "application/json", 200, true},
		{"html", "http/json", "<html>private-credential</html>", "text/html", 200, true},
		{"oversized", "http/json", strings.Repeat("x", envOTELResponseLimit+1), "application/json", 200, true},
		{"unauthorized", "http/json", "private-credential", "text/plain", 401, true},
		{"forbidden", "http/json", "private-credential", "text/plain", 403, true},
		{"unavailable", "http/json", "", "", 503, true},
		{"protobuf", "http/protobuf", "\x0a\x00", "application/x-protobuf", 200, false},
		{"protobuf-rejected", "http/protobuf", "\x0a\x02\x08\x01", "application/x-protobuf", 200, true},
		{"protobuf-malformed", "http/protobuf", "\x0a\xff", "application/x-protobuf", 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/v1/traces", r.URL.Path)
				assert.Equal(t, "private-credential", r.Header.Get("Authorization"))
				payload, err := io.ReadAll(r.Body)
				if !assert.NoError(t, err) {
					return
				}
				if tc.protocol == "http/protobuf" {
					assert.Empty(t, payload)
					assert.Equal(t, "application/x-protobuf", r.Header.Get("Content-Type"))
				} else {
					assert.JSONEq(t, `{"resourceSpans":[]}`, string(payload))
				}
				if tc.contentType != "" {
					w.Header().Set("Content-Type", tc.contentType)
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.response)
			}))
			defer server.Close()
			u, h, err := envOTELValidate(server.URL, tc.protocol, "Authorization=private-credential")
			require.NoError(t, err)
			err = envOTELProbe(t.Context(), u, h, tc.protocol)
			if tc.bad {
				require.Error(t, err)
				assert.NotContains(t, err.Error(), "private-credential")
			} else {
				require.NoError(t, err)
			}
		})
	}
	t.Run("redirect", func(t *testing.T) {
		redirected := false
		destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected = true }))
		defer destination.Close()
		source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
		}))
		defer source.Close()
		u, h, err := envOTELValidate(source.URL, "http/json", "Private-Header=private-credential")
		require.NoError(t, err)
		require.Error(t, envOTELProbe(t.Context(), u, h, "http/json"))
		assert.False(t, redirected)
	})
	t.Run("cancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		defer server.Close()
		u, h, err := envOTELValidate(server.URL, "http/json", "Auth=private-credential")
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		require.Error(t, envOTELProbe(ctx, u, h, "http/json"))
	})
}

func envOTELMockGH(t *testing.T, run func([]string, []byte) ([]byte, error)) {
	t.Helper()
	old := envOTELRunGH
	t.Cleanup(func() { envOTELRunGH = old })
	envOTELRunGH = func(_ context.Context, input []byte, args ...string) ([]byte, error) {
		for _, arg := range args {
			require.NotContains(t, arg, "private-credential")
			require.NotContains(t, arg, "encrypted_value")
		}
		return run(args, input)
	}
}

func TestEnvOTELNoWritesOnProbeFailure(t *testing.T) {
	old := defaultsExecGH
	t.Cleanup(func() { defaultsExecGH = old })
	calls := 0
	defaultsExecGH = func(args ...string) *exec.Cmd {
		calls++
		return exec.Command("gh", args...)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	defer server.Close()
	targets := []defaultsTarget{{scope: "repo", repoOwner: "one", repoName: "repo"}, {scope: "org", org: "two", visibility: "all"}}
	err := runEnvOTEL(t.Context(), targets, envOTELOptions{endpoint: server.URL, protocol: "http/json"}, "Auth=private-credential")
	require.Error(t, err)
	assert.Zero(t, calls)
}

func TestEnvOTELCLIInput(t *testing.T) {
	for _, args := range [][]string{
		{"otel", "--repo", "one/repo"},
		{"otel", "--repo", "one/repo", "--endpoint", "https://collector.example"},
		{"otel", "--repo", "one/repo", "--endpoint", "https://collector.example", "--headers-stdin"},
	} {
		cmd := NewEnvCommand()
		var output bytes.Buffer
		cmd.SetIn(strings.NewReader("Auth=private-credential"))
		cmd.SetOut(&output)
		cmd.SetErr(&output)
		cmd.SetArgs(args)
		err := cmd.Execute()
		require.Error(t, err)
		assert.NotContains(t, output.String(), "private-credential")
	}
	value, err := envOTELReadLine(strings.NewReader("endpoint\nheader-secret\n"))
	require.NoError(t, err)
	assert.Equal(t, "endpoint", value)
}

func TestEnvOTELPublicKey(t *testing.T) {
	public, private, err := box.GenerateKey(rand.Reader)
	require.NoError(t, err)
	encrypted, err := encryptWithPublicKey(base64.StdEncoding.EncodeToString(public[:]), "Auth=private-credential")
	require.NoError(t, err)
	data, err := base64.StdEncoding.DecodeString(encrypted)
	require.NoError(t, err)
	plaintext, ok := box.OpenAnonymous(nil, data, public, private)
	require.True(t, ok)
	assert.Equal(t, "Auth=private-credential", string(plaintext))
}

func TestEnvOTELSaveTargets(t *testing.T) {
	public, private, err := box.GenerateKey(rand.Reader)
	require.NoError(t, err)
	key, err := json.Marshal(repoPublicKey{ID: "1", Key: base64.StdEncoding.EncodeToString(public[:])})
	require.NoError(t, err)
	for _, tc := range []struct {
		name       string
		targets    []defaultsTarget
		selected   []string
		preFail    bool
		saveFail   bool
		writeCount int
	}{
		{"repo-multi", []defaultsTarget{{scope: "repo", repoOwner: "one", repoName: "repo"}, {scope: "repo", repoOwner: "one", repoName: "other"}}, nil, false, false, 6},
		{"org-all", []defaultsTarget{{scope: "org", org: "one", visibility: "all"}}, nil, false, false, 3},
		{"org-private", []defaultsTarget{{scope: "org", org: "one", visibility: "private"}}, nil, false, false, 3},
		{"org-selected", []defaultsTarget{{scope: "org", org: "one", visibility: "selected"}}, []string{"one/repo"}, false, false, 3},
		{"selected-other-org", []defaultsTarget{{scope: "org", org: "one", visibility: "selected"}}, []string{"other/repo"}, false, false, 0},
		{"later-preflight", []defaultsTarget{{scope: "repo", repoOwner: "one", repoName: "repo"}, {scope: "org", org: "two", visibility: "all"}}, nil, true, false, 0},
		{"save-failure", []defaultsTarget{{scope: "repo", repoOwner: "one", repoName: "repo"}}, nil, false, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "private-credential", r.Header.Get("Auth"))
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			probed := false
			writes := 0
			var saved []string
			envOTELMockGH(t, func(args []string, input []byte) ([]byte, error) {
				probed = true
				line := strings.Join(args, " ")
				if strings.Contains(line, "public-key") {
					if tc.preFail && strings.Contains(line, "orgs/two") {
						return nil, errors.New("preflight failed")
					}
					return key, nil
				}
				if len(input) == 0 {
					if strings.Contains(line, "repos/one/repo --jq .id") {
						return []byte("123"), nil
					}
					if strings.Contains(line, ".variables[].name") {
						return []byte("OTEL_EXPORTER_OTLP_PROTOCOL\nUNRELATED_DEFAULT"), nil
					}
					return []byte("0"), nil
				}
				writes++
				require.True(t, probed)
				if tc.saveFail {
					return nil, errors.New("save failed")
				}
				var payload map[string]any
				require.NoError(t, json.Unmarshal(input, &payload))
				if strings.Contains(line, "/secrets/") {
					data, err := base64.StdEncoding.DecodeString(payload["encrypted_value"].(string))
					require.NoError(t, err)
					plaintext, ok := box.OpenAnonymous(nil, data, public, private)
					require.True(t, ok)
					var config struct {
						Endpoint string `json:"endpoint"`
						Headers  string `json:"headers"`
					}
					require.NoError(t, json.Unmarshal(plaintext, &config))
					assert.Equal(t, server.URL, config.Endpoint)
					assert.Equal(t, "Auth=private-credential", config.Headers)
				} else {
					name := payload["name"].(string)
					saved = append(saved, name)
					switch name {
					case "OTEL_EXPORTER_OTLP_PROTOCOL":
						assert.Contains(t, line, "-X PATCH")
						assert.Equal(t, "http/json", payload["value"])
					case "GH_AW_OTEL_METRICS":
						assert.Contains(t, line, "-X POST")
						assert.Equal(t, "false", payload["value"])
					default:
						t.Fatalf("unexpected default mutated: %s", name)
					}
				}
				if tc.targets[0].scope == defaultsScopeOrg {
					assert.Equal(t, tc.targets[0].visibility, payload["visibility"])
					if tc.targets[0].visibility == defaultsVisibilitySel {
						assert.Equal(t, []any{float64(123)}, payload["selected_repository_ids"])
					}
				}
				return nil, nil
			})
			err := runEnvOTEL(t.Context(), tc.targets, envOTELOptions{
				endpoint: server.URL, protocol: "http/json", selected: tc.selected,
			}, "Auth=private-credential")
			if tc.writeCount == 0 || tc.saveFail {
				require.Error(t, err)
				if tc.saveFail {
					require.ErrorContains(t, err, "one/repo")
				}
			} else {
				require.NoError(t, err)
				assert.NotEmpty(t, saved)
			}
			assert.Equal(t, tc.writeCount, writes)
		})
	}
}

func TestEnvOTELPreflightErrorsNameTargets(t *testing.T) {
	public, _, err := box.GenerateKey(rand.Reader)
	require.NoError(t, err)
	key, err := json.Marshal(repoPublicKey{ID: "1", Key: base64.StdEncoding.EncodeToString(public[:])})
	require.NoError(t, err)

	t.Run("variable access", func(t *testing.T) {
		envOTELMockGH(t, func(args []string, _ []byte) ([]byte, error) {
			if strings.Contains(strings.Join(args, " "), "public-key") {
				return key, nil
			}
			return nil, errors.New("inaccessible")
		})
		_, err := envOTELPrepare(t.Context(), []defaultsTarget{{scope: "org", org: "one", visibility: "private"}}, nil)
		require.ErrorContains(t, err, "one")
	})

	t.Run("selected repository", func(t *testing.T) {
		envOTELMockGH(t, func(args []string, _ []byte) ([]byte, error) {
			line := strings.Join(args, " ")
			switch {
			case strings.Contains(line, "public-key"):
				return key, nil
			case strings.Contains(line, "--jq .total_count"):
				return []byte("0"), nil
			default:
				return nil, errors.New("inaccessible")
			}
		})
		target := defaultsTarget{scope: "org", org: "one", visibility: "selected"}
		_, err := envOTELPrepare(t.Context(), []defaultsTarget{target}, map[string][]string{"one": {"one/.github"}})
		require.ErrorContains(t, err, "one")
		require.ErrorContains(t, err, "one/.github")
	})
}

func TestEnvOTELEnterpriseExpansion(t *testing.T) {
	for _, tc := range []struct {
		name, response, visibility string
		selected                   []string
		bad                        bool
		count                      int
	}{
		{"all", "one\ntwo\n", "all", nil, false, 2},
		{"private", "one\ntwo\n", "private", nil, false, 2},
		{"selected", "one\ntwo\n", "selected", []string{"two"}, false, 1},
		{"nonmember", "one\n", "selected", []string{"two"}, true, 0},
		{"empty", "", "all", nil, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			envOTELMockGH(t, func(args []string, input []byte) ([]byte, error) {
				require.Empty(t, input)
				assert.Contains(t, strings.Join(args, " "), "--paginate")
				assert.Contains(t, strings.Join(args, " "), "organizations(first:100,after:$endCursor)")
				assert.Contains(t, strings.Join(args, " "), "slug=enterprise")
				return []byte(tc.response), nil
			})
			targets, err := envOTELExpandEnterprises(t.Context(), []defaultsTarget{{scope: "ent", enterprise: "enterprise", visibility: tc.visibility}}, tc.selected)
			if tc.bad {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Len(t, targets, tc.count)
				for _, target := range targets {
					assert.Equal(t, defaultsScopeOrg, target.scope)
					if tc.visibility == "selected" {
						assert.Equal(t, "private", target.visibility)
					} else {
						assert.Equal(t, tc.visibility, target.visibility)
					}
				}

			}
		})
	}
}

func TestEnvOTELMultipleSelectedTargets(t *testing.T) {
	targets := []defaultsTarget{{scope: "org", org: "one", visibility: "selected"}, {scope: "org", org: "two", visibility: "selected"}}
	selections, err := envOTELOrgSelections(targets, []string{"one/repo", "two/repo"})
	require.NoError(t, err)
	assert.Equal(t, []string{"one/repo"}, selections["one"])
	assert.Equal(t, []string{"two/repo"}, selections["two"])
	_, err = envOTELOrgSelections(targets, []string{"one/repo"})
	require.Error(t, err)
	_, err = envOTELOrgSelections(targets, []string{"one/repo", "two/repo", "orphan/repo"})
	require.Error(t, err)
	selected, err := envOTELOrgSelections(targets[:1], []string{"one/.github"})
	require.NoError(t, err)
	envOTELMockGH(t, func(args []string, _ []byte) ([]byte, error) {
		assert.Contains(t, strings.Join(args, " "), "repos/one/.github")
		return []byte("123"), nil
	})
	ids, err := envOTELSelected(t.Context(), targets[0], selected["one"])
	require.NoError(t, err)
	assert.Equal(t, []int64{123}, ids)
	envOTELMockGH(t, func(args []string, input []byte) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "slug=first") {
			return []byte("one\n"), nil
		}
		return []byte("two\n"), nil
	})
	expanded, err := envOTELExpandEnterprises(t.Context(), []defaultsTarget{{scope: "ent", enterprise: "first", visibility: "selected"}, {scope: "ent", enterprise: "second", visibility: "selected"}}, []string{"one", "two"})
	require.NoError(t, err)
	require.Len(t, expanded, 2)
	assert.Equal(t, "one", expanded[0].org)
	assert.Equal(t, "two", expanded[1].org)
}

func TestEnvOTELCLISuccess(t *testing.T) {
	public, private, err := box.GenerateKey(rand.Reader)
	require.NoError(t, err)
	key, err := json.Marshal(repoPublicKey{ID: "1", Key: base64.StdEncoding.EncodeToString(public[:])})
	require.NoError(t, err)
	writes := 0
	envOTELMockGH(t, func(args []string, input []byte) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "public-key") {
			return key, nil
		}
		if len(input) > 0 {
			writes++
			var payload map[string]any
			require.NoError(t, json.Unmarshal(input, &payload))
			if payload["name"] == "GH_AW_OTEL_METRICS" {
				assert.Equal(t, "true", payload["value"])
			}
			if strings.Contains(strings.Join(args, " "), "GH_AW_DEFAULT_OTLP_HEADERS") {
				encrypted, err := base64.StdEncoding.DecodeString(payload["encrypted_value"].(string))
				require.NoError(t, err)
				plaintext, ok := box.OpenAnonymous(nil, encrypted, public, private)
				require.True(t, ok)
				assert.Equal(t, "Auth=private-credential%20with+plus%2Ccomma", string(plaintext))
			}
		}
		return nil, nil
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "private-credential with+plus,comma", r.Header.Get("Auth"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	cmd := NewEnvCommand()
	cmd.SetArgs([]string{"otel", "--repo", "one/repo", "--endpoint", server.URL, "--headers-stdin", "--yes"})
	cmd.SetIn(strings.NewReader("Auth=private-credential%20with+plus%2Ccomma\n"))
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	require.NoError(t, cmd.Execute())
	assert.Equal(t, 3, writes)
	assert.Contains(t, output.String(), "configured 1 target(s)")
	assert.NotContains(t, output.String(), "private-credential")
}

func TestEnvOTELCLIEnterpriseSelected(t *testing.T) {
	public, _, err := box.GenerateKey(rand.Reader)
	require.NoError(t, err)
	key, err := json.Marshal(repoPublicKey{ID: "1", Key: base64.StdEncoding.EncodeToString(public[:])})
	require.NoError(t, err)
	writes := 0
	envOTELMockGH(t, func(args []string, input []byte) ([]byte, error) {
		line := strings.Join(args, " ")
		switch {
		case strings.Contains(line, "api graphql"):
			return []byte("my-org\n"), nil
		case strings.Contains(line, "public-key"):
			return key, nil
		case len(input) > 0:
			writes++
			var payload map[string]any
			require.NoError(t, json.Unmarshal(input, &payload))
			assert.Equal(t, "private", payload["visibility"])
			return nil, nil
		default:
			return []byte("0"), nil
		}
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "private-credential", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	cmd := NewEnvCommand()
	cmd.SetArgs([]string{"otel", "--enterprise", "enterprise", "--visibility", "selected", "--selected", "my-org", "--endpoint", server.URL, "--headers-stdin", "--yes"})
	cmd.SetIn(strings.NewReader("Authorization=private-credential\n"))
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	require.NoError(t, cmd.Execute())
	assert.Equal(t, 3, writes)
	assert.Contains(t, output.String(), "configured 1 target(s)")
}

func TestEnvOTELCanonicalHeaderLimit(t *testing.T) {
	calls := 0
	envOTELMockGH(t, func(args []string, input []byte) ([]byte, error) {
		calls++
		return nil, nil
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	err := runEnvOTEL(t.Context(), []defaultsTarget{{scope: "repo", repoOwner: "one", repoName: "repo"}},
		envOTELOptions{endpoint: server.URL, protocol: "http/json"}, "Auth="+strings.Repeat("x ", 20000))
	require.ErrorContains(t, err, "48 KiB")
	assert.Zero(t, calls)
}

func TestEnvOTELRejectsGitHubOAuthCredentials(t *testing.T) {
	_, _, err := envOTELValidate("https://collector.example", "http/json", "Auth=Bearer%20%67ho_testcredential")
	require.ErrorContains(t, err, "GitHub OAuth")
	assert.NotContains(t, err.Error(), "testcredential")
}
