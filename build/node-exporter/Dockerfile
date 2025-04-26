FROM golang:1.23 AS builder

WORKDIR /go/src/github.com/elevated-systems/compute-gardener-scheduler

# Copy go.mod and go.sum first to leverage Docker cache
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy the rest of the source code
COPY . .
ARG TARGETARCH

# Build the node-exporter
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -o node-exporter ./cmd/node-exporter

# Use distroless as minimal base image
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /go/src/github.com/elevated-systems/compute-gardener-scheduler/node-exporter /node-exporter
USER 65532:65532

ENTRYPOINT ["/node-exporter"]