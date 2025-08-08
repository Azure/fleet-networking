# Build the manager binary
FROM mcr.microsoft.com/oss/go/microsoft/golang:1.24.4 as builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# the go command will load packages from the vendor directory instead of downloading modules from their sources into
# the module cache and using packages those downloaded copies.
COPY vendor/ vendor/

# Copy the go source
COPY cmd/member-net-controller-manager/main.go main.go
COPY api/ api/
COPY pkg/ pkg/

# Build with CGO enabled and GOEXPERIMENT=systemcrypto for internal usage
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} GOEXPERIMENT=systemcrypto GO111MODULE=on go build -o member-net-controller-manager main.go

# Use Azure Linux distroless as minimal base image to package the manager binary
# Refer to https://mcr.microsoft.com/en-us/artifact/mar/azurelinux/distroless/minimal/about for more details
FROM mcr.microsoft.com/azurelinux/distroless/minimal:3.0
WORKDIR /
COPY --from=builder /workspace/member-net-controller-manager .
USER 65532:65532

ENTRYPOINT ["/member-net-controller-manager"]
