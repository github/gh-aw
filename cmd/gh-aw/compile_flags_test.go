//go:build !integration

package main

import "testing"

func TestCompileCommandShortFlags(t *testing.T) {
	t.Parallel()
	forceFlag := compileCmd.Flags().Lookup("force")
	if forceFlag == nil {
		t.Fatal("expected --force flag on compile command")
	}
	if forceFlag.Shorthand != "f" {
		t.Fatalf("expected --force shorthand to be -f, got -%s", forceFlag.Shorthand)
	}

	logicalRepoFlag := compileCmd.Flags().Lookup("logical-repo")
	if logicalRepoFlag == nil {
		t.Fatal("expected --logical-repo flag on compile command")
	}
	if logicalRepoFlag.Shorthand != "l" {
		t.Fatalf("expected --logical-repo shorthand to be -l, got -%s", logicalRepoFlag.Shorthand)
	}

	grantFlag := compileCmd.Flags().Lookup("grant")
	if grantFlag == nil {
		t.Fatal("expected --grant flag on compile command")
	}
	if grantFlag.DefValue != "false" {
		t.Fatalf("expected --grant default to be false, got %s", grantFlag.DefValue)
	}

	forceRefreshContainerPinsFlag := compileCmd.Flags().Lookup("force-refresh-container-pins")
	if forceRefreshContainerPinsFlag == nil {
		t.Fatal("expected --force-refresh-container-pins flag on compile command")
	}
	if forceRefreshContainerPinsFlag.DefValue != "false" {
		t.Fatalf("expected --force-refresh-container-pins default to be false, got %s", forceRefreshContainerPinsFlag.DefValue)
	}
}

func TestCompileOptionsPropagateForceRefreshContainerPins(t *testing.T) {
	t.Parallel()
	config := (&compileCmdOptions{forceRefreshContainerPins: true}).toCompileConfig(nil)
	if !config.ForceRefreshContainerPins {
		t.Fatal("expected ForceRefreshContainerPins to be propagated to CompileConfig")
	}
}

func TestCompileOptionsPropagateModels(t *testing.T) {
	t.Parallel()

	modelsFlag := compileCmd.Flags().Lookup("models")
	if modelsFlag == nil {
		t.Fatal("expected --models flag on compile command")
	}
	if modelsFlag.DefValue != "false" {
		t.Fatalf("expected --models default to be false, got %s", modelsFlag.DefValue)
	}

	config := (&compileCmdOptions{models: true}).toCompileConfig(nil)
	if !config.Models {
		t.Fatal("expected Models to be propagated to CompileConfig")
	}
}
