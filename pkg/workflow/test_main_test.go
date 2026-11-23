//go:build !integration

package workflow

import (
	"net/http"
	"testing"
	"time"

	"go.uber.org/goleak"
)

// TestMain enforces goroutine-leak detection for all unit tests in the workflow package.
func TestMain(m *testing.M) {
	// Unrelated compiler tests must not depend on live release metadata or the
	// current age of default engine versions. Cooldown tests inject HTTP responses.
	SetEngineReleaseTimeLookupForTesting(func(string, string) (time.Time, error) {
		return time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), nil
	})
	goleak.VerifyTestMain(workflowTestMain{m},
		goleak.IgnoreAnyFunction("net/http.(*http2ClientConn).readLoop"),
		goleak.IgnoreAnyFunction("net/http.(*http2clientConnReadLoop).run"),
	)
}

type workflowTestMain struct {
	goleak.TestingM
}

func (m workflowTestMain) Run() int {
	code := m.TestingM.Run()
	// Catalog downloads share the default transport; close its idle connections
	// before goleak checks for goroutines that outlive the test suite.
	http.DefaultClient.CloseIdleConnections()
	return code
}
