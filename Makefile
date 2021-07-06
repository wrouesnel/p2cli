
GO_SRC := $(shell find -type f -name '*.go' ! -path '*/vendor/*')
VERSION ?= $(shell git describe --long --dirty)

all: vet test p2

p2: $(GO_SRC)
	CGO_ENABLED=0 GOOS=linux go build -a \
	-ldflags "-extldflags '-static' -X main.Version=$(shell git describe --long --dirty)" \
	-o p2 .

vet:
	go vet

# Check code conforms to go fmt
style:
	! gofmt -s -l $(GO_SRC) 2>&1 | read 2>/dev/null

test:
	mkdir -p coverage
	go test -v -covermode=count -coverprofile=coverage/test.cov

# Format the code
fmt:
	gofmt -s -w $(GO_SRC)

.PHONY: test vet
