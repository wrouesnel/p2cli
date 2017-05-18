FROM golang:1.9.2 as build

COPY . /go/src/github.com/wrouesnel/p2cli
WORKDIR /go/src/github.com/wrouesnel/p2cli
RUN make

# ----

FROM scratch

COPY --from=build /go/src/github.com/wrouesnel/p2cli/p2 /
WORKDIR /root

ENTRYPOINT ["/p2"]
