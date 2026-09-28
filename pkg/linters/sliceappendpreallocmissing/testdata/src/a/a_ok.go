// Package a is the test fixture for the sliceappendpreallocmissing analyzer (negative cases).
package a

func preallocatedWithCapacity() {
	s := make([]int, 0, 10)
	for i := 0; i < 10; i++ {
		s = append(s, i)
	}
}

func dynamicLoopCount() {
	n := 10
	s := []int{}
	for i := 0; i < n; i++ {
		s = append(s, i)
	}
}

func appendOutsideLoop() {
	s := []int{}
	s = append(s, 1)
}

func sliceWithInitialValues() {
	s := []int{1, 2, 3}
	for i := 0; i < 10; i++ {
		s = append(s, i)
	}
}

func nilSlice() {
	var s []int
	for i := 0; i < 10; i++ {
		s = append(s, i)
	}
}

func suppressedWithNolint() {
	s := []int{}
	for i := 0; i < 10; i++ { //nolint:sliceappendpreallocmissing
		s = append(s, i)
	}
}

func rangeWithVariableLength() {
	items := []int{1, 2, 3}
	s := []int{}
	for _, v := range items {
		s = append(s, v)
	}
}

func reassignedWithCapacityBeforeLoop() {
	s := []int{}
	s = make([]int, 0, 10)
	for i := 0; i < 10; i++ {
		s = append(s, i)
	}
}

func steppedLoop() {
	s := []int{}
	for i := 0; i < 10; i += 2 {
		s = append(s, i)
	}
}

func mismatchedPostVariable(j int) {
	s := []int{}
	for i := 0; i < 10; j++ {
		s = append(s, i)
	}
	_ = s
}

func decrementingLoop() {
	s := []int{}
	for i := 0; i < 10; i-- {
		s = append(s, i)
	}
	_ = s
}

func conditionalAppend() {
	s := []int{}
	for i := 0; i < 10; i++ {
		if i%2 == 0 {
			s = append(s, i)
		}
	}
	_ = s
}
