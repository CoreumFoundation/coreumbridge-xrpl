FROM golang:1.21.2-alpine3.18 as builder

RUN apk add --no-cache make

COPY relayer/go.mod relayer/go.sum /code/relayer/

WORKDIR /code/relayer

RUN go mod download

COPY Makefile /code/Makefile
COPY relayer /code/relayer

WORKDIR /code

RUN make build-relayer

FROM alpine:3.18

COPY --from=builder /code/build/coreumbridge-xrpl-relayer /app/coreumbridge-xrpl-relayer

ENTRYPOINT ["./app/coreumbridge-xrpl-relayer"]
