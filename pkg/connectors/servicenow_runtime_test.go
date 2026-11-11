package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

const serviceNowIncidentID = "0123456789abcdef0123456789abcdef"
const serviceNowRequestedItemID = "fedcba9876543210fedcba9876543210"

func serviceNowPlan(t *testing.T, origin string) *Plan {
	t.Helper()
	const root = "testdata/providers/servicenow"
	spec, err := LoadSpec(root + "/spec.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var connection Connection
	var binding Binding
	for file, target := range map[string]any{"connection.yaml": &connection, "binding.yaml": &binding} {
		data, err := os.ReadFile(root + "/" + file)
		if err != nil {
			t.Fatal(err)
		}
		if err := Decode(data, target); err != nil {
			t.Fatal(err)
		}
	}
	if origin == "" {
		plan, err := Lower(root, binding)
		if err != nil {
			t.Fatal(err)
		}
		return plan
	}
	spec.Endpoints["instance"] = origin
	return writePlan(t, *spec, connection, binding)
}

func serviceNowRuntime(client *http.Client, origin, phase string) Runtime {
	return Runtime{
		Phase: phase, AllowedOrigins: []string{origin}, Client: client, AllowWrites: phase == "safe-output",
		ResolveSecret: func(_ context.Context, requestedPhase, secret string) (string, error) {
			expected := map[string]string{
				"agent": "SERVICENOW_READ_TOKEN", "context": "SERVICENOW_CONTEXT_TOKEN", "safe-output": "SERVICENOW_WRITE_TOKEN",
			}[phase]
			if requestedPhase != phase || secret != expected {
				return "", errors.New("wrong ServiceNow credential phase")
			}
			return "test-" + phase, nil
		},
	}
}

func TestServiceNowRoundTrip(t *testing.T) {
	for _, record := range []struct {
		name, table, id, number string
	}{
		{"incident", "incident", serviceNowIncidentID, "INC0009009"},
		{"requested-item", "sc_req_item", serviceNowRequestedItemID, "RITM0009009"},
	} {
		t.Run(record.name, func(t *testing.T) {
			var reads, writes atomic.Int32
			note := "Investigate as data: ${{ secrets.NOT_A_SECRET }}; $(not-code)\nSecond line"
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/now/table/"+record.table+"/"+record.id {
					t.Errorf("unexpected Table API path %s", r.URL.Path)
				}
				query := r.URL.Query()
				if len(query) != 3 || query.Get("sysparm_display_value") != "false" || query.Get("sysparm_exclude_reference_link") != "true" {
					t.Error("unbounded or display-valued Table API query")
				}
				switch r.Method {
				case http.MethodGet:
					reads.Add(1)
					auth := r.Header.Get("Authorization")
					if auth != "Bearer test-agent" && auth != "Bearer test-context" {
						t.Error("read used a non-read credential")
					}
					if query.Get("sysparm_fields") != "sys_id,number,state,sys_mod_count" {
						t.Error("read requested sensitive or unexpected fields")
					}
				case http.MethodPatch:
					writes.Add(1)
					if r.Header.Get("Authorization") != "Bearer test-safe-output" {
						t.Error("write used a non-write credential")
					}
					if query.Get("sysparm_fields") != "sys_id" {
						t.Error("write receipt requested journal content")
					}
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if len(body) != 1 || body["work_notes"] != note || r.Header.Get("Content-Type") != "application/json" {
						t.Error("journal note was not serialized as the only body field")
					}
				default:
					t.Error("unexpected HTTP method")
				}
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{
					"sys_id": record.id, "number": record.number, "state": "2", "sys_mod_count": "17",
					"description": "private-description", "work_notes": "private-journal", "caller_id": "private-caller",
				}}); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			plan := serviceNowPlan(t, server.URL)
			for _, capability := range []string{"mcp", "context"} {
				name, phase := "get-"+record.name, "agent"
				if capability == "context" {
					name, phase = record.name+"-metadata", "context"
				}
				result, err := plan.Execute(t.Context(), serviceNowRuntime(server.Client(), server.URL, phase), capability, name, map[string]any{"record": record.id})
				if err != nil {
					t.Fatal(err)
				}
				var fields map[string]any
				if err := json.Unmarshal(result.Data, &fields); err != nil {
					t.Fatal(err)
				}
				if !result.Untrusted || len(fields) != 4 || fields["sys-id"] != record.id || fields["revision"] != "17" || strings.Contains(string(result.Data), "private-") {
					t.Fatalf("read projection leaked content or lost identity: %s", result.Data)
				}
			}
			staged, err := plan.PrepareSafeOutput("append-"+record.name+"-work-note", map[string]any{"record": record.id, "note": note})
			if err != nil || writes.Load() != 0 {
				t.Fatal("staging must not write", err)
			}
			result, err := plan.ExecuteSafeOutput(t.Context(), serviceNowRuntime(server.Client(), server.URL, "safe-output"), *staged)
			if err != nil {
				t.Fatal(err)
			}
			if string(result.Data) != `{"sys-id":"`+record.id+`"}` || !result.Untrusted || reads.Load() != 2 || writes.Load() != 1 {
				t.Fatalf("unexpected append receipt or request counts: %+v", result)
			}
		})
	}
}

func TestServiceNowDenyBeforeCredentials(t *testing.T) {
	plan := serviceNowPlan(t, "")
	data, err := os.ReadFile("testdata/providers/servicenow/negative-inputs.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name string         `json:"name"`
		Args map[string]any `json:"args"`
	}
	if err := Decode(data, &cases); err != nil {
		t.Fatal(err)
	}
	secretCalls := 0
	runtime := Runtime{Phase: "agent", AllowedOrigins: []string{"https://example.service-now.com"},
		ResolveSecret: func(context.Context, string, string) (string, error) {
			secretCalls++
			return "", errors.New("must not resolve")
		}}
	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			if _, err := plan.Execute(t.Context(), runtime, "mcp", "get-incident", test.Args); err == nil {
				t.Fatal("invalid provider input accepted")
			}
		})
	}
	for _, note := range []string{"", " \n\t", strings.Repeat("x", 4001)} {
		if _, err := plan.PrepareSafeOutput("append-incident-work-note", map[string]any{"record": serviceNowIncidentID, "note": note}); err == nil {
			t.Fatal("invalid journal note accepted")
		}
	}
	valid := map[string]any{"record": serviceNowIncidentID, "note": strings.Repeat("x", 4000)}
	staged, err := plan.PrepareSafeOutput("append-incident-work-note", valid)
	if err != nil {
		t.Fatal("exact note length limit must be accepted", err)
	}
	if _, err := plan.ExecuteSafeOutput(t.Context(), runtime, *staged); err == nil {
		t.Fatal("agent-phase write accepted")
	}
	runtime.Phase, runtime.AllowWrites = "safe-output", false
	if _, err := plan.ExecuteSafeOutput(t.Context(), runtime, *staged); err == nil {
		t.Fatal("write without explicit host authorization accepted")
	}
	runtime.Phase, runtime.AllowedOrigins = "agent", nil
	if _, err := plan.Execute(t.Context(), runtime, "mcp", "get-incident", map[string]any{"record": serviceNowIncidentID}); err == nil {
		t.Fatal("unapproved instance accepted")
	}
	if secretCalls != 0 {
		t.Fatal("denied requests resolved credentials")
	}
}

func TestServiceNowEventFixtures(t *testing.T) {
	plan := serviceNowPlan(t, "")
	if !errors.Is(plan.ActivateTriggers(), ErrIngressUnavailable) {
		t.Fatal("custom ServiceNow ingress must not be activated by a declaration")
	}
	for _, name := range []string{"incident", "requested-item"} {
		data, err := os.ReadFile("testdata/providers/servicenow/" + name + "-event.json")
		if err != nil {
			t.Fatal(err)
		}
		projection, err := plan.ProjectEvent(name+"-updated", "record.updated", data)
		if err != nil || projection.Operational || !projection.Untrusted || len(projection.Fields) != 2 {
			t.Fatalf("unexpected event projection: %+v %v", projection, err)
		}
		for _, replacement := range [][2]string{
			{"record.updated", "record.deleted"},
			{"https://example.service-now.com", "https://evil.example"},
			{`"table": "` + map[string]string{"incident": "incident", "requested-item": "sc_req_item"}[name] + `"`, `"table": "sys_user"`},
			{projection.Fields["subject"].(string), "11111111111111111111111111111111"},
		} {
			invalid := strings.ReplaceAll(string(data), replacement[0], replacement[1])
			if _, err := plan.ProjectEvent(name+"-updated", "record.updated", []byte(invalid)); err == nil {
				t.Fatal("out-of-scope event fixture accepted")
			}
		}
		if _, err := plan.ProjectEvent(name+"-updated", "record.deleted", data); err == nil {
			t.Fatal("wrong native event accepted")
		}
	}
}

func TestServiceNowResponseFailures(t *testing.T) {
	tests := []struct {
		name, body string
		status     int
	}{
		{"acl-missing-state", `{"result":{"sys_id":"` + serviceNowIncidentID + `","number":"INC0009009","sys_mod_count":"17"}}`, 200},
		{"acl-empty-state", `{"result":{"sys_id":"` + serviceNowIncidentID + `","number":"INC0009009","state":"","sys_mod_count":"17"}}`, 200},
		{"nested-private-state", `{"result":{"sys_id":"` + serviceNowIncidentID + `","number":"INC0009009","state":{"private":"must-not-project"},"sys_mod_count":"17"}}`, 200},
		{"nested-private-number", `{"result":{"sys_id":"` + serviceNowIncidentID + `","number":{"private":"must-not-project"},"state":"2","sys_mod_count":"17"}}`, 200},
		{"numeric-revision", `{"result":{"sys_id":"` + serviceNowIncidentID + `","number":"INC0009009","state":"2","sys_mod_count":17}}`, 200},
		{"different-record", `{"result":{"sys_id":"` + serviceNowRequestedItemID + `","number":"INC0009009","state":"2","sys_mod_count":"17"}}`, 200},
		{"http-success-error-envelope", `{"error":{"message":"private-error"}}`, 200},
		{"unauthorized", `{"error":{"message":"private-error"}}`, 401},
		{"acl-denied", `{"error":{"message":"private-error"}}`, 403},
		{"oversize", strings.Repeat("x", 8193), 200},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.WriteHeader(test.status)
				if _, err := io.WriteString(w, test.body); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			plan := serviceNowPlan(t, server.URL)
			result, err := plan.Execute(t.Context(), serviceNowRuntime(server.Client(), server.URL, "context"), "context", "incident-metadata", map[string]any{"record": serviceNowIncidentID})
			if err == nil || result != nil || strings.Contains(err.Error(), "private-error") || requests.Load() != 1 {
				t.Fatalf("invalid response must fail once without exposing record text: %v", err)
			}
		})
	}
}

func TestServiceNowResponseMatchesRequestedRecord(t *testing.T) {
	const otherID = "11111111111111111111111111111111"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.WriteString(w, `{"result":{"sys_id":"`+otherID+`","number":"INC0009010","state":"2","sys_mod_count":"17"}}`); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	plan := serviceNowPlan(t, server.URL)
	connection := plan.connection
	connection.Resources["incidents"] = append(connection.Resources["incidents"], otherID)
	plan = writePlan(t, plan.spec, connection, plan.binding)
	for _, capability := range []string{"mcp", "context", "safe-output"} {
		name, phase := "get-incident", "agent"
		args := map[string]any{"record": serviceNowIncidentID}
		switch capability {
		case "context":
			name, phase = "incident-metadata", "context"
		case "safe-output":
			name, phase = "append-incident-work-note", "safe-output"
			args["note"] = "new note"
		}
		result, err := plan.Execute(t.Context(), serviceNowRuntime(server.Client(), server.URL, phase), capability, name, args)
		if err == nil || result != nil {
			t.Fatal("response for a different approved record must not satisfy the requested target")
		}
	}
}

func TestServiceNowAppendNotRetried(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var writes atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writes.Add(1)
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(status)
				if _, err := io.WriteString(w, `{"error":{"message":"private-journal"}}`); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			plan := serviceNowPlan(t, server.URL)
			staged, err := plan.PrepareSafeOutput("append-incident-work-note", map[string]any{"record": serviceNowIncidentID, "note": "new internal note"})
			if err != nil {
				t.Fatal(err)
			}
			result, err := plan.ExecuteSafeOutput(t.Context(), serviceNowRuntime(server.Client(), server.URL, "safe-output"), *staged)
			if err == nil || result != nil || writes.Load() != 1 || strings.Contains(err.Error(), "private-journal") {
				t.Fatal("failed journal append was retried, leaked, or reported as successful")
			}
		})
	}
}
