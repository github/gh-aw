package consolestderr

import (
	f "fmt"
	o "os"

	c "github.com/github/gh-aw/pkg/console"
)

func examples() {
	f.Fprintln(o.Stderr, c.FormatInfoMessageStdout("hello"))                                    // want "use FormatInfoMessage"
	f.Fprint((o.Stderr), (c.FormatInfoMessageStdout)("hello"))                                  // want "use FormatInfoMessage"
	f.Fprintf(o.Stderr, c.FormatInfoMessageStdout("%s\n"), "hello")                             // want "use FormatInfoMessage"
	f.Fprintf(o.Stderr, "%s %s", "prefix", f.Sprintf("%s", c.FormatInfoMessageStdout("hello"))) // want "use FormatInfoMessage"
	_, _ = f.Fprintln(o.Stderr, c.RenderStructStdout([]string{"hello"}))                        // want "use RenderStruct"
	f.Fprintln(o.Stdout, c.FormatInfoMessageStdout("hello"))
	f.Fprintln(o.Stdout, c.FormatInfoMessage("hello"))       // want "use FormatInfoMessageStdout"
	f.Fprintln(o.Stdout, c.FormatInfoMessageStderr("hello")) // want "use FormatInfoMessageStdout"
	f.Fprintln(o.Stdout, c.RenderStruct(nil))                // want "use RenderStructStdout"
	f.Fprintln(o.Stderr, c.FormatInfoMessage("hello"))
	f.Fprintln(o.Stderr, c.FormatInfoMessageStderr("hello"))
	f.Fprintln(o.Stderr, c.FormatErrorMessage("boom"), c.FormatFileSize(1))
	//nolint:consolestderr
	f.Fprintln(o.Stderr, c.FormatInfoMessageStdout("suppressed"))
	f.Fprintln(o.Stderr, func() string { return c.FormatInfoMessageStdout("not evaluated here") })
	shadow()
}

func shadow() {
	o := struct{ Stderr *o.File }{}
	f.Fprintln(o.Stderr, c.FormatInfoMessageStdout("not os.Stderr"))
}
