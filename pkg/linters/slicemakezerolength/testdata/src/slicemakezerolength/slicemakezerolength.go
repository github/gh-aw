package slicemakezerolength

func badZeroLengthNoCapacity(items []string) {
	s := make([]string, 0) // want `make\(\[\]string, 0\) before this range loop can use capacity len\(items\)`
	for _, item := range items {
		s = append(s, item)
	}
	_ = s
}

func badEquivalentZero(items map[string]int) {
	var s = make([]int, 0x0) // want `make\(\[\]int, 0x0\) before this range loop can use capacity len\(items\)`
	for _, item := range items {
		s = append(s, item)
	}
	_ = s
}

func goodWithCapacity(items []string) {
	s := make([]string, 0, 10)
	for _, item := range items {
		s = append(s, item)
	}
	_ = s
}

func goodWithLength() {
	s := make([]string, 5)
	_ = s
}

func goodWithLengthAndCapacity() {
	s := make([]string, 5, 10)
	_ = s
}

func goodArrayType() {
	s := [10]string{}
	_ = s
}

func goodArrayLiteral() {
	s := []string{"a", "b"}
	_ = s
}

func goodWithoutGrowth() {
	s := make([]string, 0)
	_ = s
}

func goodConditionalGrowth(items []string) {
	s := make([]string, 0)
	for _, item := range items {
		if item != "" {
			s = append(s, item)
		}
	}
	_ = s
}

func goodIndeterminateRange(items <-chan string) {
	s := make([]string, 0)
	for item := range items {
		s = append(s, item)
	}
	_ = s
}

func goodMultipleAppends(items []string) {
	s := make([]string, 0)
	for _, item := range items {
		s = append(s, item, item)
	}
	_ = s
}

func goodRangesOverTarget() {
	s := make([]string, 0)
	for _, item := range s[:0] {
		s = append(s, item)
	}
	_ = s
}

func suppressed(items []string) {
	//nolint:slicemakezerolength
	s := make([]string, 0)
	for _, item := range items {
		s = append(s, item)
	}
	_ = s
}
