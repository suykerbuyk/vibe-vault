BINARY  := vv
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/johns/vibe-vault/internal/help.Version=$(VERSION)
GOFLAGS := -trimpath -ldflags="$(LDFLAGS)"
MANDIR  := man
MANPREFIX := $(HOME)/.local/share/man

.PHONY: all build man install test integration check vet lint coverage bench fuzz clean pre-commit hooks

all: build man

build:
	go build $(GOFLAGS) -o $(BINARY) ./cmd/vv

man:
	go run -ldflags="-X github.com/johns/vibe-vault/internal/help.Version=$(VERSION)" ./cmd/gen-man $(MANDIR)

install: build man
	cp $(BINARY) $(HOME)/.local/bin/$(BINARY)
	install -d $(MANPREFIX)/man1
	cp $(MANDIR)/*.1 $(MANPREFIX)/man1/

test:
	go test -short ./...

integration:
	go test -run TestIntegration -timeout 60s -count=1 ./test/

check:
	go vet ./...
	go test -count=1 ./internal/...
	go test -run TestIntegration -timeout 60s -count=1 ./test/

vet:
	go vet ./...

pre-commit: vet test integration

hooks:
	git config core.hooksPath .githooks

lint:
	golangci-lint run ./...

coverage:
	go test -coverprofile=coverage.out ./internal/...
	@echo "Coverage report: coverage.out"

bench:
	go test -bench=. -benchmem ./internal/...

fuzz:
	@for pkg in $$(go list ./internal/... | xargs grep -rl 'func Fuzz' | sed 's|/[^/]*$$||' | sort -u); do \
		echo "fuzzing $$pkg"; \
		go test -fuzz=Fuzz -fuzztime=30s "$$pkg"; \
	done

clean:
	rm -f $(BINARY)
	rm -f $(MANDIR)/*.1
	rm -f coverage.out
