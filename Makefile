
GO_SRC := $(shell find -type f -name '*.go')
GO_PKGS := $(shell go list ./...)
VERSION ?= $(shell git describe --long --dirty)

all: vet test p2

p2: $(GO_SRC)
	CGO_ENABLED=0 GOOS=linux go build -a \
	-ldflags "-extldflags '-static' -X main.Version=$(shell git describe --long --dirty)" \
	-o p2 ./cmd/p2

test:
	mkdir -p coverage
	go test -v -covermode=count -coverprofile=coverage/test.cov $(GO_PKGS)

vet:
	go vet $(GO_PKGS)

# Format the code
fmt:
	gofmt -s -w $(GO_SRC)

# Check code conforms to go fmt
style:
	! gofmt -s -l $(GO_SRC) 2>&1 | read 2>/dev/null

.PHONY: test vet
