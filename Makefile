BINARY  := vv
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/suykerbuyk/vibe-vault/internal/help.Version=$(VERSION)
GOFLAGS := -trimpath -ldflags="$(LDFLAGS)"
MANDIR  := man
PREFIX  ?= $(HOME)/.local
BINDIR  ?= $(PREFIX)/bin
MANPREFIX ?= $(PREFIX)/share/man

.DEFAULT_GOAL := help

##@ General
.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)
	@echo ""
	@echo "Quick start:  make build && make test"

##@ Build
.PHONY: build man
build: ## Build binary and man pages
	go build $(GOFLAGS) -o $(BINARY) ./cmd/vv
	go run -ldflags="-X github.com/suykerbuyk/vibe-vault/internal/help.Version=$(shell git describe --tags --always 2>/dev/null || echo dev)" ./cmd/gen-man $(MANDIR)

man: ## Regenerate man pages only
	go run -ldflags="-X github.com/suykerbuyk/vibe-vault/internal/help.Version=$(shell git describe --tags --always 2>/dev/null || echo dev)" ./cmd/gen-man $(MANDIR)

##@ Test
.PHONY: test integration integration-llm check vet lint coverage bench fuzz
test: ## Run unit tests (-short)
	go test -short ./...

integration: ## Run integration tests
	go test -run TestIntegration -timeout 60s -count=1 ./test/

integration-llm: ## Live Anthropic LLM round-trip (requires ANTHROPIC_API_KEY; otherwise skips)
	@if [ -z "$$ANTHROPIC_API_KEY" ]; then \
		echo "ANTHROPIC_API_KEY not set; skipping"; \
	else \
		go test -tags=integration_anthropic -run='TestIntegration_Anthropic' -count=1 ./internal/llm/...; \
	fi

check: ## Run vet + unit + integration tests
	go vet ./...
	go test -count=1 ./internal/...
	go test -run TestIntegration -timeout 60s -count=1 ./test/

vet: ## Run go vet
	go vet ./...

lint: ## Run golangci-lint
	golangci-lint run ./...

coverage: ## Generate coverage report
	go test -coverprofile=coverage.out ./internal/...
	@echo "Coverage report: coverage.out"

bench: ## Run benchmarks
	go test -bench=. -benchmem ./internal/...

fuzz: ## Run fuzz tests (30s per package)
	@for pkg in $$(go list ./internal/... | xargs grep -rl 'func Fuzz' | sed 's|/[^/]*$$||' | sort -u); do \
		echo "fuzzing $$pkg"; \
		go test -fuzz=Fuzz -fuzztime=30s "$$pkg"; \
	done

##@ Install
.PHONY: install uninstall agents
install: build agents ## Build and install binary + man pages to PREFIX
	install -d $(BINDIR)
	install -m 755 $(BINARY) $(BINDIR)/$(BINARY)
	install -d $(MANPREFIX)/man1
	install -m 644 $(MANDIR)/*.1 $(MANPREFIX)/man1/
	@echo "Installed $(BINARY) to $(BINDIR)/$(BINARY)"
	@echo "Installed man pages to $(MANPREFIX)/man1/"

agents: build ## Regenerate .claude/agents/ from the embedded agentregistry catalogue
	./$(BINARY) internal generate-agents

uninstall: ## Remove installed binary and man pages
	rm -f $(BINDIR)/$(BINARY)
	rm -f $(MANPREFIX)/man1/vv.1 $(MANPREFIX)/man1/vv-*.1
	@echo "Uninstalled $(BINARY) from $(BINDIR)"

##@ Workflow
.PHONY: pre-commit hooks
pre-commit: vet lint test integration ## Run vet + lint + test + integration (pre-commit check)

hooks: ## Configure git hooks path
	git config core.hooksPath .githooks

##@ Clean
.PHONY: clean
clean: ## Remove build artifacts
	rm -f $(BINARY)
	rm -f $(MANDIR)/*.1
	rm -f coverage.out
