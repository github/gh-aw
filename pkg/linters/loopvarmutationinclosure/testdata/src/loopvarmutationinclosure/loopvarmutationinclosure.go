// Package loopvarmutationinclosure is the test fixture for the loopvarmutationinclosure analyzer.
package loopvarmutationinclosure

import "fmt"

// BadForLoopGoroutine captures loop variable in goroutine without shadowing
func BadForLoopGoroutine() {
	for i := 0; i < 10; i++ {
		go func() {
			fmt.Println(i) // want `loop variable i captured in closure`
		}()
	}
}

// BadForLoopDefer captures loop variable in defer without shadowing
func BadForLoopDefer() {
	for i := 0; i < 10; i++ {
		defer func() {
			fmt.Println(i) // want `loop variable i captured in closure`
		}()
	}
}

// BadRangeLoopGoroutine captures range loop variable in goroutine
func BadRangeLoopGoroutine() {
	items := []string{"a", "b", "c"}
	for _, item := range items {
		go func() {
			fmt.Println(item) // want `loop variable item captured in closure`
		}()
	}
}

// BadRangeLoopKey captures range loop key in goroutine
func BadRangeLoopKey() {
	items := []string{"a", "b", "c"}
	for i, _ := range items {
		go func() {
			fmt.Println(i) // want `loop variable i captured in closure`
		}()
	}
}

// BadForLoopAnonymousFunc captures loop variable in anonymous function
func BadForLoopAnonymousFunc() {
	for i := 0; i < 10; i++ {
		f := func() {
			fmt.Println(i) // want `loop variable i captured in closure`
		}
		f()
	}
}

// BadMultipleCaptures captures multiple loop variables
func BadMultipleCaptures() {
	for i := 0; i < 10; i++ {
		for j := 0; j < 5; j++ {
			go func() {
				fmt.Println(i, j) // want `loop variable i captured in closure` `loop variable j captured in closure`
			}()
		}
	}
}

// GoodForLoopShadow properly shadows the loop variable
func GoodForLoopShadow() {
	for i := 0; i < 10; i++ {
		i := i // Shadowing
		go func() {
			fmt.Println(i) // Safe: uses shadowed i
		}()
	}
}

// GoodForLoopParameter properly passes loop variable as parameter
func GoodForLoopParameter() {
	for i := 0; i < 10; i++ {
		go func(idx int) {
			fmt.Println(idx) // Safe: uses parameter
		}(i)
	}
}

// GoodRangeLoopShadow properly shadows range variable
func GoodRangeLoopShadow() {
	items := []string{"a", "b", "c"}
	for _, item := range items {
		item := item // Shadowing
		go func() {
			fmt.Println(item) // Safe: uses shadowed item
		}()
	}
}

// GoodRangeLoopParameter properly passes range variable as parameter
func GoodRangeLoopParameter() {
	items := []string{"a", "b", "c"}
	for _, item := range items {
		go func(s string) {
			fmt.Println(s) // Safe: uses parameter
		}(item)
	}
}

// GoodNoCapture doesn't capture any loop variables
func GoodNoCapture() {
	for i := 0; i < 10; i++ {
		j := i
		go func() {
			fmt.Println(j) // Safe: j is not a loop variable
		}()
	}
}

// GoodBlankIdentifier blank identifiers are ignored
func GoodBlankIdentifier() {
	for i := 0; i < 10; i++ {
		go func() {
			_ = 1 // No capture, so OK
		}()
	}
}

// GoodDeferWithShadow defer with shadowed variable
func GoodDeferWithShadow() {
	for i := 0; i < 10; i++ {
		i := i // Shadowing
		defer func() {
			fmt.Println(i) // Safe: uses shadowed i
		}()
	}
}

// BadDeferMultipleVars captures multiple variables in defer
func BadDeferMultipleVars() {
	for i := 0; i < 10; i++ {
		for j := 0; j < 5; j++ {
			defer func() {
				fmt.Println(i, j) // want `loop variable i captured in closure` `loop variable j captured in closure`
			}()
		}
	}
}

// GoodNestedShadowing nested loop with proper shadowing
func GoodNestedShadowing() {
	for i := 0; i < 10; i++ {
		i := i
		for j := 0; j < 5; j++ {
			j := j
			go func() {
				fmt.Println(i, j) // Safe: both shadowed
			}()
		}
	}
}

// GoodNestedMixedScoping mixed shadowing and parameter passing
func GoodNestedMixedScoping() {
	for i := 0; i < 10; i++ {
		i := i
		for j := 0; j < 5; j++ {
			go func(jCopy int) {
				fmt.Println(i, jCopy) // Safe: i shadowed, j passed as param
			}(j)
		}
	}
}

// BadPartialCapture one variable captured, one safe
func BadPartialCapture() {
	for i := 0; i < 10; i++ {
		i := i // Safe shadowing for i
		for j := 0; j < 5; j++ {
			go func() {
				fmt.Println(i, j) // want `loop variable j captured in closure`
			}()
		}
	}
}

// GoodLoopVarNotUsed loop variable not used in closure
func GoodLoopVarNotUsed() {
	for i := 0; i < 10; i++ {
		go func() {
			fmt.Println("hello") // No capture
		}()
	}
}

// GoodMultipleShadows multiple variables shadowed
func GoodMultipleShadows() {
	items := []string{"a", "b", "c"}
	for i, item := range items {
		i := i
		item := item
		go func() {
			fmt.Println(i, item) // Safe: both shadowed
		}()
	}
}

// BadComplexCapture complex case with capture and modification
func BadComplexCapture() {
	for i := 0; i < 10; i++ {
		f := func() string {
			return fmt.Sprintf("%d", i) // want `loop variable i captured in closure`
		}
		_ = f()
	}
}

// GoodDeclShadow variable shadowed via var declaration
func GoodDeclShadow() {
	for i := 0; i < 10; i++ {
		go func() {
			var i int // This creates a new i, but doesn't shadow the loop var at point of use
			fmt.Println(i) // This uses the locally declared i, not the loop variable
		}()
	}
}

// BadUnaryExpression loop variable used in unary expression
func BadUnaryExpression() {
	for i := 0; i < 10; i++ {
		go func() {
			x := &i // want `loop variable i captured in closure`
			_ = x
		}()
	}
}

// GoodParameterShadow parameter shadows loop variable name
func GoodParameterShadow() {
	for i := 0; i < 10; i++ {
		go func(i int) {
			fmt.Println(i) // Safe: parameter i shadows loop i
		}(i)
	}
}
