# build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# set GOPROXY fallback
ENV GOPROXY=https://goproxy.io,https://proxy.golang.org,direct

# copy dependency specs and vendor
COPY go.mod go.sum ./
COPY vendor/ vendor/

# copy source code
COPY . .

# build gateway binary
RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -ldflags="-w -s" -o /app/gateway ./cmd/gateway

# final stage
FROM alpine:3.19

WORKDIR /app

# copy binary and default config
COPY --from=builder /app/gateway /app/gateway
COPY --from=builder /app/config.yaml /app/config.yaml

EXPOSE 8080

ENTRYPOINT ["/app/gateway"]
