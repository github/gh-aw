// Package fmterrorfnoverbs contains test fixtures for the fmterrorfnoverbs linter.
package fmterrorfnoverbs

import (
	"errors"
	fmtalias "fmt"
)

func bad() error {
	return fmtalias.Errorf("something went wrong") // want `fmt\.Errorf called with no format verbs; use errors\.New`
}

func badMultiLine() error {
	return fmtalias.Errorf("interactive input not available in Wasm") // want `fmt\.Errorf called with no format verbs; use errors\.New`
}

func good() error {
	return errors.New("something went wrong")
}

func goodWithVerb(name string) error {
	return fmtalias.Errorf("failed to process %s", name)
}

func goodWithWrap(err error) error {
	return fmtalias.Errorf("wrapper: %w", err)
}

func suppressedPreviousLine() error {
	//nolint:fmterrorfnoverbs
	return fmtalias.Errorf("this is intentionally static")
}

func suppressedSameLine() error {
	return fmtalias.Errorf("this is intentionally static") //nolint:fmterrorfnoverbs
}

func badAliasedImport() error {
	return fmtalias.Errorf("alias import static message") // want `fmt\.Errorf called with no format verbs; use errors\.New`
}

type shadowFormatter struct{}

func (shadowFormatter) Errorf(msg string, _ ...any) error { return errors.New(msg) }

func localShadowNotFlagged() error {
	fmt := shadowFormatter{}
	return fmt.Errorf("not fmt package")
}

// escapePercentOnly has only %% (escaped literal percent) and no real verb;
// it should be flagged like a plain string.
func escapePercentOnly() error {
	return fmtalias.Errorf("disk usage exceeds 90%% limit") // want `fmt\.Errorf called with no format verbs; use errors\.New`
}

// realVerbWithEscapePercent has a real verb (%d) alongside %%; must NOT be flagged.
func realVerbWithEscapePercent(n int) error {
	return fmtalias.Errorf("%d%% done", n)
}

// multipleEscapePercents has multiple %% sequences but no real verb; should be flagged.
func multipleEscapePercents() error {
	return fmtalias.Errorf("between 50%% and 90%% utilised") // want `fmt\.Errorf called with no format verbs; use errors\.New`
}

// concatNoVerbs builds its format string via string concatenation of literals;
// the literal portion has no format verbs.
func concatNoVerbs() error {
	return fmtalias.Errorf("operation failed: " + "occurred with no additional context") // want `fmt\.Errorf called with no format verbs; use errors\.New`
}

// opaqueConcatNoVerbs builds format string with an opaque prefix;
// must NOT be flagged because prefix may contain verbs at runtime.
func opaqueConcatNoVerbs(prefix string) error {
	return fmtalias.Errorf(prefix + " occurred with no additional context")
}

// concatWithVerb builds its format string via string concatenation of literals and
// contains a real verb; must NOT be flagged.
func concatWithVerb(n int) error {
	return fmtalias.Errorf("operation failed: "+"occurred %d times", n)
}
