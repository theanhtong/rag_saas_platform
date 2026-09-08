# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

ENV GOPROXY=https://proxy.golang.org,direct

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/vector_service ./cmd/vector_service

# Final runtime stage
FROM alpine:3.19

WORKDIR /app

COPY --from=builder /app/vector_service /app/vector_service

EXPOSE 50051

ENTRYPOINT ["/app/vector_service"]
