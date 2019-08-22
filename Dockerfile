FROM golang AS build
RUN go get -u -v github.com/wrouesnel/p2cli
WORKDIR $GOPATH/src/github.com/wrouesnel/p2cli
RUN CGO_ENABLED=0 GOOS=linux go build -a \
    -ldflags "-extldflags '-static' -X main.Version=$(shell git describe --long --dirty)" \
    -o /p2 .

FROM scratch
COPY --from=build /p2 /p2
ENTRYPOINT ["/p2"]
