NAME=p2cli
VERSION=$(shell cat VERSION)
ARTIFACTORY_REPO="public-binaries"

GO_SRC := $(shell find -type f -name "*.go")

all: vet test p2

p2: $(GO_SRC)
	CGO_ENABLED=0 GOOS=linux go build -a \
	-ldflags "-extldflags '-static' -X main.Version=$VERSION" -o $(NAME) -o $(NAME) .

vet:
	go vet .

test:
	go test -v .
	./run_tests.sh
	gofmt -l ${GOFILES_NOVENDOR} | read 2>/dev/null && echo "Code differs from gofmt's style" 1>&2 && exit 1 || true

release: all
	mv $(NAME) $(NAME)-$(VERSION)
	ci/artifactory-release.sh $(ARTIFACTORY_REPO)/zbi/$(NAME) $(NAME)-$(VERSION)

.PHONY: test vet release
