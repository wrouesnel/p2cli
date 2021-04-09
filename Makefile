GO := go
GO_VER := 1.16
GO_SRC := $(wildcard '*.go')
VERSION ?= $(shell git describe --always --long --dirty 2>/dev/null || echo "undefined")
DOCKER_IMAGE := p2
BINARY := p2

GOOS := linux
GOARCH := amd64

DOCKER_BUILD := docker build --build-arg VERSION=$(VERSION) --build-arg GO_VER=$(GO_VER) --build-arg GOOS=$(GOOS) --build-arg GOARCH=$(GOARCH)

all: vet test p2

$(BINARY): $(GO_SRC)
	@mkdir -p $(dir $(BINARY))
ifeq ($(GOARCH),armv6)
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=arm GOARM=6 $(GO) build -a -ldflags "-s -w -extldflags '-static' -X main.Version=$(VERSION)" -o $@ .
else
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -a -ldflags "-s -w -extldflags '-static' -X main.Version=$(VERSION)" -o $@ .
endif

.PHONY: vet
vet:
	$(GO) vet

# Check code conforms to go fmt
style:
	! gofmt -s -l $(GO_SRC) 2>&1 | read 2>/dev/null

.PHONY: test
test:
	$(GO) test -v -covermode=count -coverprofile=cover.out

# Format the code
fmt:
	gofmt -s -w $(GO_SRC)

.PHONY: clean
clean:
	@rm -rf release
	@rm -f $(BINARY)

.PHONY: distclean
distclean: clean
	@rm -rf vendor

.PHONY: docker
docker:
	$(DOCKER_BUILD) -f Dockerfile -t $(DOCKER_IMAGE) .

.PHONY: build
build:
	@docker run --platform linux/amd64 --rm -it -v $$(pwd):/build -w /build docker.io/library/golang:$(GO_VER) make $(MAKEFLAGS) $(BINARY)

.PHONY: build-slim
build-slim: build
	@docker run --platform linux/amd64 --rm -it -v $$(pwd):/build -w /build docker.io/library/alpine:latest sh -c "apk add upx && upx --ultra-brute -q $(BINARY) && upx -t $(BINARY) && du -sh $(BINARY)"

.PHONY: release
release:
	GOOS=linux GOARCH=amd64 make clean build-slim BINARY=release/p2-v$(VERSION)-linux-amd64
