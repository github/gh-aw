//go:build !integration

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/cli"
	"github.com/spf13/cobra"
)

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

func TestCompileDevelopmentRejectsExplicitFalseFlags(t *testing.T) {
	t.Parallel()
	for _, name := range cli.DevelopmentRequiredBoolFlags() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cmd := &cobra.Command{}
			cmd.SetContext(context.Background())
			cmd.Flags().Bool("dev", false, "")
			cmd.Flags().Bool(name, false, "")
			if err := cmd.ParseFlags([]string{"--dev", "--" + name + "=false"}); err != nil {
				t.Fatal(err)
			}
			opts := getCompileCmdOptions(cmd)
			if enabled, supplied := opts.toCompileConfig(nil).ExplicitBoolFlags[name]; !supplied || enabled {
				t.Fatalf("explicit --%s=false was not propagated", name)
			}
			if err := runCompileCmd(cmd, nil); err == nil || !strings.Contains(err.Error(), "--"+name+"=false") || !strings.Contains(err.Error(), "remove") {
				t.Fatalf("expected actionable rejection before compilation, got %v", err)
			}
			if err := cmd.Flags().Set(name, "true"); err != nil {
				t.Fatal(err)
			}
			opts = getCompileCmdOptions(cmd)
			if err := cli.ValidateDevelopmentCompileFlags(opts.dev, opts.explicitBoolFlags); err != nil {
				t.Fatalf("explicitly enabling required check should be accepted: %v", err)
			}
		})
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

func TestCompileOptionsPropagateRequireSelfHostedRunners(t *testing.T) {
	t.Parallel()

	flag := compileCmd.Flags().Lookup("require-self-hosted-runners")
	if flag == nil {
		t.Fatal("expected --require-self-hosted-runners flag on compile command")
	}

	if flag.DefValue != "false" {
		t.Fatalf("expected --require-self-hosted-runners default to be false, got %s", flag.DefValue)
	}

	config := (&compileCmdOptions{requireSelfHostedRunners: true}).toCompileConfig(nil)
	if !config.RequireSelfHostedRunners {
		t.Fatal("expected RequireSelfHostedRunners to be propagated to CompileConfig")
	}
}

func TestCompileDevelopmentAndEnvironmentFlags(t *testing.T) {
	t.Parallel()
	for name, defaultValue := range map[string]string{"dev": "false", "environment": ""} {
		flag := compileCmd.Flags().Lookup(name)
		if flag == nil || flag.DefValue != defaultValue {
			t.Fatalf("expected --%s with default %q, got %v", name, defaultValue, flag)
		}
	}
	cmd := &cobra.Command{}
	cmd.Flags().Bool("dev", false, "")
	cmd.Flags().String("environment", "", "")
	if err := cmd.ParseFlags([]string{"--dev", "--environment", "test: #1"}); err != nil {
		t.Fatal(err)
	}
	opts := getCompileCmdOptions(cmd)
	config := opts.toCompileConfig(nil)
	if !config.Dev || config.EnvironmentOverride != "test: #1" {
		t.Fatalf("development flags were not propagated: %+v", config)
	}
}
