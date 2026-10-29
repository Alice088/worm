GO ?= go
BIN ?= bin
VERSION ?= dev
LDFLAGS ?= -s -w -X main.version=$(VERSION)

.PHONY: build client server test race vet check clean

build: client server

client:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/worm ./cmd

server:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/worm-server ./cmd/worm-server

test:
	$(GO) test ./... -count=1

race:
	$(GO) test ./... -count=1 -race

vet:
	$(GO) vet ./...

check: vet test race

clean:
	rm -rf $(BIN)
