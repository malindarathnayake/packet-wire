FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod ./
COPY go.sum* ./
RUN go mod download

COPY *.go ./

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /udp-sender .

FROM alpine:3.20

RUN adduser -D -g '' sender

WORKDIR /data

COPY --from=builder /udp-sender /usr/local/bin/udp-sender

USER sender

ENTRYPOINT ["/usr/local/bin/udp-sender"]

