# Build the manager binary
FROM registry.access.redhat.com/ubi8/go-toolset:1.21.13-1.1727869850 as base

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
FROM base as builder

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

FROM registry.access.redhat.com/ubi8/ubi-minimal:8.10-1018
WORKDIR /
COPY licenses/ licenses/
COPY --from=builder /workspace/manager .
USER 65534:65534

ENTRYPOINT ["/manager"]
