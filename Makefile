# Makefile for gh-aw Go project

# Variables
BINARY_NAME=gh-aw
# Add .exe extension on Windows
ifeq ($(OS),Windows_NT)
	BINARY_NAME := gh-aw.exe
endif
VERSION ?= $(shell git describe --tags --always --dirty)
DOCKER_IMAGE=ghcr.io/github/gh-aw
DOCKER_PLATFORMS=linux/amd64,linux/arm64
BASE_REF ?= origin/main
JS_IMPACTED_TEST_EXCLUDES=--exclude '**/*.integration.test.cjs' --exclude '**/frontmatter_hash_github_api.test.cjs'
CI_WORKFLOW_FILE ?= ci.yml
CI_COVERAGE_ARTIFACT_PATTERN ?= ci-integration-coverage-*
CI_COVERAGE_DIR ?= /tmp/gh-aw-ci-coverage
CI_COVERAGE_ENABLED ?= 1
CI_COVERAGE_SOURCE_BRANCH ?= main
CI_RUN_ID ?=
CI_UNIT_WORKFLOW_FILE ?= cgo.yml
CI_UNIT_TEST_ARTIFACT_PATTERN ?= test-result-cgo-unit-*
CI_UNIT_RUN_ID ?=
GO_IMPACTED_TEST_MAX_SECONDS ?= 60
GO_IMPACTED_TEST_PATTERN_MAX_CHARS ?= 8000
GO_IMPACTED_TEST_FALLBACK_MODE ?= package
GO_IMPACTED_TEST_SAMPLE_PER_PACKAGE_DEFAULT ?= 5
GO_IMPACTED_TEST_SAMPLE_PER_PACKAGE ?= $(GO_IMPACTED_TEST_SAMPLE_PER_PACKAGE_DEFAULT)
TEST_UNIT_MAX_SECONDS ?= 30
TEST_UNIT_RUN_FULL ?= 0
TEST_UNIT_IMPACTED_FALLBACK_MODE ?= sample

# Build flags
LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION)"

# Default target
.PHONY: all
all: build

# Build the binary, run make deps before this
.PHONY: build
build: sync-action-pins sync-action-scripts sync-compat
	go build $(LDFLAGS) -o $(BINARY_NAME) ./cmd/gh-aw

# Build for all platforms
.PHONY: build-all
build-all: build-linux build-darwin build-windows build-android

.PHONY: build-linux
build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_NAME)-linux-amd64 ./cmd/gh-aw
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BINARY_NAME)-linux-arm64 ./cmd/gh-aw

.PHONY: build-darwin
build-darwin:
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_NAME)-darwin-amd64 ./cmd/gh-aw
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BINARY_NAME)-darwin-arm64 ./cmd/gh-aw

.PHONY: build-windows
build-windows:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_NAME)-windows-amd64.exe ./cmd/gh-aw

.PHONY: build-android
build-android:
	CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build $(LDFLAGS) -o $(BINARY_NAME)-android-arm64 ./cmd/gh-aw

# Build WebAssembly module for browser usage
# Optionally runs wasm-opt (from Binaryen) if available for ~8% size reduction
.PHONY: build-wasm
build-wasm:
	GOOS=js GOARCH=wasm go build -ldflags="-w -s" -o gh-aw.wasm ./cmd/gh-aw-wasm
	@if command -v wasm-opt >/dev/null 2>&1; then \
		echo "Running wasm-opt -Oz (size optimization)..."; \
		BEFORE=$$(wc -c < gh-aw.wasm); \
		wasm-opt -Oz --enable-bulk-memory gh-aw.wasm -o gh-aw.opt.wasm && \
		mv gh-aw.opt.wasm gh-aw.wasm; \
		AFTER=$$(wc -c < gh-aw.wasm); \
		echo "✓ wasm-opt: $$BEFORE → $$AFTER bytes"; \
	else \
		echo "⚠ wasm-opt not found, skipping optimization (install binaryen for ~8% size reduction)"; \
	fi
	@echo "✓ Built gh-aw.wasm ($$(du -h gh-aw.wasm | cut -f1))"
	@echo "  Copy wasm_exec.js from: $$(go env GOROOT)/lib/wasm/wasm_exec.js (or misc/wasm/ for Go <1.24)"

# Test the code (runs both unlabelled unit tests and integration tests and long tests)
.PHONY: test
test: test-unit test-integration

# Test all Go unit tests only (excludes labelled integration tests and long tests)
.PHONY: test-unit-all
test-unit-all:
	go test -v -parallel=4 -timeout=10m -run='^Test' ./... -short

# Test impacted Go unit tests first for faster feedback (target budget: ~30s)
.PHONY: test-unit
test-unit:
	@echo "Running impacted Go unit tests first (time budget: $(TEST_UNIT_MAX_SECONDS)s)..."; \
	$(MAKE) --no-print-directory test-impacted-go CI_COVERAGE_ENABLED=0 GO_IMPACTED_TEST_MAX_SECONDS=$(TEST_UNIT_MAX_SECONDS) GO_IMPACTED_TEST_FALLBACK_MODE=$(TEST_UNIT_IMPACTED_FALLBACK_MODE) GO_IMPACTED_TEST_SAMPLE_PER_PACKAGE=$(GO_IMPACTED_TEST_SAMPLE_PER_PACKAGE); \
	if [ "$(TEST_UNIT_RUN_FULL)" = "1" ]; then \
		echo "TEST_UNIT_RUN_FULL=1 set; running full Go unit test suite after impacted tests."; \
		$(MAKE) --no-print-directory test-unit-all; \
	fi

.PHONY: test-integration
test-integration:
	go test -v -parallel=4 -timeout=10m -run='^Test' ./... -short

# Update golden test files
.PHONY: update-golden
update-golden:
	@echo "Updating golden test files..."
	go test -v ./pkg/console -run='^TestGolden_' -update

# Wasm golden tests — compare wasm (string API) compiler output against golden files
.PHONY: test-wasm-golden
test-wasm-golden:
	@echo "Running wasm golden tests (Go string API path)..."
	go test -v -timeout=5m -run='^TestWasmGolden_' ./pkg/workflow

# Update wasm golden files from current string API output
.PHONY: update-wasm-golden
update-wasm-golden:
	@echo "Updating wasm golden test files..."
	go test -v -timeout=5m -run='^TestWasmGolden_' ./pkg/workflow -update

# Build wasm and run Node.js golden comparison test
.PHONY: test-wasm
test-wasm: build-wasm
	@echo "Running wasm binary golden tests (Node.js)..."
	node scripts/test-wasm-golden.mjs

# Test specific integration test groups (matching CI workflow)
.PHONY: test-integration-compile
test-integration-compile:
	go test -v -timeout=3m -tags 'integration' -run 'TestCompile|TestPoutine' ./pkg/cli

.PHONY: test-integration-mcp-playwright
test-integration-mcp-playwright:
	go test -v -timeout=3m -tags 'integration' -run 'TestMCPInspectPlaywright' ./pkg/cli

.PHONY: test-integration-mcp-other
test-integration-mcp-other:
	go test -v -timeout=3m -tags 'integration' -run 'TestMCPAdd|TestMCPInspectGitHub|TestMCPServer|TestMCPConfig' ./pkg/cli

.PHONY: test-integration-logs
test-integration-logs:
	go test -v -timeout=3m -tags 'integration' -run 'TestLogs|TestFirewall|TestNoStopTime|TestLocalWorkflow' ./pkg/cli

.PHONY: test-integration-workflow
test-integration-workflow:
	go test -v -timeout=3m -tags 'integration' ./pkg/workflow ./cmd/gh-aw

.PHONY: test-perf
test-perf:
	go test -v -count=1 -timeout=3m -tags 'integration' -run='^Test' ./... | tee /tmp/gh-aw/test-output.log; \
	EXIT_CODE=$$?; \
	echo ""; \
	echo "=== SLOWEST TESTS ==="; \
	grep -E "^\s*--- (PASS|FAIL):" /tmp/gh-aw/test-output.log | \
	grep -E "\([0-9]+\.[0-9]+s\)" | \
	sed 's/.*\(Test[^ ]*\).* (\([0-9]*\.[0-9]*s\)).*/\2 \1/' | \
	sort -nr | \
	head -10; \
	rm -f /tmp/gh-aw/test-output.log; \
	exit $$EXIT_CODE

# Run benchmarks for performance testing
.PHONY: bench
bench:
	@echo "Running benchmarks..."
	go test -bench=. -benchmem -benchtime=3x -run=^$$ ./pkg/... | tee bench_results.txt

# Run only critical performance benchmarks for daily monitoring
# Uses time-based -benchtime (not a fixed low iteration count) so that results are
# averaged over many iterations and are far less sensitive to shared CI runner noise.
.PHONY: bench-performance
bench-performance:
	@echo "Running critical performance benchmarks..."
	@echo "This includes: CompileSimpleWorkflow, CompileComplexWorkflow, CompileMCPWorkflow,"
	@echo "               CompileMemoryUsage, ParseWorkflow, Validation, YAMLGeneration"
	@go test -bench='Benchmark(CompileSimpleWorkflow|CompileComplexWorkflow|CompileMCPWorkflow|CompileMemoryUsage|ParseWorkflow|Validation|YAMLGeneration)$$' \
		-benchmem -benchtime=2s -run=^$$ ./pkg/workflow | tee bench_performance.txt
	@echo ""
	@echo "Also running CLI helper benchmarks..."
	@go test -bench='Benchmark(ExtractWorkflowNameFromFile|FindIncludesInContent)$$' \
		-benchmem -benchtime=2s -run=^$$ ./pkg/cli >> bench_performance.txt
	@echo ""
	@echo "Performance benchmark results saved to bench_performance.txt"

# Run benchmarks with more iterations for comparison (saves to separate file)
.PHONY: bench-compare
bench-compare:
	@echo "Running benchmarks with more iterations for comparison..."
	go test -bench=. -benchmem -benchtime=100x -run=^$$ ./pkg/... | tee bench_compare.txt
	@echo "Comparison results saved to bench_compare.txt"
	@echo "Compare with: benchstat bench_results.txt bench_compare.txt"

# Run memory profiling benchmarks
.PHONY: bench-memory
bench-memory:
	@echo "Running memory profiling benchmarks..."
	go test -bench=. -benchmem -memprofile=mem.prof -cpuprofile=cpu.prof -benchtime=10x -run=^$$ ./pkg/workflow
	@echo "Memory profile saved to mem.prof, CPU profile saved to cpu.prof"
	@echo "View with: go tool pprof -http=:8080 mem.prof"

# Run fuzz tests
.PHONY: fuzz
fuzz:
	@echo "Running fuzz tests for 30 seconds..."
	go test -fuzz=FuzzParseFrontmatter -fuzztime=30s ./pkg/parser/
	go test -fuzz=FuzzExpressionParser -fuzztime=30s ./pkg/workflow/

# Run security regression tests
.PHONY: test-security
test-security:
	@echo "Running security regression tests..."
	go test -v -timeout=3m -run '^TestSecurity' ./pkg/workflow/... ./pkg/cli/...
	@echo "Running security fuzz test seed corpus..."
	go test -v -timeout=3m -run '^FuzzYAML|^FuzzTemplate|^FuzzInput|^FuzzNetwork|^FuzzSafeJob' ./pkg/workflow/...
	@echo "✓ Security regression tests passed"

# Security scanning with gosec and govulncheck
.PHONY: security-scan
security-scan: security-gosec security-govulncheck
	@echo "✓ All security scans completed"

.PHONY: security-gosec
security-gosec:
	@echo "Running gosec security scanner..."
	@# Keep only globally noisy rules here.
	@# G602 (slice bounds check) is excluded globally due persistent false positives.
	@# Use inline '#nosec Gxxx -- justification' suppressions for specific findings.
	@go tool gosec -fmt=json -out=gosec-report.json -stdout -exclude-generated -track-suppressions \
		-nosec-require-rules -nosec-require-justification \
		-exclude=G602 \
		./...
	@echo "✓ Gosec scan complete (results in gosec-report.json)"

.PHONY: security-govulncheck
security-govulncheck:
	@echo "Running govulncheck..."
	@go tool govulncheck ./...
	@echo "✓ Govulncheck complete"

.PHONY: security-govulncheck-sarif
security-govulncheck-sarif:
	@echo "Running govulncheck (SARIF output)..."
	@go tool govulncheck -format sarif ./... > govulncheck-results.sarif; ret=$$?; [ $$ret -eq 0 ] || [ $$ret -eq 3 ]
	@echo "✓ Govulncheck complete (results in govulncheck-results.sarif)"

# Test JavaScript files
.PHONY: check-cjs-syntax
check-cjs-syntax:
	@echo "→ Checking CommonJS runtime syntax..."
	@find actions/setup/js \
		-path 'actions/setup/js/node_modules' -prune -o \
		-type f -name '*.cjs' ! -name '*.test.cjs' -print0 | xargs -0 -r -n1 node --check
	@echo "✓ CommonJS runtime syntax validated"

.PHONY: test-js
test-js: build-js
	cd actions/setup/js && npm run test:js -- --no-file-parallelism
	cd eslint-factory && npm test

# Test impacted JavaScript unit tests only (excluding integration tests)
.PHONY: test-impacted-js
test-impacted-js: build-js
	@BASE_COMMIT=$$(bash scripts/resolve-base-commit.sh --base-ref $(BASE_REF)); \
	if [ -z "$$BASE_COMMIT" ]; then \
		echo "Set BASE_REF explicitly, for example: make test-impacted-js BASE_REF=origin/main"; \
		exit 1; \
	fi; \
	CHANGED_JS_FILES=$$({ git diff --name-only --diff-filter=ACMR "$$BASE_COMMIT" -- actions/setup/js eslint-factory; git ls-files --others --exclude-standard -- actions/setup/js eslint-factory; } | sort -u | grep -E '\.(cjs|js|mjs|ts)$$' || true); \
	if [ -z "$$CHANGED_JS_FILES" ]; then \
		echo "No changed JavaScript/TypeScript files under actions/setup/js or eslint-factory; skipping impacted JS tests."; \
		exit 0; \
	fi; \
	CHANGED_SETUP_JS_FILES=$$(printf '%s\n' "$$CHANGED_JS_FILES" | grep '^actions/setup/js/' || true); \
	CHANGED_ESLINT_FACTORY_FILES=$$(printf '%s\n' "$$CHANGED_JS_FILES" | grep '^eslint-factory/' || true); \
	ROOT=$$(pwd); \
	if [ -n "$$CHANGED_SETUP_JS_FILES" ]; then \
		echo "Running impacted JavaScript unit tests in actions/setup/js for changed files: $$CHANGED_SETUP_JS_FILES"; \
		cd "$$ROOT/actions/setup/js" && printf '%s\n' "$$CHANGED_SETUP_JS_FILES" | sed 's|^actions/setup/js/||' | tr '\n' '\0' | xargs -0 -r npm run test:js -- --no-file-parallelism --passWithNoTests $(JS_IMPACTED_TEST_EXCLUDES); \
	fi; \
	if [ -n "$$CHANGED_ESLINT_FACTORY_FILES" ]; then \
		echo "Running eslint-factory tests for changed files: $$CHANGED_ESLINT_FACTORY_FILES"; \
		cd "$$ROOT/eslint-factory" && npm test; \
	fi

# Test impacted Go unit tests only (excluding integration tests)
.PHONY: test-impacted-go
test-impacted-go:
	@BASE_COMMIT=$$(bash scripts/resolve-base-commit.sh --base-ref $(BASE_REF)); \
	if [ -z "$$BASE_COMMIT" ]; then \
		echo "Set BASE_REF explicitly, for example: make test-impacted-go BASE_REF=origin/main"; \
		exit 1; \
	fi; \
	CHANGED_GO_FILES=$$({ git diff --name-only --diff-filter=ACDMR "$$BASE_COMMIT"; git ls-files --others --exclude-standard; } | sort -u | grep -E '\.go$$' | grep -v -E '(^|/)testdata/' || true); \
	if [ -z "$$CHANGED_GO_FILES" ]; then \
		echo "No changed Go files; skipping impacted Go tests."; \
		exit 0; \
	fi; \
	COVERAGE_SOURCE_BRANCH="$(CI_COVERAGE_SOURCE_BRANCH)"; \
	COVERAGE_GO_PACKAGES=""; \
	if [ "$(CI_COVERAGE_ENABLED)" != "1" ]; then \
		echo "CI coverage correlation disabled (CI_COVERAGE_ENABLED=$(CI_COVERAGE_ENABLED)); using changed-file package selection."; \
	elif ! command -v gh >/dev/null 2>&1 || ! command -v jq >/dev/null 2>&1; then \
		echo "CI coverage correlation requires gh and jq; using changed-file package selection."; \
	else \
		RUN_ID="$(CI_RUN_ID)"; \
		if [ -z "$$RUN_ID" ]; then \
			RUN_ID=$$(gh run list --workflow "$(CI_WORKFLOW_FILE)" --branch "$$COVERAGE_SOURCE_BRANCH" --status success --limit 1 --json databaseId --jq '.[0].databaseId' 2>/dev/null || true); \
		fi; \
		if [ -n "$$RUN_ID" ]; then \
			rm -rf "$(CI_COVERAGE_DIR)"; \
			mkdir -p "$(CI_COVERAGE_DIR)"; \
			if gh run download "$$RUN_ID" --pattern "$(CI_COVERAGE_ARTIFACT_PATTERN)" --dir "$(CI_COVERAGE_DIR)" >/dev/null 2>&1; then \
				COVERAGE_FILES=$$(find "$(CI_COVERAGE_DIR)" -type f -name 'coverage-integration-*.out' 2>/dev/null || true); \
				if [ -n "$$COVERAGE_FILES" ]; then \
					CHANGED_FILE_LIST="$(CI_COVERAGE_DIR)/changed-go-files.txt"; \
					printf '%s\n' "$$CHANGED_GO_FILES" > "$$CHANGED_FILE_LIST"; \
					for coverage_file in $$COVERAGE_FILES; do \
						MATCHED_CHANGED_FILE=$$(awk -F: 'NR>1 {print $$1}' "$$coverage_file" | sed 's|^github.com/github/gh-aw/||' | grep -Fx -f "$$CHANGED_FILE_LIST" | head -n 1 || true); \
						if [ -n "$$MATCHED_CHANGED_FILE" ]; then \
							SAFE_NAME=$$(basename "$$coverage_file" .out | sed 's|^coverage-integration-||'); \
							RESULT_FILE=$$(find "$(CI_COVERAGE_DIR)" -type f -name "test-result-integration-$$SAFE_NAME.json" | head -n 1); \
							if [ -n "$$RESULT_FILE" ]; then \
								PACKAGES=$$(jq -r 'select(.Package != null) | .Package' "$$RESULT_FILE" | sort -u | sed 's|^github.com/github/gh-aw|.|'); \
								if [ -n "$$PACKAGES" ]; then \
									COVERAGE_GO_PACKAGES=$$(printf '%s\n%s\n' "$$COVERAGE_GO_PACKAGES" "$$PACKAGES" | sed '/^$$/d' | sort -u); \
								fi; \
							fi; \
						fi; \
					done; \
				else \
					echo "No CI coverage profiles found in downloaded artifacts; using changed-file package selection."; \
				fi; \
			else \
				echo "Unable to download CI coverage artifacts for run $$RUN_ID; using changed-file package selection."; \
			fi; \
		else \
			echo "No successful CI run found for branch $$COVERAGE_SOURCE_BRANCH; using changed-file package selection."; \
		fi; \
	fi; \
	if [ -n "$$COVERAGE_GO_PACKAGES" ]; then \
		CHANGED_GO_PACKAGES="$$COVERAGE_GO_PACKAGES"; \
		echo "Running impacted Go unit tests from CI coverage correlation: $$CHANGED_GO_PACKAGES"; \
	else \
		CHANGED_GO_PACKAGES=$$(printf '%s\n' "$$CHANGED_GO_FILES" | while IFS= read -r file; do \
			dir=$$(dirname "$$file"); \
			if find "$$dir" -maxdepth 1 -type f -name '*.go' -print -quit 2>/dev/null | grep -q .; then \
				printf './%s\n' "$$dir"; \
			fi; \
		done | sort -u); \
		if [ -z "$$CHANGED_GO_PACKAGES" ]; then \
			echo "No remaining Go packages for the changed files; skipping impacted Go tests."; \
			exit 0; \
		fi; \
		echo "Running impacted Go unit tests in changed-file packages: $$CHANGED_GO_PACKAGES"; \
	fi; \
	SELECTED_GO_TESTS=""; \
	if [ "$(CI_COVERAGE_ENABLED)" != "1" ]; then \
		echo "CI timing correlation disabled; using local impacted-test sampling."; \
	elif command -v gh >/dev/null 2>&1 && command -v jq >/dev/null 2>&1; then \
		UNIT_RUN_ID="$(CI_UNIT_RUN_ID)"; \
		if [ -z "$$UNIT_RUN_ID" ]; then \
			UNIT_RUN_ID=$$(gh run list --workflow "$(CI_UNIT_WORKFLOW_FILE)" --branch "$$COVERAGE_SOURCE_BRANCH" --status success --limit 1 --json databaseId --jq '.[0].databaseId' 2>/dev/null || true); \
		fi; \
		if [ -n "$$UNIT_RUN_ID" ]; then \
			UNIT_RESULT_DIR="$(CI_COVERAGE_DIR)/unit-results"; \
			rm -rf "$$UNIT_RESULT_DIR"; \
			mkdir -p "$$UNIT_RESULT_DIR"; \
			if gh run download "$$UNIT_RUN_ID" --pattern "$(CI_UNIT_TEST_ARTIFACT_PATTERN)" --dir "$$UNIT_RESULT_DIR" >/dev/null 2>&1; then \
				if find "$$UNIT_RESULT_DIR" -type f -name '*.json' -print -quit | grep -q .; then \
					IMPACTED_PACKAGE_FILE="$(CI_COVERAGE_DIR)/impacted-go-packages.txt"; \
					printf '%s\n' "$$CHANGED_GO_PACKAGES" | sed 's|^\./|github.com/github/gh-aw/|' > "$$IMPACTED_PACKAGE_FILE"; \
					IMPACTED_TEST_CANDIDATES="$(CI_COVERAGE_DIR)/impacted-go-test-candidates.tsv"; \
					find "$$UNIT_RESULT_DIR" -type f -name '*.json' -exec jq -r 'select(.Action == "pass" and .Package != null and .Test != null and (.Test | contains("/") | not) and .Elapsed != null) | [.Package, .Test, (.Elapsed | tostring)] | @tsv' {} + \
						| awk 'NR==FNR { pkgs[$$1] = 1; next } $$1 in pkgs { print }' "$$IMPACTED_PACKAGE_FILE" - \
						| sort -u > "$$IMPACTED_TEST_CANDIDATES"; \
					if [ -s "$$IMPACTED_TEST_CANDIDATES" ]; then \
						SELECTED_GO_TESTS="$(CI_COVERAGE_DIR)/selected-impacted-go-tests.tsv"; \
						awk 'BEGIN { srand() } { print rand() "\t" $$0 }' "$$IMPACTED_TEST_CANDIDATES" \
							| sort -k1,1n \
							| cut -f2- \
							| awk -F'\t' -v max="$(GO_IMPACTED_TEST_MAX_SECONDS)" 'BEGIN { total = 0; selected = 0 } { elapsed = $$3 + 0; if (selected == 0 || total + elapsed <= max) { print; total += elapsed; selected++ } }' \
							| sort -t"	" -k1,1 -k2,2 > "$$SELECTED_GO_TESTS"; \
						if [ -s "$$SELECTED_GO_TESTS" ]; then \
							ESTIMATED_DURATION=$$(awk -F'\t' '{ total += $$3 } END { printf "%.3f", total }' "$$SELECTED_GO_TESTS"); \
							echo "Running sampled impacted Go unit tests (estimated $$ESTIMATED_DURATION seconds, max $(GO_IMPACTED_TEST_MAX_SECONDS)s):"; \
							awk -F'\t' '{ print "  " $$1 " " $$2 " (" $$3 "s)" }' "$$SELECTED_GO_TESTS"; \
						else \
							SELECTED_GO_TESTS=""; \
						fi; \
					fi; \
				else \
					echo "No unit test result artifact JSON found in run $$UNIT_RUN_ID; running impacted packages instead."; \
				fi; \
			else \
				echo "Unable to download unit test results for run $$UNIT_RUN_ID; running impacted packages instead."; \
			fi; \
		else \
			echo "No successful $(CI_UNIT_WORKFLOW_FILE) run found for branch $$COVERAGE_SOURCE_BRANCH; running impacted packages instead."; \
		fi; \
	else \
		echo "Random impacted test sampling requires gh and jq; running impacted packages instead."; \
	fi; \
	if [ -n "$$SELECTED_GO_TESTS" ]; then \
		awk -F'\t' ' \
			BEGIN { current_pkg = ""; pattern = ""; max_chars = '$(GO_IMPACTED_TEST_PATTERN_MAX_CHARS)' + 0; if (max_chars <= 0) max_chars = 8000 } \
			function flush_pattern() { \
				if (current_pkg != "" && pattern != "") print current_pkg "\t^(" pattern ")$$"; \
			} \
			{ \
				if ($$1 != current_pkg) { \
					flush_pattern(); \
					current_pkg = $$1; \
					pattern = $$2; \
				} else { \
					next_pattern = pattern "|" $$2; \
					if (length(next_pattern) > max_chars) { \
						flush_pattern(); \
						pattern = $$2; \
					} else { \
						pattern = next_pattern; \
					} \
				} \
			} \
			END { \
				flush_pattern(); \
			} \
		' "$$SELECTED_GO_TESTS" | while IFS="	" read -r pkg pattern; do \
			if [ "$${#pattern}" -gt 30000 ]; then \
				echo "Running impacted Go unit tests in $$pkg (pattern too long, running full package)"; \
				go test -v -parallel=4 -timeout=10m -short "$$pkg" || exit 1; \
			else \
				echo "Running impacted Go unit tests in $$pkg with pattern $$pattern"; \
				go test -v -parallel=4 -timeout=10m -short -run "$$pattern" "$$pkg" || exit 1; \
			fi; \
		done || exit 1; \
		exit 0; \
	fi; \
	if [ "$(GO_IMPACTED_TEST_FALLBACK_MODE)" = "sample" ]; then \
		SAMPLE_PER_PACKAGE="$(GO_IMPACTED_TEST_SAMPLE_PER_PACKAGE)"; \
		case "$$SAMPLE_PER_PACKAGE" in \
			''|*[!0-9]*|0) SAMPLE_PER_PACKAGE="$(GO_IMPACTED_TEST_SAMPLE_PER_PACKAGE_DEFAULT)" ;; \
		esac; \
		echo "No impacted timing data available; running up to $$SAMPLE_PER_PACKAGE sampled top-level tests per impacted package."; \
		printf '%s\n' "$$CHANGED_GO_PACKAGES" | while IFS= read -r pkg; do \
			[ -z "$$pkg" ] && continue; \
			TEST_PATTERN=$$(go test -list '^Test' "$$pkg" 2>/dev/null | head -n "$$SAMPLE_PER_PACKAGE" | paste -sd'|' -); \
			if [ -z "$$TEST_PATTERN" ]; then \
				echo "No top-level tests discovered in $$pkg; skipping sampled run for this package."; \
				continue; \
			fi; \
			echo "Running sampled impacted Go unit tests in $$pkg with pattern ^($$TEST_PATTERN)$$"; \
			go test -v -parallel=4 -timeout=10m -short -run "^($$TEST_PATTERN)$$" "$$pkg" || exit 1; \
		done || exit 1; \
		exit 0; \
	fi; \
	# Use -short to exclude integration tests and keep execution to unit-test scope. \
	printf '%s\n' "$$CHANGED_GO_PACKAGES" | tr '\n' '\0' | xargs -0 -r go test -v -parallel=4 -timeout=10m -short

# Test both impacted JavaScript and Go unit tests
.PHONY: test-impacted
test-impacted:
	@$(MAKE) --no-print-directory -j2 test-impacted-js test-impacted-go

# Install JavaScript dependencies
.PHONY: deps-js
deps-js: check-node-version
	cd actions/setup/js && npm ci
	cd eslint-factory && npm ci

.PHONY: build-js
build-js: deps-js
	cd actions/setup/js && npm run typecheck

# Bundle JavaScript files with local requires
.PHONY: bundle-js
bundle-js:
	@echo "Building bundle-js tool..."
	@go build -o bundle-js ./cmd/bundle-js
	@echo "✓ bundle-js tool built"
	@echo "To bundle a JavaScript file: ./bundle-js <input-file> [output-file]"

# Run Bash script tests (check-stale-lock-files, check-workflow-drift, check-cgo-cjs-workflow-purity)
.PHONY: test-scripts
test-scripts: build
	@echo "Running Bash script tests..."
	bash scripts/extract-workflow-frontmatter-keys_test.sh
	bash scripts/check-stale-lock-files_test.sh
	bash scripts/check-skill-file-paths_test.sh
	bash scripts/check-safe-outputs-conformance_test.sh
	bash scripts/resolve-base-commit_test.sh
	bash scripts/check-workflow-drift_test.sh ./$(BINARY_NAME)
	bash scripts/check-cgo-cjs-workflow-purity_test.sh
	bash actions/setup/sh/copy_gh_aw_binary_for_mcp_test.sh
	@echo "✓ All Bash script tests passed"

# Test all code (Go, JavaScript, wasm golden, and shell scripts)
.PHONY: test-all
test-all: test test-js test-wasm-golden test-scripts

# Run tests with coverage
.PHONY: test-coverage
test-coverage:
	go test -v -count=1 -timeout=3m -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Clean build artifacts
.PHONY: clean
clean:
	@echo "Cleaning build artifacts..."
	@# Remove main binary and platform-specific binaries
	rm -f $(BINARY_NAME) $(BINARY_NAME)-*
	@# Remove bundle-js binary
	rm -f bundle-js
	@# Remove coverage files
	rm -f coverage.out coverage.html
	@# Remove benchmark results and profiling data
	rm -f bench_results.txt bench_compare.txt mem.prof cpu.prof
	@# Remove SBOM files
	rm -f sbom.spdx.json sbom.cdx.json
	@# Remove security scan reports
	rm -f gosec-report.json gosec-results.sarif govulncheck-results.sarif
	@# Remove downloaded logs (but keep .gitignore)
	@if [ -d .github/aw/logs ]; then \
		find .github/aw/logs -type f ! -name '.gitignore' -delete 2>/dev/null || true; \
		find .github/aw/logs -type d -empty -delete 2>/dev/null || true; \
	fi
	@# Remove installed gh extension if it exists
	@if [ -d "$$HOME/.local/share/gh/extensions/gh-aw" ]; then \
		echo "Removing installed gh-aw extension..."; \
		gh extension remove gh-aw 2>/dev/null || rm -rf "$$HOME/.local/share/gh/extensions/gh-aw"; \
	fi
	@# Clean documentation artifacts
	@rm -rf docs/dist docs/.astro 2>/dev/null || true
	@# Clean Go build cache, module cache, and test cache
	go clean -cache -modcache -testcache
	@echo "✓ Clean complete"

# Docker targets
.PHONY: docker-build
docker-build: build-linux
	@echo "Building Docker image..."
	@if ! command -v docker >/dev/null 2>&1; then \
		echo "Error: Docker is not installed."; \
		exit 1; \
	fi
	@# Build for linux/amd64 by default for local testing
	docker build -t $(DOCKER_IMAGE):$(VERSION) \
		--build-arg BINARY=$(BINARY_NAME)-linux-amd64 \
		-f Dockerfile .
	@docker tag $(DOCKER_IMAGE):$(VERSION) $(DOCKER_IMAGE):latest
	@echo "✓ Docker image built: $(DOCKER_IMAGE):$(VERSION)"
	@echo "✓ Docker image tagged: $(DOCKER_IMAGE):latest"

.PHONY: docker-build-multiarch
docker-build-multiarch: build-linux
	@echo "Building multi-architecture Docker image..."
	@if ! command -v docker >/dev/null 2>&1; then \
		echo "Error: Docker is not installed."; \
		exit 1; \
	fi
	@# Check if buildx is available
	@if ! docker buildx version >/dev/null 2>&1; then \
		echo "Error: Docker buildx is not available."; \
		echo "Install with: docker buildx install"; \
		exit 1; \
	fi
	@# Create buildx builder if it doesn't exist
	@docker buildx create --use --name gh-aw-builder 2>/dev/null || docker buildx use gh-aw-builder
	@# Build for multiple platforms
	docker buildx build --platform $(DOCKER_PLATFORMS) \
		-t $(DOCKER_IMAGE):$(VERSION) \
		-t $(DOCKER_IMAGE):latest \
		-f Dockerfile \
		--push .
	@echo "✓ Multi-architecture Docker image built and pushed"

.PHONY: docker-test
docker-test:
	@echo "Testing Docker image..."
	@docker run --rm $(DOCKER_IMAGE):$(VERSION) --version
	@docker run --rm $(DOCKER_IMAGE):$(VERSION) --help
	@echo "✓ Docker image test passed"

.PHONY: docker-push
docker-push:
	@echo "Pushing Docker image to registry..."
	@docker push $(DOCKER_IMAGE):$(VERSION)
	@docker push $(DOCKER_IMAGE):latest
	@echo "✓ Docker images pushed"

.PHONY: docker-clean
docker-clean:
	@echo "Cleaning Docker images..."
	@docker rmi $(DOCKER_IMAGE):$(VERSION) 2>/dev/null || true
	@docker rmi $(DOCKER_IMAGE):latest 2>/dev/null || true
	@echo "✓ Docker images cleaned"

# Actions management targets
.PHONY: actions-build
actions-build:
	@echo "Building all actions..."
	@go run ./internal/tools/actions-build build

.PHONY: actions-validate
actions-validate:
	@echo "Validating action.yml files..."
	@go run ./internal/tools/actions-build validate

.PHONY: actions-clean
actions-clean:
	@echo "Cleaning action artifacts..."
	@go run ./internal/tools/actions-build clean

.PHONY: generate-action-metadata
generate-action-metadata:
	@echo "Generating action metadata..."
	@go run ./internal/tools/generate-action-metadata generate

# Check Node.js version
.PHONY: check-node-version
check-node-version:
	@if ! command -v node >/dev/null 2>&1; then \
		echo "Error: Node.js is not installed."; \
		echo ""; \
		echo "This project requires Node.js 20 or higher."; \
		echo "Please install Node.js 20+ and try again."; \
		echo ""; \
		echo "For installation instructions, see:"; \
		echo "  https://github.com/github/gh-aw/blob/main/CONTRIBUTING.md#prerequisites"; \
		exit 1; \
	fi; \
	NODE_VERSION=$$(node --version); \
	NODE_VERSION_NUM=$$(echo "$$NODE_VERSION" | sed 's/v//'); \
	NODE_MAJOR=$$(echo "$$NODE_VERSION_NUM" | cut -d. -f1); \
	if [ "$$NODE_MAJOR" -lt 20 ]; then \
		echo "Error: Node.js version $$NODE_VERSION is not supported."; \
		echo ""; \
		echo "This project requires Node.js 20 or higher."; \
		echo "Your current version: $$NODE_VERSION"; \
		echo ""; \
		echo "Please upgrade Node.js and try again."; \
		echo ""; \
		echo "For installation instructions, see:"; \
		echo "  https://github.com/github/gh-aw/blob/main/CONTRIBUTING.md#prerequisites"; \
		exit 1; \
	fi; \
	echo "✓ Node.js version check passed ($$NODE_VERSION)"

.PHONY: tools
tools: ## Install build-time tools declared in go.mod tool directives
	@echo "Installing build tools..."
	@go install github.com/rhysd/actionlint/cmd/actionlint
	@go install github.com/securego/gosec/v2/cmd/gosec
	@go install golang.org/x/vuln/cmd/govulncheck
	@go install golang.org/x/tools/gopls@v0.21.1
	@echo "✓ Tools installed successfully"

# Install golangci-lint binary (avoiding GPL dependencies in go.mod)
# Downloads pre-built binary from GitHub releases
.PHONY: install-golangci-lint
install-golangci-lint:
	@echo "Installing golangci-lint binary..."
	@GOLANGCI_LINT_VERSION="v2.13.2"; \
	GOPATH=$$(go env GOPATH); \
	GOOS=$$(go env GOOS); \
	GOARCH=$$(go env GOARCH); \
	BINARY_NAME="golangci-lint"; \
	if [ "$$GOOS" = "windows" ]; then \
		BINARY_NAME="golangci-lint.exe"; \
	fi; \
	if [ -x "$$GOPATH/bin/$$BINARY_NAME" ]; then \
		INSTALLED_VERSION=$$("$$GOPATH/bin/$$BINARY_NAME" version --short 2>/dev/null || echo "unknown"); \
		if [ "$$INSTALLED_VERSION" = "$${GOLANGCI_LINT_VERSION#v}" ]; then \
			echo "✓ golangci-lint $$GOLANGCI_LINT_VERSION already installed"; \
			exit 0; \
		fi; \
	fi; \
	DOWNLOAD_URL="https://github.com/golangci/golangci-lint/releases/download/$$GOLANGCI_LINT_VERSION/golangci-lint-$${GOLANGCI_LINT_VERSION#v}-$$GOOS-$$GOARCH.tar.gz"; \
	TEMP_DIR=$$(mktemp -d); \
	ARCHIVE="$$TEMP_DIR/golangci-lint.tar.gz"; \
	EXTRACT_DIR="$$TEMP_DIR/extract"; \
	MAX_ATTEMPTS=3; \
	RETRY_DELAY=2; \
	trap "rm -rf $$TEMP_DIR" EXIT; \
	echo "Downloading golangci-lint $$GOLANGCI_LINT_VERSION for $$GOOS/$$GOARCH..."; \
	for attempt in $$(seq 1 $$MAX_ATTEMPTS); do \
		rm -f "$$ARCHIVE"; \
		rm -rf "$$EXTRACT_DIR"; \
		mkdir -p "$$EXTRACT_DIR"; \
		if curl --fail --silent --show-error --location "$$DOWNLOAD_URL" -o "$$ARCHIVE"; then \
			MAGIC_BYTES=$$(od -An -tx1 -N2 "$$ARCHIVE" | tr -d '[:space:]'); \
			if [ "$$MAGIC_BYTES" != "1f8b" ]; then \
				echo "Warning: Downloaded golangci-lint archive is not a gzip stream (attempt $$attempt/$$MAX_ATTEMPTS)"; \
			elif ! tar -tzf "$$ARCHIVE" >/dev/null 2>&1; then \
				echo "Warning: Downloaded golangci-lint archive failed validation (attempt $$attempt/$$MAX_ATTEMPTS)"; \
			elif tar -xzf "$$ARCHIVE" -C "$$EXTRACT_DIR" && \
				mkdir -p "$$GOPATH/bin" && \
				mv "$$EXTRACT_DIR"/golangci-lint-*/$$BINARY_NAME "$$GOPATH/bin/$$BINARY_NAME" && \
				chmod +x "$$GOPATH/bin/$$BINARY_NAME"; then \
				echo "✓ golangci-lint $$GOLANGCI_LINT_VERSION installed to $$GOPATH/bin/$$BINARY_NAME"; \
				exit 0; \
			else \
				echo "Warning: Failed to extract or install golangci-lint archive (attempt $$attempt/$$MAX_ATTEMPTS)"; \
			fi; \
		else \
			echo "Warning: Failed to download golangci-lint archive (attempt $$attempt/$$MAX_ATTEMPTS)"; \
		fi; \
		if [ "$$attempt" -lt "$$MAX_ATTEMPTS" ]; then \
			echo "Retrying golangci-lint download in $$RETRY_DELAY seconds..."; \
			sleep $$RETRY_DELAY; \
			RETRY_DELAY=$$((RETRY_DELAY * 2)); \
		fi; \
	done; \
	echo "Error: Failed to download a valid golangci-lint archive from $$DOWNLOAD_URL after $$MAX_ATTEMPTS attempts"; \
	exit 1

# Install shellcheck binary
# Downloads pre-built binary from GitHub releases
.PHONY: install-shellcheck
install-shellcheck:
	@echo "Installing shellcheck binary..."
	@SHELLCHECK_VERSION="v0.11.0"; \
	GOPATH=$$(go env GOPATH); \
	GOOS=$$(go env GOOS); \
	GOARCH=$$(go env GOARCH); \
	BINARY_NAME="shellcheck"; \
	if [ "$$GOOS" = "windows" ]; then \
		BINARY_NAME="shellcheck.exe"; \
	fi; \
	if [ -x "$$GOPATH/bin/$$BINARY_NAME" ]; then \
		INSTALLED_VERSION=$$("$$GOPATH/bin/$$BINARY_NAME" --version 2>/dev/null | sed -n 's/^version: //p' | head -n1 || echo "unknown"); \
		if [ "$$INSTALLED_VERSION" = "$${SHELLCHECK_VERSION#v}" ]; then \
			echo "✓ shellcheck $$SHELLCHECK_VERSION already installed"; \
			exit 0; \
		fi; \
	fi; \
	case "$$GOOS/$$GOARCH" in \
		linux/amd64) ASSET_NAME="shellcheck-$$SHELLCHECK_VERSION.linux.x86_64.tar.gz" ;; \
		linux/arm64) ASSET_NAME="shellcheck-$$SHELLCHECK_VERSION.linux.aarch64.tar.gz" ;; \
		darwin/amd64) ASSET_NAME="shellcheck-$$SHELLCHECK_VERSION.darwin.x86_64.tar.gz" ;; \
		darwin/arm64) ASSET_NAME="shellcheck-$$SHELLCHECK_VERSION.darwin.aarch64.tar.gz" ;; \
		windows/amd64) ASSET_NAME="shellcheck-$$SHELLCHECK_VERSION.zip" ;; \
		*) echo "Error: shellcheck $$SHELLCHECK_VERSION is not supported on $$GOOS/$$GOARCH"; exit 1 ;; \
	esac; \
	DOWNLOAD_URL="https://github.com/koalaman/shellcheck/releases/download/$$SHELLCHECK_VERSION/$$ASSET_NAME"; \
	TEMP_DIR=$$(mktemp -d); \
	ARCHIVE="$$TEMP_DIR/$$ASSET_NAME"; \
	EXTRACT_DIR="$$TEMP_DIR/extract"; \
	MAX_ATTEMPTS=3; \
	RETRY_DELAY=2; \
	trap "rm -rf $$TEMP_DIR" EXIT; \
	echo "Downloading shellcheck $$SHELLCHECK_VERSION for $$GOOS/$$GOARCH..."; \
	for attempt in $$(seq 1 $$MAX_ATTEMPTS); do \
		rm -f "$$ARCHIVE"; \
		rm -rf "$$EXTRACT_DIR"; \
		mkdir -p "$$EXTRACT_DIR"; \
		if curl --fail --silent --show-error --location "$$DOWNLOAD_URL" -o "$$ARCHIVE"; then \
			if [ "$$GOOS" = "windows" ]; then \
				if unzip -q "$$ARCHIVE" -d "$$EXTRACT_DIR" && \
					mkdir -p "$$GOPATH/bin" && \
					mv "$$EXTRACT_DIR/$$BINARY_NAME" "$$GOPATH/bin/$$BINARY_NAME" && \
					chmod +x "$$GOPATH/bin/$$BINARY_NAME"; then \
					echo "✓ shellcheck $$SHELLCHECK_VERSION installed to $$GOPATH/bin/$$BINARY_NAME"; \
					exit 0; \
				fi; \
			elif tar -tzf "$$ARCHIVE" >/dev/null 2>&1 && \
				tar -xzf "$$ARCHIVE" -C "$$EXTRACT_DIR" && \
				mkdir -p "$$GOPATH/bin" && \
				mv "$$EXTRACT_DIR/shellcheck-$$SHELLCHECK_VERSION/$$BINARY_NAME" "$$GOPATH/bin/$$BINARY_NAME" && \
				chmod +x "$$GOPATH/bin/$$BINARY_NAME"; then \
				echo "✓ shellcheck $$SHELLCHECK_VERSION installed to $$GOPATH/bin/$$BINARY_NAME"; \
				exit 0; \
			fi; \
			echo "Warning: Failed to extract or install shellcheck archive (attempt $$attempt/$$MAX_ATTEMPTS)"; \
		else \
			echo "Warning: Failed to download shellcheck archive (attempt $$attempt/$$MAX_ATTEMPTS)"; \
		fi; \
		if [ "$$attempt" -lt "$$MAX_ATTEMPTS" ]; then \
			echo "Retrying shellcheck download in $$RETRY_DELAY seconds..."; \
			sleep $$RETRY_DELAY; \
			RETRY_DELAY=$$((RETRY_DELAY * 2)); \
		fi; \
	done; \
	echo "Error: Failed to download a valid shellcheck archive from $$DOWNLOAD_URL after $$MAX_ATTEMPTS attempts"; \
	exit 1

# License compliance checking
.PHONY: license-check
license-check: ## Check dependency licenses for compliance
	@echo "Checking dependency licenses..."
	@command -v go-licenses >/dev/null || go install github.com/google/go-licenses@latest
	@go-licenses check --disallowed_types=forbidden,reciprocal,restricted,unknown ./...
	@echo "✓ License check passed"

.PHONY: license-report
license-report: ## Generate CSV license report
	@echo "Generating license report..."
	@command -v go-licenses >/dev/null || go install github.com/google/go-licenses@latest
	@go-licenses csv ./... > licenses.csv 2>&1 || true
	@echo "✓ Report saved to licenses.csv"

# Install dependencies
.PHONY: deps
deps: check-node-version
	go mod download
	go mod tidy
	cd actions/setup/js && npm ci
	cd eslint-factory && npm ci

# Install development tools (including linter)
.PHONY: deps-dev
deps-dev: check-node-version deps tools install-golangci-lint install-shellcheck download-github-actions-schema
	@echo "✓ Development dependencies installed"

# Download GitHub Actions workflow schema for embedded validation
.PHONY: download-github-actions-schema
download-github-actions-schema:
	@echo "Downloading GitHub Actions workflow schema..."
	@mkdir -p pkg/workflow/schemas
	@curl -s -o pkg/workflow/schemas/github-workflow.json \
		"https://raw.githubusercontent.com/SchemaStore/schemastore/master/src/schemas/json/github-workflow.json"
	@echo "Formatting schema with prettier..."
	@cd actions/setup/js && npm run format:schema >/dev/null 2>&1
	@$(MAKE) patch-github-actions-schema
	@echo "✓ Downloaded and formatted GitHub Actions schema to pkg/workflow/schemas/github-workflow.json"

# Patch the GitHub Actions workflow schema with custom permissions not yet in SchemaStore.
# This must be run after download-github-actions-schema to preserve local additions.
.PHONY: patch-github-actions-schema
patch-github-actions-schema:
	@echo "Patching GitHub Actions schema with custom permissions..."
	@tmpfile=$$(mktemp) && \
		jq 'def append_missing($$items): reduce $$items[] as $$item (. ; if index($$item) then . else . + [$$item] end); .definitions["permissions-event"].properties += {"copilot-requests": {"type": "string", "enum": ["write", "none"]}, "drives": {"$$ref": "#/definitions/permissions-level"}, "vulnerability-alerts": {"type": "string", "enum": ["read", "none"]}} | (.properties.on.oneOf[] | select(.properties? and .properties.issues? and .properties.issues.properties? and .properties.issues.properties.types?).properties.issues.properties.types.items.enum) |= append_missing(["typed", "untyped", "field_added", "field_removed"]) | (.properties.on.oneOf[] | select(.properties? and .properties.issues? and .properties.issues.properties? and .properties.issues.properties.types?).properties.issues.properties.types.default) |= append_missing(["typed", "untyped", "field_added", "field_removed"])' \
			pkg/workflow/schemas/github-workflow.json > "$$tmpfile" && \
		mv "$$tmpfile" pkg/workflow/schemas/github-workflow.json
	@cd actions/setup/js && npm run format:schema >/dev/null 2>&1
	@echo "✓ Patched GitHub Actions schema with custom permissions"

# Run linter (full repository scan)
.PHONY: golint
golint:
	@GOPATH=$$(go env GOPATH); \
	if command -v golangci-lint >/dev/null 2>&1 || [ -x "$$GOPATH/bin/golangci-lint" ]; then \
		PATH="$$GOPATH/bin:$$PATH" golangci-lint run ./cmd/... ./pkg/...; \
	else \
		echo "golangci-lint is not installed. Run 'make deps-dev' to install dependencies."; \
		exit 1; \
	fi

# Run custom Go analysis linters (pkg/linters)
# Builds and runs linters defined in cmd/linters against the full repository.
# Override the large-function line limit with: make golint-custom MAX_LINES=80
# Limit the analyzer set with: make golint-custom LINTER_FLAGS="-errstringmatch -test=false"
# Limit analyzed packages with: make golint-custom LINTER_PACKAGES="./pkg/console ./pkg/workflow"
# Prefix with GOOS/GOARCH to analyze alternate build targets without cross-compiling the linter runner itself.
MAX_LINES ?= 60
LINTER_FLAGS ?=
LINTER_PACKAGES ?= ./cmd/... ./pkg/...
.PHONY: golint-custom
golint-custom:
	@echo "Building custom linters..."
	@env -u GOOS -u GOARCH go build -o /tmp/gh-aw-linters ./cmd/linters
	@echo "Running custom linters (largefunc max-lines=$(MAX_LINES))..."
	@/tmp/gh-aw-linters $(LINTER_FLAGS) -largefunc.max-lines=$(MAX_LINES) $(LINTER_PACKAGES)

# Run incremental linter (only changed files since BASE_REF)
# This provides 50-75% faster linting on PRs by only checking changed files
# Configuration optimizations in .golangci.yml:
# - timeout: 5m prevents hanging
# - modules-download-mode: readonly uses cached modules
# Usage: make golint-incremental BASE_REF=origin/main
.PHONY: golint-incremental
golint-incremental:
	@GOPATH=$$(go env GOPATH); \
	if ! command -v golangci-lint >/dev/null 2>&1 && [ ! -x "$$GOPATH/bin/golangci-lint" ]; then \
		echo "golangci-lint is not installed. Run 'make deps-dev' to install dependencies."; \
		exit 1; \
	fi
	@if [ -z "$(BASE_REF)" ]; then \
		echo "Error: BASE_REF not set. Use: make golint-incremental BASE_REF=origin/main"; \
		exit 1; \
	fi
	@echo "Running incremental lint against $(BASE_REF)..."
	@GOPATH=$$(go env GOPATH); \
	PATH="$$GOPATH/bin:$$PATH" golangci-lint run --new-from-rev=$(BASE_REF) ./cmd/... ./pkg/...

# Validate compiled workflow lock files using Docker-based actionlint
# Uses the same Docker integration as 'make actionlint'
.PHONY: validate-workflows
validate-workflows: build
	@echo "Validating compiled workflow lock files..."
	./$(BINARY_NAME) compile --actionlint

# Run actionlint on all workflow files
.PHONY: actionlint
actionlint: build
	@echo "Validating workflows with actionlint..."
	./$(BINARY_NAME) compile --actionlint

# Run lock-file-only lint using gh aw lint
.PHONY: lint-lock
lint-lock: build
	@echo "Linting committed lock files with gh aw lint..."
	./$(BINARY_NAME) lint

# Fast pre-build guard: detects workflow .md files modified in the working tree
# without their .lock.yml being regenerated.  Uses git to identify modified files
# and does not require the gh-aw binary.  Complements check-workflow-drift (which
# does a full recompile) by catching the obvious staleness case early.
.PHONY: check-stale-lock-files
check-stale-lock-files:
	@base_ref="$${CHECK_STALE_LOCK_BASE_REF:-}"; \
	if [ -z "$$base_ref" ] && [ -n "$${GITHUB_BASE_REF:-}" ]; then \
		if git rev-parse --verify "origin/$${GITHUB_BASE_REF}^{commit}" >/dev/null 2>&1; then \
			base_ref="origin/$${GITHUB_BASE_REF}"; \
		elif git rev-parse --verify "$${GITHUB_BASE_REF}^{commit}" >/dev/null 2>&1; then \
			base_ref="$${GITHUB_BASE_REF}"; \
		fi; \
	fi; \
	if [ -n "$$base_ref" ]; then \
		bash scripts/check-stale-lock-files.sh --base-ref "$$base_ref"; \
	else \
		bash scripts/check-stale-lock-files.sh; \
	fi

# Fast guard: fails when a skill references a backticked repo path that no longer exists.
.PHONY: check-skill-file-paths
check-skill-file-paths:
	@bash scripts/check-skill-file-paths.sh

# Check for drift between workflow markdown sources and generated lock files.
# Compiles all .github/workflows/*.md files and fails if any .lock.yml would
# change, reminding contributors to run 'make recompile' before committing.
.PHONY: check-workflow-drift
check-workflow-drift:
	@if [ ! -x "./$(BINARY_NAME)" ]; then \
		echo "./$(BINARY_NAME) not found; building it first..."; \
		$(MAKE) build; \
	fi
	@bash scripts/check-workflow-drift.sh ./$(BINARY_NAME)

# Guard against a stale binary when files under pkg/parser/schemas/ are modified.
# Schema files are embedded at compile time via //go:embed, so changing them without
# rebuilding means the running binary still holds the old schema.  This target
# detects when schema files have been modified (via git) but the binary has not been
# rebuilt.  It is the schema-side parallel of check-stale-lock-files.
# Requires the gh-aw binary to exist (run `make build` first).
.PHONY: check-stale-schema-binary
check-stale-schema-binary:
	@base_ref="$${CHECK_STALE_SCHEMA_BASE_REF:-}"; \
	if [ -z "$$base_ref" ] && [ -n "$${GITHUB_BASE_REF:-}" ]; then \
		if git rev-parse --verify "origin/$${GITHUB_BASE_REF}^{commit}" >/dev/null 2>&1; then \
			base_ref="origin/$${GITHUB_BASE_REF}"; \
		elif git rev-parse --verify "$${GITHUB_BASE_REF}^{commit}" >/dev/null 2>&1; then \
			base_ref="$${GITHUB_BASE_REF}"; \
		fi; \
	fi; \
	if [ -n "$$base_ref" ]; then \
		bash scripts/check-stale-schema-binary.sh --binary "./$(BINARY_NAME)" --base-ref "$$base_ref"; \
	else \
		bash scripts/check-stale-schema-binary.sh --binary "./$(BINARY_NAME)"; \
	fi

# Format code
.PHONY: fmt
fmt: fmt-go fmt-cjs fmt-json
	@echo "✓ Code formatted successfully"

.PHONY: fmt-go
fmt-go:
	@echo "→ Formatting Go code..."
	@go fmt ./...
	@echo "✓ Go code formatted"

# Format JavaScript (.cjs and .js), TypeScript, and JSON files in runtime + eslint-factory directories
.PHONY: fmt-cjs
fmt-cjs:
	@echo "→ Formatting JavaScript files..."
	@cd actions/setup/js && npm run format:cjs --silent >/dev/null 2>&1
	@cd eslint-factory && npx prettier --write '**/*.cjs' '**/*.ts' '**/*.json' --ignore-path ../.prettierignore --log-level=error 2>&1
	@npx prettier --write 'scripts/**/*.js' --ignore-path .prettierignore --log-level=error 2>&1
	@echo "✓ JavaScript files formatted"

# Format JSON files in pkg directory (excluding actions/setup/js, which is handled by npm script)
.PHONY: fmt-json
fmt-json:
	@echo "→ Formatting JSON files..."
	@cd actions/setup/js && npm run format:pkg-json --silent >/dev/null 2>&1
	@npx prettier --write 'pkg/cli/data/models.json' 'actions/setup/js/models.json' --ignore-path .prettierignore --log-level=error 2>&1
	@echo "✓ JSON files formatted"

# Check formatting
.PHONY: fmt-check
fmt-check:
	@unformatted=$$(go fmt ./...); \
	if [ -n "$$unformatted" ]; then \
		echo "Code is not formatted. Run 'make fmt' to fix."; \
		echo "$$unformatted"; \
		exit 1; \
	fi

# Check JavaScript (.cjs and .js), TypeScript, and JSON file formatting in runtime + eslint-factory directories
.PHONY: fmt-check-cjs
fmt-check-cjs:
	cd actions/setup/js && npm run lint:cjs
	cd eslint-factory && npx prettier --check '**/*.cjs' '**/*.ts' '**/*.json' --ignore-path ../.prettierignore
	npx prettier --check 'scripts/**/*.js' --ignore-path .prettierignore

# Check JSON file formatting in pkg directory (excluding actions/setup/js, which is handled by npm script)
.PHONY: fmt-check-json
fmt-check-json:
	@if ! cd actions/setup/js && npm run check:pkg-json 2>&1 | grep -q "All matched files use Prettier code style"; then \
		echo "JSON files are not formatted. Run 'make fmt-json' to fix."; \
		exit 1; \
	fi

# Validate CJS module syntax by running `node --check` on every non-test .cjs file.
# This catches module-load SyntaxErrors (e.g. `await` inside a non-async function)
# before any workflow runs, closing the gap that caused the PR #43170 regression.
.PHONY: validate-cjs-syntax
validate-cjs-syntax:
	@bash scripts/validate-cjs-syntax.sh

# Lint JavaScript (.cjs and .js) and JSON files in actions/setup/js directory
.PHONY: lint-cjs
lint-cjs: fmt-check-cjs validate-cjs-syntax check-node-version
	@cd eslint-factory && { [ -d node_modules ] || npm ci; } && npm run lint:setup-js
	@echo "✓ JavaScript formatting, syntax, and lint validated"

# Lint JSON files in pkg directory (excluding actions/setup/js, which is handled by npm script)
.PHONY: lint-json
lint-json: fmt-check-json
	@echo "✓ JSON formatting validated"

# Full-repository error-message audit (non-blocking report).
# Reports pre-existing error-message violations across the repo so the debt can
# be tracked as a metric over time. Always exits 0.
.PHONY: lint-error-messages-report
lint-error-messages-report:
	@echo "Building custom linters..."
	@env -u GOOS -u GOARCH go build -o /tmp/gh-aw-linters ./cmd/linters
	@echo "Auditing error messages across $(LINTER_PACKAGES) (non-blocking)..."
	@/tmp/gh-aw-linters -errormessage -errormessage.full-repo $(LINTER_PACKAGES) || true

# Lint error messages for quality compliance
.PHONY: lint-errors
lint-errors:
	@echo "Running error message quality linter..."
	@go run scripts/lint_error_messages.go

.PHONY: validate-model-alias-chains
validate-model-alias-chains:
	@echo "Validating built-in model alias resolution chains..."
	@node scripts/validate-model-alias-chains.js

# Validate model_multipliers.json has no placeholder or null multipliers (R-REG-007)
# See docs/src/content/docs/specs/effective-tokens-specification.md §Model Multiplier Registry
.PHONY: validate-registry
validate-registry:
	@echo "Validating model_multipliers.json (R-REG-007: no placeholder or null multipliers)..."
	@go test ./pkg/cli/... -run TestModelMultipliersNoPlaceholders -count=1

# Validate the gh-aw OpenTelemetry compatibility contract from specs/otel-observability-spec.md.
# This is intentionally focused and isolated: schema/frontmatter acceptance, compiler env plumbing,
# raw OTLP JSONL mirrors, and shipped GenAI compatibility attributes.
.PHONY: validate-otel-contract
validate-otel-contract:
	@echo "Validating gh-aw OpenTelemetry compatibility contract..."
	@go test ./pkg/parser ./pkg/workflow -run 'TestValidateMainWorkflowFrontmatterWithSchemaAndLocation_OTLP(CustomAttributes|ResourceAttributes|GitHubAppImplicitOIDC)|TestInjectOTLPConfig|TestApplyTraceContextEnvToMap' -count=1
	@cd actions/setup/js && npm run test:js -- otel_contract.test.cjs send_otlp_span.test.cjs --no-file-parallelism >/dev/null
	@echo "✓ OpenTelemetry compatibility contract validated"

MODELS_DEV_MODELS_JSON_URL ?= https://models.dev/catalog.json

.PHONY: refresh-models-json
refresh-models-json:
	@echo "Refreshing models.json from $(MODELS_DEV_MODELS_JSON_URL)..."
	@set -e; \
	tmp=$$(mktemp); \
	src=$$(mktemp); \
	trap 'rm -f "$$tmp" "$$src"' EXIT; \
	curl -fsSL "$(MODELS_DEV_MODELS_JSON_URL)" -o "$$src"; \
	jq '{providers: ((.providers // {}) | with_entries(select(.key | test("^(anthropic|openai|github-copilot)$$"))) | with_entries(.value |= {models: ((.models // {}) | with_entries(.value |= ({cost: ((.cost // {}) | with_entries(select(.value != null and ((.value | type) == "number" or (.value | type) == "string"))) | with_entries(if (.value | type) == "number" then .value |= (./1000000 | tostring) else . end))} + (if (.provider_type | type) == "string" then {provider_type: .provider_type} else {} end) + (if (.wire_api | type) == "string" then {wire_api: .wire_api} elif (.wireApi | type) == "string" then {wire_api: .wireApi} else {} end))) )}))}' "$$src" > "$$tmp"; \
	cp "$$tmp" pkg/cli/data/models.json; \
	cp "$$tmp" actions/setup/js/models.json; \
	echo "✓ Refreshed pkg/cli/data/models.json and actions/setup/js/models.json (catalog providers: anthropic, openai, github-copilot)"

.PHONY: validate-models-json-sync
validate-models-json-sync:
	@echo "Validating models.json mirror consistency..."
	@set -e; \
	command -v jq >/dev/null 2>&1 || { echo "jq is required for validate-models-json-sync"; exit 1; }; \
	tmp1=$$(mktemp); tmp2=$$(mktemp); \
	trap 'rm -f "$$tmp1" "$$tmp2"' EXIT; \
	jq -S . pkg/cli/data/models.json > "$$tmp1"; \
	jq -S . actions/setup/js/models.json > "$$tmp2"; \
	cmp -s "$$tmp1" "$$tmp2" || { \
		echo "models.json mirrors diverged:"; \
		echo "  pkg/cli/data/models.json and actions/setup/js/models.json must remain semantically identical after key sorting."; \
		echo "  Run 'make refresh-models-json' to synchronize the mirrors."; \
		diff -u "$$tmp1" "$$tmp2" || true; \
		exit 1; \
	}
	@echo "✓ models.json mirrors are synchronized"

# Check file sizes and function counts
.PHONY: check-file-sizes
check-file-sizes:
	@bash scripts/check-file-sizes.sh

# Check that *_validation.go files stay within the 768-line hard limit
# Set WARN_ONLY=1 to report violations without failing (non-blocking mode)
.PHONY: check-validator-sizes
check-validator-sizes:
	@bash scripts/check-validator-sizes.sh

# Lint action shell scripts — ensure no python/python3 invocations in actions/**/*.sh
.PHONY: lint-action-sh
lint-action-sh:
	@echo "Checking action shell scripts for python/python3 invocations..."
	@bash scripts/check-action-sh-no-python.sh

# Run shellcheck on actions/setup/sh scripts at error severity
.PHONY: shellcheck-setup-sh
shellcheck-setup-sh:
	@GOPATH=$$(go env GOPATH); \
	GOOS=$$(go env GOOS); \
	BINARY_NAME="shellcheck"; \
	if [ "$$GOOS" = "windows" ]; then \
		BINARY_NAME="shellcheck.exe"; \
	fi; \
	if command -v shellcheck >/dev/null 2>&1 || [ -x "$$GOPATH/bin/$$BINARY_NAME" ]; then \
		echo "Running shellcheck on actions/setup/sh..."; \
		PATH="$$GOPATH/bin:$$PATH" shellcheck --severity=error actions/setup/sh/*.sh; \
		echo "✓ shellcheck passed"; \
	else \
		echo "shellcheck is not installed. Run 'make deps-dev' to install dependencies."; \
		exit 1; \
	fi

# Validate all project files
.PHONY: lint
lint: check-stale-lock-files check-skill-file-paths fmt-check fmt-check-json lint-cjs golint validate-model-alias-chains lint-action-sh shellcheck-setup-sh check-stale-schema-binary
	@echo "✓ All validations passed"

# Install the binary locally
.PHONY: install
install: build
	gh extension remove gh-aw || true
	gh extension install .

# Generate schema documentation
.PHONY: generate-schema-docs
generate-schema-docs:
	node scripts/generate-schema-docs.js

# Generate agent factory documentation page
.PHONY: generate-agent-factory
generate-agent-factory:
	node scripts/generate-agent-factory.js

# Generate llms.txt at repository root from .github/aw/*.md
.PHONY: generate-llms-txt
generate-llms-txt:
	node scripts/generate-llms-txt.js

# Build slides with Marp
.PHONY: build-slides
build-slides:
	@echo "Building slides with Marp..."
	@cd docs && npx @marp-team/marp-cli ../slides/index.md --html --allow-local-files -o public/slides/gh-aw.html
	@echo "✓ Slides built to docs/public/slides/gh-aw.html"

# Documentation targets
.PHONY: deps-docs
deps-docs: check-node-version
	@echo "Installing documentation dependencies..."
	@cd docs && npm ci
	@echo "✓ Documentation dependencies installed"

.PHONY: build-docs
build-docs: deps-docs
	@echo "Building Astro documentation..."
	@cd docs && npm run build
	@echo "✓ Documentation built to docs/dist"

.PHONY: dev-docs
dev-docs: deps-docs
	@echo "Starting Astro development server..."
	@cd docs && npm run dev -- --host 127.0.0.1 --port 4321

.PHONY: preview-docs
preview-docs: build-docs
	@echo "Starting Astro preview server..."
	@cd docs && npm run preview

.PHONY: clean-docs
clean-docs:
	@echo "Cleaning documentation artifacts..."
	@rm -rf docs/dist docs/node_modules docs/.astro
	@echo "✓ Documentation artifacts cleaned"

.PHONY: test-docs-remark
test-docs-remark:
	@echo "Running docs remark plugin unit tests..."
	@node docs/src/lib/remark/inlineMarkdownInHtml.test.js
	@echo "✓ Docs remark plugin unit tests passed"

.PHONY: test-docs-wizard-model
test-docs-wizard-model:
	@echo "Running AW wizard data model unit tests..."
	@node docs/src/lib/wizard/model.test.js
	@echo "✓ AW wizard data model unit tests passed"

# Sync templates from .github to pkg/cli/templates
# Sync action pins from .github/aw to pkg/actionpins/data and pkg/workflow/data
.PHONY: sync-action-pins
sync-action-pins:
	@echo "Syncing actions-lock.json from .github/aw to pkg/actionpins/data/action_pins.json and pkg/workflow/data/action_pins.json..."
	@if [ -f .github/aw/actions-lock.json ]; then \
		jq --indent 2 '{entries: .entries} + if ((.containers // {}) | length) > 0 then {containers: .containers} else {} end' .github/aw/actions-lock.json > pkg/actionpins/data/action_pins.json; \
		jq --indent 2 '{entries: .entries} + if ((.containers // {}) | length) > 0 then {containers: .containers} else {} end' .github/aw/actions-lock.json > pkg/workflow/data/action_pins.json; \
		echo "✓ Action pins synced successfully"; \
	else \
		echo "⚠ Warning: .github/aw/actions-lock.json does not exist yet"; \
	fi

# Sync action scripts
.PHONY: sync-action-scripts
sync-action-scripts:
	@echo "Syncing install-gh-aw.sh to actions/setup-cli/install.sh..."
	@cp install-gh-aw.sh actions/setup-cli/install.sh
	@chmod +x actions/setup-cli/install.sh
	@echo "Syncing install-gh-aw.ps1 to actions/setup-cli/install.ps1..."
	@cp install-gh-aw.ps1 actions/setup-cli/install.ps1
	@echo "✓ Action scripts synced successfully"

# Sync max-agent in the latest compat.json interval with DefaultCopilotVersion
.PHONY: sync-compat
sync-compat:
	@bash scripts/sync-compat.sh

# Check that compat.json is in sync with DefaultCopilotVersion (CI gate)
.PHONY: check-stale-compat
check-stale-compat:
	@bash scripts/sync-compat.sh --check

# Sync install-gh-aw.sh SHA/hash constants in pkg/cli/copilot_setup.go
.PHONY: sync-install-script-hashes
sync-install-script-hashes:
	@echo "Updating install-gh-aw.sh SHA and SHA256 constants..."
	@bash scripts/update-install-script-hashes.sh
	@echo "✓ Install script hashes synced successfully"

# Recompile all workflow files
.PHONY: recompile
recompile: build
	./$(BINARY_NAME) init --codespaces ""
	./$(BINARY_NAME) compile --validate --verbose --purge --schedule-seed github/gh-aw
#	./$(BINARY_NAME) compile --dir pkg/cli/workflows --validate --verbose --purge

# Compile workflows under pkg/cli/workflows
.PHONY: compile-cli-workflows
compile-cli-workflows:
	@if [ ! -x "./$(BINARY_NAME)" ]; then \
		echo "./$(BINARY_NAME) not found; building it first..."; \
		$(MAKE) build; \
	fi
	@TMP_WORKFLOWS_DIR=$$(mktemp -d); \
	trap 'rm -rf "$$TMP_WORKFLOWS_DIR"' EXIT; \
	cp -R pkg/cli/workflows "$$TMP_WORKFLOWS_DIR/workflows"; \
	WORKFLOWS=$$(find "$$TMP_WORKFLOWS_DIR/workflows" -maxdepth 1 -type f -name '*.lock.yml' | sed 's/\.lock\.yml$$/.md/' | sort | tr '\n' ' '); \
	if [ -z "$$WORKFLOWS" ]; then \
		echo "No workflow files found in pkg/cli/workflows"; \
		exit 1; \
	fi; \
	./$(BINARY_NAME) compile --fix --no-check-update $$WORKFLOWS

# Apply automatic fixes to workflow files
.PHONY: fix
fix: build
	./$(BINARY_NAME) fix --write

# Generate Dependabot manifests for npm dependencies
.PHONY: dependabot
dependabot: build
	./$(BINARY_NAME) compile --dependabot --verbose

# Update GitHub Actions and workflows, then sync action pins and rebuild
.PHONY: update
update: build
	./$(BINARY_NAME) update
	$(MAKE) sync-action-pins
	$(MAKE) sync-install-script-hashes
	$(MAKE) build

# Run development server
.PHONY: dev
dev: build
	./$(BINARY_NAME)

.PHONY: watch
watch: build
	./$(BINARY_NAME) compile --watch

.PHONY: pull-main
pull-main:
	@echo "check on main branch"
	@git checkout main
	@echo "Check out branch is clean"
	@git diff --quiet || (echo "Error: Working directory is not clean. Please commit or stash changes before pulling." && exit 1)
	@echo "Pulling latest changes..."
	@git pull

.PHONY: merge-main
merge-main:
	@echo "Formatting before merge..."
	@$(MAKE) fmt
	@echo "Fetching latest main..."
	@git fetch origin main
	@echo "Merging origin/main..."
	@git merge origin/main || (echo "Merge conflicts detected. Resolve conflicts in .go and .cjs files, stage with git add, then run: make build && make recompile && git commit && make fmt" && exit 1)
	@echo "Building after merge..."
	@$(MAKE) build
	@echo "Recompiling workflows..."
	@$(MAKE) recompile
	@echo "Formatting after merge..."
	@$(MAKE) fmt

# Generate Software Bill of Materials (SBOM)
.PHONY: sbom
sbom:
	@if ! command -v syft >/dev/null 2>&1; then \
		echo "Error: syft is not installed."; \
		echo ""; \
		echo "Install syft to generate SBOMs:"; \
		echo "  curl -sSfL https://raw.githubusercontent.com/anchore/syft/main/install.sh | sh -s -- -b /usr/local/bin"; \
		echo ""; \
		echo "Or visit: https://github.com/anchore/syft#installation"; \
		exit 1; \
	fi
	@echo "Generating SBOM in SPDX format..."
	syft packages . -o spdx-json=sbom.spdx.json
	@echo "Generating SBOM in CycloneDX format..."
	syft packages . -o cyclonedx-json=sbom.cdx.json
	@echo "✓ SBOM files generated: sbom.spdx.json, sbom.cdx.json"

# Agent should run this task before finishing its turns
.PHONY: agent-finish
agent-finish: deps-dev fmt lint build build-wasm test-all validate-otel-contract fix recompile dependabot generate-schema-docs generate-agent-factory generate-llms-txt security-scan
	@echo "Agent finished tasks successfully."

# Change-scoped pre-PR gate — run before every intermediate report_progress call.
# The driver runs independent lint checks in parallel after formatting and build.
.PHONY: agent-report-progress-no-test
agent-report-progress-no-test:
	@bash scripts/agent-report-progress.sh

# Change-scoped pre-PR gate with impacted tests — run once before the final
# report_progress call. Independent lint and test groups run in parallel; full
# workflow recompilation remains isolated because it temporarily rewrites files.
.PHONY: agent-report-progress
agent-report-progress:
	@bash scripts/agent-report-progress.sh --with-tests

# Extended pre-PR gate with lock-file-only linting.
.PHONY: agent-report-progress-lint
agent-report-progress-lint: agent-report-progress lint-lock
	@echo "Pre-PR validation + lock-file lint passed. Safe to call report_progress."

# Help target
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  build            - Build the binary for current platform"
	@echo "  build-awmg       - Build the awmg (MCP gateway) binary for current platform"
	@echo "  build-all        - Build binaries for all platforms (gh-aw and awmg)"
	@echo "  test             - Run Go tests (unit + integration)"
	@echo "  test-unit        - Run impacted Go unit tests first (~$(TEST_UNIT_MAX_SECONDS)s budget); set TEST_UNIT_RUN_FULL=1 for full suite"
	@echo "  test-unit-all    - Run full Go unit test suite"
	@echo "  test-security    - Run security regression tests"
	@echo "  test-js          - Run JavaScript tests"
	@echo "  test-impacted-js - Run impacted JavaScript unit tests for current branch changes"
	@echo "  test-impacted-go - Run impacted Go unit tests for current branch changes"
	@echo "  test-impacted    - Run impacted JavaScript and Go unit tests for current branch changes"
	@echo "  test-scripts     - Run Bash script tests (check-stale-lock-files, check-workflow-drift)"
	@echo "  test-all         - Run all tests (Go, JavaScript, wasm golden, and shell scripts)"
	@echo "  test-wasm-golden - Run wasm golden tests (Go string API path)"
	@echo "  test-wasm        - Build wasm and run Node.js golden comparison test"
	@echo "  update-wasm-golden - Regenerate wasm golden files from current compiler output"
	@echo "  test-coverage    - Run tests with coverage report"
	@echo "  bench            - Run benchmarks for performance testing"
	@echo "  bench-compare    - Run benchmarks with more iterations (for benchstat comparison)"
	@echo "  bench-memory     - Run memory profiling benchmarks with pprof output"
	@echo "  fuzz             - Run fuzz tests for 30 seconds"
	@echo "  bundle-js        - Build JavaScript bundler tool (./bundle-js <input> [output])"
	@echo "  clean            - Clean build artifacts"
	@echo "  docker-build     - Build Docker image locally (linux/amd64)"
	@echo "  docker-build-multiarch - Build multi-architecture Docker image (linux/amd64, linux/arm64)"
	@echo "  docker-test      - Test Docker image functionality"
	@echo "  docker-push      - Push Docker images to registry"
	@echo "  docker-clean     - Remove local Docker images"
	@echo "  actions-build    - Build all custom GitHub Actions from source"
	@echo "  actions-validate - Validate action.yml files"
	@echo "  actions-clean    - Clean action build artifacts"
	@echo "  generate-action-metadata - Generate action.yml and README.md from JavaScript modules"
	@echo "  tools            - Install build-time tools declared in go.mod tool directives"
	@echo "  license-check    - Check dependency licenses for compliance"
	@echo "  license-report   - Generate CSV license report"
	@echo "  deps             - Install dependencies"
	@echo "  deps-dev         - Install development dependencies (includes tools)"
	@echo "  install-shellcheck - Install pinned shellcheck binary"
	@echo "  check-node-version - Check Node.js version (20 or higher required)"
	@echo "  golint           - Run golangci-lint (full repository scan)"
	@echo "  golint-incremental - Run golangci-lint incrementally (only changed files, requires BASE_REF)"
	@echo "  lint             - Run linter"
	@echo "  fmt              - Format code"
	@echo "  fmt-cjs          - Format JavaScript/TypeScript/JSON files in actions/setup/js and eslint-factory"
	@echo "  fmt-json         - Format JSON files in pkg directory (excluding actions/setup/js)"
	@echo "  fmt-check        - Check code formatting"
	@echo "  fmt-check-cjs    - Check JavaScript/TypeScript/JSON formatting in actions/setup/js and eslint-factory"
	@echo "  fmt-check-json   - Check JSON file formatting in pkg directory (excluding actions/setup/js)"
	@echo "  lint-cjs         - Lint JavaScript/TypeScript/JSON formatting in actions/setup/js and eslint-factory"
	@echo "  validate-cjs-syntax - Syntax-check all non-test .cjs files (catches module-load SyntaxErrors)"
	@echo "  lint-json        - Lint JSON files in pkg directory (excluding actions/setup/js)"
	@echo "  lint-errors      - Lint error messages for quality compliance"
	@echo "  lint-error-messages-report - Non-blocking full-repo error message audit"
	@echo "  validate-otel-contract - Validate the gh-aw OpenTelemetry compatibility contract"
	@echo "  lint-action-sh   - Lint action shell scripts for python/python3 invocations"
	@echo "  shellcheck-setup-sh - Run shellcheck on actions/setup/sh scripts"
	@echo "  check-file-sizes - Check Go file sizes and function counts (informational)"
	@echo "  check-validator-sizes - Check *_validation.go files against the 768-line hard limit"
	@echo "  security-scan    - Run all security scans (gosec, govulncheck)"
	@echo "  security-gosec   - Run gosec Go security scanner"
	@echo "  security-govulncheck - Run govulncheck for known vulnerabilities"
	@echo "  security-govulncheck-sarif - Run govulncheck and output SARIF report (govulncheck-results.sarif)"
	@echo "  actionlint       - Validate workflows with actionlint (depends on build)"
	@echo "  lint-lock        - Run lock-file-only lint with gh aw lint (depends on build)"
	@echo "  validate-workflows - Validate compiled workflow lock files (depends on build)"
	@echo "  check-workflow-drift - Check for drift between .md sources and .lock.yml files (builds binary if missing)"
	@echo "  check-stale-lock-files - Fast guard: detect modified .md files without regenerated .lock.yml (no binary needed)"
	@echo "  check-skill-file-paths - Guard: reject invalid backticked repo paths in .github/skills/**/SKILL.md"
	@echo "  check-stale-schema-binary - Guard: detect modified schema files under pkg/parser/schemas/ without a binary rebuild"
	@echo "  install          - Install binary locally"
	@echo "  sync-action-pins - Sync actions-lock.json from .github/aw to pkg/actionpins/data and pkg/workflow/data (runs automatically during build)"
	@echo "  sync-action-scripts - Sync install-gh-aw.sh and install-gh-aw.ps1 to actions/setup-cli/ (runs automatically during build)"
	@echo "  sync-compat      - Sync max-agent in .github/aw/compat.json latest interval with DefaultCopilotVersion (runs automatically during build)"
	@echo "  check-stale-compat - Guard: detect when compat.json max-agent is out of sync with DefaultCopilotVersion"
	@echo "  sync-install-script-hashes - Update install-gh-aw.sh SHA and SHA256 constants in pkg/cli/copilot_setup.go (runs automatically during update)"
	@echo "  update           - Update GitHub Actions and workflows, sync action pins, and rebuild binary"
	@echo "  fix              - Apply automatic codemod-style fixes to workflow files (depends on build)"
	@echo "  recompile        - Recompile all workflow files (runs init, depends on build)"
	@echo "  merge-main       - Format, merge main, recompile workflows, and format again"
	@echo "  compile-cli-workflows - Compile workflows in pkg/cli/workflows (builds binary if missing)"
	@echo "  dependabot       - Generate Dependabot manifests for npm dependencies in workflows"
	@echo "  generate-schema-docs - Generate frontmatter full reference documentation from JSON schema"
	@echo "  generate-agent-factory     - Generate agent factory documentation page"
	@echo "  generate-llms-txt  - Generate llms.txt at repository root from .github/aw/*.md"
	@echo "  build-slides     - Build slides with Marp to docs/public/slides/gh-aw.html"
	@echo "  deps-docs        - Install Astro documentation dependencies"
	@echo "  build-docs       - Build Astro documentation to docs/dist"
	@echo "  dev-docs         - Start Astro development server for live preview"
	@echo "  preview-docs     - Preview built documentation with Astro"
	@echo "  clean-docs       - Clean documentation artifacts (dist, node_modules, .astro)"

	@echo "  agent-finish                - Complete validation sequence (build, test, fix, recompile, fmt, lint, security-scan)"
	@echo "  agent-report-progress-no-test - Fast change-scoped pre-PR gate without tests"
	@echo "  agent-report-progress       - Change-scoped pre-PR gate with impacted Go tests"
	@echo "  agent-report-progress-lint  - Full pre-PR gate + gh aw lint lock-file check"
	@echo "  sbom             - Generate SBOM in SPDX and CycloneDX formats (requires syft)"
	@echo "  help             - Show this help message"
