ARG GO_VER=1.16
FROM golang:${GO_VER} AS build

ARG GOOS=linux
ARG GOARCH=amd64
ARG VERSION

COPY . /git
WORKDIR /git
RUN VERSION=${VERSION} make clean p2 GOOS=$GOOS GOARCH=$GOARCH


FROM alpine as compress

COPY --from=build /git/p2 /p2

RUN apk add upx
RUN upx --ultra-brute -q /p2 && upx -t /p2


FROM scratch
COPY --from=compress /p2 /p2
ENTRYPOINT ["/p2"]
