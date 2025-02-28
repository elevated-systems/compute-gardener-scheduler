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

GO_VERSION := $(shell awk '/^go /{print $$2}' go.mod|head -n1)
PLATFORMS ?= linux/amd64
BUILDER ?= docker
REGISTRY?=docker.io/dmasselink
RELEASE_VERSION?=$(shell git tag --sort=-committerdate | head -n 1)-$(shell git rev-parse --short HEAD)
RELEASE_IMAGE:=compute-gardener-scheduler:$(RELEASE_VERSION)
GO_BASE_IMAGE?=golang:$(GO_VERSION)
DISTROLESS_BASE_IMAGE?=gcr.io/distroless/static:nonroot

VERSION=$(shell echo $(RELEASE_VERSION)')
VERSION:=$(or $(VERSION),v0.0.$(shell date +%Y%m%d))

.PHONY: all
all: build

.PHONY: build
build: build-scheduler

.PHONY: build-scheduler
build-scheduler:
	$(GO_BUILD_ENV) go build -ldflags '-X k8s.io/component-base/version.gitVersion=$(VERSION) -w' -o bin/kube-scheduler cmd/scheduler/main.go

.PHONY: build-image
build-image:
	BUILDER=$(BUILDER) \
	PLATFORMS=$(PLATFORMS) \
	RELEASE_VERSION=$(RELEASE_VERSION) \
	REGISTRY=$(REGISTRY) \
	IMAGE=$(RELEASE_IMAGE) \
	GO_BASE_IMAGE=$(GO_BASE_IMAGE) \
	DISTROLESS_BASE_IMAGE=$(DISTROLESS_BASE_IMAGE) \
	EXTRA_ARGS=$(EXTRA_ARGS) hack/build-images.sh

.PHONY: build-push-image
build-push-image: EXTRA_ARGS="--push"
build-push-image: build-image

.PHONY: update-gomod
update-gomod:
	hack/update-gomod.sh

.PHONY: unit-test
unit-test: install-envtest
	hack/unit-test.sh $(ARGS)

.PHONY: install-envtest
install-envtest:
	hack/install-envtest.sh

.PHONY: verify
verify:
	hack/verify-gomod.sh
	hack/verify-gofmt.sh
	hack/verify-crdgen.sh

.PHONY: clean
clean:
	rm -rf ./bin
