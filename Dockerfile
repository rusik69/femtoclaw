# syntax=docker/dockerfile:1

FROM golang:1.25.5-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 go build -o /out/nanoclaw ./cmd/nanoclaw

FROM golang:1.25.5-alpine
RUN apk add --no-cache \
	bash \
	ca-certificates \
	git \
	github-cli \
	openssh-client

WORKDIR /work

COPY --from=build /out/nanoclaw /usr/local/bin/nanoclaw

ENTRYPOINT ["/usr/local/bin/nanoclaw"]
