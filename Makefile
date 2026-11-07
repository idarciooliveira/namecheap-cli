VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/idarciooliveira/namecheap-cli/internal/cli.Version=$(VERSION)
BIN_DIR ?= $(HOME)/.local/bin

.PHONY: build install windows test vet clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/ncli ./cmd/ncli

install: build
	install -m 0755 bin/ncli $(BIN_DIR)/ncli

windows:
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/ncli.exe ./cmd/ncli

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -rf bin
