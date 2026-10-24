package ctxutil

import "context"

type operationReporterKey struct{}

// WithOperationReporter attaches an operation-start callback to a command context.
// The callback returns a completion callback receiving the operation's error.
func WithOperationReporter(ctx context.Context, report func(string) func(error)) context.Context {
	return context.WithValue(ctx, operationReporterKey{}, report)
}

// StartOperation reports an operation when the context has an observer.
func StartOperation(ctx context.Context, operation string) func(error) {
	if ctx != nil {
		if report, ok := ctx.Value(operationReporterKey{}).(func(string) func(error)); ok {
			return report(operation)
		}
	}
	return func(error) {}
}
