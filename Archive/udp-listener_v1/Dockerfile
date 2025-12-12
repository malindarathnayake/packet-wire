FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /udp-listener ./cmd/udp-listener

FROM alpine:3.20

RUN adduser -D -g '' listener

WORKDIR /captures

COPY --from=builder /udp-listener /usr/local/bin/udp-listener

ENV UDP_LISTEN_ADDR=":9000"
ENV CAPTURE_DIR="/captures"
ENV REPLY_MODE="false"
ENV ID_NAME="UNKNOWN"
ENV CONFIG_FILE=""

USER listener

EXPOSE 9000/udp

ENTRYPOINT ["/usr/local/bin/udp-listener"]

