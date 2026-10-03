package contextcancelnotdeferred

import (
	"context"
	"time"
)

func BadWithTimeout(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, time.Second) // want `context cancel function should be deferred immediately after context.WithCancel/WithTimeout/WithDeadline`
	_ = ctx
	cancel()
	return nil
}

func BadWithCancel(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent) // want `context cancel function should be deferred immediately after context.WithCancel/WithTimeout/WithDeadline`
	_ = ctx
	cancel()
	return nil
}

func GoodWithDeadline(parent context.Context, d time.Time) error {
	ctx, cancel := context.WithDeadline(parent, d)
	defer cancel()
	_ = ctx
	return nil
}

func GoodInClosure(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	_ = ctx

	fn := func() error {
		innerCtx, cancel := context.WithTimeout(parent, time.Second)
		defer cancel()
		_ = innerCtx
		return nil
	}
	_ = fn()
}

func BadReassignThenGood(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, time.Second) // want `context cancel function should be deferred immediately after context.WithCancel/WithTimeout/WithDeadline`
	_ = ctx
	cancel()

	ctx, cancel = context.WithTimeout(parent, time.Second)
	defer cancel()
	_ = ctx
	return nil
}

func BlankIdentifier(parent context.Context) {
	_, _ = context.WithTimeout(parent, time.Second)
}

func GoodNoLint(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, time.Second) //nolint:contextcancelnotdeferred
	_ = ctx
	cancel()
	return nil
}

func BadSiblingCancel(parent context.Context, deferred bool) {
	_, cancel := context.WithCancel(parent) // want `context cancel function should be deferred immediately after context.WithCancel/WithTimeout/WithDeadline`
	if deferred {
		defer cancel()
	} else {
		cancel()
	}
}
