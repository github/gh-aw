package manualmutexunlock

import (
	"sync"
)

// Correct: defer unlock immediately after lock
func GoodMutexPattern() {
	var mu sync.Mutex
	mu.Lock()
	defer mu.Unlock()

	// ... do work ...
}

// Wrong: manual unlock without defer - should be flagged
func BadMutexPattern() {
	var mu sync.Mutex
	mu.Lock() // want `mutex Unlock\(\) should be deferred immediately after Lock\(\) to prevent deadlocks on panic or early return`

	// ... do work ...

	mu.Unlock()
}

// Correct: RWMutex with defer
func GoodRWMutexPattern() {
	var mu sync.RWMutex
	mu.RLock()
	defer mu.RUnlock()

	// ... do work ...
}

// Wrong: RWMutex without defer - should be flagged
func BadRWMutexPattern() {
	var mu sync.RWMutex
	mu.RLock() // want `mutex Unlock\(\) should be deferred immediately after Lock\(\) to prevent deadlocks on panic or early return`

	// ... do work ...

	mu.RUnlock()
}

// Wrong: write lock without defer - should be flagged
func BadRWMutexWriteLock() {
	var mu sync.RWMutex
	mu.Lock() // want `mutex Unlock\(\) should be deferred immediately after Lock\(\) to prevent deadlocks on panic or early return`

	// ... do work ...

	mu.Unlock()
}

// Correct: nested function with defer
func GoodNestedPattern() {
	var mu sync.Mutex
	mu.Lock()
	defer mu.Unlock()

	func() {
		// This is a closure, analyzed separately
	}()
}

// Correct: multiple locks with defers
func GoodMultipleMutexes() {
	var mu1 sync.Mutex
	var mu2 sync.Mutex

	mu1.Lock()
	defer mu1.Unlock()

	mu2.Lock()
	defer mu2.Unlock()

	// ... do work ...
}

// Wrong: multiple locks, one without defer
func BadMultipleMutexes() {
	var mu1 sync.Mutex
	var mu2 sync.Mutex

	mu1.Lock()
	defer mu1.Unlock()

	mu2.Lock() // want `mutex Unlock\(\) should be deferred immediately after Lock\(\) to prevent deadlocks on panic or early return`

	// ... do work ...

	mu2.Unlock()
}

type guarded struct {
	mu sync.Mutex
}

// Wrong: selector-based mutex receiver without defer - should be flagged
func BadSelectorPattern() {
	var g guarded
	g.mu.Lock() // want `mutex Unlock\(\) should be deferred immediately after Lock\(\) to prevent deadlocks on panic or early return`
	g.mu.Unlock()
}

// Wrong: repeated lock on same mutex should still report earlier unresolved violation
func BadRepeatedLockBeforeGood() {
	var mu sync.Mutex
	mu.Lock() // want `mutex Unlock\(\) should be deferred immediately after Lock\(\) to prevent deadlocks on panic or early return`
	mu.Unlock()

	mu.Lock()
	defer mu.Unlock()
}

func NolintPreviousLineSuppressed() {
	var mu sync.Mutex
	//nolint:manualmutexunlock
	mu.Lock()
	mu.Unlock()
}

func NolintSameLineSuppressed() {
	var mu sync.Mutex
	mu.Lock() //nolint:manualmutexunlock
	mu.Unlock()
}

// Wrong: two distinct instances of same struct type — manual unlock of a.mu
// should be flagged even though b.mu has a proper defer.
func BadTwoGuardsManualFirst(a, b *guarded) {
	a.mu.Lock() // want `mutex Unlock\(\) should be deferred immediately after Lock\(\) to prevent deadlocks on panic or early return`
	b.mu.Lock()
	defer b.mu.Unlock()
	a.mu.Unlock()
}

// Wrong: same scenario but unlock of a.mu happens before b.mu.Lock() —
// the re-lock path also catches this (order-independence).
func BadTwoGuardsManualSecond(a, b *guarded) {
	a.mu.Lock() // want `mutex Unlock\(\) should be deferred immediately after Lock\(\) to prevent deadlocks on panic or early return`
	a.mu.Unlock()
	b.mu.Lock()
	defer b.mu.Unlock()
}

// Correct: both instances deferred — no report expected.
func GoodTwoGuardsBothDeferred(a, b *guarded) {
	a.mu.Lock()
	defer a.mu.Unlock()
	b.mu.Lock()
	defer b.mu.Unlock()
}

func BadSiblingUnlock(mu *sync.Mutex, deferred bool) {
	mu.Lock() // want `mutex Unlock\(\) should be deferred immediately after Lock\(\) to prevent deadlocks on panic or early return`
	if deferred {
		defer mu.Unlock()
	} else {
		mu.Unlock()
	}
}
