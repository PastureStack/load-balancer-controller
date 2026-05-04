.RECIPEPREFIX := >
TARGETS := $(shell ls scripts)

DAPPER_IMAGE ?= rc16-lb-controller-dapper:ubuntu26
DAPPER_HOST_ARCH ?= amd64
DOCKER_VERSION ?= 29.4.2
DOCKER_BUILD_NETWORK ?= host
UBUNTU_MIRROR ?= http://archive.ubuntu.com/ubuntu
DAPPER_SOURCE ?= /go/src/github.com/rancher/lb-controller

.dapper:
>docker build \
>  --network $(DOCKER_BUILD_NETWORK) \
>  --build-arg DAPPER_HOST_ARCH=$(DAPPER_HOST_ARCH) \
>  --build-arg DOCKER_VERSION=$(DOCKER_VERSION) \
>  --build-arg UBUNTU_MIRROR=$(UBUNTU_MIRROR) \
>  -t $(DAPPER_IMAGE) \
>  -f Dockerfile.dapper .

$(TARGETS): .dapper
>docker run --rm \
>  -v $(CURDIR):$(DAPPER_SOURCE) \
>  -v /var/run/docker.sock:/var/run/docker.sock \
>  -e DAPPER_UID=$$(id -u) \
>  -e DAPPER_GID=$$(id -g) \
>  -e ARCH=$(DAPPER_HOST_ARCH) \
>  -e TAG \
>  -e REPO \
>  -e IMAGE_NAMESPACE \
>  -e IMAGE_PREFIX \
>  -e VERSION_OVERRIDE \
>  -e DOCKER_BUILD_NETWORK=$(DOCKER_BUILD_NETWORK) \
>  -e UBUNTU_MIRROR=$(UBUNTU_MIRROR) \
>  $(DAPPER_IMAGE) $@

trash: deps

trash-keep: deps

deps: .dapper
>docker run --rm \
>  -v $(CURDIR):$(DAPPER_SOURCE) \
>  -e DAPPER_UID=$$(id -u) \
>  -e DAPPER_GID=$$(id -g) \
>  -e ARCH=$(DAPPER_HOST_ARCH) \
>  -e VERSION_OVERRIDE \
>  -e DOCKER_BUILD_NETWORK=$(DOCKER_BUILD_NETWORK) \
>  -e UBUNTU_MIRROR=$(UBUNTU_MIRROR) \
>  $(DAPPER_IMAGE) /bin/bash -lc 'echo "vendor directory is committed; no dependency bootstrap required"'

.DEFAULT_GOAL := ci

.PHONY: .dapper $(TARGETS) trash trash-keep deps
