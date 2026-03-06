# syntax=docker/dockerfile:1

# -- Stage 1: Downloader
FROM alpine:3.23 AS downloader
RUN apk add --no-cache ca-certificates tzdata

# -- Stage 2: Builder
FROM golang:1.26.1-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Build static binary, ensuring CGO is disabled
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-w -s" -o qbit-gluetun-sync ./cmd/sync

# -- Stage 3: Final Image
FROM gcr.io/distroless/cc-debian12:nonroot
WORKDIR /
# Copy certs & tzdata from downloader
COPY --from=downloader /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=downloader /usr/share/zoneinfo /usr/share/zoneinfo

# Copy compiled binary from builder
COPY --from=builder /app/qbit-gluetun-sync /usr/local/bin/qbit-gluetun-sync

# Adhere to rootless mandates
USER nonroot:nonroot

EXPOSE 9090

ENTRYPOINT ["/usr/local/bin/qbit-gluetun-sync"]
