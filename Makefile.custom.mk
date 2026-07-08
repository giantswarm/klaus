##@ Docker

.PHONY: docker-build docker-build-alpine docker-build-debian docker-binary generate-dockerfile-debian verify-dockerfile-debian

docker-build: docker-build-alpine docker-build-debian ## Build both Alpine and Debian Docker images

# The Dockerfile copies a prebuilt klaus-<os>-<arch> binary (built by CI in
# the architect/go-build job); build it locally before the image build.
docker-binary:
	@echo "Building klaus-linux-$$(go env GOARCH)..."
	@CGO_ENABLED=0 GOOS=linux go build -trimpath \
		-ldflags "-w -extldflags '-static' \
		-X 'github.com/giantswarm/klaus/pkg/project.gitSHA=$$(git rev-parse --short HEAD 2>/dev/null || echo unknown)' \
		-X 'github.com/giantswarm/klaus/pkg/project.buildTimestamp=$$(date -u +%Y-%m-%dT%H:%M:%SZ)'" \
		-o klaus-linux-$$(go env GOARCH) .

docker-build-alpine: docker-binary ## Build Alpine Docker image (default)
	@echo "Building Alpine image..."
	@docker build -t klaus:alpine .

docker-build-debian: docker-binary ## Build Debian Docker image
	@echo "Building Debian image..."
	@docker build -f Dockerfile.debian -t klaus:debian .

generate-dockerfile-debian: ## Regenerate Dockerfile.debian from Dockerfile (only VARIANT default and go-toolchain image differ)
	@printf '# DO NOT EDIT. Generated from Dockerfile.\n# This file exists so the CI job can build the Debian variant without --build-arg.\n# Regenerate with: make generate-dockerfile-debian\n\n' > Dockerfile.debian
	@sed \
		-e 's/^ARG VARIANT=alpine$$/ARG VARIANT=slim/' \
		-e 's/FROM golang:\([^ ]*\)-alpine AS go-toolchain/FROM golang:\1 AS go-toolchain/' \
		Dockerfile >> Dockerfile.debian

verify-dockerfile-debian: generate-dockerfile-debian ## Fail if Dockerfile.debian is stale relative to Dockerfile.
	@git diff --exit-code Dockerfile.debian || { \
		echo "ERROR: Dockerfile.debian is out of date. Run 'make generate-dockerfile-debian' and commit."; \
		exit 1; }

##@ Testing

# The architect go-build job runs `make test` (test_target: test). Extend that
# target with the Dockerfile.debian sync check that used to live in the
# hand-written ci.yaml so CI and local runs share one command. This only adds a
# prerequisite; the generated `go test` recipe in Makefile.gen.go.mk is not
# overridden.
test: verify-dockerfile-debian

.PHONY: helm-lint
helm-lint: ## Lint Helm chart
	@echo "Linting Helm chart..."
	@helm lint ./helm/klaus

.PHONY: test-vet
test-vet: ## Run go test and go vet
	@echo "Running Go tests (with NO_COLOR=true)..."
	@NO_COLOR=true go test -cover ./...
	@echo "Running go vet..."
	@go vet ./...

.PHONY: govulncheck
govulncheck: ## Run govulncheck to scan for known vulnerabilities
	@echo "Checking for known vulnerabilities..."
	@command -v govulncheck >/dev/null 2>&1 || { echo "Installing govulncheck..."; go install golang.org/x/vuln/cmd/govulncheck@latest; }
	@govulncheck ./...

# Note: These targets require Docker and 'act' to be installed.
# See: https://github.com/nektos/act#installation

.PHONY: test-auto-release
test-auto-release: ## Run 'act' to simulate the auto-release workflow
	@echo "Simulating Auto-Release workflow (merged pull_request event)..."
	@echo "NOTE: Requires 'merged_pr_event.json' in the project root."
	@echo "NOTE: Git push steps within the workflow are expected to fail locally."
	@act pull_request --job auto_release --eventpath merged_pr_event.json
