package timeafterleak

import (
	"context"
	"time"
)

// BadForLoop is the canonical timer-leak: time.After in a for+select Comm.
func BadForLoop(ctx context.Context) {
	for {
		select {
		case <-time.After(time.Second): // want `time\.After creates a new timer on each loop iteration that is not garbage collected until it fires; use time\.NewTimer with Reset and Stop instead`
			doWork()
		case <-ctx.Done():
			return
		}
	}
}

// BadForLoopAssign also leaks: the receive is the Comm of the case clause.
func BadForLoopAssign(ctx context.Context) {
	for {
		select {
		case t := <-time.After(time.Second): // want `time\.After creates a new timer on each loop iteration that is not garbage collected until it fires; use time\.NewTimer with Reset and Stop instead`
			_ = t
		case <-ctx.Done():
			return
		}
	}
}

// BadRangeLoop flags time.After in a range-based loop select.
func BadRangeLoop(items []string, ctx context.Context) {
	for range items {
		select {
		case <-time.After(time.Millisecond): // want `time\.After creates a new timer on each loop iteration that is not garbage collected until it fires; use time\.NewTimer with Reset and Stop instead`
		case <-ctx.Done():
			return
		}
	}
}

// BadNestedLoop: the inner select is still inside a loop regardless of nesting depth.
func BadNestedLoop(ctx context.Context) {
	for {
		for {
			select {
			case <-time.After(time.Second): // want `time\.After creates a new timer on each loop iteration that is not garbage collected until it fires; use time\.NewTimer with Reset and Stop instead`
			case <-ctx.Done():
				return
			}
		}
	}
}

// BadSingleCaseWithDefault: a default clause can preempt the timer — still flagged.
func BadSingleCaseWithDefault(ctx context.Context) {
	for {
		select {
		case <-time.After(time.Second): // want `time\.After creates a new timer on each loop iteration that is not garbage collected until it fires; use time\.NewTimer with Reset and Stop instead`
		default:
		}
	}
}

// BadParenthesized: parentheses around the callee, the call, or the receive
// do not change the leak and are still flagged.
func BadParenthesized(ctx context.Context) {
	for {
		select {
		case <-(time.After)(time.Second): // want `time\.After creates a new timer on each loop iteration that is not garbage collected until it fires; use time\.NewTimer with Reset and Stop instead`
		case <-ctx.Done():
			return
		}
		select {
		case <-(time.After(time.Second)): // want `time\.After creates a new timer on each loop iteration that is not garbage collected until it fires; use time\.NewTimer with Reset and Stop instead`
		case <-ctx.Done():
			return
		}
		select {
		case t := (<-time.After(time.Second)): // want `time\.After creates a new timer on each loop iteration that is not garbage collected until it fires; use time\.NewTimer with Reset and Stop instead`
			_ = t
		case <-ctx.Done():
			return
		}
	}
}

// GoodNoLoop is fine: time.After in a select that is not inside a loop.
func GoodNoLoop(ctx context.Context) {
	select {
	case <-time.After(time.Second):
		doWork()
	case <-ctx.Done():
		return
	}
}

// GoodNewTimer uses the correct pattern: a single timer reused each iteration.
func GoodNewTimer(ctx context.Context) {
	t := time.NewTimer(time.Second)
	defer t.Stop()
	for {
		if !t.Stop() {
			select {
			case <-t.C:
			default:
			}
		}
		t.Reset(time.Second)
		select {
		case <-t.C:
			doWork()
		case <-ctx.Done():
			return
		}
	}
}

// GoodTimeAfterInBody calls time.After inside the case body, not as the Comm.
// When time.After is used in the case body rather than as the Comm expression,
// the goroutine blocks on the receive until the timer fires — each timer is
// fully consumed before the loop can continue, so no timers accumulate.
func GoodTimeAfterInBody(ctx context.Context, ch <-chan struct{}) {
	for {
		select {
		case <-ch:
			<-time.After(time.Second) // in Body, not Comm — not flagged
		case <-ctx.Done():
			return
		}
	}
}

// GoodFuncLitInsideLoop: the select is inside a goroutine closure launched
// per iteration; the for loop does not directly enclose the CommClause.
func GoodFuncLitInsideLoop(ctx context.Context) {
	for {
		go func() {
			select {
			case <-time.After(time.Second): // FuncLit boundary — not flagged
			case <-ctx.Done():
			}
		}()
		<-ctx.Done()
		return
	}
}

// GoodSingleCaseSelect: the select has only one case and no default, so the
// timer must fire before the loop continues — no timer accumulation is possible.
func GoodSingleCaseSelect() {
	for {
		select {
		case <-time.After(time.Second): // single case, no default — not flagged
			doWork()
		}
	}
}

// GoodNolintPreviousLine: suppressed with a nolint directive on the previous line.
func GoodNolintPreviousLine(ctx context.Context) {
	for {
		select {
		//nolint:timeafterleak
		case <-time.After(time.Second):
			doWork()
		case <-ctx.Done():
			return
		}
	}
}

// GoodNolintSameLine: suppressed with a nolint directive on the same line.
func GoodNolintSameLine(ctx context.Context) {
	for {
		select {
		case <-time.After(time.Second): //nolint:timeafterleak
			doWork()
		case <-ctx.Done():
			return
		}
	}
}

func doWork() {}
