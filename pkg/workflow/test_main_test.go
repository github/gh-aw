//go:build !integration

package workflow

import (
	"net/http"
	"os"
	"testing"

	"go.uber.org/goleak"
)

// TestMain enforces goroutine-leak detection for all unit tests in the workflow package.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(workflowTestMain{m},
		goleak.IgnoreAnyFunction("net/http.(*http2ClientConn).readLoop"),
		goleak.IgnoreAnyFunction("net/http.(*http2clientConnReadLoop).run"),
	)
}

type workflowTestMain struct {
	goleak.TestingM
}

func (m workflowTestMain) Run() int {
	runtimeDirectory, err := os.MkdirTemp("", "gh-aw-workflow-test-runtime-")
	if err != nil {
		return 1
	}
	previousRuntimeDirectory, hadRuntimeDirectory := os.LookupEnv("GH_AW_RUNTIME_DIR")
	previousRuntimeID, hadRuntimeID := os.LookupEnv("GH_AW_RUNTIME_ID")
	_ = os.Setenv("GH_AW_RUNTIME_DIR", runtimeDirectory)
	_ = os.Setenv("GH_AW_RUNTIME_ID", "workflow-test")
	code := m.TestingM.Run()
	if hadRuntimeDirectory {
		_ = os.Setenv("GH_AW_RUNTIME_DIR", previousRuntimeDirectory)
	} else {
		_ = os.Unsetenv("GH_AW_RUNTIME_DIR")
	}
	if hadRuntimeID {
		_ = os.Setenv("GH_AW_RUNTIME_ID", previousRuntimeID)
	} else {
		_ = os.Unsetenv("GH_AW_RUNTIME_ID")
	}
	_ = os.RemoveAll(runtimeDirectory)
	// Catalog downloads share the default transport; close its idle connections
	// before goleak checks for goroutines that outlive the test suite.
	http.DefaultClient.CloseIdleConnections()
	return code
}
