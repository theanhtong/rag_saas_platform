# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

ENV GOPROXY=https://goproxy.io,https://proxy.golang.org,direct

COPY go.mod go.sum ./
COPY vendor/ vendor/

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -ldflags="-w -s" -o /app/worker ./cmd/worker

# Final runtime stage
FROM alpine:3.19

WORKDIR /app

COPY --from=builder /app/worker /app/worker
COPY --from=builder /app/config.yaml /app/config.yaml

ENTRYPOINT ["/app/worker"]
