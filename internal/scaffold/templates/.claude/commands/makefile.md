Audit or create a Makefile facade for this project's native build system.

## Rules

1. **Discover** the native build system by checking for config files:
   - `CMakeLists.txt` -> CMake
   - `Cargo.toml` -> Cargo
   - `go.mod` -> Go
   - `package.json` -> Node (npm/yarn/pnpm)
   - `meson.build` -> Meson

2. **Read** any existing `Makefile` to preserve project-specific targets.

3. **Create or update** a `Makefile` with these standard targets:

   | Target           | Purpose                          | Safety    |
   |------------------|----------------------------------|-----------|
   | `make`           | Print help (`.DEFAULT_GOAL`)     | read-only |
   | `make build`     | Default build                    | mutates   |
   | `make test`      | Unit tests (builds first)        | mutates   |
   | `make integration` | Integration tests (builds first) | mutates |
   | `make install`   | Install to `PREFIX=~/.local`     | mutates   |
   | `make clean`     | Remove build artifacts           | mutates   |

   Key constraint: **bare `make` MUST show help and NEVER mutate.** Use `.DEFAULT_GOAL := help`.

4. **Validate** by running `make` and confirming it only prints help (exit 0, no build side effects).

## Adaptation patterns

### CMake
```makefile
BUILD_DIR  ?= build
BUILD_TYPE ?= Release
PREFIX     ?= $(HOME)/.local
build:
	cmake -B $(BUILD_DIR) -G Ninja -DCMAKE_BUILD_TYPE=$(BUILD_TYPE) -DCMAKE_INSTALL_PREFIX=$(PREFIX)
	ninja -C $(BUILD_DIR)
test: build
	./$(BUILD_DIR)/*_tests "~[integration]~[benchmark]"
integration: build
	./$(BUILD_DIR)/*_tests "[integration]"
install: build
	cmake --install $(BUILD_DIR)
clean:
	rm -rf $(BUILD_DIR)
```

### Cargo
```makefile
build:
	cargo build --release
test:
	cargo test
integration:
	cargo test -- --ignored
install:
	cargo install --path .
clean:
	cargo clean
```

### Go
```makefile
build:
	go build ./...
test:
	go test ./...
integration:
	go test -tags=integration ./...
install:
	go install ./...
clean:
	go clean
```

### Node (npm)
```makefile
build:
	npm run build
test:
	npm test
install:
	npm ci
clean:
	rm -rf node_modules dist
```

### Meson
```makefile
BUILD_DIR ?= builddir
build:
	meson setup $(BUILD_DIR) --buildtype=release
	ninja -C $(BUILD_DIR)
test: build
	meson test -C $(BUILD_DIR)
install: build
	meson install -C $(BUILD_DIR)
clean:
	rm -rf $(BUILD_DIR)
```

## Help target template

The `help` target should list all targets, overridable variables, and include a "Quick start" recommendation pointing the user to the most common workflow (usually `make build && make test`).
