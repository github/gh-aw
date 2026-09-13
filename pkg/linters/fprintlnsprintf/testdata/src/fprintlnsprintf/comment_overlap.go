package fprintlnsprintf

import (
	"fmt"
	"io"
)

func overlapFprintlnSprintf(w io.Writer, s string) {
	fmt.Fprintln(w, fmt.Sprintf( /* keep */ "%s", s)) // want "use fmt.Fprintf"
}
