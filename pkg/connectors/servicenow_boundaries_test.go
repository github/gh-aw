package connectors

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestServiceNowExactResponseBound(t *testing.T) {
	for _, limit := range []int{8192, 8193} {
		t.Run(strconv.Itoa(limit), func(t *testing.T) {
			body := `{"result":{"sys_id":"` + serviceNowIncidentID + `","number":"INC0009009","state":"2","sys_mod_count":"17"}}`
			body += strings.Repeat(" ", limit-len(body))
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, err := io.WriteString(w, body); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			plan := serviceNowPlan(t, server.URL)
			result, err := plan.Execute(t.Context(), serviceNowRuntime(server.Client(), server.URL, "context"), "context", "incident-metadata", map[string]any{"record": serviceNowIncidentID})
			if limit == 8192 {
				if err != nil || result == nil {
					t.Fatal("exact response byte limit must succeed", err)
				}
			} else if err == nil || result != nil {
				t.Fatal("one byte over the response limit must fail")
			}
		})
	}
}

func TestServiceNowCredentialResolutionFailure(t *testing.T) {
	for _, value := range []string{"", "injected\r\nheader"} {
		t.Run(fmt.Sprintf("length-%d", len(value)), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer server.Close()
			plan := serviceNowPlan(t, server.URL)
			runtime := serviceNowRuntime(server.Client(), server.URL, "agent")
			runtime.ResolveSecret = func(context.Context, string, string) (string, error) { return value, nil }
			if _, err := plan.Execute(t.Context(), runtime, "mcp", "get-incident", map[string]any{"record": serviceNowIncidentID}); err == nil {
				t.Fatal("invalid credential accepted")
			}
			runtime.ResolveSecret = func(context.Context, string, string) (string, error) {
				return "", errors.New("private-secret-resolution-error")
			}
			_, err := plan.Execute(t.Context(), runtime, "mcp", "get-incident", map[string]any{"record": serviceNowIncidentID})
			if err == nil || strings.Contains(err.Error(), "private-secret-resolution-error") || calls.Load() != 0 {
				t.Fatal("failed credential resolution leaked details or issued a request")
			}
		})
	}
}

func TestServiceNowRedirectRefused(t *testing.T) {
	var redirected atomic.Int32
	destination := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer destination.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	plan := serviceNowPlan(t, server.URL)
	runtime := serviceNowRuntime(server.Client(), server.URL, "agent")
	runtime.AllowedOrigins = append(runtime.AllowedOrigins, destination.URL)
	if _, err := plan.Execute(t.Context(), runtime, "mcp", "get-incident", map[string]any{"record": serviceNowIncidentID}); err == nil {
		t.Fatal("redirect accepted")
	}
	if redirected.Load() != 0 {
		t.Fatal("redirect destination received a request")
	}
}
