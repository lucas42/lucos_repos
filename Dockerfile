FROM golang:1.26 AS builder

WORKDIR /go/src/lucos_repos

COPY go.mod ./
RUN go mod download

COPY *.go ./
RUN go build -o lucos_repos

FROM debian:trixie-slim

WORKDIR /app

COPY --from=builder /go/src/lucos_repos/lucos_repos .

CMD ["./lucos_repos"]
