GO ?= go
BIN ?= bin
VERSION ?= dev
LDFLAGS ?= -s -w -X main.version=$(VERSION)

.PHONY: build client server test race vet check clean release

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

release:
	@test -n "$(TAG)" || { echo "usage: make release TAG=v1.2.3"; exit 1; }
	@printf '%s' "$(TAG)" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$$' \
		|| { echo "TAG must look like v1.2.3"; exit 1; }
	@test -z "$$(git status --porcelain)" || { echo "working tree is dirty, commit first"; exit 1; }
	@test "$$(git rev-parse --abbrev-ref HEAD)" = "master" \
		|| { echo "release from the master branch"; exit 1; }
	@git fetch --quiet --tags origin master
	@test "$$(git rev-list --count HEAD..origin/master)" = "0" \
		|| { echo "master is behind origin, pull first"; exit 1; }
	@test -z "$$(git tag --list "$(TAG)")" \
		|| { echo "tag $(TAG) already exists locally"; exit 1; }
	@test -z "$$(git ls-remote --tags origin "refs/tags/$(TAG)")" \
		|| { echo "tag $(TAG) already exists on origin"; exit 1; }
	$(MAKE) check
	git tag -a "$(TAG)" -m "worm $(TAG)"
	@printf '\nprepared %s\npush it:\n  git push --follow-tags origin master\n' "$(TAG)"
