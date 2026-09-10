//go:build !integration

package linters_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters"
	"github.com/github/gh-aw/pkg/linters/appendbytestring"
	"github.com/github/gh-aw/pkg/linters/appendoneelement"
	"github.com/github/gh-aw/pkg/linters/bufferresetbeforereuse"
	"github.com/github/gh-aw/pkg/linters/bytesbufferstring"
	"github.com/github/gh-aw/pkg/linters/bytescomparestring"
	"github.com/github/gh-aw/pkg/linters/contextcancelnotdeferred"
	"github.com/github/gh-aw/pkg/linters/ctxbackground"
	"github.com/github/gh-aw/pkg/linters/deferinloop"
	"github.com/github/gh-aw/pkg/linters/errorfwrapv"
	"github.com/github/gh-aw/pkg/linters/errormessage"
	"github.com/github/gh-aw/pkg/linters/errortypeassertion"
	"github.com/github/gh-aw/pkg/linters/errstringmatch"
	"github.com/github/gh-aw/pkg/linters/excessivefuncparams"
	"github.com/github/gh-aw/pkg/linters/execcommandwithoutcontext"
	"github.com/github/gh-aw/pkg/linters/fileclosenotdeferred"
	"github.com/github/gh-aw/pkg/linters/fmterrorfnoverbs"
	"github.com/github/gh-aw/pkg/linters/fprintlnsprintf"
	"github.com/github/gh-aw/pkg/linters/generatedyamlheredoc"
	"github.com/github/gh-aw/pkg/linters/globwalkignorederror"
	"github.com/github/gh-aw/pkg/linters/goroutinemissingrecover"
	"github.com/github/gh-aw/pkg/linters/hardcodedfilepath"
	"github.com/github/gh-aw/pkg/linters/httpnoctx"
	"github.com/github/gh-aw/pkg/linters/httprespbodyclose"
	"github.com/github/gh-aw/pkg/linters/httpstatuscode"
	"github.com/github/gh-aw/pkg/linters/ioutildeprecated"
	"github.com/github/gh-aw/pkg/linters/jsonmarshalignoredeerror"
	"github.com/github/gh-aw/pkg/linters/largefunc"
	"github.com/github/gh-aw/pkg/linters/lenstringsplit"
	"github.com/github/gh-aw/pkg/linters/lenstringzero"
	"github.com/github/gh-aw/pkg/linters/logfatallibrary"
	"github.com/github/gh-aw/pkg/linters/manualmutexunlock"
	"github.com/github/gh-aw/pkg/linters/manualpathconcat"
	"github.com/github/gh-aw/pkg/linters/mapclearloop"
	"github.com/github/gh-aw/pkg/linters/mapdeletecheck"
	"github.com/github/gh-aw/pkg/linters/nilctxpassed"
	"github.com/github/gh-aw/pkg/linters/osexitinlibrary"
	"github.com/github/gh-aw/pkg/linters/osgetenvlibrary"
	"github.com/github/gh-aw/pkg/linters/ossetenvlibrary"
	"github.com/github/gh-aw/pkg/linters/packagelevelmutableslicemap"
	panicinlibrarycode "github.com/github/gh-aw/pkg/linters/panic-in-library-code"
	"github.com/github/gh-aw/pkg/linters/rawloginlib"
	"github.com/github/gh-aw/pkg/linters/regexpcompileinfunction"
	"github.com/github/gh-aw/pkg/linters/regexpdynamicpattern"
	"github.com/github/gh-aw/pkg/linters/seenmapbool"
	"github.com/github/gh-aw/pkg/linters/sortslice"
	"github.com/github/gh-aw/pkg/linters/sprintfbool"
	"github.com/github/gh-aw/pkg/linters/sprintferrdot"
	"github.com/github/gh-aw/pkg/linters/sprintferrorsnew"
	"github.com/github/gh-aw/pkg/linters/sprintfint"
	"github.com/github/gh-aw/pkg/linters/ssljson"
	"github.com/github/gh-aw/pkg/linters/strconvparseignorederror"
	"github.com/github/gh-aw/pkg/linters/stringbytesroundtrip"
	"github.com/github/gh-aw/pkg/linters/stringreplaceminusone"
	"github.com/github/gh-aw/pkg/linters/stringsconcatloop"
	"github.com/github/gh-aw/pkg/linters/stringscountcontains"
	"github.com/github/gh-aw/pkg/linters/stringsindexcontains"
	"github.com/github/gh-aw/pkg/linters/stringsindexhasprefix"
	"github.com/github/gh-aw/pkg/linters/stringsjoinone"
	"github.com/github/gh-aw/pkg/linters/timeafterleak"
	"github.com/github/gh-aw/pkg/linters/timenowsub"
	"github.com/github/gh-aw/pkg/linters/timesleepnocontext"
	"github.com/github/gh-aw/pkg/linters/tolowerequalfold"
	"github.com/github/gh-aw/pkg/linters/trimleftright"
	"github.com/github/gh-aw/pkg/linters/uncheckedflushreturn"
	"github.com/github/gh-aw/pkg/linters/uncheckedtypeassertion"
	"github.com/github/gh-aw/pkg/linters/walkfuncerrshadow"
	"github.com/github/gh-aw/pkg/linters/wgdonenotdeferred"
	"github.com/github/gh-aw/pkg/linters/writebytestring"
)

// TestSpec tests derive from pkg/linters/README.md. They enforce the documented
// public surface of the linters namespace (the Analyzer entry point exposed by
// each documented subpackage and the documented default thresholds) without
// coupling to analyzer internals.

// docAnalyzer pairs a README "Subpackages" table label with the Analyzer value
// that subpackage is documented to expose.
type docAnalyzer struct {
	label    string
	analyzer *analysis.Analyzer
}

// documentedAnalyzers returns the analyzer subpackages documented in the README
// "Public API > Subpackages" table. The README documents 68 analyzers
// subpackages (the non-analyzer `internal` helper subpackage is excluded because
// it exposes no Analyzer).
//
// Spec (README "Public API > Subpackages"):
//
//	appendbytestring, appendoneelement, bufferresetbeforereuse, bytesbufferstring, bytescomparestring, contextcancelnotdeferred, ctxbackground, deferinloop, errorfwrapv, excessivefuncparams, errormessage,
//	errortypeassertion, errstringmatch, execcommandwithoutcontext, fileclosenotdeferred, fmterrorfnoverbs, fprintlnsprintf,
//	generatedyamlheredoc, globwalkignorederror, goroutinemissingrecover, hardcodedfilepath, httpnoctx, httprespbodyclose, httpstatuscode, ioutildeprecated, jsonmarshalignoredeerror, largefunc, lenstringsplit, lenstringzero,
//	logfatallibrary, manualmutexunlock, manualpathconcat, mapclearloop, mapdeletecheck, nilctxpassed, osexitinlibrary, osgetenvlibrary, ossetenvlibrary, packagelevelmutableslicemap, panic-in-library-code, rawloginlib,
//	regexpcompileinfunction, regexpdynamicpattern, seenmapbool, sortslice, sprintferrdot, sprintferrorsnew, sprintfbool, sprintfint, ssljson,
//	strconvparseignorederror, stringbytesroundtrip, stringreplaceminusone, stringsconcatloop, stringscountcontains, stringsindexcontains, stringsindexhasprefix, stringsjoinone, timeafterleak, timesleepnocontext, timenowsub,
//	tolowerequalfold, trimleftright, uncheckedflushreturn, uncheckedtypeassertion, walkfuncerrshadow, wgdonenotdeferred, writebytestring
func documentedAnalyzers() []docAnalyzer {
	return []docAnalyzer{
		{"appendbytestring", appendbytestring.Analyzer},
		{"appendoneelement", appendoneelement.Analyzer},
		{"bufferresetbeforereuse", bufferresetbeforereuse.Analyzer},
		{"bytesbufferstring", bytesbufferstring.Analyzer},
		{"bytescomparestring", bytescomparestring.Analyzer},
		{"contextcancelnotdeferred", contextcancelnotdeferred.Analyzer},
		{"ctxbackground", ctxbackground.Analyzer},
		{"deferinloop", deferinloop.Analyzer},
		{"errorfwrapv", errorfwrapv.Analyzer},
		{"excessivefuncparams", excessivefuncparams.Analyzer},
		{"errormessage", errormessage.Analyzer},
		{"errortypeassertion", errortypeassertion.Analyzer},
		{"errstringmatch", errstringmatch.Analyzer},
		{"execcommandwithoutcontext", execcommandwithoutcontext.Analyzer},
		{"fileclosenotdeferred", fileclosenotdeferred.Analyzer},
		{"fmterrorfnoverbs", fmterrorfnoverbs.Analyzer},
		{"fprintlnsprintf", fprintlnsprintf.Analyzer},
		{"generatedyamlheredoc", generatedyamlheredoc.Analyzer},
		{"globwalkignorederror", globwalkignorederror.Analyzer},
		{"goroutinemissingrecover", goroutinemissingrecover.Analyzer},
		{"hardcodedfilepath", hardcodedfilepath.Analyzer},
		{"httpnoctx", httpnoctx.Analyzer},
		{"httprespbodyclose", httprespbodyclose.Analyzer},
		{"httpstatuscode", httpstatuscode.Analyzer},
		{"ioutildeprecated", ioutildeprecated.Analyzer},
		{"jsonmarshalignoredeerror", jsonmarshalignoredeerror.Analyzer},
		{"largefunc", largefunc.Analyzer},
		{"lenstringsplit", lenstringsplit.Analyzer},
		{"lenstringzero", lenstringzero.Analyzer},
		{"logfatallibrary", logfatallibrary.Analyzer},
		{"manualmutexunlock", manualmutexunlock.Analyzer},
		{"manualpathconcat", manualpathconcat.Analyzer},
		{"mapclearloop", mapclearloop.Analyzer},
		{"mapdeletecheck", mapdeletecheck.Analyzer},
		{"nilctxpassed", nilctxpassed.Analyzer},
		{"osexitinlibrary", osexitinlibrary.Analyzer},
		{"osgetenvlibrary", osgetenvlibrary.Analyzer},
		{"ossetenvlibrary", ossetenvlibrary.Analyzer},
		{"packagelevelmutableslicemap", packagelevelmutableslicemap.Analyzer},
		{"panic-in-library-code", panicinlibrarycode.Analyzer},
		{"rawloginlib", rawloginlib.Analyzer},
		{"regexpcompileinfunction", regexpcompileinfunction.Analyzer},
		{"regexpdynamicpattern", regexpdynamicpattern.Analyzer},
		{"seenmapbool", seenmapbool.Analyzer},
		{"sortslice", sortslice.Analyzer},
		{"sprintferrdot", sprintferrdot.Analyzer},
		{"sprintferrorsnew", sprintferrorsnew.Analyzer},
		{"sprintfbool", sprintfbool.Analyzer},
		{"sprintfint", sprintfint.Analyzer},
		{"ssljson", ssljson.Analyzer},
		{"strconvparseignorederror", strconvparseignorederror.Analyzer},
		{"stringbytesroundtrip", stringbytesroundtrip.Analyzer},
		{"stringreplaceminusone", stringreplaceminusone.Analyzer},
		{"stringsconcatloop", stringsconcatloop.Analyzer},
		{"stringscountcontains", stringscountcontains.Analyzer},
		{"stringsindexcontains", stringsindexcontains.Analyzer},
		{"stringsindexhasprefix", stringsindexhasprefix.Analyzer},
		{"stringsjoinone", stringsjoinone.Analyzer},
		{"timeafterleak", timeafterleak.Analyzer},
		{"timesleepnocontext", timesleepnocontext.Analyzer},
		{"timenowsub", timenowsub.Analyzer},
		{"tolowerequalfold", tolowerequalfold.Analyzer},
		{"trimleftright", trimleftright.Analyzer},
		{"uncheckedtypeassertion", uncheckedtypeassertion.Analyzer},
		{"uncheckedflushreturn", uncheckedflushreturn.Analyzer},
		{"walkfuncerrshadow", walkfuncerrshadow.Analyzer},
		{"wgdonenotdeferred", wgdonenotdeferred.Analyzer},
		{"writebytestring", writebytestring.Analyzer},
	}
}

// TestSpec_PublicAPI_SubpackageAnalyzers validates that every analyzer
// subpackage documented in the README "Subpackages" table exposes a non-nil
// `Analyzer` entry point of type *analysis.Analyzer with its Name and Run wired,
// so each can be consumed by a go/analysis driver (multichecker/singlechecker).
func TestSpec_PublicAPI_SubpackageAnalyzers(t *testing.T) {
	t.Parallel()
	for _, d := range documentedAnalyzers() {
		t.Run(d.label, func(t *testing.T) {
			t.Parallel()
			require.NotNil(t, d.analyzer, "%s must expose a non-nil *analysis.Analyzer per the README Subpackages table", d.label)
			assert.IsType(t, (*analysis.Analyzer)(nil), d.analyzer, "%s.Analyzer should be *analysis.Analyzer for go/analysis drivers", d.label)
			assert.NotEmpty(t, d.analyzer.Name, "%s.Analyzer.Name should be set so go/analysis drivers can identify it", d.label)
			assert.NotNil(t, d.analyzer.Run, "%s.Analyzer.Run must be wired so the analyzer is executable", d.label)
		})
	}
}

// TestSpec_NamespaceExports_ErrorMessageAnalyzer validates the documented
// namespace-level compatibility alias `ErrorMessageAnalyzer` referenced in the
// README "Namespace exports" table.
// Spec: "ErrorMessageAnalyzer | Compatibility alias to pkg/linters/errormessage.Analyzer"
func TestSpec_NamespaceExports_ErrorMessageAnalyzer(t *testing.T) {
	t.Parallel()
	require.NotNil(t, linters.ErrorMessageAnalyzer,
		"linters.ErrorMessageAnalyzer must be a non-nil compatibility alias per the README")
	assert.Same(t, errormessage.Analyzer, linters.ErrorMessageAnalyzer,
		"linters.ErrorMessageAnalyzer should be the same *analysis.Analyzer as errormessage.Analyzer")
}

// TestSpec_Constants_DefaultMaxParams validates the documented default
// "8 parameters" threshold for the excessivefuncparams analyzer.
// Spec: "excessivefuncparams ... defaults to 8 parameters (DefaultMaxParams)."
func TestSpec_Constants_DefaultMaxParams(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 8, excessivefuncparams.DefaultMaxParams,
		"DefaultMaxParams should match the documented default of 8")
}

// TestSpec_Constants_DefaultMaxLines validates the documented default
// "60 lines" threshold for the largefunc analyzer.
// Spec: "largefunc ... defaults to 60 lines (DefaultMaxLines)."
func TestSpec_Constants_DefaultMaxLines(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 60, largefunc.DefaultMaxLines,
		"DefaultMaxLines should match the documented default of 60")
}

// TestSpec_DesignDecision_MaxParamsFlag validates the documented "-max-params"
// analyzer flag for excessivefuncparams.
// Spec: "excessivefuncparams exposes a -max-params analyzer flag"
func TestSpec_DesignDecision_MaxParamsFlag(t *testing.T) {
	t.Parallel()
	flag := excessivefuncparams.Analyzer.Flags.Lookup("max-params")
	require.NotNil(t, flag, "excessivefuncparams should expose a -max-params flag per the spec")
}

// TestSpec_DesignDecision_MaxLinesFlag validates the documented "-max-lines"
// analyzer flag for largefunc.
// Spec: "largefunc exposes a -max-lines analyzer flag"
func TestSpec_DesignDecision_MaxLinesFlag(t *testing.T) {
	t.Parallel()
	flag := largefunc.Analyzer.Flags.Lookup("max-lines")
	require.NotNil(t, flag, "largefunc should expose a -max-lines flag per the spec")
}

// TestSpec_UsageExample_AnalyzersUsable validates the documented usage pattern:
// each documented Analyzer can be referenced (e.g. passed to a
// multichecker/singlechecker slice). The README "Usage Examples" block assigns
// `_ = <subpackage>.Analyzer` for the documented analyzers; this test exercises
// the same pattern across all documented subpackages.
func TestSpec_UsageExample_AnalyzersUsable(t *testing.T) {
	t.Parallel()
	for _, d := range documentedAnalyzers() {
		assert.NotNil(t, d.analyzer, "documented Analyzer %q should be usable in a multichecker/singlechecker slice", d.label)
	}
}

// TestSpec_DesignDecision_UniqueAnalyzerNames validates that each documented
// subpackage exposes a distinct Analyzer.Name so they can coexist in a single
// go/analysis driver (multichecker) without conflict.
// Spec: "intentionally organized as a namespace ... so individual analyzers
// remain isolated and independently testable."
func TestSpec_DesignDecision_UniqueAnalyzerNames(t *testing.T) {
	t.Parallel()
	documented := documentedAnalyzers()
	names := make(map[string]bool, len(documented))
	for _, d := range documented {
		names[d.analyzer.Name] = true
	}
	assert.Len(t, names, len(documented),
		"each documented subpackage should expose a distinct Analyzer.Name")
}

// TestRegistryMatchesDocumentation validates that linters.All() (the canonical,
// importable registry) and documentedAnalyzers() (the spec_test hand-list
// derived from the README Subpackages table) are equal sets — bidirectionally.
//
// A registered analyzer absent from any doc surface (e.g. sprintfbool added to
// cmd/linters/main.go without updating docs) causes this test to fail, closing
// the recurring doc-sync drift gap (gh-aw#40436, #45185, #46131, #46527,
// #46707, #46977).
func TestRegistryMatchesDocumentation(t *testing.T) {
	t.Parallel()
	allAnalyzers := linters.All()
	documented := documentedAnalyzers()

	registryNames := make(map[string]struct{}, len(allAnalyzers))
	for _, a := range allAnalyzers {
		registryNames[a.Name] = struct{}{}
	}

	documentedNames := make(map[string]struct{}, len(documented))
	for _, d := range documented {
		documentedNames[d.analyzer.Name] = struct{}{}
	}

	// Every registered analyzer must appear in documentedAnalyzers().
	for name := range registryNames {
		assert.Contains(t, documentedNames, name,
			"analyzer %q is in linters.All() but missing from documentedAnalyzers() in spec_test.go; "+
				"add it to documentedAnalyzers(), doc.go, and README.md", name)
	}

	// Every documented analyzer must appear in the registry.
	for name := range documentedNames {
		assert.Contains(t, registryNames, name,
			"analyzer %q is in documentedAnalyzers() but missing from linters.All(); "+
				"add it to pkg/linters/registry.go and cmd/linters/main.go", name)
	}

	assert.Len(t, allAnalyzers, len(documented),
		"linters.All() has %d analyzers but documentedAnalyzers() has %d; "+
			"keep both in sync when adding or removing a linter",
		len(allAnalyzers), len(documented))
}
