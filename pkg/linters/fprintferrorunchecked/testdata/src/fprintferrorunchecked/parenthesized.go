package fprintferrorunchecked

import (
	"bytes"
	f "fmt"
	"os"
)

func BadParenthesizedCallee() {
	w := &bytes.Buffer{}
	(f.Fprintf)(w, "text")         // want `error return from fmt.Fprintf\(\) is not checked`
	_, _ = (f.Fprintln)(w, "text") // want `error return from fmt.Fprintln\(\) is not checked`
	_, _ = ((f.Fprint))(w, "text") // want `error return from fmt.Fprint\(\) is not checked`
}

func BadParenthesizedCall() {
	w := &bytes.Buffer{}
	(f.Fprintf(w, "text"))         // want `error return from fmt.Fprintf\(\) is not checked`
	_, _ = (f.Fprintln(w, "text")) // want `error return from fmt.Fprintln\(\) is not checked`
}

func BadAliasedImport() {
	w := &bytes.Buffer{}
	f.Fprintf(w, "text")      // want `error return from fmt.Fprintf\(\) is not checked`
	_, _ = f.Fprintln(w, "x") // want `error return from fmt.Fprintln\(\) is not checked`
}

func GoodParenthesizedConsole() {
	f.Fprintln((os.Stderr), "diagnostic")
	_, _ = (f.Fprintln)((os.Stdout), "result")
}

func GoodParenthesizedSuppressed() {
	w := &bytes.Buffer{}
	(f.Fprintf)(w, "text") //nolint:fprintferrorunchecked
}

type fakeFmt struct{}

func (fakeFmt) Fprintf(w *bytes.Buffer, format string) (int, error) { return 0, nil }

func GoodShadowedFmt() {
	fmt := fakeFmt{}
	w := &bytes.Buffer{}
	fmt.Fprintf(w, "text")
}
