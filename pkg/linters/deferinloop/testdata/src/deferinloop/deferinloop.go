package deferinloop

import "os"

// BadForRange flags defer inside a range loop — resource leak.
func BadForRange(paths []string) {
	for _, p := range paths {
		f, _ := os.Open(p)
		defer f.Close() // want `defer inside a loop does not execute at the end of each iteration; it runs when the enclosing function returns, which can cause resource leaks`
	}
}

// BadForStmt flags defer inside a classic for loop.
func BadForStmt(paths []string) {
	for i := 0; i < len(paths); i++ {
		f, _ := os.Open(paths[i])
		defer f.Close() // want `defer inside a loop does not execute at the end of each iteration; it runs when the enclosing function returns, which can cause resource leaks`
	}
}

// BadForever flags defer inside an infinite for loop.
func BadForever() {
	for {
		f, _ := os.Open("file")
		defer f.Close() // want `defer inside a loop does not execute at the end of each iteration; it runs when the enclosing function returns, which can cause resource leaks`
		break
	}
}

// BadNestedLoop flags defer inside a nested loop.
func BadNestedLoop(matrix [][]string) {
	for _, row := range matrix {
		for _, p := range row {
			f, _ := os.Open(p)
			defer f.Close() // want `defer inside a loop does not execute at the end of each iteration; it runs when the enclosing function returns, which can cause resource leaks`
		}
	}
}

// GoodExplicitClose is fine — explicit close each iteration.
func GoodExplicitClose(paths []string) {
	for _, p := range paths {
		f, _ := os.Open(p)
		f.Close()
	}
}

// BadIfInLoop flags defer inside an if block within a for loop.
func BadIfInLoop(paths []string, cond bool) {
	for _, p := range paths {
		if cond {
			f, _ := os.Open(p)
			defer f.Close() // want `defer inside a loop does not execute at the end of each iteration; it runs when the enclosing function returns, which can cause resource leaks`
		}
	}
}

// BadSelectInLoop flags defer inside a select inside a for loop.
func BadSelectInLoop(ch <-chan string) {
	for {
		select {
		case p := <-ch:
			f, _ := os.Open(p)
			defer f.Close() // want `defer inside a loop does not execute at the end of each iteration; it runs when the enclosing function returns, which can cause resource leaks`
		}
	}
}

// GoodFuncLitInsideLoop is fine — defer is inside a closure (new scope).
func GoodFuncLitInsideLoop(paths []string) {
	for _, p := range paths {
		func() {
			f, _ := os.Open(p)
			defer f.Close() // FuncLit boundary — not flagged
		}()
	}
}

// GoodGoFuncLitInsideLoop is fine — goroutine func literal also forms a new scope.
func GoodGoFuncLitInsideLoop(paths []string) {
	for _, p := range paths {
		go func() {
			f, _ := os.Open(p)
			defer f.Close() // FuncLit boundary — not flagged
		}()
	}
}

func GoodRangeOverFunc() {
	iterator := func(yield func() bool) {
		yield()
	}
	for range iterator {
		defer println()
	}
}

// GoodDeferOutsideLoop is fine — defer is not inside a loop.
func GoodDeferOutsideLoop() {
	f, _ := os.Open("file")
	defer f.Close()
}

// GoodNolintSameLine: suppressed with a nolint directive on the same line.
func GoodNolintSameLine(paths []string) {
	for _, p := range paths {
		f, _ := os.Open(p)
		defer f.Close() //nolint:deferinloop
	}
}

// GoodNolintPreviousLine: suppressed with a nolint directive on the previous line.
func GoodNolintPreviousLine(paths []string) {
	for _, p := range paths {
		f, _ := os.Open(p)
		//nolint:deferinloop
		defer f.Close()
	}
}
