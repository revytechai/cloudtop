GO ?= go
PREFIX ?= /usr/local
BINARY := cloudtop
VERSION ?= 0.1.0
DIST := dist

# The go directive in go.mod is the floor the FreeBSD ports builder has.
# Keep it there, not at the version on this machine.

.PHONY: all build test vet fmt fmt-check check clean build-all install packages

all: build

build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w" -o $(DIST)/$(BINARY) ./cmd/$(BINARY)

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w .

# check must not modify the tree: it verifies, and says what to run.
fmt-check:
	@files="$$(gofmt -l .)"; \
	if [ -n "$$files" ]; then \
		echo "gofmt found unformatted files:"; \
		echo "$$files"; \
		echo "run 'make fmt' to fix"; \
		exit 1; \
	fi

check: fmt-check vet test

# The platforms this project supports.
build-all:
	CGO_ENABLED=0 GOOS=linux  GOARCH=amd64 $(GO) build -trimpath -o $(DIST)/$(BINARY)_linux_amd64  ./cmd/$(BINARY)
	CGO_ENABLED=0 GOOS=linux  GOARCH=arm64 $(GO) build -trimpath -o $(DIST)/$(BINARY)_linux_arm64  ./cmd/$(BINARY)
	CGO_ENABLED=0 GOOS=freebsd GOARCH=amd64 $(GO) build -trimpath -o $(DIST)/$(BINARY)_freebsd_amd64 ./cmd/$(BINARY)
	CGO_ENABLED=0 GOOS=freebsd GOARCH=arm64 $(GO) build -trimpath -o $(DIST)/$(BINARY)_freebsd_arm64 ./cmd/$(BINARY)

install: build
	install -d $(DESTDIR)$(PREFIX)/bin
	install -m 0755 $(DIST)/$(BINARY) $(DESTDIR)$(PREFIX)/bin/$(BINARY)

clean:
	rm -rf $(DIST)
