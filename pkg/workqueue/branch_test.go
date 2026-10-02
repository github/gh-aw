package workqueue

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func bareRemote(t *testing.T) string {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "remote.git")
	output, err := exec.Command("git", "init", "--bare", "-q", remote).CombinedOutput()
	if err != nil {
		t.Fatalf("git init: %s: %v", output, err)
	}
	return remote
}

func TestBranchWithoutCheckoutAndConflictRetry(t *testing.T) {
	branch := Branch{Remote: bareRemote(t), Name: DefaultBranch}
	ctx := context.Background()
	work, a, b := fixture(t)
	otherWorkID, otherPayload, err := WorkID([]byte(`{"task":"second"}`))
	if err != nil {
		t.Fatal(err)
	}
	otherWork := Transaction{Kind: "Work", WorkID: otherWorkID, Work: otherPayload}
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })
	appendTx := func(tx Transaction) func([]Transaction) ([]Transaction, bool, error) {
		return func(current []Transaction) ([]Transaction, bool, error) { return Apply(current, tx) }
	}
	if _, changed, err := branch.Update(ctx, appendTx(work)); err != nil || !changed {
		t.Fatalf("branch initialization: %v", err)
	}
	if _, _, err := branch.Update(ctx, appendTx(b)); err != nil {
		t.Fatal(err)
	}
	raced := false
	next, changed, err := branch.Update(ctx, func(current []Transaction) ([]Transaction, bool, error) {
		if !raced {
			raced = true
			if _, _, err := branch.Update(ctx, appendTx(otherWork)); err != nil {
				return nil, false, err
			}
		}
		return Apply(current, a)
	})
	if err != nil || !changed || len(next) != 4 {
		t.Fatalf("conflict retry did not replay latest branch: %v, %d", err, len(next))
	}
	projection, err := Replay(next)
	if err != nil || projection.Stats.Work != 2 {
		t.Fatalf("lost concurrent work: %v, %v", projection, err)
	}
	latest, err := branch.Read(ctx)
	if err != nil || len(latest) != 4 {
		t.Fatalf("remote read did not reflect both writers: %v, %v", latest, err)
	}
}

func TestBranchRejectsInvalidRepositoryAndBranch(t *testing.T) {
	for _, branch := range []Branch{
		{Remote: "not-a-repo", Name: DefaultBranch},
		{Remote: "owner/repo", Name: "../oops"},
		{Remote: "owner/repo", Name: "a.lock"},
	} {
		if _, err := branch.Read(context.Background()); err == nil {
			t.Errorf("accepted invalid branch: %+v", branch)
		}
	}
}
