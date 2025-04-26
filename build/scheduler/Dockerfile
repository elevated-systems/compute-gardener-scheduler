# Copyright 2020 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
ARG GO_BASE_IMAGE=golang:1.21
ARG DISTROLESS_BASE_IMAGE=gcr.io/distroless/static:nonroot
# Use a newer Go version for building
FROM --platform=${BUILDPLATFORM} golang:1.23 AS builder

WORKDIR /workspace

# Copy go.mod and go.sum first to leverage Docker cache
COPY go.mod go.sum ./

# Download dependencies 
RUN go mod download

# Copy the rest of the source code
COPY . .
ARG TARGETARCH
# Build the scheduler
RUN mkdir -p bin && \
    CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} \
    go build \
    -ldflags "-X k8s.io/component-base/version.gitVersion=v0.0.0 -w" \
    -o bin/kube-scheduler cmd/scheduler/main.go

FROM --platform=${BUILDPLATFORM} $DISTROLESS_BASE_IMAGE

WORKDIR /bin
COPY --from=builder /workspace/bin/kube-scheduler .
USER 65532:65532
