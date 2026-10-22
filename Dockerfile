# Build the manager binary
FROM golang:1.27.3 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY cmd/main.go cmd/main.go
COPY api/ api/
COPY internal/ internal/
COPY pkg/ pkg/

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager cmd/main.go

FROM registry.access.redhat.com/ubi9/ubi-minimal:latest
# Refresh the system trust store as Red Hat publishes CA revocations.
RUN microdnf update -y ca-certificates && microdnf clean all
# RHSB-2026-012: distrust both affected roots even before vendor packages ship.
RUN trust extract --overwrite --format=pem-bundle \
      --filter='pkcs11:object=Comodo%20AAA%20Services%20root;type=cert' \
      /etc/pki/ca-trust/source/blocklist/Comodo_AAA_Services_root.pem && \
    trust extract --overwrite --format=pem-bundle \
      --filter='pkcs11:object=emSign%20ECC%20Root%20CA%20-%20G3;type=cert' \
      /etc/pki/ca-trust/source/blocklist/emSign_ECC_Root_CA_-_G3.pem && \
    update-ca-trust extract
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
