# Build the crdinstaller binary
FROM mcr.microsoft.com/oss/go/microsoft/golang:1.24.4 AS builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY cmd/net-crd-installer/ cmd/net-crd-installer/

ARG TARGETARCH

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} GO111MODULE=on go build -o net-crd-installer cmd/net-crd-installer/main.go

# Use distroless as minimal base image to package the net-crd-installer binary
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/net-crd-installer .
COPY config/crd/bases/ /workspace/config/crd/bases/

USER 65532:65532

ENTRYPOINT ["/net-crd-installer"]