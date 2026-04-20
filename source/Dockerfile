# Thank you https://github.com/dreth/dac.ac-go/blob/main/Dockerfile
FROM golang:1.24-alpine AS builder

# Move to working directory (/build).
WORKDIR /build

# Copy and download dependency using go mod.
COPY go.mod go.sum ./
RUN go mod download

# Copy the code into the container.
COPY . .

# Native arch matches the machine running docker build (e.g. amd64 on Railway).
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -ldflags="-s -w" -o main .

# Empty image
FROM scratch

# Copy binary and config files from /build to root folder of scratch container.
COPY --from=builder /build/main .

EXPOSE 3000

# Command to run when starting the container.
ENTRYPOINT ["/main"]
