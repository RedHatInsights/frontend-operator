# Build the manager binary
FROM registry.access.redhat.com/ubi9/go-toolset:1.26.2-1779886993@sha256:2dc44e9ae10db3f7cfb06a0063e9d792350fb004a65bf59f4268c164cacffe54 AS base

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

FROM registry.access.redhat.com/ubi9-minimal:9.8-1779809423@sha256:5b74fce9d6e629942a0c6dc0f546c193e70d7f974d999a48c948c53dd3d36362
WORKDIR /
COPY licenses/ licenses/
COPY --from=builder /workspace/manager .
USER 65534:65534

ENTRYPOINT ["/manager"]
