package workqueue

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestSettingsApplyPreservesCompilerTemplates(t *testing.T) {
	section := `{"concurrency":3,"pending_limit":30,"pools":{"reports":{"per_account_limit":1}},"issues":{"label":"work"}}`
	settings, err := ParseSettings([]byte(section))
	if err != nil {
		t.Fatal(err)
	}
	policy := DefaultPolicy("", "${{ github.repository }}")
	policy.Authorization = "aw"
	policy.Producers = map[string]ProducerRule{}
	pool := policy.Pools["default"]
	profile := pool.Profiles["default"]
	profile.Principal, profile.Ref = "", "${{ github.sha }}"
	pool.Profiles["default"] = profile
	policy.Pools["default"] = pool
	result, err := settings.Apply(policy)
	if err != nil {
		t.Fatalf("Apply prematurely validated compiler-owned route templates: %v", err)
	}
	if result.Pools["reports"].Profiles["default"].Ref != "${{ github.sha }}" ||
		result.Pools["reports"].Profiles["default"].EffectScope != "${{ github.repository }}" ||
		result.Pools["reports"].AllowedRepositories[0] != "${{ github.repository }}" ||
		result.Pools["reports"].PerAccountLimit != 1 {
		t.Fatal("Apply changed compiler templates or omitted scheduling overrides")
	}
	input, _ := json.Marshal(map[string]any{"policy": policy, "section": section})
	command := exec.Command("node", "-e", `
const fs = require("node:fs");
const {parseSettings, applySettings} = require("../../actions/setup/js/work_queue_settings.cjs");
const input = JSON.parse(fs.readFileSync(0, "utf8"));
process.stdout.write(JSON.stringify(applySettings(input.policy, parseSettings(input.section))));
`)
	command.Stdin = strings.NewReader(string(input))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("JS Apply prematurely validated compiler templates: %v %s", err, output)
	}
	var actual Policy
	if err := json.Unmarshal(output, &actual); err != nil || !sameJSON(result, actual) {
		t.Fatalf("Go/JS template-preserving scheduling differs: %v %s", err, output)
	}
}

func TestSettingsDefaultOnly(t *testing.T) {
	for _, data := range []string{"", "{}", `{"retry":{},"pools":{},"accounting_weights":{}}`} {
		settings, err := ParseSettings([]byte(data))
		if err != nil {
			t.Fatal(err)
		}
		policy := DefaultPolicy(testPrincipal, testRepository)
		policy.Pools["default"] = PoolPolicy{
			DefaultProfile: "default", Profiles: policy.Pools["default"].Profiles,
			LogicalLimit: 2, NativeLimit: 3, PerAccountLimit: 2,
			Retry: RetryPolicy{MaxAttempts: 1, BackoffMS: 1},
		}
		result, err := settings.Apply(policy)
		if err != nil {
			t.Fatal(err)
		}
		pool := result.Pools["default"]
		if result.Mode != "weighted-priority" || pool.LogicalLimit != 16 || pool.NativeLimit != 16 ||
			result.Limits.PendingNodes != 4096 || pool.Retry.MaxAttempts != 3 || pool.Retry.BackoffMS != 30000 ||
			pool.Profiles["default"].MaxClaims != 1 || pool.PerAccountLimit != 0 ||
			pool.Profiles["default"].Workflow != policy.Pools["default"].Profiles["default"].Workflow {
			t.Fatalf("defaults changed authority or omitted scheduling defaults: %+v", result)
		}
		if policy.Pools["default"].LogicalLimit != 2 {
			t.Fatal("settings mutated the installed policy")
		}
	}
}

func TestSettingsOverridesInheritApprovedRoutes(t *testing.T) {
	settings, err := ParseSettings([]byte(`{
		"mode":"strict-priority","class_weights":[1,2,3,4,5],
		"accounting_weights":{"team":3},"concurrency":32,"pending_limit":42,
		"retry":{"max_attempts":4,"backoff_seconds":60},
		"pools":{"default":{"concurrency":2},"reviews":{"per_account_limit":1,"retry":{"max_attempts":7}}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	policy := DefaultPolicy(testPrincipal, testRepository)
	original, _ := json.Marshal(policy)
	secondary := policy.Pools["default"]
	policy.Pools["existing"] = secondary
	result, err := settings.Apply(policy)
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != "strict-priority" || result.Limits.PendingNodes != 42 ||
		result.AccountingWeights[""] != 1 || result.AccountingWeights["team"] != 3 ||
		result.Pools["default"].NativeLimit != 2 || result.Pools["existing"].NativeLimit != 32 ||
		result.Pools["reviews"].Retry.MaxAttempts != 7 || result.Pools["reviews"].Retry.BackoffMS != 60000 ||
		result.Pools["reviews"].PerAccountLimit != 1 || result.Pools["default"].PerAccountLimit != 0 {
		t.Fatalf("settings did not apply deterministic inheritance: %+v", result)
	}
	if !sameJSON(result.Pools["reviews"].Profiles, policy.Pools["default"].Profiles) ||
		!sameJSON(result.Producers, policy.Producers) {
		t.Fatal("scheduling configuration authored profile/producer authority")
	}
	result.Pools["reviews"].Profiles["default"] = WorkerProfile{}
	result.Producers[testPrincipal].Pools[0] = "reviews"
	delete(policy.Pools, "existing")
	after, _ := json.Marshal(policy)
	if string(original) != string(after) {
		t.Fatal("Apply aliased approved policy maps")
	}
}

func TestSettingsRejectMalformedConfiguration(t *testing.T) {
	tests := map[string]string{
		"null": "work_queue", "[]": "work_queue", "true": "work_queue", " ": "work_queue",
		"{} {}": "work_queue", `{"concurrency":0}`: "work_queue.concurrency",
		`{"concurrency":null}`: "work_queue.concurrency", `{"concurrency":"16"}`: "work_queue.concurrency",
		`{"concurrency":1.5}`: "work_queue.concurrency", `{"concurrency":4097}`: "work_queue.concurrency",
		`{"concurrency":1e1}`: "work_queue.concurrency",
		`{"pending_limit":0}`: "work_queue.pending_limit", `{"pending_limit":4097}`: "work_queue.pending_limit",
		`{"mode":null}`: "work_queue.mode", `{"mode":"disabled"}`: "work_queue.mode",
		`{"class_weights":null}`: "work_queue.class_weights", `{"class_weights":[1,2,3,4,0]}`: "work_queue.class_weights[4]",
		`{"accounting_weights":null}`:       "work_queue.accounting_weights",
		`{"accounting_weights":{"":2}}`:     "work_queue.accounting_weights.",
		`{"accounting_weights":{"team":0}}`: "work_queue.accounting_weights.team",
		`{"retry":null}`:                    "work_queue.retry", `{"retry":{"max_attempts":17}}`: "work_queue.retry.max_attempts",
		`{"retry":{"backoff_seconds":0}}`:    "work_queue.retry.backoff_seconds",
		`{"retry":{"backoff_seconds":3601}}`: "work_queue.retry.backoff_seconds",
		`{"retry":{"backoff_ms":30}}`:        "work_queue.retry.backoff_ms",
		`{"pools":[]}`:                       "work_queue.pools", `{"pools":{"default":null}}`: "work_queue.pools.default",
		`{"pools":{"default":{"concurrency":0}}}`:          "work_queue.pools.default.concurrency",
		`{"pools":{"default":{"per_account_limit":0}}}`:    "work_queue.pools.default.per_account_limit",
		`{"pools":{"default":{"per_account_limit":4097}}}`: "work_queue.pools.default.per_account_limit",
		`{"pools":{"default":{"per_account_limit":null}}}`: "work_queue.pools.default.per_account_limit",
		`{"pools":{"default":{"per_account_limit":"1"}}}`:  "work_queue.pools.default.per_account_limit",
		`{"pools":{"default":{"profiles":{}}}}`:            "work_queue.pools.default.profiles",
		`{"producers":{}}`:                                 "work_queue.producers", `{"principal":"1"}`: "work_queue.principal",
		`{"concurrency":1,"concurrency":2}`:   "work_queue.concurrency",
		`{"accounting_weights":{"\ud800":1}}`: "work_queue",
	}
	for input, property := range tests {
		t.Run(input, func(t *testing.T) {
			_, err := ParseSettings([]byte(input))
			if err == nil || !strings.Contains(err.Error(), property) || !strings.Contains(err.Error(), ".github/workflows/aw.json") {
				t.Fatalf("malformed setting did not have actionable property error: %v", err)
			}
		})
	}
	for _, input := range []string{
		`{"concurrency":1,"pending_limit":1,"retry":{"max_attempts":1,"backoff_seconds":1},"pools":{"default":{"per_account_limit":1}}}`,
		`{"concurrency":4096,"pending_limit":4096,"retry":{"max_attempts":16,"backoff_seconds":3600},"pools":{"default":{"per_account_limit":4096}}}`,
	} {
		if _, err := ParseSettings([]byte(input)); err != nil {
			t.Fatalf("valid bounds rejected: %v", err)
		}
	}
}

func TestRepositorySettingsUsesOnlyStandardSection(t *testing.T) {
	if result, err := ParseRepositorySettings(nil); err != nil || result.Concurrency != 16 {
		t.Fatalf("missing file lost defaults: %+v %v", result, err)
	}
	for _, input := range []string{`{}`, `{"engine":"copilot"}`} {
		result, err := ParseRepositorySettings([]byte(input))
		if err != nil || result.Concurrency != 16 {
			t.Fatalf("missing file/section lost defaults: %+v %v", result, err)
		}
	}
	result, err := ParseRepositorySettings([]byte(`{"engine":"copilot","work_queue":{"concurrency":2}}`))
	if err != nil || result.Concurrency != 2 {
		t.Fatalf("section extraction failed: %+v %v", result, err)
	}
	for _, input := range []string{"", `null`, `[]`, `{"work_queue":null}`, `{"work_queue":{"routes":[]}}`} {
		if _, err := ParseRepositorySettings([]byte(input)); err == nil {
			t.Fatalf("invalid repository settings silently defaulted: %s", input)
		}
	}
}

func TestSettingsJavaScriptParity(t *testing.T) {
	for _, section := range []string{
		`{}`,
		`{"concurrency":1,"pending_limit":1,"retry":{"max_attempts":1,"backoff_seconds":1}}`,
		`{"concurrency":4096,"pending_limit":4096,"retry":{"max_attempts":16,"backoff_seconds":3600}}`,
		`{"mode":"strict-priority","class_weights":[1,2,3,4,5],"accounting_weights":{"__proto__":4,"team":2},"concurrency":42,"retry":{"max_attempts":4},"pools":{"__proto__":{"concurrency":2},"default":{"retry":{"backoff_seconds":60}},"review":{"per_account_limit":1}}}`,
		`{"issues":false}`,
		`{"issues":true}`,
		`{"issues":{"label":"tasks"}}`,
	} {
		settings, err := ParseSettings([]byte(section))
		if err != nil {
			t.Fatal(err)
		}
		policy := DefaultPolicy(testPrincipal, testRepository)
		expected, err := settings.Apply(policy)
		if err != nil {
			t.Fatal(err)
		}
		input, _ := json.Marshal(map[string]any{"section": section, "policy": policy})
		command := exec.Command("node", "-e", `
const fs = require("node:fs");
const {parseSettings, applySettings} = require("../../actions/setup/js/work_queue_settings.cjs");
const input = JSON.parse(fs.readFileSync(0, "utf8"));
const settings = parseSettings(input.section);
process.stdout.write(JSON.stringify({settings, policy: applySettings(input.policy, settings)}));
`)
		command.Stdin = strings.NewReader(string(input))
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("JS settings failed: %v %s", err, output)
		}
		var actual struct {
			Settings Settings `json:"settings"`
			Policy   Policy   `json:"policy"`
		}
		if err := json.Unmarshal(output, &actual); err != nil {
			t.Fatal(err)
		}
		if !sameJSON(settings, actual.Settings) || !sameJSON(expected, actual.Policy) {
			t.Fatalf("Go/JS scheduling settings differ for %s:\n%s", section, output)
		}
	}

}

func TestSettingsAWPolicyBuilderParity(t *testing.T) {
	section := `{"concurrency":4,"pending_limit":64,"retry":{"backoff_seconds":60},"pools":{"reviews":{"concurrency":2,"per_account_limit":1}},"issues":{"label":"tasks"}}`
	settings, err := ParseSettings([]byte(section))
	if err != nil {
		t.Fatal(err)
	}
	policy := DefaultPolicy("", testRepository)
	policy.Authorization = "aw"
	policy.Producers = map[string]ProducerRule{}
	pool := policy.Pools["default"]
	template := pool.Profiles["default"]
	template.Principal, template.Ref = "", strings.Repeat("a", 40)
	pool.Profiles = map[string]WorkerProfile{}
	for _, name := range []string{"alpha", "zeta"} {
		profile := template
		profile.Workflow = ".github/workflows/" + name + ".lock.yml"
		pool.Profiles[name] = profile
	}
	pool.DefaultProfile = "alpha"
	policy.Pools["default"] = pool
	expected, err := settings.Apply(policy)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("node", "-e", `
	const fs = require("node:fs");
	const {buildAWPolicy} = require("../../actions/setup/js/work_queue_settings.cjs");
	const settings = JSON.parse(fs.readFileSync(0, "utf8"));
	process.stdout.write(JSON.stringify(buildAWPolicy({
	  repository: "owner/repo", ref: "a".repeat(40), workflows: ["zeta", "alpha"], settings,
	})));
	`)
	command.Stdin = strings.NewReader(section)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("JavaScript AW policy construction failed: %v %s", err, output)
	}
	var actual Policy
	if err := json.Unmarshal(output, &actual); err != nil {
		t.Fatal(err)
	}
	if !sameJSON(expected, actual) {
		t.Fatalf("Go/JS AW policy construction differs:\n%s", output)
	}
}
