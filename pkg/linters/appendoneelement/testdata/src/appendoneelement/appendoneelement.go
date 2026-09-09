package appendoneelement

func bad() {
	s := []int{1, 2}
	s = append(s, []int{3}...) // want `append\(s, \[\]int\{3\}\.\.\.\) can be simplified to append\(s, 3\)`
	_ = s
}

func badString() {
	s := []string{"a"}
	s = append(s, []string{"b"}...) // want `append\(s, \[\]string\{"b"\}\.\.\.\) can be simplified to append\(s, "b"\)`
	_ = s
}

func badVar() {
	s := []int{1}
	x := 99
	s = append(s, []int{x}...) // want `append\(s, \[\]int\{x\}\.\.\.\) can be simplified to append\(s, x\)`
	_ = s
}

func badWithComments() {
	s := []int{1}
	x := 99
	s = append(s, []int{x /* important */}...) // want `append\(s, \[\]int\{x\}\.\.\.\) can be simplified to append\(s, x\)`
	_ = s
}

func good() {
	s := []int{1, 2}
	// Multiple elements — keep the spread form.
	s = append(s, []int{3, 4}...)
	_ = s
}

func goodDirect() {
	s := []int{1, 2}
	// Direct element, no spread: already idiomatic.
	s = append(s, 3)
	_ = s
}

func goodEmptyLit() {
	s := []int{1, 2}
	other := []int{3, 4}
	// Spreading a variable, not a literal.
	s = append(s, other...)
	_ = s
}

func goodKeyedLiteral() {
	s := []int{1}
	// One AST element, but keyed form represents multiple runtime elements.
	s = append(s, []int{5: 3}...)
	_ = s
}

func goodTypeElidedNestedLiteral() {
	s := [][]int{{0}}
	// Nested literal omits type; suggested text would be invalid without reconstruction.
	s = append(s, [][]int{{1}}...)
	_ = s
}

func goodNolint() {
	s := []int{1}
	s = append(s, []int{2}...) //nolint:appendoneelement
	_ = s
}

func goodShadowedAppend() {
	append := func(dst []int, rest ...int) []int { return dst }
	s := []int{1}
	s = append(s, []int{2}...)
	_ = s
}
