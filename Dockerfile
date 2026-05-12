# Build the manager binary
FROM registry.access.redhat.com/ubi9/go-toolset:1.25.9-1778504036@sha256:2c17ce45c735ad308240139d807eeb22f4499fd90e883634ba5a191779f1ff94 AS base

ENV GOTOOLCHAIN=auto

WORKDIR /workspace

COPY go.mod go.mod
COPY go.sum go.sum

RUN go mod download

COPY main.go main.go
COPY api/ api/
COPY controllers/ controllers/

USER 0

RUN CGO_ENABLED=0 GOOS=linux go build -o manager main.go

RUN rm main.go
RUN rm -rf api
RUN rm -rf controllers

# Build the manager binary
FROM base AS builder

WORKDIR /workspace

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY api/ api/
COPY controllers/ controllers/

# Build
RUN CGO_ENABLED=0 GOOS=linux go build -o manager main.go

FROM registry.access.redhat.com/ubi9-minimal:9.7-1778461551@sha256:fe9e574f04371b333ed4e21d30d984f6b7fcd1046e579f5ddab4816c0c8e231d
WORKDIR /
COPY licenses/ licenses/
COPY --from=builder /workspace/manager .
USER 65534:65534

ENTRYPOINT ["/manager"]
