package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var inlineDriverLog = logger.New("workflow:copilot_inline_driver")

const (
	inlineCopilotSDKDriverDir           = ".gh-aw/copilot-sdk"
	inlineCopilotSDKDriverWrapperPath   = inlineCopilotSDKDriverDir + "/inline-driver"
	inlineCopilotSDKDriverNodePath      = inlineCopilotSDKDriverDir + "/inline-driver.cjs"
	inlineCopilotSDKDriverPythonPath    = inlineCopilotSDKDriverDir + "/inline-driver.py"
	inlineCopilotSDKDriverGoPath        = inlineCopilotSDKDriverDir + "/inline-driver.go"
	inlineCopilotSDKDriverGoBinPath     = inlineCopilotSDKDriverDir + "/inline-driver-bin"
	inlineCopilotSDKDriverGoModPath     = inlineCopilotSDKDriverDir + "/go.mod"
	inlineCopilotSDKDriverJavaPath      = inlineCopilotSDKDriverDir + "/Main.java"
	inlineCopilotSDKDriverJavaPomPath   = inlineCopilotSDKDriverDir + "/pom.xml"
	inlineCopilotSDKDriverJavaClassPath = inlineCopilotSDKDriverDir + "/classpath.txt"
	inlineCopilotSDKDriverGoModuleName  = "gh-aw-inline-copilot-sdk-driver"
	inlineCopilotSDKDriverJavaMainClass = "Main"
)

func (d *InlineEngineDriver) sourcePath() string {
	if d == nil {
		return ""
	}
	switch d.Runtime {
	case "node":
		return inlineCopilotSDKDriverNodePath
	case "python":
		return inlineCopilotSDKDriverPythonPath
	case "go":
		return inlineCopilotSDKDriverGoPath
	case "java":
		return inlineCopilotSDKDriverJavaPath
	default:
		return ""
	}
}

func (d *InlineEngineDriver) wrapperScript() string {
	if d == nil {
		return ""
	}

	inlineDriverLog.Printf("Generating wrapper script for runtime=%s", d.Runtime)
	sourcePath := d.sourcePath()
	switch d.Runtime {
	case "node":
		return "#!/usr/bin/env bash\nset -euo pipefail\nexec node --import \"" + SetupActionDestinationShell + "/copilot_sdk_module_resolution.cjs\" \"${GITHUB_WORKSPACE}/" + sourcePath + "\" \"$@\"\n"
	case "python":
		return "#!/usr/bin/env bash\nset -euo pipefail\nexec python3 \"${GITHUB_WORKSPACE}/" + sourcePath + "\" \"$@\"\n"
	case "go":
		// Execute the pre-compiled binary produced during the install step.
		// Using a compiled binary avoids the recompilation overhead of `go run`
		// on every agent invocation and removes the need for the Go toolchain at runtime.
		return "#!/usr/bin/env bash\nset -euo pipefail\nexec \"${GITHUB_WORKSPACE}/" + inlineCopilotSDKDriverGoBinPath + "\" \"$@\"\n"
	case "java":
		// classpath.txt is written by the Maven install step; its absence means
		// the install step failed and we should surface that clearly rather than
		// attempting a classpath-less invocation that would fail with a cryptic JVM error.
		return "#!/usr/bin/env bash\nset -euo pipefail\n" +
			"CLASSPATH_CONTENT=$(cat \"${GITHUB_WORKSPACE}/" + inlineCopilotSDKDriverJavaClassPath + "\")\n" +
			"exec java -cp \"$CLASSPATH_CONTENT\" " + inlineCopilotSDKDriverJavaMainClass + " \"$@\"\n"
	default:
		return ""
	}
}

func (d *InlineEngineDriver) additionalFiles(goVersion string) map[string]string {
	if d == nil {
		return nil
	}

	switch d.Runtime {
	case "go":
		return map[string]string{
			inlineCopilotSDKDriverGoModPath: fmt.Sprintf("module %s\n\ngo %s\n", inlineCopilotSDKDriverGoModuleName, goVersion),
		}
	case "java":
		return map[string]string{
			inlineCopilotSDKDriverJavaPomPath: inlineCopilotSDKDriverPomXML(string(constants.DefaultCopilotSDKVersion)),
		}
	default:
		return nil
	}
}

func inlineCopilotSDKDriverPomXML(version string) string {
	return fmt.Sprintf(`<project xmlns="http://maven.apache.org/POM/4.0.0"
         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
         xsi:schemaLocation="http://maven.apache.org/POM/4.0.0 https://maven.apache.org/xsd/maven-4.0.0.xsd">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.github.gh-aw</groupId>
  <artifactId>inline-copilot-sdk-driver</artifactId>
  <version>1.0.0</version>
  <dependencies>
    <dependency>
      <groupId>com.github</groupId>
      <artifactId>copilot-sdk-java</artifactId>
      <version>%s</version>
    </dependency>
  </dependencies>
</project>
`, version)
}

func copilotSDKInlineDriverRuntimeID(workflowData *WorkflowData) string {
	if workflowData == nil || workflowData.EngineConfig == nil || workflowData.EngineConfig.InlineDriver == nil {
		return ""
	}
	return workflowData.EngineConfig.InlineDriver.Runtime
}

func buildInlineCopilotSDKDriverWriteStep(workflowData *WorkflowData) GitHubActionStep {
	if workflowData == nil || workflowData.EngineConfig == nil || workflowData.EngineConfig.InlineDriver == nil {
		return GitHubActionStep{}
	}

	inlineDriver := workflowData.EngineConfig.InlineDriver
	sourcePath := inlineDriver.sourcePath()
	if sourcePath == "" {
		inlineDriverLog.Printf("No source path for inline driver runtime=%s, skipping write step", inlineDriver.Runtime)
		return GitHubActionStep{}
	}

	// Resolve the effective Go version: honour an explicit runtimes.go.version pin,
	// falling back to the repository default when no explicit version is configured.
	goVersion := string(constants.DefaultGoVersion)
	if workflowData.ParsedFrontmatter != nil &&
		workflowData.ParsedFrontmatter.RuntimesTyped != nil &&
		workflowData.ParsedFrontmatter.RuntimesTyped.Go != nil &&
		workflowData.ParsedFrontmatter.RuntimesTyped.Go.Version != "" {
		goVersion = workflowData.ParsedFrontmatter.RuntimesTyped.Go.Version
	}
	inlineDriverLog.Printf("Building inline Copilot SDK driver write step: runtime=%s source=%s goVersion=%s", inlineDriver.Runtime, sourcePath, goVersion)

	step := GitHubActionStep{
		"      - name: Write Inline Copilot SDK Driver",
		"        run: |",
		fmt.Sprintf("          mkdir -p \"${GITHUB_WORKSPACE}/%s\"", inlineCopilotSDKDriverDir),
	}

	appendHeredocWrite := func(path, content string, chmod bool) {
		delimiter := GenerateHeredocDelimiterFromContent("INLINE_COPILOT_SDK_DRIVER", content)
		step = append(step, fmt.Sprintf("          cat > \"${GITHUB_WORKSPACE}/%s\" << '%s'", path, delimiter)) //nolint:generatedyamlheredoc // Legacy inline-driver rendering remains to be migrated.
		// Trim all trailing newlines before splitting so source files ending with
		// one or more newlines don't emit extra blank lines before the heredoc delimiter.
		for line := range strings.SplitSeq(strings.TrimRight(content, "\n"), "\n") {
			step = append(step, "          "+line)
		}
		step = append(step, "          "+delimiter)
		if chmod {
			step = append(step, fmt.Sprintf("          chmod +x \"${GITHUB_WORKSPACE}/%s\"", path))
		}
	}

	appendHeredocWrite(sourcePath, inlineDriver.Source, false)
	for path, content := range inlineDriver.additionalFiles(goVersion) {
		appendHeredocWrite(path, content, false)
	}
	appendHeredocWrite(inlineCopilotSDKDriverWrapperPath, inlineDriver.wrapperScript(), true)

	return step
}
