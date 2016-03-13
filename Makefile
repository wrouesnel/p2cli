
GO_SRC := $(shell find -type f -name "*.go")

all: vet test p2

p2: $(GO_SRC)
	go build -ldflags "-X main.Version=git:$(shell git rev-parse HEAD)" -o p2 .

vet:
	go vet .

test:
	go test -v .

.PHONY: test vet
