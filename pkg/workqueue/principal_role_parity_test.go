package workqueue

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestNativePrincipalAndRequestRoleParity(t *testing.T) {
	type parityCase struct {
		Name   string `json:"name"`
		Scope  string `json:"scope"`
		Actor  Actor  `json:"actor"`
		Policy Policy `json:"policy"`
		Kind   string `json:"kind"`
		Code   string `json:"code"`
	}
	var cases []parityCase
	errorCode := func(err error) string {
		if err == nil {
			return "allowed"
		}
		code, _, _ := strings.Cut(err.Error(), ":")
		return code
	}
	for _, principal := range []string{"1001", strings.Repeat("9", 256), "operator", "0", "001", "-1", "1.0", "1e3"} {
		allowed := principal == "1001" || principal == strings.Repeat("9", 256)
		for _, scope := range []string{"actor", "profile", "producer"} {
			actor := testActor("administrator")
			policy := DefaultPolicy(testPrincipal, testRepository)
			expected := "allowed"
			var err error
			switch scope {
			case "actor":
				actor.Principal = principal
				if !allowed {
					expected = "actor_unauthorized"
				}
				err = validateActorOrigin(actor)
			case "profile":
				pool := policy.Pools["default"]
				profile := pool.Profiles["default"]
				profile.Principal = principal
				pool.Profiles["default"] = profile
				policy.Pools["default"] = pool
				if !allowed {
					expected = "policy_invalid"
				}
				err = ValidatePolicy(policy)
			case "producer":
				policy.Producers[principal] = policy.Producers[testPrincipal]
				if principal != testPrincipal {
					delete(policy.Producers, testPrincipal)
				}
				if !allowed {
					expected = "policy_invalid"
				}
				err = ValidatePolicy(policy)
			}
			if got := errorCode(err); got != expected {
				t.Fatalf("Go %s principal %q expected %s, got %s", scope, principal, expected, got)
			}
			cases = append(cases, parityCase{
				Name: scope + "/" + principal, Scope: scope, Actor: actor, Policy: policy, Code: expected,
			})
		}
	}
	fixtureData, err := os.ReadFile("../../specs/work-queue/fixtures/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		RequestRoles map[string][]string `json:"request_roles"`
	}
	if err := json.Unmarshal(fixtureData, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.RequestRoles) != 5 {
		t.Fatal("independent request-role contract is incomplete")
	}
	kinds := []string{"policy", "control", "submit", "dispatch_next", "observe", "finish", "dispatch",
		"release", "result", "delivery_failure", "cancel_claim", "cancel_work", "unknown"}
	for role, allowed := range fixture.RequestRoles {
		actor := testActor(role)
		if role == "worker" {
			actor.Workflow, actor.RunID, actor.RunAttempt = ".github/workflows/worker.lock.yml", "42", 1
			actor.DispatchID, actor.ClaimHandle = "dispatch", "h1"
		}
		for _, kind := range kinds {
			expected := "actor_unauthorized"
			if slices.Contains(allowed, kind) {
				expected = "allowed"
			}
			if got := errorCode(validateRequestRole(actor, kind)); got != expected {
				t.Fatalf("Go role %s/%s expected %s, got %s", role, kind, expected, got)
			}
			cases = append(cases, parityCase{
				Name: role + "/" + kind, Scope: "role", Actor: actor, Kind: kind, Code: expected,
			})
		}
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Log("JavaScript principal/role parity tooling unavailable; all native contract checks passed")
		return
	}
	const script = `
const fs = require("node:fs");
const { validateActor, validatePolicy, validateRequestRole } =
  require("../../actions/setup/js/work_queue_policy.cjs");
const cases = JSON.parse(fs.readFileSync(0, "utf8"));
process.stdout.write(JSON.stringify(cases.map(test => {
  try {
    if (test.scope === "actor") validateActor(test.actor);
    else if (test.scope === "role") validateRequestRole(test.actor, test.kind);
    else validatePolicy(test.policy);
    return "allowed";
  } catch (error) {
    if (typeof error.code !== "string") throw error;
    return error.code;
  }
})));
`
	input, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, node, "-e", script)
	command.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("JavaScript principal/role parity: %v\n%s", err, stderr.String())
	}
	var codes []string
	if err := json.Unmarshal(output, &codes); err != nil {
		t.Fatal(err)
	}
	if len(codes) != len(cases) {
		t.Fatalf("expected %d independent cases, received %d", len(cases), len(codes))
	}
	for index, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			if codes[index] != test.Code {
				t.Fatalf("expected %s in both engines, JavaScript returned %s", test.Code, codes[index])
			}
		})
	}
}
