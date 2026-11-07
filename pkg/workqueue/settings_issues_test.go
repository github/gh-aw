package workqueue

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSettingsIssuesDisabledByDefault(t *testing.T) {
	for _, section := range []string{"", `{}`, `{"issues":false}`} {
		settings, err := ParseSettings([]byte(section))
		if err != nil || settings.Issues != nil {
			t.Fatalf("Issues projection was implicitly enabled: %+v %v", settings.Issues, err)
		}
	}
}

func TestSettingsIssuesEnabledWithoutPolicyAuthority(t *testing.T) {
	for _, test := range []struct {
		section string
		label   string
	}{
		{`{"issues":true}`, "work"},
		{`{"issues":{}}`, "work"},
		{`{"issues":{"label":"tasks"}}`, "tasks"},
		{`{"issues":{"title_prefix":"[tasks]"}}`, "work"},
		{`{"issues":{"label":"  work  "}}`, "  work  "},
		{`{"issues":{"label":"` + strings.Repeat("a", 33) + `"}}`, strings.Repeat("a", 33)},
		{`{"issues":{"label":"` + strings.Repeat("😀", 8) + `a"}}`, strings.Repeat("😀", 8) + "a"},
	} {
		t.Run(test.section, func(t *testing.T) {
			settings, err := ParseSettings([]byte(test.section))
			if err != nil || settings.Issues == nil || settings.Issues.Label != test.label {
				t.Fatalf("Issues preferences did not retain the configured label: %+v %v", settings.Issues, err)
			}
			wantPrefix := "[work queue]"
			if strings.Contains(test.section, "title_prefix") {
				wantPrefix = "[tasks]"
			}
			if settings.Issues.TitlePrefix != wantPrefix {
				t.Fatalf("Issues preferences did not retain the title prefix: %+v", settings.Issues)
			}
			if settings.Issues.OrphanedAfterDays != 30 {
				t.Fatalf("Issues preferences did not retain the orphan age default: %+v", settings.Issues)
			}
			policy := DefaultPolicy(testPrincipal, testRepository)
			result, err := settings.Apply(policy)
			if err != nil || !sameJSON(result, policy) {
				t.Fatalf("Issues preferences changed Policy authority/scheduling: %+v %v", result, err)
			}
			encoded, err := json.Marshal(settings)
			if err != nil || !strings.Contains(string(encoded), `"issues":{"label":`) {
				t.Fatal("normalized settings lost enabled Issues preferences")
			}
		})
	}
}

func TestSettingsIssuesMalformedConfiguration(t *testing.T) {
	for input, property := range map[string]string{
		`{"issues":null}`:                                               "work_queue.issues",
		`{"issues":[]}`:                                                 "work_queue.issues",
		`{"issues":0}`:                                                  "work_queue.issues",
		`{"issues":"true"}`:                                             "work_queue.issues",
		`{"issues":{"label":null}}`:                                     "work_queue.issues.label",
		`{"issues":{"label":true}}`:                                     "work_queue.issues.label",
		`{"issues":{"label":""}}`:                                       "work_queue.issues.label",
		`{"issues":{"label":" \t "}}`:                                   "work_queue.issues.label",
		`{"issues":{"label":"${{ vars.LABEL }}"}}`:                      "work_queue.issues.label",
		`{"issues":{"label":"work\n"}}`:                                 "work_queue.issues.label",
		`{"issues":{"label":"work\u0085"}}`:                             "work_queue.issues.label",
		`{"issues":{"label":"` + strings.Repeat("a", 34) + `"}}`:        "work_queue.issues.label",
		`{"issues":{"label":"` + strings.Repeat("😀", 9) + `"}}`:         "work_queue.issues.label",
		`{"issues":{"title_prefix":null}}`:                              "work_queue.issues.title_prefix",
		`{"issues":{"title_prefix":""}}`:                                "work_queue.issues.title_prefix",
		`{"issues":{"title_prefix":"${{ vars.TITLE }}"}}`:               "work_queue.issues.title_prefix",
		`{"issues":{"title_prefix":"queue\n"}}`:                         "work_queue.issues.title_prefix",
		`{"issues":{"title_prefix":"queue\u0085"}}`:                     "work_queue.issues.title_prefix",
		`{"issues":{"title_prefix":"` + strings.Repeat("a", 81) + `"}}`: "work_queue.issues.title_prefix",
		`{"issues":{"orphaned_after_days":null}}`:                       "work_queue.issues.orphaned_after_days",
		`{"issues":{"orphaned_after_days":0}}`:                          "work_queue.issues.orphaned_after_days",
		`{"issues":{"orphaned_after_days":3651}}`:                       "work_queue.issues.orphaned_after_days",
		`{"issues":{"orphaned_after_days":1.5}}`:                        "work_queue.issues.orphaned_after_days",
		`{"issues":{"orphaned_after_days":"30"}}`:                       "work_queue.issues.orphaned_after_days",
		`{"issues":{"tracking-label":"work"}}`:                          "work_queue.issues.tracking-label",
		`{"issues":{"status-label-prefix":"queue"}}`:                    "work_queue.issues.status-label-prefix",
		`{"issues":{"routes":[]}}`:                                      "work_queue.issues.routes",
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := ParseSettings([]byte(input)); err == nil || !strings.Contains(err.Error(), property) ||
				!strings.Contains(err.Error(), ".github/workflows/aw.json") {
				t.Fatalf("malformed Issues preferences were silently accepted: %v", err)
			}
		})
	}
}

func TestSettingsIssuesOrphanAgeCanBeConfigured(t *testing.T) {
	settings, err := ParseSettings([]byte(`{"issues":{"orphaned_after_days":7}}`))
	if err != nil || settings.Issues == nil || settings.Issues.OrphanedAfterDays != 7 {
		t.Fatalf("configured orphan age was not retained: %+v %v", settings.Issues, err)
	}
}
