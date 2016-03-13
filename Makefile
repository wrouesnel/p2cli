all: vet test p2

p2:
	go build -o p2 .

vet:
	go vet .

test:
	go test -v .

.PHONY: test vet
