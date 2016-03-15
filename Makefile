NAME=p2cli
VERSION=$(shell cat VERSION)
ARTIFACTORY_REPO="public-binaries"

GO_SRC := $(shell find -type f -name "*.go")

all: vet test p2

p2: $(GO_SRC)
	CGO_ENABLED=0 GOOS=linux go build -a -ldflags "-extldflags '-static' -X main.Version=$VERSION" -o p2 .

vet:
	go vet .

test:
	go test -v .

release: all
	ci/artifactory-release.sh $(ARTIFACTORY_REPO) $(NAME)-$(VERSION).tar.gz

.PHONY: test vet release
