VERSION := 0.2.0
BIN := bin/ror2

.PHONY: build install test

build:
	GOTOOLCHAIN=local GOARCH=arm64 go build -ldflags "-s -w -X ror2mm/internal/ui.Version=$(VERSION)" -o $(BIN) ./cmd/ror2

install: build
	mkdir -p $(HOME)/.local/bin
	rm -f $(HOME)/.local/bin/ror2
	cp $(BIN) $(HOME)/.local/bin/ror2

test:
	GOTOOLCHAIN=local go test ./...
