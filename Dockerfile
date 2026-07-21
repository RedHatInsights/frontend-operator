# Build the manager binary
FROM registry.access.redhat.com/hi/go:latest-fips-builder AS builder

USER 0
WORKDIR /workspace

# Copy the Go Modules manifests
COPY go.mod go.sum ./

# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY api/ api/
COPY controllers/ controllers/

# Build with CGO enabled for FIPS-compliant crypto (BoringSSL)
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags "-w -s" -o manager main.go

# Runtime
FROM registry.access.redhat.com/hi/go:latest-fips
WORKDIR /
COPY licenses/ licenses/
COPY --from=builder /workspace/manager .
USER 1001

CMD ["/manager"]
