// Package a is the test fixture for the sliceappendpreallocmissing analyzer.
package a

func simpleLoopAppend() {
	s := []int{}
	for i := 0; i < 10; i++ {
		s = append(s, i) // want `slice s should be pre-allocated with capacity 10 instead of dynamically growing via repeated append calls in a loop`
	}
}

func rangeLoopAppendLiteral() {
	s := []int{}
	for _, v := range []int{1, 2, 3, 4, 5} {
		s = append(s, v) // want `slice s should be pre-allocated with capacity len\(\[\]int\{1, 2, 3, 4, 5\}\) instead of dynamically growing via repeated append calls in a loop`
	}
}

func nestedLoops() {
	for i := 0; i < 5; i++ {
		inner := []int{}
		for j := 0; j < 3; j++ {
			inner = append(inner, j) // want `slice inner should be pre-allocated with capacity 3 instead of dynamically growing via repeated append calls in a loop`
		}
	}
}

func multipleAppends() {
	s := []int{}
	for i := 0; i < 10; i++ {
		s = append(s, i) // want `slice s should be pre-allocated with capacity 10 instead of dynamically growing via repeated append calls in a loop`
		s = append(s, i*2)
	}
}

func loopWithLeEq() {
	s := []int{}
	for i := 0; i <= 9; i++ {
		s = append(s, i) // want `slice s should be pre-allocated with capacity 9 \+ 1 instead of dynamically growing via repeated append calls in a loop`
	}
}
