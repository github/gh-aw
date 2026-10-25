//go:build !integration

package workflow

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestAICLimitParserObjectErrors(t *testing.T) {
	for _, input := range []string{
		`{}`, `{"value":null}`, `{"value":0}`, `{"value":-1}`,
		`{"value":"-1"}`, `{"value":false}`, `{"value":{}}`,
	} {
		t.Run(input, func(t *testing.T) {
			limit, err := parseFrontmatterAICLimit(json.RawMessage(input), true)
			if err == nil || limit != nil {
				t.Fatalf("expected an object value error, got limit %v, error %v", limit, err)
			}
			for _, hint := range []string{
				"object 'value'", "positive integer", "K/M suffix string",
				"GitHub Actions expression", "{value: 20K}",
				"scalar max-daily-ai-credits: -1",
			} {
				if !strings.Contains(err.Error(), hint) {
					t.Errorf("object error %q lacks guidance %q", err, hint)
				}
			}
		})
	}
}

func TestAICLimitParserAcceptedForms(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  string
	}{
		{`20`, "20"},
		{`-1`, "-1"},
		{`"-1"`, "-1"},
		{`"20K"`, "20000"},
		{`"2m"`, "2000000"},
		{`"9223372036854775807"`, "9223372036854775807"},
		{`{"value":"20K"}`, "20000"},
		{`{"value":"${{ inputs.credits }}"}`, "${{ inputs.credits }}"},
	} {
		t.Run(tt.input, func(t *testing.T) {
			limit, err := parseFrontmatterAICLimit(json.RawMessage(tt.input), true)
			if err != nil || limit == nil || limit.String() != tt.want {
				t.Fatalf("got limit %v, error %v; want %q", limit, err, tt.want)
			}
		})
	}
	for _, input := range []json.RawMessage{nil, json.RawMessage("null")} {
		if limit, err := parseFrontmatterAICLimit(input, true); limit != nil || err != nil {
			t.Fatalf("absent limit: got limit %v, error %v", limit, err)
		}
	}
}

func TestAICLimitParserNormalization(t *testing.T) {
	for _, tt := range []struct {
		input any
		want  string
	}{
		{20, "20"},
		{" +20k ", "20000"},
		{"2M", "2000000"},
		{"9223372036854775K", "9223372036854775000"},
		{"9223372036854M", "9223372036854000000"},
		{"9223372036854776K", ""},
		{"9223372036855M", ""},
		{"9223372036854775808", ""},
		{"0", ""},
		{"-1", ""},
		{"0K", ""},
		{"-2M", ""},
		{"K", ""},
		{"1.5K", ""},
		{"1_000", ""},
		{"", ""},
		{nil, ""},
	} {
		got, ok := normalizePositiveAICLimit(tt.input)
		if got != tt.want || ok != (tt.want != "") {
			t.Errorf("normalizePositiveAICLimit(%v) = (%q, %v); want (%q, %v)",
				tt.input, got, ok, tt.want, tt.want != "")
		}
	}
}

func TestAICLimitParserDebugRedactsInput(t *testing.T) {
	const helperEnv = "GH_AW_AIC_REDACTION_TEST"
	const secret = "accidental-secret-credit-value-123456"
	if os.Getenv(helperEnv) == "1" {
		if !aicLimitLog.Enabled() {
			t.Fatal("DEBUG must enable the credit parser logger")
		}
		for _, input := range []string{
			`"` + secret + `"`,
			`{"value":"` + secret + `"}`,
			`{"` + secret + `":true}`,
			`["` + secret + `"]`,
			`"` + secret + `K"`,
			`"9223372036854775808"`,
			`"9223372036854776K"`,
		} {
			_, err := parseFrontmatterAICLimit(json.RawMessage(input), true)
			if err == nil {
				t.Fatal("expected invalid credit input to fail")
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "922337203685477") {
				t.Fatal("credit parser error exposed input")
			}
		}
		if _, err := parseFrontmatterAICLimit(json.RawMessage(`"123456789K"`), true); err != nil {
			t.Fatal("expected valid suffix input to succeed")
		}
		return
	}

	// DEBUG is read at package initialization, so enable it in a fresh process.
	cmd := exec.Command(os.Args[0], "-test.run=^TestAICLimitParserDebugRedactsInput$")
	cmd.Env = append(os.Environ(), helperEnv+"=1", "DEBUG=*", "DEBUG_COLORS=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("redaction subprocess failed: %v\n%s\n%s", err, &stdout, &stderr)
	}
	if !strings.Contains(stderr.String(), "workflow:aic_limit_parser") {
		t.Fatal("expected enabled credit parser debug output")
	}
	for _, sensitive := range []string{secret, "accidental-secret", "922337203685477", "123456789"} {
		if strings.Contains(stderr.String(), sensitive) {
			t.Errorf("DEBUG stderr exposed input %q", sensitive)
		}
	}
}
