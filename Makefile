BINARY := sesscap
VERSION := 0.1.0
GOFLAGS := -trimpath -ldflags="-s -w"

.PHONY: build install test clean

build:
	go build $(GOFLAGS) -o $(BINARY) ./cmd/sesscap

install: build
	cp $(BINARY) $(HOME)/.local/bin/$(BINARY)

test:
	go test ./...

clean:
	rm -f $(BINARY)
