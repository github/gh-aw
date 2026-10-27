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

func TestCompileNoLintFlag(t *testing.T) {
	t.Parallel()
	flag := compileCmd.Flags().Lookup("no-lint")
	if flag == nil || flag.DefValue != "false" {
		t.Fatal("expected --no-lint to default to false")
	}
	for _, skip := range []bool{false, true} {
		cmd := &cobra.Command{}
		cmd.Flags().Bool("no-lint", skip, "")
		options := getCompileCmdOptions(cmd)
		config := options.toCompileConfig(nil)
		if config.SkipLint != skip {
			t.Fatalf("expected SkipLint=%t, got %t", skip, config.SkipLint)
		}
	}
}

func TestCompileDevelopmentRejectsExplicitFalseFlags(t *testing.T) {
	t.Parallel()
	for _, name := range cli.DryRunRequiredBoolFlags() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cmd := &cobra.Command{}
			cmd.SetContext(context.Background())
			cmd.Flags().Bool("dry-run", false, "")
			cmd.Flags().Bool(name, false, "")
			if err := cmd.ParseFlags([]string{"--dry-run", "--" + name + "=false"}); err != nil {
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
			if err := cli.ValidateDevelopmentCompileFlags(opts.dryRun, opts.explicitBoolFlags); err != nil {
				t.Fatalf("explicitly enabling required check should be accepted: %v", err)
			}
		})
	}
}

func TestCompileDevelopmentAllowsOptionalDockerChecks(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"false", "true"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			cmd := &cobra.Command{}
			cmd.Flags().Bool("dry-run", false, "")
			scanners := []string{
				"validate-images", "zizmor", "actionlint", "poutine", "runner-guard",
				"syft", "grype", "grant", "yamllint",
			}
			args := []string{"--dry-run"}
			for _, name := range scanners {
				cmd.Flags().Bool(name, false, "")
				args = append(args, "--"+name+"="+value)
			}
			if err := cmd.ParseFlags(args); err != nil {
				t.Fatal(err)
			}
			opts := getCompileCmdOptions(cmd)
			if err := cli.ValidateDevelopmentCompileFlags(opts.dryRun, opts.explicitBoolFlags); err != nil {
				t.Fatalf("Docker checks must remain optional in dry-run mode: %v", err)
			}
			config := opts.toCompileConfig(nil)
			for _, actual := range []bool{
				config.ValidateImages, config.Zizmor, config.Actionlint, config.Poutine,
				config.RunnerGuard, config.Syft, config.Grype, config.Grant, config.Yamllint,
			} {
				if actual != (value == "true") {
					t.Fatalf("optional Docker check value %q was not preserved", value)
				}
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

func TestCompileReadOnlyOptions(t *testing.T) {
	t.Parallel()
	flag := compileCmd.Flags().Lookup("read-only")
	if flag == nil || !flag.Hidden || flag.DefValue != "false" {
		t.Fatal("expected hidden --read-only flag to default to false")
	}
	for _, readOnly := range []bool{false, true} {
		cmd := &cobra.Command{}
		cmd.Flags().Bool("read-only", false, "")
		cmd.Flags().Bool("no-emit", false, "")
		cmd.Flags().Bool("no-check-update", false, "")
		if readOnly {
			if err := cmd.ParseFlags([]string{"--read-only", "--no-emit=false", "--no-check-update=false"}); err != nil {
				t.Fatal(err)
			}
		}
		opts := getCompileCmdOptions(cmd)
		config := opts.toCompileConfig(nil)
		if config.ReadOnly != readOnly || config.NoEmit != readOnly || opts.noCheckUpdate != readOnly {
			t.Fatalf("read-only=%t must force no-emit and disable update checks: config=%+v, noCheckUpdate=%t", readOnly, config, opts.noCheckUpdate)
		}
	}
}

func TestCompileOptionsPropagateAllowExperimental(t *testing.T) {
	t.Parallel()
	flag := compileCmd.Flags().Lookup("allow-experimental")
	if flag == nil || flag.DefValue != "false" {
		t.Fatal("expected --allow-experimental to default to false")
	}
	cmd := &cobra.Command{}
	cmd.Flags().Bool("allow-experimental", false, "")
	if err := cmd.ParseFlags([]string{"--allow-experimental"}); err != nil {
		t.Fatal(err)
	}
	opts := getCompileCmdOptions(cmd)
	if !opts.toCompileConfig(nil).AllowExperimental {
		t.Fatal("expected experimental opt-in to reach CompileConfig")
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

func TestCompileOptionsPropagateNoLogo(t *testing.T) {
	t.Parallel()
	flag := compileCmd.Flags().Lookup("no-logo")
	if flag == nil || flag.DefValue != "false" {
		t.Fatalf("expected --no-logo with default false, got %v", flag)
	}
	cmd := &cobra.Command{}
	cmd.Flags().Bool("no-logo", false, "")
	if err := cmd.ParseFlags([]string{"--no-logo"}); err != nil {
		t.Fatal(err)
	}
	opts := getCompileCmdOptions(cmd)
	if !opts.toCompileConfig(nil).NoLogo {
		t.Fatal("expected --no-logo to be propagated to CompileConfig")
	}
}

func TestCompileDevelopmentAndEnvironmentFlags(t *testing.T) {
	t.Parallel()
	for name, defaultValue := range map[string]string{"dry-run": "false", "environment": ""} {
		flag := compileCmd.Flags().Lookup(name)
		if flag == nil || flag.DefValue != defaultValue {
			t.Fatalf("expected --%s with default %q, got %v", name, defaultValue, flag)
		}
		if compileCmd.Flags().Lookup("dev") != nil {
			t.Fatal("expected --dev to be replaced by --dry-run")
		}
	}
	cmd := &cobra.Command{}
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().String("environment", "", "")
	if err := cmd.ParseFlags([]string{"--dry-run", "--environment", "test: #1"}); err != nil {
		t.Fatal(err)
	}
	opts := getCompileCmdOptions(cmd)
	config := opts.toCompileConfig(nil)
	if !config.DryRun || config.EnvironmentOverride != "test: #1" {
		t.Fatalf("development flags were not propagated: %+v", config)
	}
}
