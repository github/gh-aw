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

# Build flags
LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION)"

# Default target
.PHONY: all
all: build

# Build the binary, run make deps before this
.PHONY: build
build: sync-action-pins sync-action-scripts
	go build $(LDFLAGS) -o $(BINARY_NAME) ./cmd/gh-aw

# Build for all platforms
.PHONY: build-all
build-all: build-linux build-darwin build-windows

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

# Test the code (runs both unlabelled unit tests and integration tests and long tests)
.PHONY: test
test: test-unit test-integration

# Test unit tests only (excludes labelled integration tests and long tests)
.PHONY: test-unit
test-unit:
	go test -v -parallel=4 -timeout=10m -run='^Test' ./... -short

.PHONY: test-integration
test-integration:
	go test -v -parallel=4 -timeout=10m -run='^Test' ./... -short

# Update golden test files
.PHONY: update-golden
update-golden:
	@echo "Updating golden test files..."
	go test -v ./pkg/console -run='^TestGolden_' -update

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
.PHONY: bench-performance
bench-performance:
	@echo "Running critical performance benchmarks..."
	@echo "This includes: CompileSimpleWorkflow, CompileComplexWorkflow, CompileMCPWorkflow,"
	@echo "               CompileMemoryUsage, ParseWorkflow, Validation, YAMLGeneration"
	@go test -bench='Benchmark(CompileSimpleWorkflow|CompileComplexWorkflow|CompileMCPWorkflow|CompileMemoryUsage|ParseWorkflow|Validation|YAMLGeneration)$$' \
		-benchmem -benchtime=3x -run=^$$ ./pkg/workflow | tee bench_performance.txt
	@echo ""
	@echo "Also running CLI helper benchmarks..."
	@go test -bench='Benchmark(ExtractWorkflowNameFromFile|UpdateWorkflowTitle|FindIncludesInContent)$$' \
		-benchmem -benchtime=3x -run=^$$ ./pkg/cli >> bench_performance.txt
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

# Security scanning with gosec, govulncheck, and trivy
.PHONY: security-scan
security-scan: security-gosec security-govulncheck security-trivy
	@echo "✓ All security scans completed"

.PHONY: security-gosec
security-gosec:
	@echo "Running gosec security scanner..."
	@command -v gosec >/dev/null || go install github.com/securego/gosec/v2/cmd/gosec@v2.22.11
	@# Exclusions configured in .golangci.yml (linters-settings.gosec.exclude)
	@# Keep this list in sync with .golangci.yml for consistency
	@GOPATH=$$(go env GOPATH); \
	PATH="$$GOPATH/bin:$$PATH" gosec -fmt=json -out=gosec-report.json -stdout -exclude-generated -track-suppressions \
		-exclude=G101,G115,G602,G301,G302,G304,G306 \
		./...
	@echo "✓ Gosec scan complete (results in gosec-report.json)"

.PHONY: security-govulncheck
security-govulncheck:
	@echo "Running govulncheck..."
	@command -v govulncheck >/dev/null || go install golang.org/x/vuln/cmd/govulncheck@latest
	govulncheck ./...
	@echo "✓ Govulncheck complete"

.PHONY: security-trivy
security-trivy:
	@echo "Running trivy filesystem scan..."
	@if command -v trivy >/dev/null 2>&1; then \
		trivy fs --severity HIGH,CRITICAL .; \
	else \
		echo "⚠ Trivy not installed. Install with: brew install trivy (macOS) or see https://aquasecurity.github.io/trivy/latest/getting-started/installation/"; \
	fi
	@echo "✓ Trivy scan complete"

# Test JavaScript files
.PHONY: test-js
test-js: build-js
	cd actions/setup/js && npm run test:js -- --no-file-parallelism

# Install JavaScript dependencies
.PHONY: deps-js
deps-js: check-node-version
	cd actions/setup/js && npm ci

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

# Test all code (Go and JavaScript)
.PHONY: test-all
test-all: test test-js

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
	rm -f gosec-report.json gosec-results.sarif govulncheck-results.sarif trivy-results.sarif
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
tools: ## Install build-time tools from tools.go
	@echo "Installing build tools..."
	@grep _ tools.go | awk -F'"' '{print $$2}' | xargs -tI % go install %
	@echo "✓ Tools installed successfully"

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

# Install development tools (including linter)
.PHONY: deps-dev
deps-dev: check-node-version deps tools download-github-actions-schema
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
	@echo "✓ Downloaded and formatted GitHub Actions schema to pkg/workflow/schemas/github-workflow.json"

# Run linter (full repository scan)
.PHONY: golint
golint:
	@GOPATH=$$(go env GOPATH); \
	if command -v golangci-lint >/dev/null 2>&1 || [ -x "$$GOPATH/bin/golangci-lint" ]; then \
		PATH="$$GOPATH/bin:$$PATH" golangci-lint run; \
	else \
		echo "golangci-lint is not installed. Run 'make deps-dev' to install dependencies."; \
		exit 1; \
	fi

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
	PATH="$$GOPATH/bin:$$PATH" golangci-lint run --new-from-rev=$(BASE_REF)

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

# Format code
.PHONY: fmt
fmt: fmt-go fmt-cjs fmt-json
	@echo "✓ Code formatted successfully"

.PHONY: fmt-go
fmt-go:
	@echo "→ Formatting Go code..."
	@go fmt ./...
	@echo "✓ Go code formatted"

# Format JavaScript (.cjs and .js) and JSON files in actions/setup/js directory
.PHONY: fmt-cjs
fmt-cjs:
	@echo "→ Formatting JavaScript files..."
	@cd actions/setup/js && npm run format:cjs
	@npx prettier --write 'scripts/**/*.js' --ignore-path .prettierignore
	@echo "✓ JavaScript files formatted"

# Format JSON files in pkg directory (excluding actions/setup/js, which is handled by npm script)
.PHONY: fmt-json
fmt-json:
	@echo "→ Formatting JSON files..."
	@cd actions/setup/js && npm run format:pkg-json
	@echo "✓ JSON files formatted"

# Check formatting
.PHONY: fmt-check
fmt-check:
	@GOPATH=$$(go env GOPATH); \
	if command -v golangci-lint >/dev/null 2>&1 || [ -x "$$GOPATH/bin/golangci-lint" ]; then \
		diff_output=$$(PATH="$$GOPATH/bin:$$PATH" golangci-lint fmt --diff 2>&1); \
		if [ -n "$$diff_output" ]; then \
			echo "Code is not formatted. Run 'make fmt' to fix."; \
			exit 1; \
		fi; \
	else \
		echo "golangci-lint is not installed. Run 'make deps-dev' to install dependencies."; \
		exit 1; \
	fi

# Check JavaScript (.cjs and .js) and JSON file formatting in actions/setup/js directory
.PHONY: fmt-check-cjs
fmt-check-cjs:
	cd actions/setup/js && npm run lint:cjs
	npx prettier --check 'scripts/**/*.js' --ignore-path .prettierignore

# Check JSON file formatting in pkg directory (excluding actions/setup/js, which is handled by npm script)
.PHONY: fmt-check-json
fmt-check-json:
	@if ! cd actions/setup/js && npm run check:pkg-json 2>&1 | grep -q "All matched files use Prettier code style"; then \
		echo "JSON files are not formatted. Run 'make fmt-json' to fix."; \
		exit 1; \
	fi

# Lint JavaScript (.cjs and .js) and JSON files in actions/setup/js directory
.PHONY: lint-cjs
lint-cjs: fmt-check-cjs
	@echo "✓ JavaScript formatting validated"

# Lint JSON files in pkg directory (excluding actions/setup/js, which is handled by npm script)
.PHONY: lint-json
lint-json: fmt-check-json
	@echo "✓ JSON formatting validated"

# Lint error messages for quality compliance
.PHONY: lint-errors
lint-errors:
	@echo "Running error message quality linter..."
	@go run scripts/lint_error_messages.go

# Check file sizes and function counts
.PHONY: check-file-sizes
check-file-sizes:
	@bash scripts/check-file-sizes.sh

# Validate all project files
.PHONY: lint
lint: fmt-check fmt-check-json lint-cjs golint
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
	@cd docs && npm run dev

.PHONY: preview-docs
preview-docs: build-docs
	@echo "Starting Astro preview server..."
	@cd docs && npm run preview

.PHONY: clean-docs
clean-docs:
	@echo "Cleaning documentation artifacts..."
	@rm -rf docs/dist docs/node_modules docs/.astro
	@echo "✓ Documentation artifacts cleaned"

# Sync templates from .github to pkg/cli/templates
# Sync action pins from .github/aw to pkg/workflow/data
.PHONY: sync-action-pins
sync-action-pins:
	@echo "Syncing actions-lock.json from .github/aw to pkg/workflow/data/action_pins.json..."
	@if [ -f .github/aw/actions-lock.json ]; then \
		cp .github/aw/actions-lock.json pkg/workflow/data/action_pins.json; \
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
	@echo "✓ Action scripts synced successfully"

# Recompile all workflow files
.PHONY: recompile
recompile: build
	./$(BINARY_NAME) init --codespaces
	./$(BINARY_NAME) compile --validate --verbose --purge --stats
#	./$(BINARY_NAME) compile --dir pkg/cli/workflows --validate --verbose --purge

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
agent-finish: deps-dev fmt lint build test-all fix recompile dependabot generate-schema-docs generate-agent-factory security-scan
	@echo "Agent finished tasks successfully."

# Help target
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  build            - Build the binary for current platform"
	@echo "  build-awmg       - Build the awmg (MCP gateway) binary for current platform"
	@echo "  build-all        - Build binaries for all platforms (gh-aw and awmg)"
	@echo "  test             - Run Go tests (unit + integration)"
	@echo "  test-unit        - Run Go unit tests only (faster)"
	@echo "  test-security    - Run security regression tests"
	@echo "  test-js          - Run JavaScript tests"
	@echo "  test-all         - Run all tests (Go and JavaScript)"
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
	@echo "  tools            - Install build-time tools from tools.go"
	@echo "  license-check    - Check dependency licenses for compliance"
	@echo "  license-report   - Generate CSV license report"
	@echo "  deps             - Install dependencies"
	@echo "  deps-dev         - Install development dependencies (includes tools)"
	@echo "  check-node-version - Check Node.js version (20 or higher required)"
	@echo "  golint           - Run golangci-lint (full repository scan)"
	@echo "  golint-incremental - Run golangci-lint incrementally (only changed files, requires BASE_REF)"
	@echo "  lint             - Run linter"
	@echo "  fmt              - Format code"
	@echo "  fmt-cjs          - Format JavaScript (.cjs and .js) and JSON files in actions/setup/js"
	@echo "  fmt-json         - Format JSON files in pkg directory (excluding actions/setup/js)"
	@echo "  fmt-check        - Check code formatting"
	@echo "  fmt-check-cjs    - Check JavaScript (.cjs) and JSON file formatting in actions/setup/js"
	@echo "  fmt-check-json   - Check JSON file formatting in pkg directory (excluding actions/setup/js)"
	@echo "  lint-cjs         - Lint JavaScript (.cjs) and JSON files in actions/setup/js"
	@echo "  lint-json        - Lint JSON files in pkg directory (excluding actions/setup/js)"
	@echo "  lint-errors      - Lint error messages for quality compliance"
	@echo "  security-scan    - Run all security scans (gosec, govulncheck, trivy)"
	@echo "  security-gosec   - Run gosec Go security scanner"
	@echo "  security-govulncheck - Run govulncheck for known vulnerabilities"
	@echo "  security-trivy   - Run trivy filesystem scanner"
	@echo "  actionlint       - Validate workflows with actionlint (depends on build)"
	@echo "  validate-workflows - Validate compiled workflow lock files (depends on build)"
	@echo "  install          - Install binary locally"
	@echo "  sync-action-pins - Sync actions-lock.json from .github/aw to pkg/workflow/data (runs automatically during build)"
	@echo "  sync-action-scripts - Sync install-gh-aw.sh to actions/setup-cli/install.sh (runs automatically during build)"
	@echo "  update           - Update GitHub Actions and workflows, sync action pins, and rebuild binary"
	@echo "  fix              - Apply automatic codemod-style fixes to workflow files (depends on build)"
	@echo "  recompile        - Recompile all workflow files (runs init, depends on build)"
	@echo "  dependabot       - Generate Dependabot manifests for npm dependencies in workflows"
	@echo "  generate-schema-docs - Generate frontmatter full reference documentation from JSON schema"
	@echo "  generate-agent-factory     - Generate agent factory documentation page"
	@echo "  build-slides     - Build slides with Marp to docs/public/slides/gh-aw.html"
	@echo "  deps-docs        - Install Astro documentation dependencies"
	@echo "  build-docs       - Build Astro documentation to docs/dist"
	@echo "  dev-docs         - Start Astro development server for live preview"
	@echo "  preview-docs     - Preview built documentation with Astro"
	@echo "  clean-docs       - Clean documentation artifacts (dist, node_modules, .astro)"

	@echo "  agent-finish     - Complete validation sequence (build, test, fix, recompile, fmt, lint, security-scan)"
	@echo "  sbom             - Generate SBOM in SPDX and CycloneDX formats (requires syft)"
	@echo "  help             - Show this help message"
