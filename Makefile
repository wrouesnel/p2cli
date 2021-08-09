
GO_SRC := $(shell find . -type f -name '*.go' ! -path '*/vendor/*')
VERSION ?= $(shell git describe --long --dirty)
GOARCH = amd64
LDFLAGS = -ldflags "-extldflags '-static' -X main.Version=${VERSION}"

all: vet test p2

p2: linux darwin

linux: $(GO_SRC)
	CGO_ENABLED=0 GOOS=linux go build -a ${LDFLAGS}	-o p2-linux-${GOARCH} .

darwin: $(GO_SRC)
	CGO_ENABLED=0 GOOS=darwin go build -a ${LDFLAGS} -o p2-darwin-${GOARCH} .

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
