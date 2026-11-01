package boolliteralcompare

type flag bool

func ready() bool { return true }

func flagged(b bool, f flag, m map[string]bool) {
	_ = b == true        // want `comparing a bool to true is redundant`
	_ = b != false       // want `comparing a bool to false is redundant`
	_ = b == false       // want `comparing a bool to false is redundant`
	_ = b != true        // want `comparing a bool to true is redundant`
	_ = true == b        // want `comparing a bool to true is redundant`
	_ = f == true        // want `comparing a bool to true is redundant`
	_ = ready() == false // want `comparing a bool to false is redundant`
	_ = m["a"] == true   // want `comparing a bool to true is redundant`
	if b == true {       // want `comparing a bool to true is redundant`
		return
	}
}

func notFlagged(b bool, a any, m map[string]any) {
	_ = b
	_ = !b
	_ = a == true
	_ = m["x"] != false
	_ = b == ready()
	_ = true == true
	_ = b == true //nolint:boolliteralcompare
}
