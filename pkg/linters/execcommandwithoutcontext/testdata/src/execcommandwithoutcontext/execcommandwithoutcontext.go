package execcommandwithoutcontext

import (
	"context"
	"net/http"
	"os/exec"
)

// Bad: exec.Command inside a function that already receives a context.
func BadRunCommand(ctx context.Context, name string) error {
	cmd := exec.Command(name) // want `use exec\.CommandContext\(ctx, \.\.\.\) instead of exec\.Command to propagate context cancellation`
	return cmd.Run()
}

// Bad: exec.Command with extra args inside a context-receiving function.
func BadRunCommandArgs(ctx context.Context, name string, args ...string) {
	cmd := exec.Command(name, args...) // want `use exec\.CommandContext\(ctx, \.\.\.\) instead of exec\.Command to propagate context cancellation`
	_ = cmd
}

// Good: exec.CommandContext is used correctly.
func GoodRunCommandContext(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, name)
	return cmd.Run()
}

// Good: no context parameter, exec.Command is fine.
func GoodNoContext(name string) error {
	cmd := exec.Command(name)
	return cmd.Run()
}

// Good: context parameter is blank, exec.Command is acceptable.
func GoodBlankContext(_ context.Context, name string) error {
	cmd := exec.Command(name)
	return cmd.Run()
}

type Runner struct{}

// Bad: method with context parameter should use CommandContext.
func (r *Runner) Run(ctx context.Context, name string) error {
	cmd := exec.Command(name) // want `use exec\.CommandContext\(ctx, \.\.\.\) instead of exec\.Command to propagate context cancellation`
	return cmd.Run()
}

func doWork(fn func(context.Context, string)) {
	fn(context.Background(), "ls")
}

// Bad: function literal with context parameter should use CommandContext.
func BadFuncLitCtx() {
	doWork(func(ctx context.Context, name string) {
		_ = exec.Command(name) // want `use exec\.CommandContext\(ctx, \.\.\.\) instead of exec\.Command to propagate context cancellation`
	})
}

// Good: function literal with context parameter already uses CommandContext.
func GoodFuncLitCtx() {
	doWork(func(ctx context.Context, name string) {
		_ = exec.CommandContext(ctx, name)
	})
}

// Bad: closure in context-receiving function should use outer ctx.
func OuterCtxInnerClosure(ctx context.Context) {
	go func() {
		_ = exec.Command("ls") // want `use exec\.CommandContext\(ctx, \.\.\.\) instead of exec\.Command to propagate context cancellation`
	}()
}

// Good: inline nolint suppresses an intentional detached command.
func GoodNoLint(ctx context.Context, name string) {
	_ = ctx
	_ = exec.Command(name) //nolint:execcommandwithoutcontext
}

// Good: non-go/defer callback closure nested in ctx func — outer ctx is not in scope.
func GoodHTTPHandleFuncCallbackInCtxFunc(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("/run", func(w http.ResponseWriter, r *http.Request) {
		_ = w
		_ = r
		_ = exec.Command("ls")
	})
	_ = ctx
}

// Good: ordinary synchronous callback closure should not be attributed to outer ctx.
func GoodSyncCallbackInCtxFunc(ctx context.Context) {
	register(func() {
		_ = exec.Command("ls")
	})
	_ = ctx
}

// Bad: deferred closure shares outer context lifetime.
func BadDeferWithCtx(ctx context.Context) {
	defer func() {
		_ = exec.Command("ls") // want `use exec\.CommandContext\(ctx, \.\.\.\) instead of exec\.Command to propagate context cancellation`
	}()
	_ = ctx
}

// Bad: parenthesized deferred closure shares outer context lifetime.
func BadDeferParenWithCtx(ctx context.Context) {
	defer (func() {
		_ = exec.Command("ls") // want `use exec\.CommandContext\(ctx, \.\.\.\) instead of exec\.Command to propagate context cancellation`
	})()
	_ = ctx
}

// Bad: parenthesized goroutine closure shares outer context lifetime.
func BadGoParenWithCtx(ctx context.Context) {
	go (func() {
		_ = exec.Command("ls") // want `use exec\.CommandContext\(ctx, \.\.\.\) instead of exec\.Command to propagate context cancellation`
	})()
	_ = ctx
}

// Bad: plain goroutine closure shares outer context lifetime.
func BadGoWithCtx(ctx context.Context) {
	go func() {
		_ = exec.Command("ls") // want `use exec\.CommandContext\(ctx, \.\.\.\) instead of exec\.Command to propagate context cancellation`
	}()
	_ = ctx
}

func register(fn func()) {
	fn()
}

// Bad: parenthesized exec.Command callee inside a context-receiving function.
func BadParenCommand(ctx context.Context, name string) {
	_ = (exec.Command)(name) // want `use exec\.CommandContext\(ctx, \.\.\.\) instead of exec\.Command to propagate context cancellation`
}
