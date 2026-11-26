package linters

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Tier names accepted by SelectTier and the GH_AW_LINT_TIER environment
// variable of cmd/linters.
const (
	TierAll  = "all"
	TierFast = "fast"
	TierSlow = "slow"
)

// slowAnalyzers lists analyzers whose measured cost dominates a full custom
// lint run. Profile with:
//
//	go build -o /tmp/gh-aw-linters ./cmd/linters
//	/tmp/gh-aw-linters -cpuprofile=/tmp/lint.prof ./cmd/... ./pkg/...
//	go tool pprof -top -cum /tmp/lint.prof | grep 'pkg/linters/.*\.run$'
//
// An analyzer belongs here when its cumulative run time is at least ~0.3s per
// full-repository run, or when it depends on an expensive shared prerequisite
// such as internal/parentmap. Every other registered analyzer is fast.
var slowAnalyzers = map[string]string{
	"bufferresetbeforereuse":           "tracks buffer state across whole function bodies",
	"bufioscannererunchecked":          "tracks scanner loops across whole function bodies",
	"filepathwalkpreferwalkdir":        "resolves walk callbacks across function bodies",
	"goroutinemissingrecover":          "inspects every goroutine body for deferred recover",
	"goroutineresourceleak":            "requires the shared parentmap index",
	"ignoredlocalerror":                "tracks local error variables across function bodies",
	"ioutildeprecated":                 "resolves every selector against package types",
	"jsondecodertrailingdataunchecked": "walks every function body for in-memory JSON decoders",
	"lenstringzero":                    "type-checks every len comparison",
	"logfatallibrary":                  "resolves every call through astutil.PackageCall aliases",
	"osisnotexist":                     "type-checks every error predicate comparison",
	"osopenreadallpreferreadfile":      "tracks file handles across function bodies",
	"osstatignoredeerrorderef":         "tracks os.Stat results across function bodies",
	"rawloginlib":                      "resolves every call through astutil.PackageCall aliases",
	"reflectdeepequalusage":            "inspects every reflect.DeepEqual use site and its context",
	"tolowerequalfold":                 "collects case-conversion aliases across the package",
	"typeassertionnil":                 "requires the shared parentmap index",
	"typeassertionokdiscarded":         "requires the shared parentmap index",
	"uncheckedsliceindex":              "requires the shared parentmap index",
	"uncheckedtypeassertion":           "requires the shared parentmap index",
}

// IsSlow reports whether the named analyzer belongs to the slow tier.
func IsSlow(name string) bool {
	_, ok := slowAnalyzers[name]
	return ok
}

// Fast returns registered analyzers in the fast tier.
func Fast() []*analysis.Analyzer {
	return slices.DeleteFunc(All(), func(a *analysis.Analyzer) bool { return IsSlow(a.Name) })
}

// SlowAnalyzerReasons returns a copy of the slow-tier analyzer names mapped to
// the reason each one is classified as slow.
func SlowAnalyzerReasons() map[string]string {
	return maps.Clone(slowAnalyzers)
}

// Slow returns registered analyzers in the slow tier.
func Slow() []*analysis.Analyzer {
	return slices.DeleteFunc(All(), func(a *analysis.Analyzer) bool { return !IsSlow(a.Name) })
}

// SelectTier returns the analyzers to hand to multichecker for tier.
//
// Analyzers outside the tier are replaced by inert stubs that keep their name
// and flags, so shared command lines (for example -largefunc.max-lines or
// -writefilepublicpermsensitive=false) still parse while only the tier's
// analyzers do work. Explicitly enabling an analyzer from another tier in args
// is rejected so CI fast/slow splits cannot silently skip a linter.
func SelectTier(tier string, args []string) ([]*analysis.Analyzer, error) {
	all := All()
	var inTier func(*analysis.Analyzer) bool
	switch tier {
	case "", TierAll:
		return all, nil
	case TierFast:
		inTier = func(a *analysis.Analyzer) bool { return !IsSlow(a.Name) }
	case TierSlow:
		inTier = func(a *analysis.Analyzer) bool { return IsSlow(a.Name) }
	default:
		return nil, fmt.Errorf("unknown linter tier %q: expected %q, %q, or %q", tier, TierAll, TierFast, TierSlow)
	}

	byName := make(map[string]*analysis.Analyzer, len(all))
	for _, a := range all {
		byName[a.Name] = a
	}
	for _, name := range enabledAnalyzerFlags(args) {
		if a, ok := byName[name]; ok && !inTier(a) {
			return nil, fmt.Errorf("analyzer %q is not in the %s tier; run it with GH_AW_LINT_TIER=%s", name, tier, otherTier(tier))
		}
	}

	selected := make([]*analysis.Analyzer, 0, len(all))
	for _, a := range all {
		if inTier(a) {
			selected = append(selected, a)
			continue
		}
		stub := *a
		stub.Requires = nil
		stub.FactTypes = nil
		stub.ResultType = nil
		stub.Run = func(*analysis.Pass) (any, error) { return nil, nil }
		selected = append(selected, &stub)
	}
	return selected, nil
}

func otherTier(tier string) string {
	if tier == TierFast {
		return TierSlow
	}
	return TierFast
}

// enabledAnalyzerFlags returns analyzer names explicitly enabled in args, such
// as -name, --name, -name=true, or -name=1. Dotted analyzer-specific flags and
// disabling forms such as -name=false are ignored.
func enabledAnalyzerFlags(args []string) []string {
	var names []string
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if name == "" || strings.Contains(name, ".") {
			continue
		}
		if hasValue {
			switch strings.ToLower(value) {
			case "1", "t", "true":
			default:
				continue
			}
		}
		names = append(names, name)
	}
	return names
}
