package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
)

func TestEnvOTELProtoWireFields(t *testing.T) {
	unknown := protowire.AppendTag(nil, 3, protowire.VarintType)
	unknown = protowire.AppendVarint(unknown, 42)
	unknown = protowire.AppendTag(unknown, 4, protowire.Fixed32Type)
	unknown = protowire.AppendFixed32(unknown, 42)
	unknown = protowire.AppendTag(unknown, 5, protowire.Fixed64Type)
	unknown = protowire.AppendFixed64(unknown, 42)
	unknown = protowire.AppendTag(unknown, 6, protowire.BytesType)
	unknown = protowire.AppendString(unknown, "unknown")
	unknown = protowire.AppendTag(unknown, 7, protowire.StartGroupType)
	unknown = append(unknown, 0x08, 0x01)
	unknown = protowire.AppendTag(unknown, 7, protowire.EndGroupType)
	maxField := protowire.AppendTag(nil, protowire.MaxValidNumber, protowire.VarintType)
	maxField = protowire.AppendVarint(maxField, 0)
	partial := protowire.AppendTag(nil, 1, protowire.BytesType)
	partial = protowire.AppendBytes(partial, unknown)

	for _, tc := range []struct {
		name string
		data []byte
		bad  bool
	}{
		{"unknown fields", unknown, false},
		{"unknown partial fields", partial, false},
		{"maximum field number", maxField, false},
		{"zero field number", []byte{0x00, 0x00}, true},
		{"out of range field number", protowire.AppendTag(nil, protowire.MaxValidNumber+1, protowire.VarintType), true},
		{"truncated tag", []byte{0x80}, true},
		{"truncated unknown varint", []byte{0x18, 0x80}, true},
		{"truncated unknown fixed32", []byte{0x1d, 0x00}, true},
		{"truncated unknown fixed64", []byte{0x19, 0x00}, true},
		{"truncated unknown bytes", []byte{0x1a, 0x02, 0x00}, true},
		{"mismatched unknown group", []byte{0x1b, 0x24}, true},
		{"invalid wire type", []byte{0x1e}, true},
		{"wrong partial wire type", []byte{0x08, 0x00}, true},
		{"wrong count wire type", []byte{0x0a, 0x02, 0x0a, 0x00}, true},
		{"wrong message wire type", []byte{0x0a, 0x02, 0x10, 0x00}, true},
		{"invalid message UTF-8", []byte{0x0a, 0x03, 0x12, 0x01, 0xff}, true},
		{"rejected spans", []byte{0x0a, 0x02, 0x08, 0x01}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := envOTELProtoResponse(tc.data)
			if tc.bad {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestEnvOTELProbePreservesEndpointEscapes(t *testing.T) {
	for _, path := range []string{"/tenant%2Fname", "/tenant%2Fname/", "/tenant%2Fname/v1/traces"} {
		t.Run(path, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/tenant%2Fname/v1/traces?key=%22value%5C%22", r.RequestURI)
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			u, headers, err := envOTELValidate(server.URL+path+"?key=%22value%5C%22", "http/json", "Auth=credential")
			require.NoError(t, err)
			require.NoError(t, envOTELProbe(t.Context(), u, headers, "http/json"))
		})
	}
}

func TestEnvOTELProbeRejectsDecodedOAuthEndpoints(t *testing.T) {
	for _, endpoint := range []string{
		"https://collector.example/%67ho_testcredential",
		"https://collector.example?token=%67%68%6f%5ftestcredential",
		"https://collector.example?token=Bearer+%67ho_testcredential",
	} {
		_, _, err := envOTELValidate(endpoint, "http/json", "Auth=credential")
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "testcredential")
	}
}

func TestEnvOTELProbeSentryDomainBoundaries(t *testing.T) {
	for _, tc := range []struct {
		host   string
		sentry bool
	}{
		{"SENTRY.IO.", true},
		{"o123.ingest.us.sentry.io", true},
		{"sentry.io.corp.example", false},
		{"notsentry.io", false},
		{"sentry-proxy.corp.example", false},
	} {
		t.Run(tc.host, func(t *testing.T) {
			u, headers, err := envOTELValidate("https://"+tc.host, "http/json", "Authorization=credential")
			require.NoError(t, err)
			if tc.sentry {
				assert.Empty(t, headers.Get("Authorization"))
				assert.Equal(t, "credential", headers.Get("x-sentry-auth"))
			} else {
				assert.Equal(t, "credential", headers.Get("Authorization"))
				assert.Empty(t, headers.Get("x-sentry-auth"))
			}
			_, err = envOTELHeaders(u, "sentry_key=credential")
			assert.Equal(t, tc.sentry, err != nil)
		})
	}
	for _, scheme := range []string{"OAuth", "API-KEY"} {
		_, _, err := envOTELValidate("https://collector.example", "http/json", "x-sentry-auth="+strings.ToUpper(scheme)+"%20")
		require.Error(t, err)
	}
}
