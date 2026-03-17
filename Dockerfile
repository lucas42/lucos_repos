FROM lucas42/lucos_navbar:2.1.6 AS navbar

FROM golang:1.26 AS builder

WORKDIR /go/src/lucos_repos

COPY go.mod go.sum ./
RUN go mod download

COPY conventions/ ./conventions/
COPY src/ ./src/
RUN go build -o lucos_repos ./src

FROM debian:trixie-slim

RUN apt-get update && apt-get install -y ca-certificates curl && rm -rf /var/lib/apt/lists/*

WORKDIR /app

RUN mkdir -p /data

COPY --from=builder /go/src/lucos_repos/lucos_repos .
COPY --from=navbar lucos_navbar.js .

CMD ["./lucos_repos"]
