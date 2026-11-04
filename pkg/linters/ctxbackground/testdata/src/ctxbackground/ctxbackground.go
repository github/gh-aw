package ctxbackground

import (
	"context"
)

// flagged: function receives ctx context.Context but calls context.Background()
func DoWork(ctx context.Context) {
	_ = context.Background() // want `use the context.Context parameter instead of context.Background\(\)`
}

// not flagged: no context parameter
func DoWorkNoCtx() {
	_ = context.Background()
}

// not flagged: blank identifier context parameter
func DoWorkBlank(_ context.Context) {
	_ = context.Background()
}

// flagged: method with context param
type Worker struct{}

func (w *Worker) Run(ctx context.Context) {
	_ = context.Background() // want `use the context.Context parameter instead of context.Background\(\)`
}

// not flagged: init function
func init() {
	_ = context.Background()
}

type contextShadow struct{}

func (contextShadow) Background() context.Context { return context.TODO() }

// not flagged: local non-package identifier named context
func DoWorkShadowedContext(ctx context.Context) {
	context := contextShadow{}
	_ = context.Background()
	_ = ctx
}

// flagged: closure whose own signature receives ctx
func RegisterHandler() {
	handle(func(ctx context.Context) {
		_ = context.Background() // want `use the context.Context parameter instead of context.Background\(\)`
	})
}

// not flagged: closure with no ctx param nested inside a function that does receive ctx;
// the nearest enclosing function (the closure) has no ctx, so we stop there.
func DoWorkWithDetachedClosure(ctx context.Context) {
	go func() {
		_ = context.Background()
	}()
	_ = ctx
}

// not flagged: inline nolint suppresses intentional detachment.
func DoWorkNoLint(ctx context.Context) {
	_ = context.Background() //nolint:ctxbackground
	_ = ctx
}

func handle(f func(context.Context)) { f(nil) }

// flagged: parenthesized context.Background callee
func DoWorkParen(ctx context.Context) {
	_ = (context.Background)() // want `use the context.Context parameter instead of context.Background\(\)`
}
