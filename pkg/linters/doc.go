// Package linters is a namespace for gh-aw's custom Go analysis linters.
//
// All 69 active analyzers:
//
//   - appendbytestring — flags append(b, []byte(s)...) calls where s is a string that can be simplified to append(b, s...)
//   - appendoneelement — flags append(s, []T{x}...) calls where a single-element slice literal is spread and can be simplified to append(s, x)
//   - bufferresetbeforereuse — flags bytes.Buffer or strings.Builder reuse after a read without an intervening Reset
//   - bytesbufferstring — reports string(buf.Bytes()) calls where buf is a bytes.Buffer value and suggests buf.String() instead
//   - bytescomparestring — flags string(a) == string(b) and string(a) != string(b) comparisons where a and b are []byte values and recommends bytes.Equal for clearer intent
//   - contextcancelnotdeferred — flags context cancel functions called directly instead of deferred
//   - ctxbackground — flags context.Background() inside functions that already receive a context
//   - deferinloop — flags defer statements placed directly inside for or range loop bodies
//   - errorfwrapv — flags fmt.Errorf calls that pass error arguments without %w wrapping
//   - errormessage — flags non-actionable error message patterns in changed files
//   - errortypeassertion — flags type assertions from error to concrete types and recommends errors.As
//   - errstringmatch — flags brittle strings.Contains(err.Error(), "...") checks
//   - excessivefuncparams — flags function declarations with too many positional parameters
//   - execcommandwithoutcontext — flags exec.Command calls inside functions that already receive context.Context
//   - fileclosenotdeferred — flags file Close() calls that are not deferred
//   - fmterrorfnoverbs — flags fmt.Errorf calls with no format verbs, recommending errors.New
//   - fprintlnsprintf — flags fmt.Fprintln(..., fmt.Sprintf(...)) patterns
//   - generatedyamlheredoc — flags shell heredocs embedded in generated workflow YAML and directs callers to JavaScript rendering
//   - globwalkignorederror — flags filepath.Glob and os.ReadDir calls where the error return is discarded with _
//   - goroutinemissingrecover — flags goroutines started via a function literal whose body does not install a top-level defer/recover guard
//   - hardcodedfilepath — flags hard-coded file path string literals that match known path constants or should be extracted as named constants
//   - httpnoctx — flags HTTP calls that do not accept a context.Context
//   - httprespbodyclose — flags HTTP response bodies that are not closed
//   - httpstatuscode — flags HTTP status code anti-patterns
//   - ioutildeprecated — reports uses of deprecated io/ioutil functions that should be replaced with io or os package equivalents
//   - jsonmarshalignoredeerror — flags json.Marshal/Unmarshal calls where the error is discarded with _
//   - largefunc — flags function bodies that exceed a configurable line-count threshold
//   - lenstringsplit — flags len(strings.Split(s, sep)) with a non-empty separator that should use strings.Count(s, sep)+1
//   - lenstringzero — flags len(s) == 0 / len(s) != 0 on string values that should use s == "" / s != ""
//   - logfatallibrary — reports log.Fatal, log.Fatalf, and log.Fatalln calls inside library packages where they implicitly call os.Exit and bypass deferred cleanup
//   - manualmutexunlock — flags non-deferred mutex Unlock() calls
//   - manualpathconcat — flags manual "/" separator string concatenation used to build paths that should use filepath.Join or path.Join
//   - mapclearloop — reports range-over-map loops that delete every entry and can be replaced with clear(m)
//   - mapdeletecheck — reports redundant map membership checks before delete(m, k) calls since delete is already a no-op for missing keys
//   - nilctxpassed — flags function calls where nil is passed as a context.Context argument
//   - osexitinlibrary — flags os.Exit calls in library packages
//   - osgetenvlibrary — flags os.Getenv calls in library packages
//   - ossetenvlibrary — flags os.Setenv calls in library packages
//   - packagelevelmutableslicemap — reports mutation of package-level slice/map variables from inside function bodies, which risks data races and cross-call state leaks
//   - panic-in-library-code — flags panic() calls in library packages
//   - rawloginlib — flags direct usage of the standard log package in library packages
//   - regexpcompileinfunction — flags regexp.MustCompile/Compile calls inside functions
//   - regexpdynamicpattern — flags regexp compile calls whose pattern is not a compile-time constant
//   - seenmapbool — flags map[string]bool used as a set that should use map[string]struct{}
//   - slicemakezerolength — flags zero-length slice allocations before known-size range loops that append one element per iteration
//   - sortslice — flags sort.Slice / sort.SliceStable calls that should use slices.SortFunc / slices.SortStableFunc
//   - sprintferrdot — flags redundant .Error() calls on error values passed to fmt format functions
//   - sprintferrorsnew — flags errors.New(fmt.Sprintf(...)) calls that should use fmt.Errorf instead
//   - sprintfbool — flags fmt.Sprintf("%t", b) calls where b is a single bool value and suggests using strconv.FormatBool(b) instead
//   - sprintfint — flags fmt.Sprintf calls that format integers that should use strconv.Itoa
//   - ssljson — validates ssl.json skill artifacts in .github/skills/ against the SSL spec
//   - strconvparseignorederror — flags strconv parsing calls where the error is discarded with _
//   - stringbytesroundtrip — reports redundant string/[]byte round-trip conversions such as string([]byte(s)) or []byte(string(b)) that produce a wasteful intermediate copy
//   - stringreplaceminusone — flags strings.Replace calls with n=-1 that should use strings.ReplaceAll
//   - stringsconcatloop — flags string += or x = x + y concatenation inside for/range loops that should use strings.Builder
//   - stringscountcontains — reports strings.Count(s, sub) comparisons with 0 or 1 (e.g. > 0, >= 1, == 0, != 0, < 1, <= 0) and their yoda-order variants that should use strings.Contains(s, sub) or !strings.Contains(s, sub)
//   - stringsindexcontains — flags strings.Index(s, substr) comparisons that should use strings.Contains
//   - stringsindexhasprefix — reports strings.Index(s, sub) comparisons with 0 (== 0 and != 0) and their yoda-order variants that should use strings.HasPrefix(s, sub) or !strings.HasPrefix(s, sub)
//   - stringsjoinone — flags strings.Join([]string{s}, sep) calls with a single-element slice literal where the separator is unused and the call is equivalent to just s
//   - timeafterleak — flags time.After in select cases inside loops that leak timer channels
//   - timesleepnocontext — flags time.Sleep calls in context-aware functions that should propagate cancellation
//   - timenowsub — reports time.Now().Sub(t) calls that should be simplified to time.Since(t)
//   - tolowerequalfold — flags case-insensitive comparisons via ToLower/ToUpper that should use EqualFold
//   - trimleftright — flags strings.TrimLeft/TrimRight calls with a multi-character literal cutset where TrimPrefix/TrimSuffix was likely intended
//   - uncheckedtypeassertion — flags unchecked single-value type assertions
//   - uncheckedflushreturn — flags Flush() method calls where the error return is discarded
//   - walkfuncerrshadow — flags filepath.Walk/WalkDir callbacks whose err parameter shadows an outer err variable assigned from the walk call
//   - wgdonenotdeferred — flags non-deferred sync.WaitGroup.Done() calls
//   - writebytestring — flags w.Write([]byte(s)) calls where s is a string that can be replaced with io.WriteString(w, s)
//
// The package also exposes a compatibility alias (ErrorMessageAnalyzer) that
// points to the errormessage subpackage analyzer.
package linters
