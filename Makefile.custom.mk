##@ Release

.PHONY: release-dry-run
release-dry-run: ## Test the release process without publishing (all platforms)
	goreleaser release --snapshot --clean --skip=announce,publish,validate

.PHONY: release-dry-run-fast
release-dry-run-fast: ## Fast release dry-run for CI (linux/amd64 only, ~6min faster)
	goreleaser release --config .goreleaser.ci.yaml --snapshot --clean --skip=announce,publish,validate

.PHONY: release-local
release-local: ## Create a release locally
	goreleaser release --clean

##@ Docker

.PHONY: docker-build docker-build-alpine docker-build-debian docker-binary generate-dockerfile-debian

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

generate-dockerfile-debian: ## Regenerate Dockerfile.debian from Dockerfile (only VARIANT default differs)
	@printf '# DO NOT EDIT. Generated from Dockerfile.\n# This file exists so the CI job can build the Debian variant without --build-arg.\n# Regenerate with: make generate-dockerfile-debian\n\n' > Dockerfile.debian
	@sed 's/^ARG VARIANT=alpine$$/ARG VARIANT=slim/' Dockerfile >> Dockerfile.debian

##@ Development

.PHONY: lint-yaml
lint-yaml: ## Run YAML linter
	@echo "Running YAML linter..."
	@yamllint .github/workflows/auto-release.yaml .github/workflows/ci.yaml .goreleaser.yaml .goreleaser.ci.yaml

.PHONY: check
check: lint-yaml test-vet ## Run YAML linter and Go tests

##@ Testing

.PHONY: helm-lint
helm-lint: ## Lint Helm chart
	@echo "Linting Helm chart..."
	@helm lint ./helm/klaus

.PHONY: helm-test
helm-test: ## Run Helm chart unit tests (requires helm-unittest plugin)
	@echo "Running Helm unit tests..."
	@helm unittest ./helm/klaus

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

.PHONY: test-ci-pr
test-ci-pr: ## Run 'act' to simulate CI checks for a pull request
	@echo "Simulating CI workflow (pull_request event)..."
	@act pull_request --job check

.PHONY: test-ci-push
test-ci-push: ## Run 'act' to simulate CI checks for a push to main
	@echo "Simulating CI workflow (push event)..."
	@act push --job check

.PHONY: test-auto-release
test-auto-release: ## Run 'act' to simulate the auto-release workflow
	@echo "Simulating Auto-Release workflow (merged pull_request event)..."
	@echo "NOTE: Requires 'merged_pr_event.json' in the project root."
	@echo "NOTE: Git push steps within the workflow are expected to fail locally."
	@act pull_request --job auto_release --eventpath merged_pr_event.json
