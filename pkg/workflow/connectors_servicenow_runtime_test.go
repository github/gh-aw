//go:build !integration

package workflow

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceNowMarkdownRuntime(t *testing.T) {
	for _, target := range []struct{ table, name, record string }{
		{"incident", "incident", serviceNowImportIncident},
		{"sc_req_item", "requested-item", serviceNowImportItem},
	} {
		t.Run(target.table, func(t *testing.T) {
			calls := 0
			note := "Synthetic review note\n${{ secrets.NOT_A_SECRET }} $(not-code)"
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				assert.Equal(t, "/api/now/table/"+target.table+"/"+target.record, r.URL.Path)
				query := r.URL.Query()
				assert.Len(t, query, 3)
				assert.Equal(t, "false", query.Get("sysparm_display_value"))
				assert.Equal(t, "true", query.Get("sysparm_exclude_reference_link"))
				switch r.Method {
				case http.MethodGet:
					assert.Equal(t, "Bearer synthetic-reader", r.Header.Get("Authorization"))
					assert.Equal(t, "sys_id,number,state,sys_mod_count", query.Get("sysparm_fields"))
					_, err := io.WriteString(w, `{"result":{"sys_id":"`+target.record+`","number":"SYNTHETIC1","state":"2","sys_mod_count":"17","description":"private-fixture","work_notes":"private-fixture"}}`)
					assert.NoError(t, err)
				case http.MethodPatch:
					assert.Equal(t, "Bearer synthetic-writer", r.Header.Get("Authorization"))
					assert.Equal(t, "sys_id", query.Get("sysparm_fields"))
					assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
					var body map[string]any
					assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					assert.Equal(t, map[string]any{"work_notes": note}, body)
					_, err := io.WriteString(w, `{"result":{"sys_id":"`+target.record+`"}}`)
					assert.NoError(t, err)
				default:
					t.Errorf("unexpected method %s", r.Method)
				}
			}))
			defer server.Close()
			with := strings.Replace(serviceNowImportGrants, "https://example.service-now.com", server.URL, 1)
			selection := "tools:\n  servicenow: [get-" + target.name + "]\nsafe-outputs:\n  servicenow-append-" + target.name + "-work-note:\n"
			compiler, path := serviceNowImportCompiler(t, with, selection)
			data, err := compiler.ParseWorkflowFile(path)
			require.NoError(t, err)
			config := data.Connectors["servicenow"]
			resolutions := 0
			runtime := connectors.Runtime{Phase: "agent", AllowedOrigins: []string{server.URL}, Client: server.Client(),
				ResolveSecret: func(_ context.Context, phase, secret string) (string, error) {
					resolutions++
					if phase == "agent" {
						assert.Equal(t, "APPROVED_SERVICENOW_READER", secret)
						return "synthetic-reader", nil
					}
					assert.Equal(t, "safe-output", phase)
					assert.Equal(t, "APPROVED_SERVICENOW_WRITER", secret)
					return "synthetic-writer", nil
				},
			}
			result, err := config.Agent.Execute(t.Context(), runtime, "mcp", "get-"+target.name, map[string]any{"record": target.record})
			require.NoError(t, err)
			assert.True(t, result.Untrusted)
			var fields map[string]any
			require.NoError(t, json.Unmarshal(result.Data, &fields))
			assert.Equal(t, map[string]any{"sys-id": target.record, "number": "SYNTHETIC1", "state": "2", "revision": "17"}, fields)
			effect := "append-" + target.name + "-work-note"
			intent, err := config.SafeOutputs.PrepareSafeOutput(effect, map[string]any{"record": target.record, "note": note})
			require.NoError(t, err)
			assert.Equal(t, 1, calls, "staging must not send a PATCH")
			assert.Equal(t, 1, resolutions, "staging must not resolve the writer")
			otherRecord := serviceNowImportIncident
			if target.record == otherRecord {
				otherRecord = serviceNowImportItem
			}
			for _, args := range []map[string]any{
				{"record": otherRecord},
				{"record": "INC0009009"},
				{"record": strings.ToUpper(target.record)},
				{"record": target.record, "table": "sys_user"},
			} {
				_, err := config.Agent.Execute(t.Context(), runtime, "mcp", "get-"+target.name, args)
				require.Error(t, err)
			}
			for _, invalidNote := range []string{"", " \n\t", strings.Repeat("x", 4001), strings.Repeat("\u00e9", 4001), strings.Repeat("\U0001f600", 4001)} {
				_, err := config.SafeOutputs.PrepareSafeOutput(effect, map[string]any{"record": target.record, "note": invalidNote})
				require.Error(t, err)
			}
			for _, validNote := range []string{strings.Repeat("x", 4000), strings.Repeat("\u00e9", 4000), strings.Repeat("\U0001f600", 4000)} {
				_, err = config.SafeOutputs.PrepareSafeOutput(effect, map[string]any{"record": target.record, "note": validNote})
				require.NoError(t, err)
			}
			assert.Equal(t, 1, calls)
			assert.Equal(t, 1, resolutions, "invalid requests must fail before credentials")
			runtime.Phase, runtime.AllowWrites = "safe-output", true
			result, err = config.SafeOutputs.ExecuteSafeOutput(t.Context(), runtime, *intent)
			require.NoError(t, err)
			assert.JSONEq(t, `{"sys-id":"`+target.record+`"}`, string(result.Data))
			assert.Equal(t, 2, calls)
			assert.Equal(t, 2, resolutions)
		})
	}
}

func TestServiceNowMarkdownResponseChecks(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
		write      bool
	}{
		{"missing-state", `{"result":{"sys_id":"` + serviceNowImportIncident + `","number":"SYNTHETIC1","sys_mod_count":"17"}}`, 200, false},
		{"empty-state", `{"result":{"sys_id":"` + serviceNowImportIncident + `","number":"SYNTHETIC1","state":"","sys_mod_count":"17"}}`, 200, false},
		{"numeric-revision", `{"result":{"sys_id":"` + serviceNowImportIncident + `","number":"SYNTHETIC1","state":"2","sys_mod_count":17}}`, 200, false},
		{"different-approved-record", `{"result":{"sys_id":"` + serviceNowOtherRecordID + `","number":"SYNTHETIC1","state":"2","sys_mod_count":"17"}}`, 200, false},
		{"error-envelope", `{"error":{"message":"private-fixture"}}`, 200, false},
		{"reader-over-bound", strings.Repeat("x", 8193), 200, false},
		{"writer-over-bound", strings.Repeat("x", 1025), 200, true},
		{"empty-receipt", `{"result":{}}`, 200, true},
		{"wrong-receipt-type", `{"result":{"sys_id":17}}`, 200, true},
		{"different-approved-receipt", `{"result":{"sys_id":"` + serviceNowOtherRecordID + `"}}`, 200, true},
		{"writer-acl-denied", `{"error":{"message":"private-fixture"}}`, 403, true},
		{"rate-limited", `{"error":{"message":"private-fixture"}}`, 429, true},
		{"ambiguous-server-error", `{"error":{"message":"private-fixture"}}`, 500, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(test.status)
				_, err := io.WriteString(w, test.body)
				assert.NoError(t, err)
			}))
			defer server.Close()
			with := strings.Replace(serviceNowImportGrants, "https://example.service-now.com", server.URL, 1)
			with = strings.Replace(with, `incidents: ["`+serviceNowImportIncident+`"]`, `incidents: ["`+serviceNowImportIncident+`", "`+serviceNowOtherRecordID+`"]`, 1)
			selection := "tools:\n  servicenow: [get-incident]\nsafe-outputs:\n  servicenow-append-incident-work-note:\n"
			compiler, path := serviceNowImportCompiler(t, with, selection)
			data, err := compiler.ParseWorkflowFile(path)
			require.NoError(t, err)
			config := data.Connectors["servicenow"]
			runtime := connectors.Runtime{Phase: "agent", AllowedOrigins: []string{server.URL}, Client: server.Client(),
				ResolveSecret: func(context.Context, string, string) (string, error) { return "synthetic", nil }}
			args := map[string]any{"record": serviceNowImportIncident}
			plan, capability, name := config.Agent, "mcp", "get-incident"
			if test.write {
				runtime.Phase, runtime.AllowWrites = "safe-output", true
				plan, capability, name = config.SafeOutputs, "safe-output", "append-incident-work-note"
				args["note"] = "Synthetic note"
			}
			result, err := plan.Execute(t.Context(), runtime, capability, name, args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.NotContains(t, err.Error(), "private-fixture")
			assert.Equal(t, 1, calls, "failed appends must not be retried")
		})
	}
}

func TestServiceNowMarkdownExactResponseBounds(t *testing.T) {
	for _, write := range []bool{false, true} {
		limit := 8192
		body := `{"result":{"sys_id":"` + serviceNowImportIncident + `","number":"SYNTHETIC1","state":"2","sys_mod_count":"17"}}`
		selection := "tools:\n  servicenow: [get-incident]\n"
		if write {
			limit = 1024
			body = `{"result":{"sys_id":"` + serviceNowImportIncident + `"}}`
			selection = "safe-outputs:\n  servicenow-append-incident-work-note:\n"
		}
		for _, size := range []int{limit, limit + 1} {
			response := body + strings.Repeat(" ", size-len(body))
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, err := io.WriteString(w, response)
				assert.NoError(t, err)
			}))
			with := strings.Replace(serviceNowImportGrants, "https://example.service-now.com", server.URL, 1)
			compiler, path := serviceNowImportCompiler(t, with, selection)
			data, err := compiler.ParseWorkflowFile(path)
			require.NoError(t, err)
			config := data.Connectors["servicenow"]
			runtime := connectors.Runtime{Phase: "agent", AllowedOrigins: []string{server.URL}, Client: server.Client(),
				ResolveSecret: func(context.Context, string, string) (string, error) { return "synthetic", nil }}
			args := map[string]any{"record": serviceNowImportIncident}
			plan, capability, name := config.Agent, "mcp", "get-incident"
			if write {
				runtime.Phase, runtime.AllowWrites = "safe-output", true
				plan, capability, name = config.SafeOutputs, "safe-output", "append-incident-work-note"
				args["note"] = "Synthetic note"
			}
			result, err := plan.Execute(t.Context(), runtime, capability, name, args)
			server.Close()
			if size == limit {
				require.NoError(t, err, "exact response byte bound must succeed")
				require.NotNil(t, result)
			} else {
				require.Error(t, err, "one byte over the response bound must fail")
				assert.Nil(t, result)
			}
		}
	}
}
