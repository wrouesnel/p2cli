
GO_SRC := $(shell find -type f -name '*.go')
GO_PKGS := $(shell go list ./...)
VERSION ?= $(shell git describe --long --dirty)

all: vet test p2

p2: $(GO_SRC)
	CGO_ENABLED=0 GOOS=linux go build -a \
	-ldflags "-extldflags '-static' -X main.Version=$(shell git describe --long --dirty)" \
	-o p2 ./cmd/p2

release: p2-linux-arm64 p2-linux-x86_64 p2-linux-i386 p2-windows-i386.exe p2-windows-x86_64.exe p2-darwin-x86_64 p2-darwin-arm64 p2-freebsd-x86_64

p2-linux-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -a \
    	-ldflags "-extldflags '-static' -X main.Version=$(shell git describe --long --dirty)" \
    	-o $@ ./cmd/p2

p2-linux-x86_64:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a \
    	-ldflags "-extldflags '-static' -X main.Version=$(shell git describe --long --dirty)" \
    	-o $@ ./cmd/p2

p2-linux-i386:
	CGO_ENABLED=0 GOOS=linux GOARCH=386 go build -a \
    	-ldflags "-extldflags '-static' -X main.Version=$(shell git describe --long --dirty)" \
    	-o $@ ./cmd/p2

p2-windows-i386.exe:
	CGO_ENABLED=0 GOOS=windows GOARCH=386 go build -a \
    	-ldflags "-extldflags '-static' -X main.Version=$(shell git describe --long --dirty)" \
    	-o $@ ./cmd/p2

p2-windows-x86_64.exe:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -a \
    	-ldflags "-extldflags '-static' -X main.Version=$(shell git describe --long --dirty)" \
    	-o $@ ./cmd/p2

p2-darwin-x86_64:
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -a \
    	-ldflags "-extldflags '-static' -X main.Version=$(shell git describe --long --dirty)" \
    	-o $@ ./cmd/p2

p2-darwin-arm64:
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -a \
    	-ldflags "-extldflags '-static' -X main.Version=$(shell git describe --long --dirty)" \
    	-o $@ ./cmd/p2

p2-freebsd-x86_64:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a \
    	-ldflags "-extldflags '-static' -X main.Version=$(shell git describe --long --dirty)" \
    	-o $@ ./cmd/p2


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

.PHONY: test vet release