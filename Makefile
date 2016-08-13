
GOFILES_NOVENDOR = $(shell find . -type f -name '*.go' -not -path "./vendor/*")

all: vet test p2

p2: $(GO_SRC)
	CGO_ENABLED=0 GOOS=linux go build -a \
	-ldflags "-extldflags '-static' -X main.Version=$(shell git describe --long --dirty)" \
	-o p2 .

vet:
	go vet .

test: p2
	go test -v .
	./run_tests.sh
	gofmt -l ${GOFILES_NOVENDOR} | read 2>/dev/null && echo "Code differs from gofmt's style" 1>&2 && exit 1 || true

.PHONY: test vet
