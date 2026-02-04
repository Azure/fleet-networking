# Build the net-crd-installer binary
FROM mcr.microsoft.com/oss/go/microsoft/golang:1.24.12 AS builder

ARG GOOS=linux
ARG GOARCH=amd64

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# Cache the downloaded dependency modules across different builds to expedite the progress.
# This also helps reduce downloading related reliability issues in our build environment.
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# Copy the go source
COPY cmd/net-crd-installer/ cmd/net-crd-installer/

# Build with CGO enabled and GOEXPERIMENT=systemcrypto for internal usage
RUN echo "Building images with GOOS=linux GOARCH=$GOARCH"
RUN --mount=type=cache,target=/go/pkg/mod CGO_ENABLED=1 GOOS=linux GOARCH=$GOARCH GOEXPERIMENT=systemcrypto GO111MODULE=on go build -o net-crd-installer cmd/net-crd-installer/main.go

# Use Azure Linux distroless base image to package net-crd-installer binary
# Refer to https://mcr.microsoft.com/en-us/artifact/mar/azurelinux/distroless/base/about for more details
FROM mcr.microsoft.com/azurelinux/distroless/base:3.0
WORKDIR /
COPY --from=builder /workspace/net-crd-installer .
COPY config/crd/bases/ /workspace/config/crd/bases/

USER 65532:65532

ENTRYPOINT ["/net-crd-installer"]
