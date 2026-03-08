FROM golang:1.25.5-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 go build -o /out/femtoclaw ./cmd/femtoclaw

FROM golang:1.25.5-alpine
RUN apk add --no-cache \
	bash \
	ca-certificates \
	curl \
	git \
	github-cli \
	gcc \
	make \
	musl-dev \
	nodejs \
	npm \
	openssh-client \
	py3-pip \
	python3 \
	rust \
	cargo

RUN npm install -g nx

WORKDIR /work

COPY --from=build /out/femtoclaw /usr/local/bin/femtoclaw
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
