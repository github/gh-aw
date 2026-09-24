package loopappendinefficency

func bad() {
	items := []int{1, 2, 3}

	// Basic range loop – should be flagged.
	result := []int{}
	for _, item := range items {
		result = append(result, item) // want `append inside a loop without pre-allocation`
	}
	_ = result

	// Classic for loop – should be flagged.
	nums := []string{}
	for i := 0; i < len(items); i++ {
		nums = append(nums, string(rune(items[i]))) // want `append inside a loop without pre-allocation`
	}
	_ = nums

	// x = append(x, ...) form in a range loop – should be flagged.
	accum := []byte{}
	for _, item := range items {
		accum = append(accum, byte(item)) // want `append inside a loop without pre-allocation`
	}
	_ = accum

	// x = append(x, ...) form in a classic for loop – should be flagged.
	strs := []string{}
	for i := 0; i < len(items); i++ {
		strs = append(strs, "item") // want `append inside a loop without pre-allocation`
	}
	_ = strs

	// for-init slice accumulator – the init clause runs once, so the variable
	// carries state across all iterations and is a genuine accumulator.
	for s := []int{}; len(s) < 10; {
		s = append(s, 1) // want `append inside a loop without pre-allocation`
		_ = s
	}
}

func good() {
	items := []int{1, 2, 3}

	// append outside any loop – not flagged.
	nums := []int{1, 2}
	nums = append(nums, 3)
	_ = nums

	// append inside a func literal inside a loop – not flagged. The linter
	// intentionally stops at func literal boundaries.
	acc := []int{}
	for _, item := range items {
		func() {
			acc = append(acc, item)
		}()
	}
	_ = acc

	// x = append(y, ...) (different variable) – not flagged.
	accum := []int{}
	other := []int{}
	for _, item := range items {
		accum = append(other, item)
	}
	_ = accum

	// Range value variable reassigned per iteration – not a cross-iteration
	// accumulator, so not flagged.
	myInts := []int{}
	for _, line := range items {
		myInts = append([]int{}, line)
		_ = myInts
	}

	// x = append(x, ...) inside a func literal inside a loop – not flagged. The linter
	// intentionally stops at func literal boundaries.
	accum2 := []int{}
	for _, item := range items {
		func() {
			accum2 = append(accum2, item)
		}()
	}
	_ = accum2

	// Variable declared inside the loop body is a per-iteration local, not a
	// cross-iteration accumulator – not flagged.
	for _, item := range items {
		var local []int
		local = append(local, item)
		_ = local
	}

	// append without assignment – not flagged.
	result2 := []int{}
	for _, item := range items {
		_ = append(result2, item)
	}
	_ = result2
}

func nolintDirective() {
	items := []int{1, 2, 3}

	result := []int{}
	for _, item := range items { //nolint:loopappendinefficency
		result = append(result, item)
	}
	_ = result

	result2 := []int{}
	for _, item := range items { //nolint:loopappendinefficency
		_ = item
		result2 = append(result2, item)
	}
	_ = result2
}
