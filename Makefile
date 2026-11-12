VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.2.0)
BIN     := bin/kindtui
LDFLAGS := -s -w -X main.version=$(VERSION)
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

.PHONY: build test vet fmt run install tidy dist clean

build: ## Build bin/kindtui
	mkdir -p bin
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) .

test: ## Run unit tests
	go test ./...

vet: ## go vet + gofmt check
	go vet ./...
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed:"; gofmt -l .; exit 1)

fmt:
	gofmt -w .

run: build ## Build and start the UI
	./$(BIN)

install: ## go install into $GOBIN
	go install -trimpath -ldflags "$(LDFLAGS)" .

tidy:
	go mod tidy

dist: ## Cross-compile release binaries into dist/
	@mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		echo "building $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" \
			-o dist/kindtui-$$os-$$arch . || exit 1; \
	done

clean:
	rm -rf bin dist
