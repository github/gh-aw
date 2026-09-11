# linters Package

The `linters` package namespace contains custom static analysis linters used by `gh-aw` quality checks.

## Overview

This package currently provides custom Go analyzers in the following subpackages:

- `appendbytestring` — reports `append(b, []byte(s)...)` calls where `b` is `[]byte` and `s` is a string, which can be simplified to `append(b, s...)`.
- `appendoneelement` — reports `append(s, []T{x}...)` calls where a single-element slice literal is spread and can be simplified to `append(s, x)`.
- `bufferresetbeforereuse` — reports `bytes.Buffer` or `strings.Builder` writes after a read without an intervening `Reset()`, which can accumulate stale content.
- `bytescomparestring` — reports `string(a) == string(b)` and `string(a) != string(b)` comparisons where `a` and `b` are `[]byte` values; use `bytes.Equal(a, b)` for `==` and `!bytes.Equal(a, b)` for `!=`.
- `bytesbufferstring` — reports `string(buf.Bytes())` calls where `buf` is a `bytes.Buffer` value receiver, suggesting `buf.String()` instead.
- `contextcancelnotdeferred` — reports context cancel functions that are called directly instead of deferred.
- `ctxbackground` — reports `context.Background()` calls inside functions that already receive a `context.Context` parameter.
- `deferinloop` — reports `defer` statements placed directly inside `for`/`range` loop bodies, which execute when the enclosing function returns rather than each iteration and can cause resource leaks.
- `errorfwrapv` — reports `fmt.Errorf` calls that pass error arguments without `%w` wrapping.
- `excessivefuncparams` — reports function declarations that exceed a configurable parameter-count threshold.
- `errormessage` — reports non-actionable error-message patterns in changed files; pass `-errormessage.full-repo` (or `-errormessage.changed-files=all`) to audit the whole repository.
- `errortypeassertion` — reports type assertions from `error` to concrete types and recommends `errors.As`.
- `errstringmatch` — reports `strings.Contains(err.Error(), "...")` patterns and recommends `errors.Is` / `errors.As`.
- `fileclosenotdeferred` — reports non-deferred file `Close()` calls that can leak resources.
- `execcommandwithoutcontext` — reports `exec.Command(...)` calls inside functions that already receive `context.Context` and should use `exec.CommandContext(...)`.
- `fmterrorfnoverbs` — reports `fmt.Errorf` calls whose format string contains no verbs, recommending `errors.New` instead.
- `fprintlnsprintf` — reports `fmt.Fprintln(..., fmt.Sprintf(...))` patterns and recommends direct formatting calls.
- `generatedyamlheredoc` — reports shell heredocs embedded in generated workflow YAML and recommends passing plain environment-variable content to a JavaScript renderer. Run `make golint-custom` to verify that an unsuppressed heredoc in `pkg/workflow` is reported.
- `globwalkignorederror` — reports `filepath.Glob` and `os.ReadDir` calls where the error return is discarded with `_`.
- `goroutinemissingrecover` — reports goroutines started via a function literal whose body does not install a top-level `defer func() { recover() }()` guard.
- `hardcodedfilepath` — reports hard-coded file path string literals that match known path constants or should be extracted into named constants; also annotates paths that appear in log/print calls.
- `httpnoctx` — reports HTTP client and package-level HTTP calls that do not accept a `context.Context`.
- `httprespbodyclose` — reports HTTP responses whose `Body.Close()` call is missing or not deferred.
- `httpstatuscode` — reports raw HTTP status-code integer literals that should use `net/http` named constants.
- `ioutildeprecated` — reports uses of deprecated `io/ioutil` functions (deprecated since Go 1.16) and suggests their replacements in the `io` and `os` packages.
- `jsonmarshalignoredeerror` — reports `json.Marshal` and `json.Unmarshal` calls where the error return is discarded.
- `largefunc` — reports function bodies that exceed a configurable line-count threshold.
- `lenstringsplit` — reports `len(strings.Split(s, sep))` expressions with a non-empty separator that should use `strings.Count(s, sep)+1` to avoid an intermediate slice allocation.
- `lenstringzero` — reports `len(s) == 0` / `len(s) != 0` comparisons on string values that should use `s == ""` / `s != ""`.
- `logfatallibrary` — reports `log.Fatal`, `log.Fatalf`, and `log.Fatalln` calls in library packages (`pkg/*`) where they implicitly call `os.Exit` and bypass deferred cleanup.
- `mapclearloop` — reports range-over-map loops that delete every entry and can be replaced with `clear(m)`.
- `mapdeletecheck` — reports redundant map membership checks before `delete(m, k)` calls since `delete` is already a no-op for missing keys.
- `manualmutexunlock` — reports non-deferred mutex `Unlock()` calls that can lead to deadlocks on early returns or panics.
- `manualpathconcat` — reports manual `"/"` separator string concatenation used to build paths (e.g. `dir + "/" + file`) that should use `filepath.Join` or `path.Join`.
- `nilctxpassed` — reports function calls where `nil` is passed as a `context.Context` argument; the correct idioms are `context.Background()` or `context.TODO()`.
- `osgetenvlibrary` — reports `os.Getenv` calls in library packages (`pkg/*`) where environment access should be injected.
- `osexitinlibrary` — reports `os.Exit` calls in library packages (`pkg/*`) where process termination should be delegated to `cmd/*` entry points.
- `ossetenvlibrary` — reports `os.Setenv` calls in library packages (`pkg/*`) where side effects should be isolated.
- `packagelevelmutableslicemap` — reports package-level (file/package-scope) `var` slice/map declarations mutated from inside a function body via `append()` re-assignment, index assignment, or `delete()`. Mutations inside a top-level `init()` are exempt.
- `panic-in-library-code` — reports `panic()` calls in library packages (`pkg/*`) where errors should be returned instead.
- `rawloginlib` — reports direct usage of the standard `log` package in library packages, where `pkg/logger` should be used.
- `regexpcompileinfunction` — reports `regexp.Compile` / `regexp.MustCompile` and their POSIX variants called inside functions that should be package-level.
- `regexpdynamicpattern` — reports regexp compile calls whose pattern is not a compile-time constant string.
- `seenmapbool` — reports `map[string]bool` used as a set (values always `true`) that should use `map[string]struct{}` instead.
- `slicemakezerolength` — reports zero-length slice allocations before known-size range loops that append exactly one element per iteration, where the range length provides a capacity hint.
- `sortslice` — reports `sort.Slice` / `sort.SliceStable` calls that should use `slices.SortFunc` / `slices.SortStableFunc`.
- `sprintferrdot` — reports redundant `.Error()` calls on error values passed to `fmt` format functions where the fmt package calls `.Error()` automatically.
- `sprintferrorsnew` — reports `errors.New(fmt.Sprintf(...))` calls that should use `fmt.Errorf` instead.
- `sprintfbool` — reports `fmt.Sprintf("%t", b)` calls where `b` is a single bool value and suggests using `strconv.FormatBool(b)` instead.
- `sprintfint` — reports `fmt.Sprintf("%d", ...)` and related conversions that should use `strconv` helpers.
- `ssljson` — validates `ssl.json` skill artifacts found in `.github/skills/` against the SSL spec (enum membership, graph integrity, transition targets, entry pointer validity).
- `strconvparseignorederror` — reports `strconv` parsing calls (`Atoi`, `ParseInt`, etc.) where the error return is discarded with `_`.
- `stringbytesroundtrip` — reports redundant `string([]byte(s))` or `[]byte(string(b))` round-trip conversions that produce a wasteful intermediate copy.
- `stringreplaceminusone` — reports `strings.Replace` calls whose `n` argument is `-1`, which should use the more readable `strings.ReplaceAll`.
- `stringsconcatloop` — reports `string +=` concatenation inside `for`/`range` loop bodies, which allocates a new string copy on every iteration (O(n²) memory); use `strings.Builder` instead.
- `stringscountcontains` — reports `strings.Count(s, sub)` comparisons with `0` or `1` (e.g. `> 0`, `>= 1`, `== 0`, `!= 0`, `< 1`, `<= 0`) and their yoda-order variants that should use `strings.Contains(s, sub)` or `!strings.Contains(s, sub)` instead.
- `stringsindexcontains` — reports `strings.Index(s, substr)` comparisons with `-1` or `0` (e.g. `!= -1`, `>= 0`, `> -1`, `== -1`, `< 0`, `<= -1`) and their yoda-order variants that should use `strings.Contains(s, substr)` or `!strings.Contains(s, substr)` instead.
- `stringsindexhasprefix` — reports `strings.Index(s, sub)` comparisons with `0` (`== 0`, `!= 0`) and their yoda-order variants that should use `strings.HasPrefix(s, sub)` or `!strings.HasPrefix(s, sub)` instead.
- `stringsjoinone` — reports `strings.Join([]string{s}, sep)` calls with a single-element slice literal where the separator is never used and the call is equivalent to just `s`.
- `timeafterleak` — reports `time.After` calls used as the channel-receive expression in a `select` case inside a `for` or `range` loop that leak a timer channel on each iteration when another case fires first.
- `timesleepnocontext` — reports `time.Sleep` calls inside functions that already receive a `context.Context`, where a context-aware `select` should be used instead.
- `timenowsub` — reports `time.Now().Sub(t)` calls that should be simplified to `time.Since(t)`.
- `tolowerequalfold` — reports case-insensitive string comparisons using `strings.ToLower`/`ToUpper` that should use `strings.EqualFold`.
- `trimleftright` — reports `strings.TrimLeft`/`TrimRight` calls with a multi-character literal cutset where `TrimPrefix`/`TrimSuffix` was likely intended.
- `uncheckedtypeassertion` — reports single-value type assertions where unchecked panics are possible.
- `uncheckedflushreturn` — reports `Flush()` method calls where the error return is discarded, which silently drops buffered data on failure.
- `wgdonenotdeferred` — reports non-deferred `sync.WaitGroup.Done()` calls that can deadlock on panics or early returns.
- `writebytestring` — reports `w.Write([]byte(s))` calls where `s` is a string, which can be replaced with `io.WriteString` to avoid an unnecessary `[]byte` allocation.
- `internal` — shared helper packages for analyzers (file checks, `nolint` handling, and coverage-aware perf gating).

## Coverage-aware perf gating

Micro-optimizations flagged by allocation/perf linters (e.g. `stringsconcatloop`, `appendoneelement`,
`appendbytestring`, `bytesbufferstring`, `bytescomparestring`, `lenstringsplit`, `mapclearloop`,
`seenmapbool`, `slicemakezerolength`, `sortslice`, `stringbytesroundtrip`, `stringsjoinone`, `tolowerequalfold`, and
`writebytestring`) only matter on hot paths: applying them to code that tests never execute adds
churn without a measurable benefit. These linters consult the shared
`pkg/linters/internal/coverage` package, which loads a Go coverage profile (produced by
`go test -covermode=count -coverprofile=<path>`) referenced by the `GH_AW_LINT_COVERAGE_PROFILE`
environment variable and gates findings on the recorded execution hit count for the reported line.

- When `GH_AW_LINT_COVERAGE_PROFILE` is unset (the default), coverage gating is a no-op and every
  perf linter reports exactly as it did before coverage-awareness was introduced.
- When a profile is loaded, a perf linter only reports a finding once the code path's execution
  count is at least its `-hot-threshold` flag (default `1`: any recorded execution).
- Pass `-<linter>.hot-threshold=0` to disable coverage gating for a specific linter even when a
  profile is loaded.
- Purely stylistic/readability linters (e.g. `stringsindexcontains`, `stringsindexhasprefix`,
  `stringscountcontains`, `lenstringzero`) are intentionally **not** coverage-gated: they carry no
  measurable performance difference, so "hot path" relevance does not apply to them.

## Public API

### Subpackages

| Subpackage | Description |
|------------|-------------|
| `appendbytestring` | Custom `go/analysis` analyzer that flags `append(b, []byte(s)...)` calls where `s` is a string that can be simplified to `append(b, s...)` |
| `appendoneelement` | Custom `go/analysis` analyzer that flags `append(s, []T{x}...)` calls where a single-element slice literal is spread and can be simplified to `append(s, x)` |
| `bufferresetbeforereuse` | Custom `go/analysis` analyzer that flags `bytes.Buffer` or `strings.Builder` writes after a read without an intervening `Reset()` |
| `bytescomparestring` | Custom `go/analysis` analyzer that flags `string(a) == string(b)` / `!=` comparisons on `[]byte` values; use `bytes.Equal(a, b)` for `==` and `!bytes.Equal(a, b)` for `!=` |
| `bytesbufferstring` | Custom `go/analysis` analyzer that flags `string(buf.Bytes())` calls where `buf` is a `bytes.Buffer` value and suggests `buf.String()` instead |
| `contextcancelnotdeferred` | Custom `go/analysis` analyzer that flags context cancel functions called directly instead of deferred |
| `ctxbackground` | Custom `go/analysis` analyzer that flags `context.Background()` calls inside functions that already receive a context parameter |
| `deferinloop` | Custom `go/analysis` analyzer that flags `defer` statements inside `for`/`range` loop bodies that execute when the enclosing function returns rather than each iteration |
| `errorfwrapv` | Custom `go/analysis` analyzer that flags `fmt.Errorf` calls that pass error arguments without `%w` wrapping |
| `excessivefuncparams` | Custom `go/analysis` analyzer that flags function declarations with too many positional parameters |
| `errormessage` | Custom `go/analysis` analyzer that flags non-actionable error message patterns in changed files (or all files with `-errormessage.full-repo`) |
| `errortypeassertion` | Custom `go/analysis` analyzer that flags type assertions from `error` to concrete types and recommends `errors.As` |
| `errstringmatch` | Custom `go/analysis` analyzer that flags brittle `strings.Contains(err.Error(), "...")` checks |
| `execcommandwithoutcontext` | Custom `go/analysis` analyzer that flags `exec.Command(...)` calls that should use `exec.CommandContext(...)` in context-receiving functions |
| `fileclosenotdeferred` | Custom `go/analysis` analyzer that flags file `Close()` calls that are not deferred immediately |
| `fmterrorfnoverbs` | Custom `go/analysis` analyzer that flags `fmt.Errorf` calls with no format verbs, recommending `errors.New` |
| `fprintlnsprintf` | Custom `go/analysis` analyzer that flags `fmt.Fprintln(..., fmt.Sprintf(...))` patterns |
| `generatedyamlheredoc` | Custom `go/analysis` analyzer that flags shell heredocs embedded in generated workflow YAML |
| `globwalkignorederror` | Custom `go/analysis` analyzer that flags `filepath.Glob` and `os.ReadDir` calls where the error return is discarded with `_` |
| `goroutinemissingrecover` | Custom `go/analysis` analyzer that flags goroutines started via a function literal that do not install a top-level defer/recover guard |
| `hardcodedfilepath` | Custom `go/analysis` analyzer that flags hard-coded file path string literals that match known path constants or should be extracted as named constants; annotates paths in log/print calls |
| `httpnoctx` | Custom `go/analysis` analyzer that flags HTTP calls that do not accept a `context.Context` |
| `httprespbodyclose` | Custom `go/analysis` analyzer that flags HTTP response bodies that are not closed (or not deferred) |
| `httpstatuscode` | Custom `go/analysis` analyzer that flags raw HTTP status-code integer literals that should use `net/http` named constants |
| `ioutildeprecated` | Custom `go/analysis` analyzer that flags deprecated `io/ioutil` function calls (deprecated since Go 1.16) and suggests replacements in the `io` and `os` packages |
| `jsonmarshalignoredeerror` | Custom `go/analysis` analyzer that flags `json.Marshal`/`json.Unmarshal` calls where the error return is discarded |
| `largefunc` | Custom `go/analysis` analyzer that flags large functions with actionable diagnostics |
| `lenstringsplit` | Custom `go/analysis` analyzer that flags `len(strings.Split(s, sep))` with a non-empty separator that should use `strings.Count(s, sep)+1` |
| `lenstringzero` | Custom `go/analysis` analyzer that flags `len(s) == 0` / `len(s) != 0` on string values that should use `s == ""` / `s != ""` |
| `logfatallibrary` | Custom `go/analysis` analyzer that flags `log.Fatal`, `log.Fatalf`, and `log.Fatalln` calls in library packages where they implicitly call `os.Exit` and bypass deferred cleanup |
| `mapclearloop` | Custom `go/analysis` analyzer that flags range-over-map loops that delete every entry and can be replaced with `clear(m)` |
| `mapdeletecheck` | Custom `go/analysis` analyzer that flags redundant map membership checks before `delete(m, k)` calls since `delete` is a no-op for missing keys |
| `manualmutexunlock` | Custom `go/analysis` analyzer that flags mutex `Unlock()` calls that are not deferred |
| `manualpathconcat` | Custom `go/analysis` analyzer that flags manual `"/"` separator string concatenation used to build paths that should use `filepath.Join` or `path.Join` |
| `nilctxpassed` | Custom `go/analysis` analyzer that flags function calls where `nil` is passed as a `context.Context` argument |
| `osgetenvlibrary` | Custom `go/analysis` analyzer that flags `os.Getenv` usage in library packages |
| `osexitinlibrary` | Custom `go/analysis` analyzer that flags `os.Exit` usage in library packages |
| `ossetenvlibrary` | Custom `go/analysis` analyzer that flags `os.Setenv` usage in library packages |
| `packagelevelmutableslicemap` | Custom `go/analysis` analyzer that flags package-level slice/map `var` declarations mutated from inside a function body via `append()` re-assignment, index assignment, or `delete()` |
| `panic-in-library-code` | Custom `go/analysis` analyzer that flags `panic()` usage in library packages |
| `rawloginlib` | Custom `go/analysis` analyzer that flags standard-library `log` package calls in library packages |
| `regexpcompileinfunction` | Custom `go/analysis` analyzer that flags regexp compilation inside function bodies |
| `regexpdynamicpattern` | Custom `go/analysis` analyzer that flags regexp compile calls with non-constant patterns |
| `seenmapbool` | Custom `go/analysis` analyzer that flags `map[string]bool` used as a set that should use `map[string]struct{}` |
| `slicemakezerolength` | Custom `go/analysis` analyzer that flags zero-length slice allocations before known-size range loops that append one element per iteration |
| `sortslice` | Custom `go/analysis` analyzer that flags `sort.Slice` / `sort.SliceStable` calls that should use `slices.SortFunc` / `slices.SortStableFunc` |
| `sprintferrdot` | Custom `go/analysis` analyzer that flags redundant `.Error()` calls on error values passed to `fmt` format functions |
| `sprintferrorsnew` | Custom `go/analysis` analyzer that flags `errors.New(fmt.Sprintf(...))` calls that should use `fmt.Errorf` instead |
| `sprintfbool` | Custom `go/analysis` analyzer that flags `fmt.Sprintf("%t", b)` calls where `b` is a single bool value and suggests using `strconv.FormatBool(b)` instead |
| `sprintfint` | Custom `go/analysis` analyzer that flags `fmt.Sprintf` integer conversions that should use `strconv` helpers |
| `ssljson` | Custom `go/analysis` analyzer that validates SSL JSON skill artifacts in `.github/skills/` |
| `strconvparseignorederror` | Custom `go/analysis` analyzer that flags `strconv` parsing calls where the error return is discarded with `_` |
| `stringbytesroundtrip` | Custom `go/analysis` analyzer that flags redundant `string([]byte(s))` or `[]byte(string(b))` round-trip conversions that produce a wasteful intermediate copy |
| `stringreplaceminusone` | Custom `go/analysis` analyzer that flags `strings.Replace` calls with `n=-1` that should use `strings.ReplaceAll` |
| `stringsconcatloop` | Custom `go/analysis` analyzer that flags `string +=` concatenation inside `for`/`range` loops that should use `strings.Builder` |
| `stringscountcontains` | Custom `go/analysis` analyzer that flags `strings.Count(s, sub)` comparisons with `0` or `1` that should use `strings.Contains` or `!strings.Contains` |
| `stringsindexcontains` | Custom `go/analysis` analyzer that flags `strings.Index(s, substr)` comparisons with `-1` or `0` that should use `strings.Contains` or `!strings.Contains` |
| `stringsindexhasprefix` | Custom `go/analysis` analyzer that flags `strings.Index(s, sub)` comparisons with `0` (`== 0`, `!= 0`) that should use `strings.HasPrefix` or `!strings.HasPrefix` |
| `stringsjoinone` | Custom `go/analysis` analyzer that flags `strings.Join([]string{s}, sep)` calls with a single-element slice literal where the separator is unused and the call is equivalent to just `s` |
| `timeafterleak` | Custom `go/analysis` analyzer that flags `time.After` in `select` cases inside loops that leak a timer channel on each iteration when another case fires first |
| `timesleepnocontext` | Custom `go/analysis` analyzer that flags `time.Sleep` calls in context-aware functions |
| `timenowsub` | Custom `go/analysis` analyzer that flags `time.Now().Sub(t)` calls that should use `time.Since(t)` |
| `tolowerequalfold` | Custom `go/analysis` analyzer that flags case-insensitive comparisons via `strings.ToLower`/`ToUpper` that should use `strings.EqualFold` |
| `trimleftright` | Custom `go/analysis` analyzer that flags `strings.TrimLeft`/`TrimRight` calls with a multi-character literal cutset where `TrimPrefix`/`TrimSuffix` was likely intended |
| `uncheckedtypeassertion` | Custom `go/analysis` analyzer that flags unchecked single-value type assertions |
| `uncheckedflushreturn` | Custom `go/analysis` analyzer that flags `Flush()` method calls where the error return is discarded |
| `walkfuncerrshadow` | Custom `go/analysis` analyzer that flags `filepath.Walk`/`filepath.WalkDir` callbacks whose `err` parameter shadows an outer `err` variable assigned from the walk call |
| `wgdonenotdeferred` | Custom `go/analysis` analyzer that flags non-deferred `sync.WaitGroup.Done()` calls |
| `writebytestring` | Custom `go/analysis` analyzer that flags `w.Write([]byte(s))` calls where `s` is a string that can be replaced with `io.WriteString` |
| `internal` | Shared helper subpackages used by analyzers (`internal/filecheck`, `internal/nolint`, `internal/resourcetracker`) |

### Namespace exports

| Symbol | Description |
|---|---|
| `All` | Returns the canonical registry of all custom analyzers as `[]*analysis.Analyzer` for multichecker wiring, test assertions, and documentation consistency checks |
| `ErrorMessageAnalyzer` | Compatibility alias to `pkg/linters/errormessage.Analyzer` |

## Usage Examples

```go
import (
	"github.com/github/gh-aw/pkg/linters/deferinloop"
	"github.com/github/gh-aw/pkg/linters/errorfwrapv"
	"github.com/github/gh-aw/pkg/linters/ctxbackground"
	"github.com/github/gh-aw/pkg/linters/excessivefuncparams"
	"github.com/github/gh-aw/pkg/linters/errormessage"
	"github.com/github/gh-aw/pkg/linters/errstringmatch"
	"github.com/github/gh-aw/pkg/linters/execcommandwithoutcontext"
	"github.com/github/gh-aw/pkg/linters/fileclosenotdeferred"
	"github.com/github/gh-aw/pkg/linters/hardcodedfilepath"
	"github.com/github/gh-aw/pkg/linters/httpnoctx"
	"github.com/github/gh-aw/pkg/linters/httprespbodyclose"
	"github.com/github/gh-aw/pkg/linters/httpstatuscode"
	"github.com/github/gh-aw/pkg/linters/largefunc"
	"github.com/github/gh-aw/pkg/linters/lenstringzero"
	"github.com/github/gh-aw/pkg/linters/manualmutexunlock"
	"github.com/github/gh-aw/pkg/linters/osgetenvlibrary"
	"github.com/github/gh-aw/pkg/linters/osexitinlibrary"
	panicinlibrarycode "github.com/github/gh-aw/pkg/linters/panic-in-library-code"
	"github.com/github/gh-aw/pkg/linters/rawloginlib"
	"github.com/github/gh-aw/pkg/linters/regexpcompileinfunction"
	"github.com/github/gh-aw/pkg/linters/regexpdynamicpattern"
	"github.com/github/gh-aw/pkg/linters/sortslice"
	"github.com/github/gh-aw/pkg/linters/sprintfbool"
	"github.com/github/gh-aw/pkg/linters/sprintfint"
	"github.com/github/gh-aw/pkg/linters/ssljson"
	"github.com/github/gh-aw/pkg/linters/timesleepnocontext"
	"github.com/github/gh-aw/pkg/linters/trimleftright"
)

// Use with multichecker, singlechecker, or custom go/analysis driver.
_ = ctxbackground.Analyzer
_ = deferinloop.Analyzer
_ = errorfwrapv.Analyzer
_ = excessivefuncparams.Analyzer
_ = errormessage.Analyzer
_ = errstringmatch.Analyzer
_ = execcommandwithoutcontext.Analyzer
_ = fileclosenotdeferred.Analyzer
_ = hardcodedfilepath.Analyzer
_ = httpnoctx.Analyzer
_ = httprespbodyclose.Analyzer
_ = httpstatuscode.Analyzer
_ = largefunc.Analyzer
_ = lenstringzero.Analyzer
_ = manualmutexunlock.Analyzer
_ = osgetenvlibrary.Analyzer
_ = osexitinlibrary.Analyzer
_ = panicinlibrarycode.Analyzer
_ = rawloginlib.Analyzer
_ = regexpcompileinfunction.Analyzer
_ = regexpdynamicpattern.Analyzer
_ = sortslice.Analyzer
_ = sprintfbool.Analyzer
_ = sprintfint.Analyzer
_ = ssljson.Analyzer
_ = timesleepnocontext.Analyzer
_ = trimleftright.Analyzer
```

## Dependencies

**Internal**:
- `github.com/github/gh-aw/pkg/linters/appendbytestring` — append-byte-string analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/appendoneelement` — append-one-element analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/bytescomparestring` — bytes-compare-string analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/bytesbufferstring` — bytes-buffer-string analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/contextcancelnotdeferred` — context-cancel-not-deferred analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/ctxbackground` — context-background analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/deferinloop` — defer-in-loop analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/errorfwrapv` — fmt-errorf-wrap-v analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/errormessage` — error-message analyzer subpackage (also re-exported as `ErrorMessageAnalyzer`)
- `github.com/github/gh-aw/pkg/linters/errortypeassertion` — error-type-assertion analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/errstringmatch` — err-string-match analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/execcommandwithoutcontext` — exec-command-without-context analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/excessivefuncparams` — excessive-func-params analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/fileclosenotdeferred` — file-close-not-deferred analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/fmterrorfnoverbs` — fmt-errorf-no-verbs analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/fprintlnsprintf` — fprintln-sprintf analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/globwalkignorederror` — glob-walk-ignored-error analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/hardcodedfilepath` — hard-coded-file-path analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/httpnoctx` — HTTP-no-context analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/httprespbodyclose` — HTTP-response-body-close analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/httpstatuscode` — http-status-code analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/ioutildeprecated` — ioutil-deprecated analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/jsonmarshalignoredeerror` — json-marshal-ignored-error analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/largefunc` — large-func analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/lenstringsplit` — len-strings-split analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/lenstringzero` — len-string-zero analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/logfatallibrary` — log-fatal-library analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/mapclearloop` — map-clear-loop analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/mapdeletecheck` — map-delete-check analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/manualmutexunlock` — manual-mutex-unlock analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/manualpathconcat` — manual-path-concat analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/osgetenvlibrary` — os-getenv-library analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/osexitinlibrary` — os-exit-in-library analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/ossetenvlibrary` — os-setenv-library analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/packagelevelmutableslicemap` — package-level-mutable-slice-map analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/panic-in-library-code` — panic-in-library-code analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/rawloginlib` — raw-log-in-lib analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/regexpcompileinfunction` — regexp-compile-in-function analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/regexpdynamicpattern` — regexp-dynamic-pattern analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/seenmapbool` — seen-map-bool analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/sortslice` — sort-slice analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/sprintferrdot` — sprintf-err-dot analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/sprintferrorsnew` — sprintf-errors-new analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/sprintfbool` — sprintf-bool analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/sprintfint` — sprintf-int analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/ssljson` — ssl-json analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/strconvparseignorederror` — strconv-parse-ignored-error analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/stringbytesroundtrip` — string-bytes-round-trip analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/stringreplaceminusone` — string-replace-minus-one analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/stringscountcontains` — strings-count-contains analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/stringsindexcontains` — strings-index-contains analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/stringsindexhasprefix` — strings-index-has-prefix analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/stringsjoinone` — strings-join-one analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/timeafterleak` — time-after-leak analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/timesleepnocontext` — time-sleep-no-context analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/tolowerequalfold` — to-lower-equal-fold analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/trimleftright` — trim-left-right analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/uncheckedtypeassertion` — unchecked-type-assertion analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/uncheckedflushreturn` — unchecked-flush-return analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/walkfuncerrshadow` — walk-func-err-shadow analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/wgdonenotdeferred` — wg-done-not-deferred analyzer subpackage
- `github.com/github/gh-aw/pkg/linters/writebytestring` — write-byte-string analyzer subpackage

**Transitive / Internal helpers**:
- `github.com/github/gh-aw/pkg/linters/internal/filecheck` — shared file-path filtering helpers used by multiple analyzers
- `github.com/github/gh-aw/pkg/linters/internal/nolint` — shared `//nolint` directive parsing helpers used by multiple analyzers
- `github.com/github/gh-aw/pkg/linters/internal/resourcetracker` — shared deferred-cleanup state machine used by analyzers that flag manual resource cleanup

**External**:
- `golang.org/x/tools/go/analysis` — analyzer framework
- `golang.org/x/tools/go/analysis/passes/inspect` — AST inspection support
- `golang.org/x/tools/go/ast/inspector` — efficient AST traversal

## Design Notes

- The package is intentionally organized as a namespace (`pkg/linters/*`) so individual analyzers remain isolated and independently testable.
- CI currently enforces the `errstringmatch`, `manualmutexunlock`, `panicinlibrarycode`, `osexitinlibrary`, and `rawloginlib` analyzers via `.github/workflows/cgo.yml`.
- `excessivefuncparams` exposes a `-max-params` analyzer flag and defaults to `8` parameters (`DefaultMaxParams`).
- `largefunc` exposes a `-max-lines` analyzer flag, defaults to `60` lines (`DefaultMaxLines`), and skips `_test.go` files.
- `osexitinlibrary` helps enforce separation between library logic and process-level termination.

<!-- BEGIN SOURCE-VERIFIED EXPORT COVERAGE -->
## Source-verified export coverage

This appendix is generated from the current non-test Go source files in this package and records any exported top-level symbols that are not already described above.

| Category | Count |
|----------|------:|
| Types | 0 |
| Constants | 0 |
| Variables | 1 |
| Functions and methods | 1 |
| Additional symbols documented in this appendix | 0 |

The sections above already mention every exported top-level symbol in the current source tree.
<!-- END SOURCE-VERIFIED EXPORT COVERAGE -->

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*
