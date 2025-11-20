FROM golang:1.25 AS build

RUN mkdir /build

WORKDIR /build

COPY ./ ./

RUN go run mage.go binary

FROM scratch

COPY --from=build /build/p2 /bin/p2

ENV PATH=/bin:$PATH

ENTRYPOINT ["p2"]
