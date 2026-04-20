# Build from repository root (Railway / CI). Application code lives in source/.
FROM golang:1.24-alpine AS builder

WORKDIR /build

COPY source/go.mod source/go.sum ./
RUN go mod download

COPY source/ .

# Target the builder's native arch (amd64 on Railway, arm64 on Apple Silicon, etc.)
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -ldflags="-s -w" -o main .

FROM scratch

COPY --from=builder /build/main .

EXPOSE 3000

ENTRYPOINT ["/main"]
