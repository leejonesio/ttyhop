# Copyright (c) 2025 Lee Jones
# Licensed under the MIT License. See LICENSE file in the project root for details.

BINARY ?= ttyhop
PREFIX ?= /usr/local/bin

# --- Dynamic Versioning ---
# Find latest v* tag (semantic-ish). If none, we will create v0.1.0 on first release.
LATEST_TAG_CANDIDATE := $(shell git tag --list 'v*' | sort -V | tail -n 1)
HAS_TAGS := $(if $(strip $(LATEST_TAG_CANDIDATE)),yes,no)

# When there are no tags yet, CURRENT_VERSION is 0.1.0 (the first tag to create).
CURRENT_VERSION := $(if $(filter no,$(HAS_TAGS)),0.1.0,$(patsubst v,%,$(LATEST_TAG_CANDIDATE)))

# Next patch version (only used when tags already exist).
NEXT_VERSION := $(shell awk -F. '{printf "%d.%d.%d", $$1, $$2, $$3+1}' <<< '$(CURRENT_VERSION)')

# Tag we will create on `make release`:
# - no tags  -> v0.1.0
# - has tags -> bump patch (vX.Y.(Z+1))
TAG_TO_CREATE := $(if $(filter no,$(HAS_TAGS)),v$(CURRENT_VERSION),v$(NEXT_VERSION))

# For ldflags/reporting keep "latest tag" coherent for builds without a tag.
LATEST_TAG := $(if $(filter no,$(HAS_TAGS)),(none),$(LATEST_TAG_CANDIDATE))

# --- Build Flags ---
BRANCH ?= $(shell git symbolic-ref --short -q HEAD 2>/dev/null || echo "")
LDFLAGS := -X 'main.branch=$(BRANCH)' -X 'main.tag=$(LATEST_TAG)' -X 'main.version=$(CURRENT_VERSION)'

.DEFAULT_GOAL := help
.PHONY: build install clean lint run release test help

run: build ## compile and run the binary
	@./$(BINARY) $(ARGS)

build: ## compile the binary with version metadata
	@mkdir -p $(dir $(BINARY))
	@CGO_ENABLED=1 go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

install: ## build and install the binary system-wide
	@echo "==> Building release binary for installation..."
	@CGO_ENABLED=1 go build -ldflags "-s -w $(LDFLAGS)" -o $(BINARY) .
	@echo "==> Installing to $(PREFIX)/..."
	@mkdir -p $(PREFIX)
	@install -m 755 $(BINARY) $(PREFIX)
	@echo "==> Installed."

clean: ## remove the compiled binary
	@rm -f $(BINARY)

test: ## run all tests
	@CGO_ENABLED=1 go test ./...

lint: ## run linters and formatters
	@go fmt ./...
	@go vet ./...
	@if command -v golangci-lint >/dev/null 2>&1; then \
		echo "==> Running golangci-lint..."; \
		golangci-lint run; \
	else \
		echo "==> golangci-lint not found, skipping. For more thorough checks, please install it."; \
		echo "    (e.g., 'brew install golangci-lint' on macOS)"; \
	fi

release: ## create and push a new git tag to trigger the release workflow
	@if [ -n "$(shell git status --porcelain)" ]; then \
		echo "Error: Working directory is not clean. Please commit changes before releasing."; \
		exit 1; \
	fi
	@git checkout main
	@echo "==> Fetching latest changes from origin..."
	@git fetch
	@if [ "$(shell git rev-list --count --left-right @...@{u} 2>/dev/null || echo '0	0')" != "0	0" ]; then \
		echo "Error: Your local branch has diverged from its upstream counterpart."; \
		echo "Please run 'git pull --rebase' to sync your changes and try again."; \
		exit 1; \
	fi
	@echo "==> Previous tag: $(LATEST_TAG)"
	@echo "==> New tag:      $(TAG_TO_CREATE)"
	@git tag -a $(TAG_TO_CREATE) -m "Release $(TAG_TO_CREATE)"
	@echo "==> Pushing tag to origin to trigger release workflow..."
	@git push origin $(TAG_TO_CREATE)
	@echo "==> Tag pushed. Monitor the GitHub Actions workflow for release progress."

help: ## display this help screen
	@echo "Usage: make <target>"
	@echo
	@echo "Targets:"
	@grep -E '^[a-zA-Z0-9_.-]+:.*##' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS=":.*## "}; {printf "  %-20s %s\n", $$1, $$2}'
