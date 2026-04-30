//go:build tools

package tools

// Tool dependencies — imported here so `go mod tidy` keeps them in go.mod.
// cachi2 (Konflux prefetch) then caches these alongside project dependencies.
// The `tools` build tag ensures they are never compiled into the operator binary.
import (
	_ "sigs.k8s.io/controller-runtime/tools/setup-envtest"
	_ "sigs.k8s.io/controller-tools/cmd/controller-gen"
	_ "sigs.k8s.io/kustomize/kustomize/v5"
)
