package consolestderr

import (
	"fmt"
	"os"

	c "github.com/github/gh-aw/pkg/console"
)

func options(unknown bool, settings c.RenderOptions) {
	const stderr = true
	fmt.Fprint(os.Stderr, c.RenderStructWithOptions(nil, c.RenderOptions{MaxWidth: 80}))    // want "requires RenderOptions matching Stderr"
	fmt.Fprint(os.Stderr, c.RenderStructWithOptions(nil, c.RenderOptions{Stderr: false}))   // want "requires RenderOptions matching Stderr"
	fmt.Fprint(os.Stderr, c.RenderStructWithOptions(nil, c.RenderOptions{Stderr: unknown})) // want "requires RenderOptions matching Stderr"
	fmt.Fprint(os.Stderr, c.RenderStructWithOptions(nil, settings))                         // want "requires RenderOptions matching Stderr"
	fmt.Fprint(os.Stderr, c.RenderStructWithOptions(nil, c.RenderOptions{}))                // want "requires RenderOptions matching Stderr"
	fmt.Fprint(os.Stderr, c.RenderStructWithOptions(nil, c.RenderOptions{false, 80}))       // want "requires RenderOptions matching Stderr"
	fmt.Fprint(os.Stderr, c.RenderStructWithOptions(nil, c.RenderOptions{Stderr: true, MaxWidth: 80}))
	fmt.Fprint((os.Stderr), (c.RenderStructWithOptions)(nil, (c.RenderOptions{Stderr: stderr})))
	fmt.Fprint(os.Stderr, c.RenderStructWithOptions(nil, c.RenderOptions{true, 80}))
	fmt.Fprint(os.Stdout, c.RenderStructWithOptions(nil, c.RenderOptions{MaxWidth: 80}))
	fmt.Fprint(os.Stdout, c.RenderStructWithOptions(nil, c.RenderOptions{Stderr: true})) // want "requires RenderOptions matching Stdout"
	fmt.Fprint(os.Stdout, c.RenderStructWithOptions(nil, settings))                      // want "requires RenderOptions matching Stdout"
	//nolint:consolestderr
	fmt.Fprint(os.Stderr, c.RenderStructWithOptions(nil, settings))
}
