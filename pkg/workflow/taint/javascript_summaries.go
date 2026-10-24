package taint

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"sync"

	"github.com/github/gh-aw/pkg/logger"
)

var javascriptSummariesLog = logger.New("workflow:taint:javascript_summaries")

//go:embed javascript/summaries.json
var javaScriptSummaryData []byte

type javaScriptModuleSummary struct {
	SHA256       string            `json:"sha256"`
	Dependencies []string          `json:"dependencies"`
	Exports      map[string]string `json:"exports"`
}

type javaScriptSummaryCatalog struct {
	Version int                                `json:"version"`
	Modules map[string]javaScriptModuleSummary `json:"modules"`
}

var javaScriptSummaries = sync.OnceValues(func() (javaScriptSummaryCatalog, error) {
	var catalog javaScriptSummaryCatalog
	if err := json.Unmarshal(javaScriptSummaryData, &catalog); err != nil || catalog.Version != 1 {
		return catalog, errors.New("JavaScript API summary catalog version is unavailable")
	}
	return catalog, nil
})

func (j *javascript) summaryModule(n *JavaScriptNode, target string, source []byte) jsValue {
	catalog, err := javaScriptSummaries()
	if err != nil {
		javascriptSummariesLog.Printf("JavaScript API summary catalog unavailable: %v", err)
		j.gap(n, "JavaScript API summary catalog is unavailable")
		return jsValue{}
	}
	if !j.verifySummary(catalog, target, source, make(map[string]bool)) {
		javascriptSummariesLog.Printf("Summary verification failed for module %q", target)
		j.gap(n, "JavaScript runtime API summary is unavailable or its source/dependency hash changed; use deep mode to traverse runtime sources")
		return jsValue{}
	}
	result := jsValue{fields: make(map[string]jsValue)}
	for name, model := range catalog.Modules[target].Exports {
		result.fields[name] = jsValue{ref: "gh-aw-summary:" + model}
	}
	j.modules[target] = result
	return result
}

func (j *javascript) verifySummary(catalog javaScriptSummaryCatalog, target string, source []byte, seen map[string]bool) bool {
	if seen[target] {
		return true
	}
	contract, exists := catalog.Modules[target]
	hash := sha256.Sum256(source)
	if !exists || contract.SHA256 != hex.EncodeToString(hash[:]) {
		return false
	}
	seen[target] = true
	for _, dependency := range contract.Dependencies {
		source, err := javascriptReadSource(j.options.Files, dependency)
		if err != nil || !j.verifySummary(catalog, dependency, source, seen) {
			return false
		}
	}
	return true
}

// These reviewed transfer functions describe effects, not cached runtime values.
// Registering or escaping a secret never declassifies a later disclosure.
func (j *javascript) callSummary(n *JavaScriptNode, model string, args []jsValue) jsValue { //nolint:largefunc // Keep reviewed transfer functions together for catalog auditing.
	if j.summaryDepth >= 32 {
		j.gap(n, "recursive JavaScript API summary callback is not modeled")
		return jsValue{}
	}
	j.summaryDepth++
	defer func() { j.summaryDepth-- }()
	first, _ := sliceAt(args, 0)
	if jsSummaryInputHasCallbacks(first, make(map[uintptr]bool)) {
		j.gap(n, "JavaScript API summary does not model callable input properties or coercion callbacks")
	}
	switch model {
	case "escaped-command-value":
		if first.kind != jsString {
			j.gap(n, "JavaScript escaping API summary requires a proven string; custom member callbacks are not modeled")
			return jsValue{sources: first.taint()}
		}
		return jsValue{sources: first.taint(), kind: jsString, lines: jsSingleLine}
	case "mask-secret":
		if first.kind == jsUnknown && (first.fields != nil || first.fn != nil || first.ref != "") {
			j.gap(n, "JavaScript masking API summary does not model custom string coercion callbacks")
		}
		j.summaryCoreCallback(n, "setSecret", []jsValue{first})
		if j.input.CaptureStdout {
			j.writeStdout(n, first.taint())
		}
		return jsValue{}
	case "argument-object", "error-message":
		result := jsValue{sources: first.taint()}
		if model == "error-message" {
			result.kind = jsString
		}
		return result
	case "argument-array":
		return jsValue{sources: first.taint(), kind: jsArray}
	case "otlp-values":
		j.summaryCoreCallback(n, "warning", []jsValue{jsStringLiteral("Unable to collect OTLP endpoint credentials: invalid endpoint JSON")})
		var sources value
		for _, name := range []string{"OTEL_EXPORTER_OTLP_HEADERS", "GH_AW_OTLP_ALL_HEADERS", "GH_AW_OTLP_ENDPOINTS"} {
			sources = union(sources, j.summaryEnvironmentValue(name).taint())
		}
		return jsValue{sources: sources, kind: jsArray}
	case "boolean":
		return jsValue{}
	case "git-auth-env":
		encoded := jsValue{sources: first.taint(), kind: jsString, lines: jsSingleLine}
		if callback, exists := sliceAt(args, 1); exists {
			j.callValue(n, callback, []jsValue{encoded})
		}
		server := j.summaryEnvironmentValue("GITHUB_SERVER_URL")
		return jsValue{fields: map[string]jsValue{
			"GIT_CONFIG_COUNT":   jsStringLiteral("1"),
			"GIT_CONFIG_KEY_0":   {sources: server.taint(), kind: jsString},
			"GIT_CONFIG_VALUE_0": encoded,
		}}
	default:
		javascriptSummariesLog.Printf("Unknown JavaScript API summary model %q", model)
		j.gap(n, "JavaScript API summary transfer function is unavailable")
		return jsValue{}
	}
}

func (j *javascript) summaryEnvironmentValue(name string) jsValue {
	env := j.runtimeGlobals["process"].property("env")
	v := env.property(name)
	v.sources = union(v.sources, j.a.source(v.ref, j.input.Name, nil))
	return v
}

func (j *javascript) summaryCoreCallback(n *JavaScriptNode, method string, args []jsValue) {
	core := j.runtimeGlobals["global"].property("core")
	if core.fields == nil && core.ref == "" && len(core.taint()) == 0 {
		core = j.runtimeGlobals["core"]
	}

	callback := core.property(method)
	_, provided := core.fields[method]
	if provided || callback.fn != nil || callback.ref != "" {
		j.callValue(n, callback, args)
	}
}

func jsSummaryInputHasCallbacks(v jsValue, seen map[uintptr]bool) bool {
	if v.fn != nil || (v.ref != "" && v.kind == jsUnknown) {
		return true
	}
	id := reflect.ValueOf(v.fields).Pointer()
	if id == 0 || seen[id] {
		return false
	}
	seen[id] = true
	for _, member := range v.fields {
		if jsSummaryInputHasCallbacks(member, seen) {
			return true
		}
	}
	return false
}
