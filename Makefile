WAILS := $(shell go env GOPATH)/bin/wails

# ── App ──────────────────────────────────────────────────────────────────────

.PHONY: dev
dev:          ## Run the app in development mode (hot-reload)
	$(WAILS) dev

.PHONY: build
build:        ## Build a production binary into build/bin/
	$(WAILS) build

.PHONY: build-debug
build-debug:  ## Build with debug symbols and devtools enabled
	$(WAILS) build -debug -devtools

.PHONY: clean
clean:        ## Remove build artefacts
	rm -rf build/bin

# ── Go tooling ───────────────────────────────────────────────────────────────

.PHONY: tidy
tidy:         ## Tidy go.mod / go.sum
	go mod tidy

.PHONY: vet
vet:          ## Run go vet
	go vet ./...

# ── Debugging ────────────────────────────────────────────────────────────────

.PHONY: debug-server
debug-server: ## Build debug bundle, then start Delve on :2345 for VSCode attach
	$(WAILS) build -debug -devtools
	dlv exec --headless --listen=:2345 --api-version=2 --accept-multiclient \
		./build/bin/PhotoCaption.app/Contents/MacOS/PhotoCaption

# ── Project tools ────────────────────────────────────────────────────────────

.PHONY: exifdump
exifdump:     ## Print all EXIF tags from an image  usage: make exifdump FILE=photo.jpg
ifndef FILE
	$(error FILE is required — usage: make exifdump FILE=path/to/image.jpg)
endif
	go run ./cmd/exifdump "$(FILE)"

# ── Help ─────────────────────────────────────────────────────────────────────

.PHONY: help
help:         ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	  awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
