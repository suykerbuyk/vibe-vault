BINARY  := vv
VERSION := 0.3.0
GOFLAGS := -trimpath -ldflags="-s -w"
MANDIR  := man
MANPREFIX := $(HOME)/.local/share/man

.PHONY: all build man install test integration check vet clean

all: build man

build:
	go build $(GOFLAGS) -o $(BINARY) ./cmd/vv

man:
	go run ./cmd/gen-man $(MANDIR)

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

clean:
	rm -f $(BINARY)
	rm -f $(MANDIR)/*.1
