FROM --platform=$BUILDPLATFORM golang:1.21.2-alpine3.18 as builder

RUN apk add --no-cache make git

COPY relayer/go.mod relayer/go.sum /code/relayer/

WORKDIR /code/relayer

RUN go mod download

COPY Makefile /code/Makefile
COPY relayer /code/relayer
COPY .git /code/.git

WORKDIR /code

ARG GOOS=linux
ARG GOARCH=amd64

RUN make build-relayer GOOS=${GOOS} GOARCH=${GOARCH}

FROM --platform=$TARGETPLATFORM  alpine:3.18

COPY --from=builder /code/build/coreumbridge-xrpl-relayer /bin/coreumbridge-xrpl-relayer
RUN mkdir -m 777 /.coreumbridge-xrpl-relayer

ENTRYPOINT ["/bin/coreumbridge-xrpl-relayer"]
