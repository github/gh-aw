package uncheckedflushreturn

import (
	"bufio"
	"strings"
	"text/tabwriter"
)

func bad() {
	var sb strings.Builder
	bw := bufio.NewWriter(&sb)
	bw.Flush()   // want `error return from Flush\(\) is discarded`
	(bw.Flush()) // want `error return from Flush\(\) is discarded`

	tw := tabwriter.NewWriter(&sb, 0, 0, 1, ' ', 0)
	tw.Flush() // want `error return from Flush\(\) is discarded`
}

func badBlankAssign() {
	var sb strings.Builder
	bw := bufio.NewWriter(&sb)
	_ = bw.Flush()   // want `error return from Flush\(\) is discarded`
	_ = (bw.Flush()) // want `error return from Flush\(\) is discarded`
}

func deferBad() {
	var sb strings.Builder
	bw := bufio.NewWriter(&sb)
	defer bw.Flush() // want `error return from Flush\(\) is discarded`
}

func good() {
	var sb strings.Builder
	bw := bufio.NewWriter(&sb)
	if err := bw.Flush(); err != nil {
		_ = err
	}

	tw := tabwriter.NewWriter(&sb, 0, 0, 1, ' ', 0)
	err := tw.Flush()
	if err != nil {
		_ = err
	}
}

func suppressed() {
	var sb strings.Builder
	bw := bufio.NewWriter(&sb)
	//nolint:uncheckedflushreturn
	bw.Flush()

	tw := tabwriter.NewWriter(&sb, 0, 0, 1, ' ', 0)
	tw.Flush() //nolint:uncheckedflushreturn
}

func deferSuppressed() {
	var sb strings.Builder
	bw := bufio.NewWriter(&sb)
	defer bw.Flush() //nolint:uncheckedflushreturn
}
