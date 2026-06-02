# Build the manager binary
FROM registry.access.redhat.com/ubi9/go-toolset:1.26.3-1780373831@sha256:49f5929f6674d75377902ddcc2f46baf7a5cfcaada2497ee43f66e090943afd6 AS base

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

FROM registry.access.redhat.com/ubi9-minimal:9.8-1780378819@sha256:ae09ecc3d754bc1726cbda3e2599cc7839e09fe1cc547ce173cf669b645be3cc
WORKDIR /
COPY licenses/ licenses/
COPY --from=builder /workspace/manager .
USER 65534:65534

ENTRYPOINT ["/manager"]
