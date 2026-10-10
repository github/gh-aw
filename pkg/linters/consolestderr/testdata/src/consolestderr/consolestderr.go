package consolestderr

import (
	f "fmt"
	o "os"

	c "github.com/github/gh-aw/pkg/console"
)

func examples() {
	f.Fprintln(o.Stderr, c.FormatInfoMessage("hello"))                                    // want "use FormatInfoMessageStderr"
	f.Fprint((o.Stderr), (c.FormatInfoMessage)("hello"))                                  // want "use FormatInfoMessageStderr"
	f.Fprintf(o.Stderr, c.FormatInfoMessage("%s\n"), "hello")                             // want "use FormatInfoMessageStderr"
	f.Fprintf(o.Stderr, "%s %s", "prefix", f.Sprintf("%s", c.FormatInfoMessage("hello"))) // want "use FormatInfoMessageStderr"
	_, _ = f.Fprintln(o.Stderr, c.RenderStruct([]string{"hello"}))                        // want "use RenderStructStderr"
	f.Fprintln(o.Stdout, c.FormatInfoMessage("hello"))
	f.Fprintln(o.Stderr, c.FormatInfoMessageStderr("hello"))
	f.Fprintln(o.Stderr, c.FormatErrorMessage("boom"), c.FormatFileSize(1))
	//nolint:consolestderr
	f.Fprintln(o.Stderr, c.FormatInfoMessage("suppressed"))
	f.Fprintln(o.Stderr, func() string { return c.FormatInfoMessage("not evaluated here") })
	shadow()
}

func shadow() {
	o := struct{ Stderr *o.File }{}
	f.Fprintln(o.Stderr, c.FormatInfoMessage("not os.Stderr"))
}
