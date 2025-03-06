FROM golang:1.20-alpine AS builder

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY pkg/ pkg/
COPY webhooks/energy-policy/ webhooks/energy-policy/

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -o energy-policy-webhook webhooks/energy-policy/main.go

# Use distroless for minimal image size
FROM gcr.io/distroless/static:nonroot

WORKDIR /

COPY --from=builder /app/energy-policy-webhook /app/energy-policy-webhook

# Run as non-root user
USER 65532:65532

ENTRYPOINT ["/app/energy-policy-webhook"]