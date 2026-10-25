package wgdonenotdeferred

import "sync"

// Bad: Done() not deferred in a goroutine — if the goroutine panics the WaitGroup will deadlock.
func BadGoroutine(wg *sync.WaitGroup) {
	go func() {
		wg.Done() // want `sync.WaitGroup Done\(\) should be deferred to prevent deadlock if the function panics`
		doWork()
	}()
}

// Bad: Done() not deferred in a regular function that receives the WaitGroup pointer.
func BadHelper(wg *sync.WaitGroup) {
	doWork()
	wg.Done() // want `sync.WaitGroup Done\(\) should be deferred to prevent deadlock if the function panics`
}

// Bad: parentheses do not make a non-deferred Done() call safe.
func BadParenthesized(wg *sync.WaitGroup) {
	(wg.Done()) // want `sync.WaitGroup Done\(\) should be deferred to prevent deadlock if the function panics`
	(wg.Done)() // want `sync.WaitGroup Done\(\) should be deferred to prevent deadlock if the function panics`
}

// Bad: non-deferred Done() with early return paths.
func BadEarlyReturn(wg *sync.WaitGroup, err error) {
	if err != nil {
		return
	}
	doWork()
	wg.Done() // want `sync.WaitGroup Done\(\) should be deferred to prevent deadlock if the function panics`
}

// Bad: Done() not deferred on a value receiver WaitGroup field.
type Worker struct {
	wg sync.WaitGroup
}

func (w *Worker) BadMethod() {
	w.wg.Done() // want `sync.WaitGroup Done\(\) should be deferred to prevent deadlock if the function panics`
}

// Good: Done() is properly deferred in a goroutine.
func GoodGoroutine(wg *sync.WaitGroup) {
	go func() {
		defer wg.Done()
		doWork()
	}()
}

// Good: Done() is properly deferred in a regular function.
func GoodHelper(wg *sync.WaitGroup) {
	defer wg.Done()
	doWork()
}

// Good: Done() is properly deferred on a struct field.
func (w *Worker) GoodMethod() {
	defer w.wg.Done()
	doWork()
}

// Good: deferred closure containing Done() is already safe.
func GoodDeferredClosure(wg *sync.WaitGroup) {
	defer func() {
		wg.Done()
		(wg.Done())
	}()
	doWork()
}

// Good: a parenthesized deferred closure is also safe.
func GoodParenthesizedDeferredClosure(wg *sync.WaitGroup) {
	defer (func() {
		(wg.Done())
	})()
}

// Good: loop-body Done() calls are intentionally not flagged.
func GoodLoopDone(wg *sync.WaitGroup, n int) {
	for range n {
		wg.Done()
		(wg.Done())
	}
}

// Bad: a goroutine launched inside a loop still needs to defer Done().
func BadGoroutineInLoop(wg *sync.WaitGroup, n int) {
	for range n {
		go func() {
			wg.Done() // want `sync.WaitGroup Done\(\) should be deferred to prevent deadlock if the function panics`
			doWork()
		}()
	}
}

// Good: a goroutine launched inside a loop may safely defer Done().
func GoodDeferredGoroutineInLoop(wg *sync.WaitGroup, n int) {
	for range n {
		go func() {
			defer wg.Done()
			doWork()
		}()
	}
}

// Bad: embedded WaitGroup still needs deferred Done().
type EmbeddedWorker struct {
	sync.WaitGroup
}

func (w *EmbeddedWorker) BadEmbedded() {
	w.Done() // want `sync.WaitGroup Done\(\) should be deferred to prevent deadlock if the function panics`
}

// Good: a different type has a Done() method — must not be flagged.
type Finisher struct{}

func (f *Finisher) Done() {}

func GoodOtherDone() {
	f := &Finisher{}
	f.Done()
	(f.Done())
}

func doWork() {}

func NolintPreviousLineSuppressed(wg *sync.WaitGroup) {
	//nolint:wgdonenotdeferred
	wg.Done()
}

func NolintSameLineSuppressed(wg *sync.WaitGroup) {
	wg.Done()   //nolint:wgdonenotdeferred
	(wg.Done()) //nolint:wgdonenotdeferred
}
