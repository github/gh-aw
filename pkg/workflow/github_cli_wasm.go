//go:build js || wasm

package workflow

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"

	"github.com/github/gh-aw/pkg/ctxutil"
)

// Note: os/exec compiles fine for GOOS=js GOARCH=wasm (it just fails at runtime).
// We must keep the *exec.Cmd return types because non-constrained callers like
// action_resolver.go reference these functions and expect *exec.Cmd. The stubs
// are never called at runtime in the wasm build since compilation skips external
// tool validation (WithSkipValidation(true)).

func setupGHCommand(ctx context.Context, args ...string) *exec.Cmd {
	return ghUnavailableCommand(ctx)
}

func ExecGH(args ...string) *exec.Cmd {
	return ghUnavailableCommand(context.Background())
}

func ExecGHContext(ctx context.Context, args ...string) *exec.Cmd {
	return ghUnavailableCommand(ctx)
}

func ExecGHWithOutput(args ...string) (stdout, stderr bytes.Buffer, err error) {
	return stdout, stderr, errors.New("gh CLI not available in Wasm")
}

func RunGH(spinnerMessage string, args ...string) ([]byte, error) {
	return nil, errors.New("gh CLI not available in Wasm")
}

func RunGHContext(ctx context.Context, spinnerMessage string, args ...string) ([]byte, error) {
	return nil, errors.New("gh CLI not available in Wasm")
}

func RunGHCombined(spinnerMessage string, args ...string) ([]byte, error) {
	return nil, errors.New("gh CLI not available in Wasm")
}

// RunGHInputContext is a no-op stub for Wasm builds.
// The input reader is intentionally not used in this build target.
func RunGHInputContext(ctx context.Context, spinnerMessage string, input io.Reader, args ...string) ([]byte, error) {
	return nil, errors.New("gh CLI not available in Wasm")
}

// enrichGHError is a pass-through stub in Wasm builds; gh CLI subprocesses are
// never executed, so there is no stderr to append.
func enrichGHError(err error) error { return err }

func ghUnavailableCommand(ctx context.Context) *exec.Cmd {
	ctx = ctxutil.OrBackground(ctx)
	return exec.CommandContext(ctx, "echo", "gh CLI not available in Wasm")
}

func ForceGHHostEnv(cmd *exec.Cmd, host string) {
	// no-op in Wasm: gh CLI subprocesses are not run
}

// SetDefaultGHHost is a no-op in Wasm builds; GH CLI is unavailable.
func SetDefaultGHHost(_ string) {}

// getDefaultGHHost always returns "" in Wasm builds.
func getDefaultGHHost() string { return "" }

// GitHubCLIHost returns no host in Wasm, where GitHub CLI calls are unavailable.
func GitHubCLIHost() string { return "" }
