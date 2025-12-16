# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build both binaries
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o kube-plex .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o kube-plex-webhook ./cmd/webhook

# Transcoder image
FROM alpine:3.20 AS transcoder

RUN apk --no-cache add ca-certificates

COPY --from=builder /app/kube-plex /kube-plex

ENTRYPOINT ["/kube-plex"]

# Webhook image
FROM alpine:3.20 AS webhook

RUN apk --no-cache add ca-certificates

COPY --from=builder /app/kube-plex-webhook /kube-plex-webhook

USER 65534:65534

ENTRYPOINT ["/kube-plex-webhook"]
