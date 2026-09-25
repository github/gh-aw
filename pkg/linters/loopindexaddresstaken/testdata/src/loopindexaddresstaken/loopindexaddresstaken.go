package loopindexaddresstaken

import "fmt"

// BadGoroutineAddressOfIndex flags taking address of loop index in goroutine.
func BadGoroutineAddressOfIndex(items []string) {
	for i, v := range items {
		go func() {
			fmt.Println(&i) // want `taking the address of loop variable i in a goroutine; the variable may be reassigned before the goroutine runs`
			fmt.Println(v)
		}()
	}
}

// BadGoroutineAddressOfSingleIndex flags taking address of loop index in goroutine.
func BadGoroutineAddressOfSingleIndex(items []string) {
	for i := range items {
		go func() {
			fmt.Println(&i) // want `taking the address of loop variable i in a goroutine; the variable may be reassigned before the goroutine runs`
		}()
	}
}

// BadDeferAddressOfIndex flags taking address of loop index in defer.
func BadDeferAddressOfIndex(items []string) {
	for i := range items {
		defer func() {
			fmt.Println(&i) // want `taking the address of loop variable i in a deferred function; the variable may be reassigned before the function executes`
		}()
	}
}

// BadDeferAddressOfIndexWithValue flags taking address of loop index in defer.
func BadDeferAddressOfIndexWithValue(items []string) {
	for i, v := range items {
		defer func() {
			fmt.Println(&i) // want `taking the address of loop variable i in a deferred function; the variable may be reassigned before the function executes`
			fmt.Println(v)
		}()
	}
}

// GoodGoroutineCaptureByValue is fine — loop variable passed as parameter.
func GoodGoroutineCaptureByValue(items []string) {
	for i, v := range items {
		go func(idx int, val string) {
			fmt.Println(&idx) // Not the loop variable
			fmt.Println(val)
		}(i, v)
	}
}

// GoodGoroutineNoAddress is fine — loop variable used but not addressed.
func GoodGoroutineNoAddress(items []string) {
	for i, v := range items {
		go func() {
			fmt.Println(i) // Variable used but not addressed
			fmt.Println(v)
		}()
	}
}

// GoodGoroutineAddressOfValue is fine — addressing value, not index.
func GoodGoroutineAddressOfValue(items []string) {
	for _, v := range items {
		go func() {
			fmt.Println(&v) // Addressing value, not index
		}()
	}
}

// GoodDeferCaptureByValue is fine — loop variable passed as parameter.
func GoodDeferCaptureByValue(items []string) {
	for i, v := range items {
		defer func(idx int, val string) {
			fmt.Println(&idx) // Not the loop variable
			fmt.Println(val)
		}(i, v)
	}
}

// GoodDeferNoAddress is fine — loop variable used but not addressed.
func GoodDeferNoAddress(items []string) {
	for i, v := range items {
		defer func() {
			fmt.Println(i) // Variable used but not addressed
			fmt.Println(v)
		}()
	}
}

// GoodDeferAddressOfValue is fine — addressing value, not index.
func GoodDeferAddressOfValue(items []string) {
	for _, v := range items {
		defer func() {
			fmt.Println(&v) // Addressing value, not index
		}()
	}
}

// GoodUnrelatedVar is fine — addressing a different variable.
func GoodUnrelatedVar(items []string) {
	x := 0
	for i, v := range items {
		go func() {
			fmt.Println(&x) // Not the loop variable
			fmt.Println(i)
			fmt.Println(v)
		}()
	}
}

// GoodNestedFuncLit is fine — nested function literal creates new scope.
func GoodNestedFuncLit(items []string) {
	for i, v := range items {
		go func() {
			innerFunc := func() {
				fmt.Println(&i) // Inside nested func literal — different scope
			}
			innerFunc()
			fmt.Println(v)
		}()
	}
}

// GoodClassicFor is fine with classic for loop (not range).
func GoodClassicFor(n int) {
	for i := 0; i < n; i++ {
		go func() {
			fmt.Println(i)
		}()
	}
}
