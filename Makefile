# claude-says — dev tasks.
#
# Day-to-day loop (NO publishing):
#   make run              build + run the TUI from source in this terminal
#   make install          install the dev build to $(PREFIX) so `claude-says` is your local code
#   make test             race test suite
#
# Publishing is a SEPARATE, deliberate step — only when a batch is ironed out:
#   make release VERSION=v2.1.0
#
BINARY  := claude-says
CMD     := ./cmd/claude-says
BIN     := bin/$(BINARY)
PREFIX  ?= /usr/local/bin
GORELEASER := github.com/goreleaser/goreleaser/v2@v2.12.5

.PHONY: build run install test race vet fmt tidy check clean release help

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-14s %s\n", $$1, $$2}'

build: ## Build the dev binary to ./bin/claude-says
	@go build -o $(BIN) $(CMD) && echo "built $(BIN)"

run: build ## Build and run (start + TUI) in this terminal
	@./$(BIN) start $(ARGS)

install: build ## Install the dev build to $(PREFIX) so `claude-says` is your local code
	@cp $(BIN) "$(PREFIX)/$(BINARY)" && echo "installed dev build -> $(PREFIX)/$(BINARY)"
	@resolved="$$(command -v $(BINARY))"; \
	 case "$$resolved" in "$(PREFIX)/$(BINARY)") echo "PATH ok: $$resolved" ;; \
	   *) echo "WARNING: 'claude-says' resolves to $$resolved, not $(PREFIX)/$(BINARY) — that dir shadows the dev build. Fix PATH or 'npm uninstall -g claude-says'." ;; esac

test: ## Race test suite
	@go test -race ./...

vet: ## go vet
	@go vet ./...

fmt: ## gofmt the tree
	@gofmt -w .

tidy: ## go mod tidy
	@go mod tidy

check: fmt vet ## fmt + vet + build + race tests (run before a release)
	@go build ./... && go test -race ./... && echo "check: green"

clean: ## Remove build artifacts
	@rm -rf bin dist

release: ## Publish a release: make release VERSION=v2.1.0  (tags, pushes, goreleaser)
	@test -n "$(VERSION)" || { echo "usage: make release VERSION=v2.1.0"; exit 1; }
	@test -z "$$(git status --porcelain)" || { echo "working tree not clean — commit first"; exit 1; }
	@echo "==> checks"; go build ./... && go vet ./... && go test -race ./... >/dev/null
	@echo "==> tag + push $(VERSION)"; git tag -a $(VERSION) -m "$(VERSION)" && git push origin $(VERSION)
	@echo "==> goreleaser"; GITHUB_TOKEN="$$(gh auth token)" go run $(GORELEASER) release --clean
