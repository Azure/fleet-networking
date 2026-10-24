# Build the complete Gateway manager package, including its Azure and readiness helpers.
FROM mcr.microsoft.com/oss/go/microsoft/golang:1.26.9 AS builder

ARG GOOS=linux
ARG GOARCH=amd64

WORKDIR /workspace
COPY go.mod go.mod
COPY go.sum go.sum
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY cmd/hub-gateway-controller-manager/ cmd/hub-gateway-controller-manager/
COPY api/ api/
COPY pkg/ pkg/

# Preserve the existing controller image CGO policy for internal usage.
RUN --mount=type=cache,target=/go/pkg/mod CGO_ENABLED=1 GOOS=$GOOS GOARCH=$GOARCH GO111MODULE=on go build -o hub-gateway-controller-manager ./cmd/hub-gateway-controller-manager

FROM mcr.microsoft.com/azurelinux/distroless/base:3.0
WORKDIR /
COPY --from=builder /workspace/hub-gateway-controller-manager .
USER 65532:65532

ENTRYPOINT ["/hub-gateway-controller-manager"]
