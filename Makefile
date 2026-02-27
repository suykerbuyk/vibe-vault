BINARY := vv
VERSION := 0.1.0
GOFLAGS := -trimpath -ldflags="-s -w"

.PHONY: build install test integration clean

build:
	go build $(GOFLAGS) -o $(BINARY) ./cmd/vv

install: build
	cp $(BINARY) $(HOME)/.local/bin/$(BINARY)

test:
	go test -short ./...

integration:
	go test -run TestIntegration -timeout 60s -count=1 ./test/

clean:
	rm -f $(BINARY)
