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

.PHONY: test vet
